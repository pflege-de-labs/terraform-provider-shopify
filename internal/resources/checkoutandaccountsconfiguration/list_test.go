package checkoutandaccountsconfiguration_test

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

list "shopify_checkout_and_accounts_configuration" "all" {
  provider         = shopify
  include_resource = true
}
`

func TestAccListCheckoutAndAccountsConfiguration(t *testing.T) {
	accSetup(t)
	ownBranding(t, pCheckbox)
	name := published(t).Name
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		Steps: []resource.TestStep{
			{
				PreConfig: func() { mustWrite(t, map[string]any{pCheckbox: "LARGE"}) },
				Query:     true,
				Config:    listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_checkout_and_accounts_configuration.all", 1),
					querycheck.ExpectIdentity("shopify_checkout_and_accounts_configuration.all", map[string]knownvalue.Check{"id": knownvalue.StringRegexp(gidRe)}),
					querycheck.ExpectResourceKnownValues("shopify_checkout_and_accounts_configuration.all",
						queryfilter.ByDisplayName(knownvalue.StringExact(name+" (published)")),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("id"), KnownValue: knownvalue.StringRegexp(gidRe)},
							{Path: tfjsonpath.New("name"), KnownValue: knownvalue.StringExact(name)},
							{Path: tfjsonpath.New("is_published"), KnownValue: knownvalue.Bool(true)},
							// Live branding without nulls: the unmanaged dev store leaves and the one written above.
							{Path: tfjsonpath.New("branding"), KnownValue: knownvalue.StringRegexp(regexp.MustCompile(
								`^\{"components":\{"checkbox":\{"cornerRadius":"LARGE"\}\},"surfaces":\{.*"background":"#FFFFFF".*\}\}$`))},
						}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _basic.
		},
	})
}
