package deliveryprofile_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const addr = "shopify_delivery_profile.test"

// The test changes the store's real general profile, so it snapshots it as HCL first and re-applies that at the end.
func TestAccDeliveryProfile_general(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	acctest.PreCheck(t)
	original := snapshot(t)
	t.Cleanup(func() { removeTestZones(t) })
	testZone := func(price, freePrice, threshold, countries string) string {
		return fmt.Sprintf(`
    zone {
      name      = "tfacc zone"
      countries = [%s]

      method {
        name     = "tfacc Standard"
        price    = %q
        currency = "EUR"
      }
      method {
        name      = "tfacc Standard"
        price     = %q
        currency  = "EUR"
        condition = { field = "TOTAL_PRICE", operator = "GREATER_THAN_OR_EQUAL_TO", value = %q }
      }
    }
`, countries, price, freePrice, threshold)
	}
	with := func(extra string) func() string {
		return func() string { return strings.Replace(original, "#ZONES#", extra, 1) }
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkNoTestZone(t),
		Steps: []resource.TestStep{
			{
				// "0.00" and "49.00" come back as "0.0" and "49.0"; state keeps the configured strings.
				Config: with(testZone("4.95", "0.00", "49.00", `"IS"`))(),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/DeliveryProfile/\d+$`))),
				},
			},
			{
				// Flat price updated in place, conditional rate recreated, a country added.
				// Values are in Shopify's format here so the import step can verify them.
				Config: with(testZone("5.49", "0.0", "55.0", `"IS", "LI"`))(),
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateId:     "default",
				ImportStateVerify: true,
			},
			{
				// Back to the original profile: the test zone is deleted, everything else stays untouched.
				Config: with("")(),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.NotNull()),
				},
			},
		},
	})
}

// snapshot renders the general profile as config, with #ZONES# where test zones go in the first group.
func snapshot(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	c := acctest.Client(t)
	list, err := client.ListDeliveryProfiles(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, p := range list.DeliveryProfiles.Nodes {
		if p.Default {
			id = p.Id
		}
	}
	res, err := client.GetDeliveryProfile(ctx, c, id)
	if err != nil || res.DeliveryProfile == nil {
		t.Fatalf("reading general profile: %v", err)
	}

	var b strings.Builder
	b.WriteString("resource \"shopify_delivery_profile\" \"test\" {\n")
	for gi, g := range res.DeliveryProfile.ProfileLocationGroups {
		var locs []string
		for _, l := range g.LocationGroup.Locations.Nodes {
			locs = append(locs, strconv.Quote(l.Id))
		}
		fmt.Fprintf(&b, "  location_group {\n    location_ids = [%s]\n", strings.Join(locs, ", "))
		for _, lz := range g.LocationGroupZones.Nodes {
			var countries []string
			for _, cc := range lz.Zone.Countries {
				if cc.Code.RestOfWorld {
					countries = append(countries, `"*"`)
				} else if cc.Code.CountryCode != nil {
					countries = append(countries, strconv.Quote(string(*cc.Code.CountryCode)))
				}
			}
			fmt.Fprintf(&b, "    zone {\n      name = %q\n      countries = [%s]\n", lz.Zone.Name, strings.Join(countries, ", "))
			for _, md := range lz.MethodDefinitions.Nodes {
				rate, ok := md.RateProvider.(*client.DeliveryMethodFieldsRateProviderDeliveryRateDefinition)
				if !ok {
					continue
				}
				fmt.Fprintf(&b, "      method {\n        name = %q\n        price = %q\n        currency = %q\n        active = %t\n",
					md.Name, rate.Price.Amount, rate.Price.CurrencyCode, md.Active)
				if md.Description != nil && *md.Description != "" {
					fmt.Fprintf(&b, "        description = %q\n", *md.Description)
				}
				for _, mc := range md.MethodConditions {
					switch crit := mc.ConditionCriteria.(type) {
					case *client.DeliveryMethodFieldsMethodConditionsDeliveryConditionConditionCriteriaMoneyV2:
						fmt.Fprintf(&b, "        condition = { field = %q, operator = %q, value = %q }\n", mc.Field, mc.Operator, crit.Amount)
					case *client.DeliveryMethodFieldsMethodConditionsDeliveryConditionConditionCriteriaWeight:
						fmt.Fprintf(&b, "        condition = { field = %q, operator = %q, value = %q, unit = %q }\n",
							mc.Field, mc.Operator, strconv.FormatFloat(crit.Value, 'f', -1, 64), crit.Unit)
					}
				}
				b.WriteString("      }\n")
			}
			b.WriteString("    }\n")
		}
		if gi == 0 {
			b.WriteString("#ZONES#")
		}
		b.WriteString("  }\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// removeTestZones deletes zones left by a failed run, so the next snapshot doesn't include them.
func removeTestZones(t *testing.T) {
	ctx := context.Background()
	c := acctest.Client(t)
	list, err := client.ListDeliveryProfiles(ctx, c)
	if err != nil {
		t.Error(err)
		return
	}
	for _, p := range list.DeliveryProfiles.Nodes {
		if !p.Default {
			continue
		}
		res, err := client.GetDeliveryProfile(ctx, c, p.Id)
		if err != nil || res.DeliveryProfile == nil {
			t.Errorf("cleanup: %v", err)
			return
		}
		var ids []string
		for _, g := range res.DeliveryProfile.ProfileLocationGroups {
			for _, lz := range g.LocationGroupZones.Nodes {
				if strings.HasPrefix(lz.Zone.Name, "tfacc") {
					ids = append(ids, lz.Zone.Id)
				}
			}
		}
		if len(ids) > 0 {
			if _, err := client.UpdateDeliveryProfile(ctx, c, p.Id, client.DeliveryProfileInput{ZonesToDelete: ids}); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	}
}

func checkNoTestZone(t *testing.T) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if s := snapshot(t); strings.Contains(s, "tfacc") {
			return fmt.Errorf("test zone left in the general profile")
		}
		return nil
	}
}
