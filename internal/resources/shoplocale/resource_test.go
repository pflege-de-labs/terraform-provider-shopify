package shoplocale_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const (
	addr = "shopify_shop_locale.test"
	// Non-primary locales the tests enable and disable again; the dev store starts with only `en`.
	locFr = "fr"
	locIt = "it"
	// The dev store's primary locale.
	locPrimary = "en"
)

// ShopLocale has no GID, so identity carries the locale code.
var localeRe = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]+)*$`)

func expectIdentity(code string) statecheck.StateCheck {
	return statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringExact(code)})
}

func config(body string) string {
	return fmt.Sprintf("resource \"shopify_shop_locale\" \"test\" {\n%s\n}\n", body)
}

// accSetup skips without TF_ACC, because these tests call the API before resource.Test would skip them.
func accSetup(t *testing.T) {
	t.Helper()
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skip("Acceptance tests skipped unless env 'TF_ACC' set")
	}
	acctest.PreCheck(t)
}

func lookup(t *testing.T, code string) *client.ShopLocaleFields {
	t.Helper()
	res, err := client.ListShopLocales(context.Background(), acctest.Client(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range res.ShopLocales {
		if l.Locale == code {
			return &l.ShopLocaleFields
		}
	}
	return nil
}

func enable(t *testing.T, code string) {
	t.Helper()
	res, err := client.EnableShopLocale(context.Background(), acctest.Client(t), code)
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.ShopLocaleEnable.UserErrors; len(errs) > 0 {
		t.Fatalf("enabling %s: %v", code, errs)
	}
}

func setPublished(t *testing.T, code string, published bool) {
	t.Helper()
	res, err := client.UpdateShopLocale(context.Background(), acctest.Client(t), code, client.ShopLocaleInput{Published: &published})
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.ShopLocaleUpdate.UserErrors; len(errs) > 0 {
		t.Fatalf("updating %s: %v", code, errs)
	}
}

// disable returns an error instead of failing so cleanups and CheckDestroy can report it their way.
func disable(t *testing.T, code string) error {
	t.Helper()
	if lookup(t, code) == nil {
		return nil
	}
	res, err := client.DisableShopLocale(context.Background(), acctest.Client(t), code)
	if err != nil {
		return err
	}
	if errs := res.ShopLocaleDisable.UserErrors; len(errs) > 0 {
		return fmt.Errorf("disabling %s: %v", code, errs)
	}
	return nil
}

// ownLocales clears leftovers of an aborted run and removes what the test enabled, dev store only.
func ownLocales(t *testing.T, codes ...string) {
	t.Helper()
	for _, c := range codes {
		if err := disable(t, c); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, c := range codes {
			if err := disable(t, c); err != nil {
				t.Error(err)
			}
		}
	})
}

// checkGone fails if a locale survived destroy.
func checkGone(t *testing.T, codes ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for _, c := range codes {
			if lookup(t, c) != nil {
				return fmt.Errorf("locale %s still enabled", c)
			}
		}
		return nil
	}
}

func TestAccShopLocale_basic(t *testing.T) {
	accSetup(t)
	ownLocales(t, locFr)
	cfg := func(published bool) string {
		return config(fmt.Sprintf(`
  locale              = %q
  published           = %t
  allow_data_deletion = true
