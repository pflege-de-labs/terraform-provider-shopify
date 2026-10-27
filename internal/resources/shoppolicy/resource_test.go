package shoppolicy_test

import (
	"context"
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
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const (
	addr = "shopify_shop_policy.test"
	// Types the tests write. PRIVACY_POLICY is never written: Shopify generates its text and the auto-managed flag can't be restored.
	typeLegal    = client.ShopPolicyTypeLegalNotice
	typeShipping = client.ShopPolicyTypeShippingPolicy
	// Every body the tests write starts with this, so leftovers of an aborted run are told apart from real text.
	marker = "<p>tf-acc-test "
)

var gidRe = regexp.MustCompile(`^gid://shopify/ShopPolicy/`)

func expectIdentity() statecheck.StateCheck {
	return statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(gidRe)})
}

func body(s string) string { return marker + s + "</p>" }

func config(typ client.ShopPolicyType, body string, extra ...string) string {
	return fmt.Sprintf("resource \"shopify_shop_policy\" \"test\" {\n  type = %q\n  body = %q\n%s\n}\n", typ, body, strings.Join(extra, "\n"))
}

// lookup returns nil when the type has no row.
func lookup(t *testing.T, typ client.ShopPolicyType) *client.ShopPolicyFields {
	t.Helper()
	res, err := client.ListShopPolicies(context.Background(), acctest.Client(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.Shop.ShopPolicies {
		if p.Type == typ {
			return &p.ShopPolicyFields
		}
	}
	return nil
}

// write returns an error instead of failing so cleanups can report it their way.
func write(t *testing.T, typ client.ShopPolicyType, body string) error {
	t.Helper()
	res, err := client.UpdateShopPolicy(context.Background(), acctest.Client(t), client.ShopPolicyInput{Type: typ, Body: body})
	if err != nil {
		return err
	}
	if errs := res.ShopPolicyUpdate.UserErrors; len(errs) > 0 {
		return fmt.Errorf("writing %s: %v", typ, errs)
	}
	return nil
}

func mustWrite(t *testing.T, typ client.ShopPolicyType, body string) {
	t.Helper()
	if err := write(t, typ, body); err != nil {
		t.Fatal(err)
	}
}

// ownPolicy refuses to touch a type with real text, clears leftovers of an aborted run, and restores the
// pre-test body afterwards; rows can't be deleted, so a type that had none ends up with an empty one.
func ownPolicy(t *testing.T, owned ...client.ShopPolicyType) {
	t.Helper()
	for _, typ := range owned {
		if typ == client.ShopPolicyTypePrivacyPolicy {
			t.Fatal("PRIVACY_POLICY must not be written by tests")
		}
		if p := lookup(t, typ); p != nil && p.Body != "" && !strings.HasPrefix(p.Body, marker) {
			t.Fatalf("dev store %s has text that is not from a test; not overwriting it", typ)
		}
		mustWrite(t, typ, "")
	}
	t.Cleanup(func() {
		for _, typ := range owned {
			if err := write(t, typ, ""); err != nil {
				t.Error(err)
			}
		}
	})
}

// checkBody fails unless the type still has exactly this text.
func checkBody(t *testing.T, typ client.ShopPolicyType, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		p := lookup(t, typ)
		if p == nil || p.Body != want {
			return fmt.Errorf("%s: policy changed, want body %q, got %+v", typ, want, p)
		}
		return nil
	}
}

func TestAccShopPolicy_basic(t *testing.T) {
	acctest.Setup(t)
	ownPolicy(t, typeLegal)
	first, second := body("one"), body("two &amp; <br> <b>verbatim</b>")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Review Focus 1: destroy only drops the policy from state.
		CheckDestroy: checkBody(t, typeLegal, second),
		Steps: []resource.TestStep{
			{Config: config(typeLegal, ""), ExpectError: regexp.MustCompile(`at least 1`)},
			{Config: config("NOPE", "x"), ExpectError: regexp.MustCompile(`must be one of`)},
			{
				Config: config(typeLegal, first),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity(),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringRegexp(gidRe)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("type"), knownvalue.StringExact(string(typeLegal))),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("body"), knownvalue.StringExact(first)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("title"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("url"), knownvalue.StringRegexp(regexp.MustCompile(`^https://`))),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("adopt_existing"), knownvalue.Bool(false)),
				},
			},
			{
				Config: config(typeLegal, second),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("body"), knownvalue.StringExact(second)),
				},
			},
			{RefreshState: true},
			{
				// Refresh steps can't carry state checks; this no-op step checks identity survives the refresh.
				Config:            config(typeLegal, second),
				ConfigStateChecks: []statecheck.StateCheck{expectIdentity()},
				ConfigPlanChecks:  resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{Config: config(typeLegal, second), ResourceName: addr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				// Generated config must pass validation and plan as import-only.
				Config:          config(typeLegal, second),
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{ResourceName: addr, ImportState: true, ImportStateId: string(typeLegal), ImportStateVerify: true},
		},
	})
}

