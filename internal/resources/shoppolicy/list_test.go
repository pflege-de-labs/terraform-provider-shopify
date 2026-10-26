package shoppolicy_test

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

list "shopify_shop_policy" "all" {
  provider         = shopify
  include_resource = true
}
`

func TestAccListShopPolicy(t *testing.T) {
	accSetup(t)
	ownPolicy(t, typeLegal)
	text := body("listed")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkBody(t, typeLegal, text),
		Steps: []resource.TestStep{
			{Config: config(typeLegal, text)},
			{
				Query:  true,
				Config: listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					// The dev store's generated privacy policy plus the one above.
					querycheck.ExpectLengthAtLeast("shopify_shop_policy.all", 2),
					querycheck.ExpectIdentity("shopify_shop_policy.all", map[string]knownvalue.Check{"id": knownvalue.StringRegexp(gidRe)}),
					querycheck.ExpectResourceKnownValues("shopify_shop_policy.all",
						queryfilter.ByDisplayName(knownvalue.StringRegexp(regexp.MustCompile(`^LEGAL_NOTICE \(`))),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("id"), KnownValue: knownvalue.StringRegexp(gidRe)},
							{Path: tfjsonpath.New("type"), KnownValue: knownvalue.StringExact(string(typeLegal))},
							{Path: tfjsonpath.New("body"), KnownValue: knownvalue.StringExact(text)},
							{Path: tfjsonpath.New("adopt_existing"), KnownValue: knownvalue.Bool(false)},
						}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _basic.
		},
	})
}
