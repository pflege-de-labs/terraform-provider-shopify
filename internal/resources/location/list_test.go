package location_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/querycheck/queryfilter"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

func listConfig(includeInactive bool) string {
	return fmt.Sprintf(`
provider "shopify" {}

list "shopify_location" "all" {
  provider         = shopify
  include_resource = true

  config {
    include_inactive = %t
  }
}
`, includeInactive)
}

func TestAccListLocation(t *testing.T) {
	nameA, nameB := testName(), testName()
	var gidA, gidB string
	gid := knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/Location/\d+$`))
	locations := `
resource "shopify_location" "a" {
  name    = "` + nameA + `"
  address = { city = "Hamburg", country_code = "DE" }
` + deleteOnDestroy + `
}

resource "shopify_location" "b" {
  name    = "` + nameB + `"
  address = { country_code = "DE" }
  active  = false
  depends_on = [shopify_location.a]
` + deleteOnDestroy + `
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy: func(s *terraform.State) error {
			if err := checkLocation(t, &gidA, false)(s); err != nil {
				return err
			}
			return checkLocation(t, &gidB, false)(s)
		},
		Steps: []resource.TestStep{
			{
				Config: locations,
				Check: resource.ComposeTestCheckFunc(
					func(s *terraform.State) error {
						gidA = s.RootModule().Resources["shopify_location.a"].Primary.ID
						gidB = s.RootModule().Resources["shopify_location.b"].Primary.ID
						return nil
					},
				),
			},
			{
				Query:  true,
				Config: listConfig(false),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectIdentity("shopify_location.all", map[string]knownvalue.Check{"id": gid}),
					querycheck.ExpectResourceKnownValues("shopify_location.all",
						queryfilter.ByDisplayName(knownvalue.StringExact(nameA)),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("name"), KnownValue: knownvalue.StringExact(nameA)},
							{Path: tfjsonpath.New("active"), KnownValue: knownvalue.Bool(true)},
							{Path: tfjsonpath.New("address").AtMapKey("city"), KnownValue: knownvalue.StringExact("Hamburg")},
							{Path: tfjsonpath.New("address").AtMapKey("country_code"), KnownValue: knownvalue.StringExact("DE")},
							{Path: tfjsonpath.New("deletion_policy"), KnownValue: knownvalue.StringExact("deactivate")},
							{Path: tfjsonpath.New("allow_data_deletion"), KnownValue: knownvalue.Bool(false)},
						}),
				},
			},
			{
				Query:  true,
				Config: listConfig(true),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectResourceKnownValues("shopify_location.all",
						queryfilter.ByDisplayName(knownvalue.StringExact(nameB)),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("active"), KnownValue: knownvalue.Bool(false)},
						}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by _identityImport.
		},
	})
}
