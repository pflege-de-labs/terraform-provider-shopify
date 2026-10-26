package webpresence_test

import (
	"fmt"
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

list "shopify_web_presence" "all" {
  provider         = shopify
  include_resource = true
}
`

func TestAccListWebPresence(t *testing.T) {
	accSetup(t)
	suffixA, suffixB := testSuffix(), testSuffix()
	presence := func(name, suffix string) string {
		return fmt.Sprintf(`
resource "shopify_web_presence" %q {
  subfolder_suffix = %q
  default_locale   = "en"
}
`, name, suffix)
	}
	gid := knownvalue.StringRegexp(gidRe)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy: func(s *terraform.State) error {
			if err := checkGone(t, suffixA)(s); err != nil {
				return err
			}
			return checkGone(t, suffixB)(s)
		},
		Steps: []resource.TestStep{
			{Config: presence("a", suffixA) + presence("b", suffixB)},
			{
				Query:  true,
				Config: listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					// The primary-domain presence plus the two created above.
					querycheck.ExpectLengthAtLeast("shopify_web_presence.all", 3),
					querycheck.ExpectIdentity("shopify_web_presence.all", map[string]knownvalue.Check{"id": gid}),
					querycheck.ExpectResourceKnownValues("shopify_web_presence.all",
						queryfilter.ByDisplayName(knownvalue.StringExact(suffixA)),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("id"), KnownValue: gid},
							{Path: tfjsonpath.New("subfolder_suffix"), KnownValue: knownvalue.StringExact(suffixA)},
							{Path: tfjsonpath.New("domain_id"), KnownValue: knownvalue.Null()},
							{Path: tfjsonpath.New("default_locale"), KnownValue: knownvalue.StringExact("en")},
							{Path: tfjsonpath.New("alternate_locales"), KnownValue: knownvalue.SetSizeExact(0)},
							{Path: tfjsonpath.New("adopt_existing"), KnownValue: knownvalue.Bool(false)},
						}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _basic.
		},
	})
}
