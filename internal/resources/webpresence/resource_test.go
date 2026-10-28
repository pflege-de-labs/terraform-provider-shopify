package webpresence_test

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/Khan/genqlient/graphql"
	sdkacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
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
	addr       = "shopify_web_presence.test"
	testPrefix = "tfacc"
	// A locale that is not the shop's primary one; enabled and published for the test.
	// Must differ from shoplocale's locales (nl, it): packages run in parallel on one store (#33).
	altLocale = "fr"
)

// testSuffix keeps test presences apart from real ones; Shopify allows letters only.
func testSuffix() string {
	return testPrefix + strings.ToLower(sdkacctest.RandStringFromCharSet(4, sdkacctest.CharSetAlpha))
}

var gidRe = regexp.MustCompile(`^gid://shopify/MarketWebPresence/\d+$`)

var expectIdentity = statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(gidRe)})

func config(body string) string {
	return fmt.Sprintf("resource \"shopify_web_presence\" \"test\" {\n%s\n}\n", body)
}

func graphqlRaw(t *testing.T, query string) json.RawMessage {
	t.Helper()
	var out json.RawMessage
	if err := acctest.Client(t).MakeRequest(context.Background(), &graphql.Request{Query: query}, &graphql.Response{Data: &out}); err != nil {
		t.Fatal(err)
	}
	return out
}

// publishLocale makes altLocale usable as an alternate locale and restores the previous state afterwards.
func publishLocale(t *testing.T) {
	t.Helper()
	var before struct {
		ShopLocales []struct {
			Locale    string
			Published bool
		}
	}
	if err := json.Unmarshal(graphqlRaw(t, `{ shopLocales { locale published } }`), &before); err != nil {
		t.Fatal(err)
	}
	present, published := false, false
	for _, l := range before.ShopLocales {
		if l.Locale == altLocale {
			present, published = true, l.Published
		}
	}
	if !present {
		graphqlRaw(t, fmt.Sprintf(`mutation { shopLocaleEnable(locale: %q) { userErrors { message } } }`, altLocale))
	}
	graphqlRaw(t, fmt.Sprintf(`mutation { shopLocaleUpdate(locale: %q, shopLocale: {published: true}) { userErrors { message } } }`, altLocale))
	t.Cleanup(func() {
		switch {
		case !present:
			graphqlRaw(t, fmt.Sprintf(`mutation { shopLocaleDisable(locale: %q) { userErrors { message } } }`, altLocale))
		case !published:
			graphqlRaw(t, fmt.Sprintf(`mutation { shopLocaleUpdate(locale: %q, shopLocale: {published: false}) { userErrors { message } } }`, altLocale))
		}
	})
}

func findBySuffix(t *testing.T, suffix string) *client.WebPresenceFields {
	t.Helper()
	res, err := client.ListWebPresences(context.Background(), acctest.Client(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.WebPresences.Nodes {
		if p.SubfolderSuffix != nil && strings.HasPrefix(*p.SubfolderSuffix, suffix) {
			return &p.WebPresenceFields
		}
	}
	return nil
}

func checkGone(t *testing.T, suffix string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if p := findBySuffix(t, suffix); p != nil {
			_, _ = client.DeleteWebPresence(context.Background(), acctest.Client(t), p.Id)
			return fmt.Errorf("web presence %s still exists", *p.SubfolderSuffix)
		}
		return nil
	}
}

func TestAccWebPresence_basic(t *testing.T) {
	acctest.Setup(t)
	publishLocale(t)
	suffix := testSuffix()
	updated := config(fmt.Sprintf(`
  subfolder_suffix  = %q
  default_locale    = "en"
  alternate_locales = []
`, suffix+"x"))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, suffix),
		Steps: []resource.TestStep{
			{
				Config:      config(`default_locale = "en"`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`exactly one of subfolder_suffix and domain_id`),
			},
			{
				Config: config(fmt.Sprintf(`
  subfolder_suffix  = %q
  default_locale    = "en"
  alternate_locales = [%q]
`, suffix, altLocale)),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringRegexp(gidRe)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("alternate_locales"), knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact(altLocale)})),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("root_urls").AtMapKey(altLocale), knownvalue.StringRegexp(regexp.MustCompile(`/`+altLocale+`-`+suffix+`/?$`))),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("domain_id"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("adopt_existing"), knownvalue.Bool(false)),
				},
			},
			{
				// The suffix changes in place and an empty set clears the alternates.
				Config: updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("subfolder_suffix"), knownvalue.StringExact(suffix+"x")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("alternate_locales"), knownvalue.SetSizeExact(0)),
				},
			},
			{RefreshState: true},
			{
				// Refresh steps can't carry state checks; this no-op step checks identity survives the refresh.
				Config:            updated,
				ConfigStateChecks: []statecheck.StateCheck{expectIdentity},
			},
			{Config: updated, ResourceName: addr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				// Generated config must pass validation and plan as import-only, without adopt_existing.
				Config:          updated,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        suffix + "x",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "subfolder_suffix",
				ImportStateVerifyIgnore:              []string{"adopt_existing"},
			},
		},
	})
}

// Unset alternate_locales is unmanaged: a change made outside Terraform must not show up in the plan.
func TestAccWebPresence_unmanagedAlternates(t *testing.T) {
	acctest.Setup(t)
	publishLocale(t)
	suffix := testSuffix()
	cfg := config(fmt.Sprintf(`
  subfolder_suffix = %q
  default_locale   = "en"
`, suffix))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, suffix),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					p := findBySuffix(t, suffix)
					en := "en"
					res, err := client.UpdateWebPresence(context.Background(), acctest.Client(t), p.Id, client.WebPresenceUpdateInput{
						DefaultLocale:    &en,
						AlternateLocales: json.RawMessage(fmt.Sprintf("[%q]", altLocale)),
					})
					if err != nil || len(res.WebPresenceUpdate.UserErrors) > 0 {
						t.Fatalf("updating web presence: %v %v", err, res)
					}
				},
				Config:   cfg,
				PlanOnly: true,
			},
		},
	})
}

// The shop's primary-domain presence can't be deleted, so it is adopted and left alone on destroy.
func TestAccWebPresence_adoptPrimary(t *testing.T) {
	acctest.Setup(t)
	res, err := client.ListWebPresences(context.Background(), acctest.Client(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	var primary *client.WebPresenceFields
	for _, p := range res.WebPresences.Nodes {
		if p.Domain != nil {
			primary = &p.WebPresenceFields
			break
		}
	}
	if primary == nil {
		t.Fatal("dev store has no domain web presence")
	}
	cfg := func(adopt bool) string {
		return config(fmt.Sprintf(`
  domain_id      = %q
  default_locale = %q
  adopt_existing = %t
`, primary.Domain.Id, primary.DefaultLocale.Locale, adopt))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			res, err := client.ListWebPresences(context.Background(), acctest.Client(t), nil)
			if err != nil {
				return err
			}
			for _, p := range res.WebPresences.Nodes {
				if p.Id == primary.Id {
					return nil
				}
			}
			return fmt.Errorf("primary web presence %s was deleted, want it kept", primary.Id)
		},
		Steps: []resource.TestStep{
			{Config: cfg(false), ExpectError: regexp.MustCompile(`adopt_existing = true`)},
			{
				Config: cfg(true),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact(primary.Id)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("subfolder_suffix"), knownvalue.Null()),
				},
			},
			{
				// A listed domain presence must also generate valid config that plans as import-only.
				Config:          cfg(true),
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        primary.Domain.Host,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "domain_id",
				ImportStateVerifyIgnore:              []string{"adopt_existing"},
			},
		},
	})
}
