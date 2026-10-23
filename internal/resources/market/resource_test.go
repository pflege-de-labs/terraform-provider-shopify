package market_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	sdkacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const addr = "shopify_market.test"

// testHandle keeps test markets apart from real ones; only these are ever deleted by tests.
func testHandle() string {
	return "tfacc-" + strings.ToLower(sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlpha))
}

func config(handle, body string) string {
	return fmt.Sprintf(`
resource "shopify_market" "test" {
  handle = %q
%s
}
`, handle, body)
}

func regions(codes ...string) statecheck.StateCheck {
	checks := make([]knownvalue.Check, len(codes))
	for i, c := range codes {
		checks[i] = knownvalue.StringExact(c)
	}
	return statecheck.ExpectKnownValue(addr, tfjsonpath.New("conditions").AtMapKey("regions"), knownvalue.SetExact(checks))
}

func TestAccMarket_basic(t *testing.T) {
	handle := testHandle()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(t, handle),
		Steps: []resource.TestStep{
			{
				Config: config(handle, `
  name       = "Nordics (test)"
  enabled    = false
  conditions = { regions = ["IS", "LI"] }
`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/Market/\d+$`))),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("enabled"), knownvalue.Bool(false)),
					regions("IS", "LI"),
				},
			},
			{
				// One region swapped, renamed, activated; currency settings and links stay unmanaged.
				Config: config(handle, `
  name       = "Nordics (test, updated)"
  enabled    = true
  conditions = { regions = ["IS", "NO"] }
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Nordics (test, updated)")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("enabled"), knownvalue.Bool(true)),
					regions("IS", "NO"),
				},
			},
			{
				Config: config(handle, `
  name              = "Nordics (test, updated)"
  enabled           = true
  conditions        = { regions = ["IS", "NO"] }
  currency_settings = { base_currency = "EUR", local_currencies = false }
`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("currency_settings").AtMapKey("base_currency"), knownvalue.StringExact("EUR")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("currency_settings").AtMapKey("local_currencies"), knownvalue.Bool(false)),
				},
			},
			{
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        handle,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "handle",
			},
		},
	})
}

func TestAccMarket_duplicateRegions(t *testing.T) {
	handle := testHandle()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(t, handle),
		Steps: []resource.TestStep{
			{
				// Two active markets for exactly the same countries conflict. Uses two test markets so the store's own markets don't matter.
				Config: config(handle, `
  name       = "Dup A"
  enabled    = true
  conditions = { regions = ["IS", "LI"] }
`) + fmt.Sprintf(`
resource "shopify_market" "dup" {
  handle     = "%s-dup"
  name       = "Dup B"
  enabled    = true
  conditions = { regions = ["LI", "IS"] }
  depends_on = [shopify_market.test]
}
`, handle),
				ExpectError: regexp.MustCompile(`existing region market with the same regions`),
			},
		},
	})
}

func checkDestroyed(t *testing.T, handle string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		var after *string
		for {
			res, err := client.ListMarkets(context.Background(), acctest.Client(t), after)
			if err != nil {
				return err
			}
			for _, m := range res.Markets.Nodes {
				if strings.HasPrefix(m.Handle, handle) {
					return fmt.Errorf("market %s still exists", m.Handle)
				}
			}
			if !res.Markets.PageInfo.HasNextPage {
				return nil
			}
			after = res.Markets.PageInfo.EndCursor
		}
	}
}
