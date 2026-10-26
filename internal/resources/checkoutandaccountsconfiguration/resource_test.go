package checkoutandaccountsconfiguration_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

const (
	addr = "shopify_checkout_and_accounts_configuration.test"
	// Paths the tests write; none is set on the dev store, so cleanup resets them with null.
	pBase     = "designTokens.cornerRadius.base"
	pSmall    = "designTokens.cornerRadius.small"
	pCheckbox = "components.checkbox.cornerRadius"
	pColor    = "designTokens.colors.palette.color1"
	// A leaf the dev store had before any test; tests must leave it alone.
	pSignIn = "surfaces.signIn.components.main.colors.base.background"
)

var gidRe = regexp.MustCompile(`^gid://shopify/CheckoutAndAccountsConfiguration/`)

func expectIdentity() statecheck.StateCheck {
	return statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(gidRe)})
}

// config renders the resource; branding is an HCL object expression passed to jsonencode, or "" for none.
func config(branding string, extra ...string) string {
	b := ""
	if branding != "" {
		b = "  branding = jsonencode(" + branding + ")\n"
	}
	return fmt.Sprintf("resource \"shopify_checkout_and_accounts_configuration\" \"test\" {\n%s%s\n}\n", b, strings.Join(extra, "\n"))
}

func expectUpdate() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)}}
}

func expectEmpty() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
}

// importedBranding checks that import emitted the live branding, unmanaged leaves included.
func importedBranding(want ...string) resource.ImportStateCheckFunc {
	return func(states []*terraform.InstanceState) error {
		if len(states) != 1 {
			return fmt.Errorf("want one imported instance, got %d", len(states))
		}
		b := states[0].Attributes["branding"]
		for _, w := range want {
			if !strings.Contains(b, w) {
				return fmt.Errorf("imported branding %s lacks %s", b, w)
			}
		}
		return nil
	}
}

