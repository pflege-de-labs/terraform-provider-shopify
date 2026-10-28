package shoplocale_test

import (
	"fmt"
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

list "shopify_shop_locale" "all" {
  provider         = shopify
  include_resource = true
}
`

func TestAccListShopLocale(t *testing.T) {
	acctest.Setup(t)
	ownLocales(t, locNl, locIt)
	locale := func(name, code string) string {
		return fmt.Sprintf(`
resource "shopify_shop_locale" %q {
  locale              = %q
  allow_data_deletion = true
}
`, name, code)
	}
	id := knownvalue.StringRegexp(localeRe)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkGone(t, locNl, locIt),
		Steps: []resource.TestStep{
			{Config: locale("a", locNl) + locale("b", locIt)},
			{
				Query:  true,
				Config: listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					// The primary locale plus the two created above.
					querycheck.ExpectLengthAtLeast("shopify_shop_locale.all", 3),
					querycheck.ExpectIdentity("shopify_shop_locale.all", map[string]knownvalue.Check{"id": id}),
					querycheck.ExpectResourceKnownValues("shopify_shop_locale.all",
						queryfilter.ByDisplayName(knownvalue.StringExact("nl (Dutch)")),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("id"), KnownValue: knownvalue.StringExact(locNl)},
							{Path: tfjsonpath.New("locale"), KnownValue: knownvalue.StringExact(locNl)},
							{Path: tfjsonpath.New("published"), KnownValue: knownvalue.Bool(false)},
							{Path: tfjsonpath.New("primary"), KnownValue: knownvalue.Bool(false)},
							{Path: tfjsonpath.New("adopt_existing"), KnownValue: knownvalue.Bool(false)},
							{Path: tfjsonpath.New("allow_data_deletion"), KnownValue: knownvalue.Bool(false)},
						}),
					querycheck.ExpectResourceKnownValues("shopify_shop_locale.all",
						queryfilter.ByDisplayName(knownvalue.StringRegexp(regexp.MustCompile(`, primary\)$`))),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("locale"), KnownValue: knownvalue.StringExact(locPrimary)},
							{Path: tfjsonpath.New("primary"), KnownValue: knownvalue.Bool(true)},
						}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _basic.
		},
	})
}
