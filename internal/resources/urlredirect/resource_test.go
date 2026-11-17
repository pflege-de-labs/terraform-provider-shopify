package urlredirect_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

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

const addr = "shopify_url_redirect.test"

var gidRe = regexp.MustCompile(`^gid://shopify/UrlRedirect/\d+$`)

var expectIdentity = statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(gidRe)})

// testPath keeps test redirects apart from real ones; Shopify stores paths lower case.
func testPath() string {
	return "/tf-acc-redirect-" + strings.ToLower(sdkacctest.RandStringFromCharSet(6, sdkacctest.CharSetAlphaNum))
}

// lookup returns nil when no redirect has the path.
func lookup(t *testing.T, p string) *client.UrlRedirectFields {
	t.Helper()
	q := fmt.Sprintf("path:%q", p)
	res, err := client.ListUrlRedirects(context.Background(), acctest.Client(t), nil, &q)
	if err != nil {
		t.Fatal(err)
	}
	for i := range res.UrlRedirects.Nodes {
		if res.UrlRedirects.Nodes[i].Path == p {
			return &res.UrlRedirects.Nodes[i].UrlRedirectFields
		}
	}
	return nil
}

func mustLookup(t *testing.T, p string) *client.UrlRedirectFields {
	t.Helper()
	r := lookup(t, p)
	if r == nil {
		t.Fatalf("redirect %q does not exist", p)
	}
	return r
}

// mustCreate makes a redirect through the client, outside Terraform.
func mustCreate(t *testing.T, p, target string) {
	t.Helper()
	res, err := client.CreateUrlRedirect(context.Background(), acctest.Client(t), client.UrlRedirectInput{Path: &p, Target: &target})
	if err != nil {
		t.Fatal(err)
	}
	if res.UrlRedirectCreate == nil || res.UrlRedirectCreate.UrlRedirect == nil || len(res.UrlRedirectCreate.UserErrors) > 0 {
		t.Fatalf("urlRedirectCreate: %+v", res.UrlRedirectCreate)
	}
}

func deleteByID(t *testing.T, id string) error {
	t.Helper()
	res, err := client.DeleteUrlRedirect(context.Background(), acctest.Client(t), id)
	if err != nil {
		return err
	}
	if res.UrlRedirectDelete == nil || len(res.UrlRedirectDelete.UserErrors) > 0 {
		return fmt.Errorf("urlRedirectDelete %s: %+v", id, res.UrlRedirectDelete)
	}
	return nil
}

// cleanup deletes the test redirects with these paths when the test ends, whatever state they are in.
func cleanup(t *testing.T, paths ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, p := range paths {
			if r := lookup(t, p); r != nil {
				if err := deleteByID(t, r.Id); err != nil {
					t.Error(err)
				}
			}
		}
	})
}

// checkGone fails, and removes the leftovers, when a test redirect still exists.
func checkGone(t *testing.T, paths ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for _, p := range paths {
			if r := lookup(t, p); r != nil {
				_ = deleteByID(t, r.Id)
				return fmt.Errorf("redirect %s still exists", p)
			}
		}
		return nil
	}
}

func str(s string) knownvalue.Check { return knownvalue.StringExact(s) }

func stateValue(path string, check knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(addr, tfjsonpath.New(path), check)
}

func config(body string) string {
	return fmt.Sprintf("resource \"shopify_url_redirect\" \"test\" {\n%s\n}\n", body)
}

func emptyPlan() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
}

func updatePlan() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)}}
}

func body(p, target string) string {
	return config(fmt.Sprintf("  path   = %q\n  target = %q\n", p, target))
}