func TestAccCheckoutAndAccountsConfiguration_basic(t *testing.T) {
	accSetup(t)
	ownBranding(t, pBase, pSmall, pCheckbox, pColor)
	name := published(t).Name
	first := `{ designTokens = { cornerRadius = { base = 6 }, colors = { palette = { color1 = "#1a4d8f" } } } }`
	second := `{ designTokens = { cornerRadius = { base = 8 }, colors = { palette = { color1 = "#1a4d8f" } } }, components = { checkbox = { cornerRadius = "LARGE" } } }`
	reset := `{ designTokens = { cornerRadius = { base = null }, colors = { palette = { color1 = "#1a4d8f" } } }, components = { checkbox = { cornerRadius = "LARGE" } } }`
	dropped := `{ designTokens = { cornerRadius = { base = null } }, components = { checkbox = { cornerRadius = "LARGE" } } }`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Review Focus 1: destroy only drops state and never resets branding.
		CheckDestroy: checkLive(t, map[string]any{pCheckbox: "LARGE", pColor: "#1A4D8F", pSignIn: "#FFFFFF"}),
		Steps: []resource.TestStep{
			{Config: config(`[]`), ExpectError: regexp.MustCompile(`JSON object`)},
			{Config: config("", `name = "tf-acc-no-such-configuration"`), ExpectError: regexp.MustCompile(`not found`)},
			{
				// Atomic: the invalid colour rejects the whole write, so base stays unset.
				Config:      config(`{ designTokens = { cornerRadius = { base = 6 }, colors = { palette = { color1 = "red" } } } }`),
				ExpectError: regexp.MustCompile(`branding_invalid_hex_color`),
			},
			{
				PreConfig: func() {
					if err := checkLive(t, map[string]any{pBase: nil})(nil); err != nil {
						t.Fatal(err)
					}
				},
				Config: config(first),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity(),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringRegexp(gidRe)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact(name)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("is_published"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("branding"),
						knownvalue.StringExact(`{"designTokens":{"colors":{"palette":{"color1":"#1a4d8f"}},"cornerRadius":{"base":6}}}`)),
				},
				// Shopify uppercases the colour; the lowercase config must not plan again.
				Check: checkLive(t, map[string]any{pBase: 6.0, pColor: "#1A4D8F", pSignIn: "#FFFFFF"}),
			},
			{Config: config(first), ConfigPlanChecks: expectEmpty()},
			{Config: config(second), ConfigPlanChecks: expectUpdate(), Check: checkLive(t, map[string]any{pBase: 8.0, pCheckbox: "LARGE"})},
			{
				// Review Focus 2: an unmanaged path changed through the client doesn't plan.
				PreConfig:        func() { mustWrite(t, map[string]any{pSmall: 3}) },
				Config:           config(second),
				ConfigPlanChecks: expectEmpty(),
			},
			{
				// An edit to a managed path is drift, and apply writes it back.
				PreConfig:        func() { mustWrite(t, map[string]any{pBase: 2}) },
				Config:           config(second),
				ConfigPlanChecks: expectUpdate(),
				Check:            checkLive(t, map[string]any{pBase: 8.0, pSmall: 3.0}),
			},
			{RefreshState: true},
			{
				// Refresh steps can't carry state checks; this no-op step checks identity survives the refresh.
				Config:            config(second),
				ConfigStateChecks: []statecheck.StateCheck{expectIdentity()},
				ConfigPlanChecks:  expectEmpty(),
			},
			// Naming the configuration it already follows changes nothing, and neither does unsetting the name again.
			{Config: config(second, fmt.Sprintf("name = %q", name)), ConfigPlanChecks: expectEmpty()},
			{Config: config(second), ConfigPlanChecks: expectEmpty()},
			{
				// Generated config carries the live branding and must plan as import-only.
				Config:          config(second),
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				// Import emits the whole live branding, so it differs from the managed subset by design.
				ResourceName:            addr,
				ImportState:             true,
				ImportStateId:           "published",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"branding"},
				ImportStateCheck:        importedBranding(`"base":8`, `"small":3`, `"cornerRadius":"LARGE"`, `"#FFFFFF"`),
			},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateId:           name,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"branding"},
			},
			{Config: config(reset), ConfigPlanChecks: expectUpdate(), Check: checkLive(t, map[string]any{pBase: nil, pCheckbox: "LARGE"})},
			{Config: config(reset), ConfigPlanChecks: expectEmpty()},
			{
				// Dropping a key stops managing it; the live value stays.
				Config:           config(dropped),
				ConfigPlanChecks: expectUpdate(),
				Check:            checkLive(t, map[string]any{pColor: "#1A4D8F"}),
			},
			{Config: config(dropped), ConfigPlanChecks: expectEmpty()},
		},
	})
}

// Without branding nothing is written: adopting and destroying leave the branding as it was.
func TestAccCheckoutAndAccountsConfiguration_unmanaged(t *testing.T) {
	accSetup(t)
	ownBranding(t, pBase)
	name := published(t).Name
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkLive(t, map[string]any{pSignIn: "#FFFFFF", pBase: nil}),
		Steps: []resource.TestStep{
			{
				Config: config("", fmt.Sprintf("name = %q", name)),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity(),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact(name)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("is_published"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("branding"), knownvalue.Null()),
				},
			},
			{
				PreConfig:        func() { mustWrite(t, map[string]any{pBase: 4}) },
				Config:           config("", fmt.Sprintf("name = %q", name)),
				ConfigPlanChecks: expectEmpty(),
			},
			{
				// Unsetting the name keeps following the same configuration.
				PreConfig:        func() { mustWrite(t, map[string]any{pBase: nil}) },
				Config:           config(""),
				ConfigPlanChecks: expectEmpty(),
			},
			{
				// A different name means a different configuration.
				Config: config("", `name = "tf-acc-other"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate),
				}},
				ExpectError: regexp.MustCompile(`not found`),
			},
		},
	})
}
