package market_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/querycheck/queryfilter"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

const listConfig = `
provider "shopify" {}

list "shopify_market" "all" {
  provider         = shopify
  include_resource = true
}
`

func TestAccListMarket(t *testing.T) {
	handle := testHandle()
	markets := config(handle, `
  name       = "List A (test)"
  enabled    = false
  conditions = { regions = ["IS", "LI"] }
`) + `
resource "shopify_market" "b" {
  handle     = "` + handle + `-b"
  name       = "List B (test)"
  enabled    = false
  conditions = { regions = ["NO"] }
  depends_on = [shopify_market.test]
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkDestroyed(t, handle),
		Steps: []resource.TestStep{
			{Config: markets},
			{
				Query:  true,
				Config: listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_market.all", 2),
					querycheck.ExpectIdentity("shopify_market.all", map[string]knownvalue.Check{
						"id": knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/Market/\d+$`)),
					}),
					querycheck.ExpectResourceKnownValues("shopify_market.all",
						queryfilter.ByDisplayName(knownvalue.StringExact(handle)),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("name"), KnownValue: knownvalue.StringExact("List A (test)")},
							{Path: tfjsonpath.New("enabled"), KnownValue: knownvalue.Bool(false)},
							{Path: tfjsonpath.New("conditions").AtMapKey("regions"), KnownValue: knownvalue.SetExact([]knownvalue.Check{
								knownvalue.StringExact("IS"), knownvalue.StringExact("LI"),
							})},
						}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by _identityImport.
		},
	})
}
