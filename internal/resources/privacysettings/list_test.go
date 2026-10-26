package privacysettings_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/querycheck/queryfilter"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

func listConfig(typ string) string {
	return `
provider "shopify" {}

list "` + typ + `" "all" {
  provider         = shopify
  include_resource = true
}
`
}

func TestAccListPrivacySettings(t *testing.T) {
	accSetup(t)
	live := livePrivacy(t)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		Steps: []resource.TestStep{
			{
				Query:  true,
				Config: listConfig("shopify_privacy_settings"),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLength("shopify_privacy_settings.all", 1),
					querycheck.ExpectIdentity("shopify_privacy_settings.all", map[string]knownvalue.Check{"id": knownvalue.StringRegexp(shopGIDRe)}),
					querycheck.ExpectResourceKnownValues("shopify_privacy_settings.all", queryfilter.ByDisplayName(knownvalue.StringExact("Privacy settings")), []querycheck.KnownValueCheck{
						{Path: tfjsonpath.New("cookie_banner_auto_managed"), KnownValue: knownvalue.Bool(live.Banner.AutoManaged)},
						{Path: tfjsonpath.New("data_sale_opt_out_page_auto_managed"), KnownValue: knownvalue.Bool(live.DataSaleOptOutPage.AutoManaged)},
						{Path: tfjsonpath.New("privacy_policy_auto_managed"), KnownValue: knownvalue.Bool(live.PrivacyPolicy.AutoManaged)},
					}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _unmanaged.
		},
	})
}

func TestAccListConsentPolicy(t *testing.T) {
	accSetup(t)
	de := consentRow(t, "DE", "")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		Steps: []resource.TestStep{
			{
				Query:  true,
				Config: listConfig("shopify_consent_policy"),
				QueryResultChecks: []querycheck.QueryResultCheck{
					// Shopify's untouched `SD-` defaults stay out, so the 316 of them don't bury the written rows (DE, IS, LI on the dev store).
					querycheck.ExpectLengthAtLeast("shopify_consent_policy.all", 1),
					querycheck.ExpectIdentity("shopify_consent_policy.all", map[string]knownvalue.Check{"id": knownvalue.StringRegexp(numericIDRe)}),
					querycheck.ExpectResourceKnownValues("shopify_consent_policy.all", queryfilter.ByDisplayName(knownvalue.StringExact("DE")), []querycheck.KnownValueCheck{
						{Path: tfjsonpath.New("id"), KnownValue: knownvalue.StringExact(de.Id)},
						{Path: tfjsonpath.New("country_code"), KnownValue: knownvalue.StringExact("DE")},
						{Path: tfjsonpath.New("region_code"), KnownValue: knownvalue.Null()},
						{Path: tfjsonpath.New("consent_required"), KnownValue: knownvalue.Bool(boolOr(de.ConsentRequired))},
						{Path: tfjsonpath.New("data_sale_opt_out_required"), KnownValue: knownvalue.Bool(boolOr(de.DataSaleOptOutRequired))},
					}),
				},
			},
		},
	})
}
