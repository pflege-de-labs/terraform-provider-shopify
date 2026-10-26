package privacysettings_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

const psAddr = "shopify_privacy_settings.test"

var shopGIDRe = regexp.MustCompile(`^gid://shopify/Shop/\d+$`)

func psIdentity() statecheck.StateCheck {
	return statecheck.ExpectIdentity(psAddr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(shopGIDRe)})
}

func psConfig(body string) string {
	return fmt.Sprintf("resource \"shopify_privacy_settings\" \"test\" {\n%s\n}\n", body)
}

// checkUnchanged fails when destroy (or anything else) touched a live value; the API can't undo a change.
func checkUnchanged(t *testing.T, want func() (banner, dso, pp bool)) resource.TestCheckFunc {
	return func(*terraform.State) error {
		b, d, p := want()
		now := livePrivacy(t)
		if now.Banner.AutoManaged != b || now.DataSaleOptOutPage.AutoManaged != d || now.PrivacyPolicy.AutoManaged != p {
			return fmt.Errorf("privacy settings changed: want banner=%v dso=%v pp=%v, got %+v %+v %+v", b, d, p, now.Banner, now.DataSaleOptOutPage, now.PrivacyPolicy)
		}
		return nil
	}
}

// Nothing configured means nothing managed: this never calls privacyFeaturesDisable, whatever the store's state.
func TestAccPrivacySettings_unmanaged(t *testing.T) {
	accSetup(t)
	before := livePrivacy(t)
	b, d, p := before.Banner.AutoManaged, before.DataSaleOptOutPage.AutoManaged, before.PrivacyPolicy.AutoManaged
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Review Focus 1: destroy only drops the resource from state.
		CheckDestroy: checkUnchanged(t, func() (bool, bool, bool) { return b, d, p }),
		Steps: []resource.TestStep{
			{
				Config: psConfig(""),
				ConfigStateChecks: []statecheck.StateCheck{
					psIdentity(),
					statecheck.ExpectKnownValue(psAddr, tfjsonpath.New("id"), knownvalue.StringRegexp(shopGIDRe)),
					statecheck.ExpectKnownValue(psAddr, tfjsonpath.New("cookie_banner_auto_managed"), knownvalue.Bool(b)),
					statecheck.ExpectKnownValue(psAddr, tfjsonpath.New("data_sale_opt_out_page_auto_managed"), knownvalue.Bool(d)),
					statecheck.ExpectKnownValue(psAddr, tfjsonpath.New("privacy_policy_auto_managed"), knownvalue.Bool(p)),
					statecheck.ExpectKnownValue(psAddr, tfjsonpath.New("cookie_banner_enabled"), knownvalue.NotNull()),
				},
			},
			{
				Config:            psConfig(""),
				ConfigStateChecks: []statecheck.StateCheck{psIdentity()},
				ConfigPlanChecks:  resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{ResourceName: psAddr, ImportState: true, ImportStateId: "default", ImportStateVerify: true, ImportStateVerifyIdentifierAttribute: "id"},
			{Config: psConfig(""), ResourceName: psAddr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				// Generated config must pass validation and plan as import-only.
				Config:          psConfig(""),
				ResourceName:    psAddr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{ResourceName: psAddr, ImportState: true, ImportStateId: "nope", ExpectError: regexp.MustCompile(`default`)},
		},
	})
}

// false for the banner is the only write the tests make. With the banner already off it sends nothing, so it
// runs only then: otherwise it would switch the banner off for good.
func TestAccPrivacySettings_bannerOff(t *testing.T) {
	accSetup(t)
	requireBannerOff(t)
	before := livePrivacy(t)
	d, p := before.DataSaleOptOutPage.AutoManaged, before.PrivacyPolicy.AutoManaged
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkUnchanged(t, func() (bool, bool, bool) { return false, d, p }),
		Steps: []resource.TestStep{
			{
				Config: psConfig("cookie_banner_auto_managed = false"),
				ConfigStateChecks: []statecheck.StateCheck{
					psIdentity(),
					statecheck.ExpectKnownValue(psAddr, tfjsonpath.New("cookie_banner_auto_managed"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue(psAddr, tfjsonpath.New("cookie_banner_enabled"), knownvalue.Bool(false)),
					// Unset means unmanaged: the other two keep their live values.
					statecheck.ExpectKnownValue(psAddr, tfjsonpath.New("data_sale_opt_out_page_auto_managed"), knownvalue.Bool(d)),
					statecheck.ExpectKnownValue(psAddr, tfjsonpath.New("privacy_policy_auto_managed"), knownvalue.Bool(p)),
				},
			},
			{
				Config:           psConfig("cookie_banner_auto_managed = false"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				// The API can't turn a feature back on: caught at plan time once state knows it is off.
				Config:      psConfig("cookie_banner_auto_managed = true"),
				ExpectError: regexp.MustCompile(`can't be turned back on`),
			},
		},
	})
}

// A fresh resource has no state for the plan-time check, so Create has to refuse.
func TestAccPrivacySettings_createCannotEnable(t *testing.T) {
	accSetup(t)
	requireBannerOff(t)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      psConfig("cookie_banner_auto_managed = true"),
				ExpectError: regexp.MustCompile(`can't be turned back on`),
			},
		},
	})
}