func TestAccURLRedirect_basic(t *testing.T) {
	acctest.Setup(t)
	p := testPath()
	renamed := p + "-b"
	cleanup(t, p, renamed)
	var id string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, p, renamed),
		Steps: []resource.TestStep{
			// Non-canonical input is refused at plan time, not rewritten after apply.
			{Config: body("/Tf-Acc-Upper", "/x"), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Invalid path.*lower case`)},
			{Config: body("/tf-acc-trailing/", "/x"), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Invalid path.*must not end with /`)},
			{Config: body("tf-acc-noslash", "/x"), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Invalid path.*must start with /`)},
			{Config: body("https://example.com/tf-acc", "/x"), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Invalid path.*must start with /`)},
			{Config: body("/tf-acc space", "/x"), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Invalid path.*whitespace`)},
			{Config: body("/tf-acc", "foo"), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Invalid target.*absolute URL`)},
			{Config: body("/tf-acc", "https://example.com"), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Invalid target.*needs a host`)},
			{Config: body("/tf-acc", ""), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Invalid target`)},
			{
				Config: body(p, "/tf-acc-target"),
				Check: func(*terraform.State) error {
					id = mustLookup(t, p).Id
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("id", knownvalue.StringRegexp(gidRe)),
					stateValue("path", str(p)),
					stateValue("target", str("/tf-acc-target")),
					stateValue("adopt_existing", knownvalue.Bool(false)),
				},
			},
			{Config: body(p, "/tf-acc-target"), ConfigPlanChecks: emptyPlan()},
			{
				Config:           body(p, "/tf-acc-target-2"),
				ConfigPlanChecks: updatePlan(),
				Check: func(*terraform.State) error {
					if r := mustLookup(t, p); r.Target != "/tf-acc-target-2" {
						return fmt.Errorf("live target = %q", r.Target)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{stateValue("target", str("/tf-acc-target-2"))},
			},
			{
				// An absolute URL is kept as given.
				Config:            body(p, "https://example.com/tf-acc?x=B"),
				ConfigPlanChecks:  updatePlan(),
				ConfigStateChecks: []statecheck.StateCheck{stateValue("target", str("https://example.com/tf-acc?x=B"))},
			},
			{Config: body(p, "https://example.com/tf-acc?x=B"), ConfigPlanChecks: emptyPlan()},
			{
				// A new path renames in place: same GID.
				Config:           body(renamed, "https://example.com/tf-acc?x=B"),
				ConfigPlanChecks: updatePlan(),
				Check: func(*terraform.State) error {
					if got := mustLookup(t, renamed).Id; got != id {
						return fmt.Errorf("GID changed on rename: %s -> %s", id, got)
					}
					if lookup(t, p) != nil {
						return fmt.Errorf("old path %s still exists", p)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{stateValue("path", str(renamed))},
			},
			{RefreshState: true},
			{Config: body(renamed, "https://example.com/tf-acc?x=B"), ResourceName: addr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				Config:                               body(renamed, "https://example.com/tf-acc?x=B"),
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        renamed,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "path",
				ImportStateVerifyIgnore:              []string{"adopt_existing"},
			},
			{
				// Generated config must pass validation and plan as import-only, without adopt_existing.
				Config:          ` `,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
		},
	})
}

// Percent-encoded and query-string paths, as Shopify stores them, pass the validator and read back unchanged.
func TestAccURLRedirect_encodedPath(t *testing.T) {
	acctest.Setup(t)
	p := testPath() + "/caf%c3%a9%20x?ref=a"
	cleanup(t, p)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, p),
		Steps: []resource.TestStep{
			{Config: body(p, "/tf-acc-target"), ConfigStateChecks: []statecheck.StateCheck{stateValue("path", str(p))}},
			{Config: body(p, "/tf-acc-target"), ConfigPlanChecks: emptyPlan()},
			{Config: body(p, "/tf-acc-target"), ResourceName: addr, ImportState: true, ImportStateId: p, ImportStateVerify: true, ImportStateVerifyIdentifierAttribute: "path", ImportStateVerifyIgnore: []string{"adopt_existing"}},
		},
	})
}

// Pattern A: an existing redirect is only taken over with adopt_existing, and destroy leaves it as Terraform left it.
func TestAccURLRedirect_adopt(t *testing.T) {
	acctest.Setup(t)
	p := testPath()
	cleanup(t, p)
	mustCreate(t, p, "/tf-acc-client")
	adopt := func(on bool) string {
		return config(fmt.Sprintf("  path           = %q\n  target         = \"/tf-acc-terraform\"\n  adopt_existing = %t\n", p, on))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Destroy only drops state, so the redirect is still there with Terraform's target.
		CheckDestroy: func(*terraform.State) error {
			r := lookup(t, p)
			switch {
			case r == nil:
				return fmt.Errorf("adopted redirect %s was deleted", p)
			case r.Target != "/tf-acc-terraform":
				return fmt.Errorf("adopted redirect changed on destroy: %+v", r)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: adopt(false), ExpectError: regexp.MustCompile(`Redirect already exists`)},
			{
				Config: adopt(true),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("target", str("/tf-acc-terraform")),
					stateValue("adopt_existing", knownvalue.Bool(true)),
				},
			},
		},
	})
}

// A taken path is refused on create without adopt_existing, and on update by Shopify; nothing is created or changed.
func TestAccURLRedirect_takenPath(t *testing.T) {
	acctest.Setup(t)
	taken, mine := testPath(), testPath()
	cleanup(t, taken, mine)
	mustCreate(t, taken, "/tf-acc-other")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(s *terraform.State) error {
			if r := mustLookup(t, taken); r.Target != "/tf-acc-other" {
				return fmt.Errorf("taken redirect changed: %+v", r)
			}
			return checkGone(t, mine)(s)
		},
		Steps: []resource.TestStep{
			{Config: body(taken, "/tf-acc-mine"), ExpectError: regexp.MustCompile(`Redirect already exists`)},
			{Config: body(mine, "/tf-acc-mine")},
			{Config: body(taken, "/tf-acc-mine"), ExpectError: regexp.MustCompile(`(?s)Failed to update URL redirect.*already been taken`)},
		},
	})
}

// A target equal to the path is refused by Shopify at apply, on the target attribute.
func TestAccURLRedirect_sameTarget(t *testing.T) {
	acctest.Setup(t)
	p := testPath()
	cleanup(t, p)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, p),
		Steps: []resource.TestStep{
			{Config: body(p, p), ExpectError: regexp.MustCompile(`(?s)Failed to create URL redirect.*same as path`)},
		},
	})
}

// A redirect deleted outside Terraform is planned for recreation.
func TestAccURLRedirect_deletedOutside(t *testing.T) {
	acctest.Setup(t)
	p := testPath()
	cleanup(t, p)
	cfg := body(p, "/tf-acc-target")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, p),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					if err := deleteByID(t, mustLookup(t, p).Id); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)},
				},
			},
		},
	})
}