`, locFr, published))
	}
	// Imported state has allow_data_deletion = false, so import steps use a config without it.
	imported := config(fmt.Sprintf("locale = %q\npublished = false", locFr))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, locFr),
		Steps: []resource.TestStep{
			{
				Config:      config(`locale = "xx"`),
				ExpectError: regexp.MustCompile(`Locale is invalid`),
			},
			{
				Config: cfg(true),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity(locFr),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact(locFr)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("locale"), knownvalue.StringExact(locFr)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("published"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("French")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("primary"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("adopt_existing"), knownvalue.Bool(false)),
				},
			},
			{
				Config: cfg(false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("published"), knownvalue.Bool(false)),
				},
			},
			{RefreshState: true},
			{
				// Refresh steps can't carry state checks; this no-op step checks identity survives the refresh.
				Config:            cfg(false),
				ConfigStateChecks: []statecheck.StateCheck{expectIdentity(locFr)},
			},
			{Config: imported, ResourceName: addr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				// Generated config must pass validation and plan as import-only.
				Config:          imported,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateId:           locFr,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"allow_data_deletion"},
			},
		},
	})
}

// Unset published is unmanaged: a change made outside Terraform must not show up in the plan.
func TestAccShopLocale_unmanagedPublished(t *testing.T) {
	accSetup(t)
	ownLocales(t, locFr)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, locFr),
		Steps: []resource.TestStep{
			{Config: config(fmt.Sprintf("locale = %q\npublished = true\nallow_data_deletion = true", locFr))},
			{
				PreConfig: func() { setPublished(t, locFr, false) },
				Config:    config(fmt.Sprintf("locale = %q\nallow_data_deletion = true", locFr)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("published"), knownvalue.Bool(false)),
				},
			},
		},
	})
}

// The primary locale can't be disabled, so it is adopted and left alone on destroy, whatever the flags say.
func TestAccShopLocale_adoptPrimary(t *testing.T) {
	accSetup(t)
	primary := lookup(t, locPrimary)
	if primary == nil || !primary.Primary {
		t.Fatalf("dev store primary locale is not %s", locPrimary)
	}
	cfg := func(adopt bool) string {
		return config(fmt.Sprintf("locale = %q\nadopt_existing = %t", locPrimary, adopt))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if l := lookup(t, locPrimary); l == nil || !l.Primary {
				return fmt.Errorf("primary locale %s was disabled, want it kept", locPrimary)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: cfg(false), ExpectError: regexp.MustCompile(`adopt_existing = true`)},
			{
				Config: cfg(true),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity(locPrimary),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("primary"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("published"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringRegexp(regexp.MustCompile(`^English`))),
				},
			},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateId:           locPrimary,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"adopt_existing"},
			},
			{
				// Without adopt_existing (as after an import) the final destroy must still only drop it from state.
				Config: cfg(false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
			},
		},
	})
}

// An adopted locale stays enabled on destroy, and the data-deletion guard doesn't apply to it.
func TestAccShopLocale_adopt(t *testing.T) {
	accSetup(t)
	ownLocales(t, locFr)
	cfg := func(adopt bool) string {
		return config(fmt.Sprintf("locale = %q\npublished = true\nadopt_existing = %t", locFr, adopt))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if lookup(t, locFr) == nil {
				return fmt.Errorf("adopted locale %s was disabled, want it kept", locFr)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				PreConfig:   func() { enable(t, locFr) },
				Config:      cfg(false),
				ExpectError: regexp.MustCompile(`adopt_existing = true`),
			},
			{
				Config: cfg(true),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity(locFr),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("published"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("adopt_existing"), knownvalue.Bool(true)),
				},
			},
		},
	})
}

// Disabling deletes the locale's translations, so destroy and replace need allow_data_deletion in state.
func TestAccShopLocale_dataDeletionGuard(t *testing.T) {
	accSetup(t)
	ownLocales(t, locFr, locIt)
	guarded := config(fmt.Sprintf("locale = %q", locFr))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, locFr, locIt),
		Steps: []resource.TestStep{
			{
				Config: guarded,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("allow_data_deletion"), knownvalue.Bool(false)),
				},
			},
			{
				// Replacing changes the key and disables the old locale first.
				Config:      config(fmt.Sprintf("locale = %q", locIt)),
				ExpectError: regexp.MustCompile(`allow_data_deletion = true`),
			},
			{Config: guarded, Destroy: true, ExpectError: regexp.MustCompile(`allow_data_deletion = true`)},
			{
				// Opt in so the test can clean up after itself.
				Config: config(fmt.Sprintf("locale = %q\nallow_data_deletion = true", locFr)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
			},
		},
	})
}