// A type with text needs adopt_existing, so hand-written text isn't overwritten silently.
func TestAccShopPolicy_adopt(t *testing.T) {
	acctest.Setup(t)
	ownPolicy(t, typeShipping)
	written, want := body("by hand"), body("from config")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Review Focus 1: the adopted policy keeps the configured text after destroy.
		CheckDestroy: checkBody(t, typeShipping, want),
		Steps: []resource.TestStep{
			{
				PreConfig:   func() { mustWrite(t, typeShipping, written) },
				Config:      config(typeShipping, want),
				ExpectError: regexp.MustCompile(`adopt_existing = true`),
			},
			{
				// The refused create must not have touched the text.
				PreConfig: func() {
					if err := checkBody(t, typeShipping, written)(nil); err != nil {
						t.Fatal(err)
					}
				},
				Config: config(typeShipping, want, "adopt_existing = true"),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity(),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("body"), knownvalue.StringExact(want)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("adopt_existing"), knownvalue.Bool(true)),
				},
			},
		},
	})
}

// Safety path: destroying a policy that was imported rather than adopted leaves its text alone too.
func TestAccShopPolicy_destroyKeepsImported(t *testing.T) {
	acctest.Setup(t)
	ownPolicy(t, typeShipping)
	text := body("imported")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkBody(t, typeShipping, text),
		Steps: []resource.TestStep{
			{
				PreConfig: func() { mustWrite(t, typeShipping, text) },
				Config: fmt.Sprintf("import {\n  to = shopify_shop_policy.test\n  id = %q\n}\n", typeShipping) +
					config(typeShipping, text),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity(),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("adopt_existing"), knownvalue.Bool(false)),
				},
			},
		},
	})
}

// Body is the only managed attribute: other types stay untouched, edits to this one show as drift, and an emptied row counts as gone.
func TestAccShopPolicy_drift(t *testing.T) {
	acctest.Setup(t)
	ownPolicy(t, typeShipping, typeLegal)
	want, other := body("managed"), body("other type")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkBody(t, typeShipping, want),
		Steps: []resource.TestStep{
			{Config: config(typeShipping, want)},
			{
				// Review Focus 2: another type's text changed through the client isn't this resource's business.
				PreConfig: func() { mustWrite(t, typeLegal, other) },
				Config:    config(typeShipping, want),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				PreConfig: func() { mustWrite(t, typeShipping, body("edited in admin")) },
				Config:    config(typeShipping, want),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
			},
			{
				PreConfig: func() { mustWrite(t, typeShipping, "") },
				Config:    config(typeShipping, want),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)},
				},
			},
			{
				// Once other types exist, the checks above must not have changed this one's other-type text.
				Config: config(typeShipping, want),
				Check: func(*terraform.State) error {
					return checkBody(t, typeLegal, other)(nil)
				},
			},
		},
	})
}
