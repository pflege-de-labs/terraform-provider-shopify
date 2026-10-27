package privacysettings_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

const cpAddr = "shopify_consent_policy.test"

var (
	numericIDRe = regexp.MustCompile(`^gid://shopify/ConsentPolicy/\d+$`)
)

func cpIdentity(re *regexp.Regexp) statecheck.StateCheck {
	return statecheck.ExpectIdentity(cpAddr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(re)})
}

func cpConfig(country, region string, attrs ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "resource \"shopify_consent_policy\" \"test\" {\n  country_code = %q\n", country)
	if region != "" {
		fmt.Fprintf(&b, "  region_code  = %q\n", region)
	}
	for _, a := range attrs {
		b.WriteString("  " + a + "\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// checkRow fails unless the row has exactly these values.
func checkRow(t *testing.T, country, region string, cr, ds bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		r := consentRow(t, country, region)
		if boolOr(r.ConsentRequired) != cr || boolOr(r.DataSaleOptOutRequired) != ds {
			return fmt.Errorf("%s/%s changed: want consent=%v optout=%v, got %+v", country, region, cr, ds, r)
		}
		return nil
	}
}

func attr(name string, v bool) string { return fmt.Sprintf("%s = %t", name, v) }

// Adopting a row whose values already match writes nothing, so this runs with Shopify's banner on too:
// DE is a written row, AT is Shopify's `SD-` default.
func TestAccConsentPolicy_adoptWithoutWrite(t *testing.T) {
	acctest.Setup(t)
	de, at := consentRow(t, "DE", ""), consentRow(t, "AT", "")
	deCR, deDS := boolOr(de.ConsentRequired), boolOr(de.DataSaleOptOutRequired)
	atCfg := cpConfig("AT", "", attr("consent_required", boolOr(at.ConsentRequired)))
	if !strings.HasPrefix(at.Id, "gid://shopify/ConsentPolicy/SD-") {
		t.Skipf("AT is no longer a system default (%s); this test needs one", at.Id)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Review Focus 1: destroy leaves both rows as they were.
		CheckDestroy: func(s *terraform.State) error {
			if err := checkRow(t, "DE", "", deCR, deDS)(s); err != nil {
				return err
			}
			return checkRow(t, "AT", "", boolOr(at.ConsentRequired), boolOr(at.DataSaleOptOutRequired))(s)
		},
		Steps: []resource.TestStep{
			{
				Config: cpConfig("DE", "", attr("consent_required", deCR), attr("data_sale_opt_out_required", deDS)),
				ConfigStateChecks: []statecheck.StateCheck{
					cpIdentity(numericIDRe),
					statecheck.ExpectKnownValue(cpAddr, tfjsonpath.New("id"), knownvalue.StringRegexp(numericIDRe)),
					statecheck.ExpectKnownValue(cpAddr, tfjsonpath.New("country_code"), knownvalue.StringExact("DE")),
					statecheck.ExpectKnownValue(cpAddr, tfjsonpath.New("region_code"), knownvalue.Null()),
					statecheck.ExpectKnownValue(cpAddr, tfjsonpath.New("consent_required"), knownvalue.Bool(deCR)),
				},
			},
			{
				Config:           cpConfig("DE", "", attr("consent_required", deCR)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{ResourceName: cpAddr, ImportState: true, ImportStateId: "DE", ImportStateVerify: true},
			{Config: cpConfig("DE", ""), ResourceName: cpAddr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				// Generated config must pass validation and plan as import-only.
				Config:          cpConfig("DE", ""),
				ResourceName:    cpAddr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{ResourceName: cpAddr, ImportState: true, ImportStateId: "de", ExpectError: regexp.MustCompile(`must have the form`)},
			{
				// An `SD-` row resolves by its key, and its synthetic ID by identity.
				Config:            atCfg,
				ConfigStateChecks: []statecheck.StateCheck{cpIdentity(regexp.MustCompile(`^gid://shopify/ConsentPolicy/SD-AT$`))},
			},
			{ResourceName: cpAddr, ImportState: true, ImportStateId: "AT", ImportStateVerify: true},
			{Config: atCfg, ResourceName: cpAddr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
		},
	})
}

// Writes: runs only with Shopify's banner off, and puts every row it wrote back.
func TestAccConsentPolicy_write(t *testing.T) {
	acctest.Setup(t)
	requireBannerOff(t)
	restoreConsent(t, "IS", "")
	restoreConsent(t, "US", "USCA")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Review Focus 1: destroy leaves the last written values.
		CheckDestroy: checkRow(t, "IS", "", true, true),
		Steps: []resource.TestStep{
			{
				Config: cpConfig("IS", "", attr("consent_required", true)),
				ConfigStateChecks: []statecheck.StateCheck{
					cpIdentity(numericIDRe),
					statecheck.ExpectKnownValue(cpAddr, tfjsonpath.New("consent_required"), knownvalue.Bool(true)),
					// Unset means unmanaged: the other flag keeps its value.
					statecheck.ExpectKnownValue(cpAddr, tfjsonpath.New("data_sale_opt_out_required"), knownvalue.NotNull()),
				},
			},
			{
				Config: cpConfig("IS", "", attr("consent_required", true), attr("data_sale_opt_out_required", true)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(cpAddr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(cpAddr, tfjsonpath.New("data_sale_opt_out_required"), knownvalue.Bool(true)),
				},
			},
			{RefreshState: true},
			{ResourceName: cpAddr, ImportState: true, ImportStateId: "IS", ImportStateVerify: true},
			{
				// A region is its own row, keyed by country and region code.
				Config: cpConfig("US", "USCA", attr("data_sale_opt_out_required", true)),
				ConfigStateChecks: []statecheck.StateCheck{
					cpIdentity(numericIDRe),
					statecheck.ExpectKnownValue(cpAddr, tfjsonpath.New("region_code"), knownvalue.StringExact("USCA")),
					statecheck.ExpectKnownValue(cpAddr, tfjsonpath.New("data_sale_opt_out_required"), knownvalue.Bool(true)),
				},
			},
			{ResourceName: cpAddr, ImportState: true, ImportStateId: "US/USCA", ImportStateVerify: true},
			{
				// REGION_CODE_MUST_MATCH_COUNTRY_CODE lands on region_code.
				Config:      cpConfig("US", "CAAB", attr("consent_required", false)),
				ExpectError: regexp.MustCompile(`(?s)region_code.*must match the country code|must match the country code`),
			},
		},
	})
	// IS ends with the values the last step wrote; the cleanup restores it.
}

// A value changed outside Terraform for an attribute that config leaves unset isn't drift.
func TestAccConsentPolicy_unmanagedValueIgnored(t *testing.T) {
	acctest.Setup(t)
	requireBannerOff(t)
	restoreConsent(t, "LI", "")
	li := consentRow(t, "LI", "")
	cr := boolOr(li.ConsentRequired)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cpConfig("LI", "", attr("consent_required", cr))},
			{
				// Review Focus 2: data_sale_opt_out_required is unset, so an edit to it through the client is not a diff.
				PreConfig: func() {
					if err := writeConsent(t, "LI", "", cr, true); err != nil {
						t.Fatal(err)
					}
				},
				Config:           cpConfig("LI", "", attr("consent_required", cr)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				// An edit to the managed value is.
				PreConfig: func() {
					if err := writeConsent(t, "LI", "", !cr, true); err != nil {
						t.Fatal(err)
					}
				},
				Config:           cpConfig("LI", "", attr("consent_required", cr)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(cpAddr, plancheck.ResourceActionUpdate)}},
			},
		},
	})
}
