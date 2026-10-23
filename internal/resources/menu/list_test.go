package menu_test

import (
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

const listConfig = `
provider "shopify" {}

list "shopify_menu" "all" {
  provider         = shopify
  include_resource = true
}
`

func TestAccListMenu(t *testing.T) {
	handleA, handleB := testHandle(), testHandle()
	gid := knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/Menu/\d+$`))
	menu := func(name, handle string) string {
		return `
resource "shopify_menu" "` + name + `" {
  handle = "` + handle + `"
  title  = "List ` + name + `"

  item {
    title = "Home"
    type  = "FRONTPAGE"
  }
}
`
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy: func(s *terraform.State) error {
			if err := checkMenu(t, handleA, false)(s); err != nil {
				return err
			}
			return checkMenu(t, handleB, false)(s)
		},
		Steps: []resource.TestStep{
			{
				Config: menu("a", handleA) + menu("b", handleB),
				// The menus list lags behind writes.
				PostApplyFunc: func() { waitListed(t, handleA); waitListed(t, handleB) },
			},
			{
				Query:  true,
				Config: listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_menu.all", 2),
					querycheck.ExpectIdentity("shopify_menu.all", map[string]knownvalue.Check{"id": gid}),
					querycheck.ExpectResourceKnownValues("shopify_menu.all",
						queryfilter.ByDisplayName(knownvalue.StringExact(handleA)),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("id"), KnownValue: gid},
							{Path: tfjsonpath.New("handle"), KnownValue: knownvalue.StringExact(handleA)},
							{Path: tfjsonpath.New("title"), KnownValue: knownvalue.StringExact("List a")},
							{Path: tfjsonpath.New("is_default"), KnownValue: knownvalue.Bool(false)},
							{Path: tfjsonpath.New("adopt_existing"), KnownValue: knownvalue.Bool(false)},
						}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _basic.
		},
	})
}
