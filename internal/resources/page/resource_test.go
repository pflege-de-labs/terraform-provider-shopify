package page_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

const addr = "shopify_page.test"

var gidRe = regexp.MustCompile(`^gid://shopify/Page/\d+$`)

var expectIdentity = statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(gidRe)})

// testHandle keeps test pages apart from real ones.
func testHandle() string {
	return "tf-acc-page-" + strings.ToLower(sdkacctest.RandStringFromCharSet(6, sdkacctest.CharSetAlphaNum))
}

// accSetup skips without TF_ACC, because these tests call the API before resource.Test would skip them.
func accSetup(t *testing.T) {
	t.Helper()
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skip("Acceptance tests skipped unless env 'TF_ACC' set")
	}
	acctest.PreCheck(t)
}

// lookup returns nil when no page has the handle.
func lookup(t *testing.T, handle string) *client.PageFields {
	t.Helper()
	res, err := client.FindPages(context.Background(), acctest.Client(t), fmt.Sprintf("handle:%q", handle))
	if err != nil {
		t.Fatal(err)
	}
	for i := range res.Pages.Nodes {
		if res.Pages.Nodes[i].Handle == handle {
			return &res.Pages.Nodes[i].PageFields
		}
	}
	return nil
}

func mustLookup(t *testing.T, handle string) *client.PageFields {
	t.Helper()
	p := lookup(t, handle)
	if p == nil {
		t.Fatalf("page %q does not exist", handle)
	}
	return p
}

// mustCreate makes a page through the client, outside Terraform.
func mustCreate(t *testing.T, in client.PageCreateInput) {
	t.Helper()
	res, err := client.CreatePage(context.Background(), acctest.Client(t), in)
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.PageCreate.UserErrors; len(errs) > 0 {
		t.Fatalf("pageCreate: %v", errs)
	}
}

// mustUpdate changes a page through the client, outside Terraform.
func mustUpdate(t *testing.T, id string, in client.PageUpdateInput) {
	t.Helper()
	res, err := client.UpdatePage(context.Background(), acctest.Client(t), id, in)
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.PageUpdate.UserErrors; len(errs) > 0 {
		t.Fatalf("pageUpdate: %v", errs)
	}
}

func deleteByID(t *testing.T, id string) error {
	t.Helper()
	res, err := client.DeletePage(context.Background(), acctest.Client(t), id)
	if err != nil {
		return err
	}
	if errs := res.PageDelete.UserErrors; len(errs) > 0 {
		return fmt.Errorf("pageDelete %s: %v", id, errs)
	}
	return nil
}

// cleanup deletes the test pages with these handles when the test ends, whatever state they are in.
func cleanup(t *testing.T, handles ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, h := range handles {
			if p := lookup(t, h); p != nil {
				if err := deleteByID(t, p.Id); err != nil {
					t.Error(err)
				}
			}
		}
	})
}

// checkGone fails, and removes the leftovers, when a test page still exists.
func checkGone(t *testing.T, handles ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for _, h := range handles {
			if p := lookup(t, h); p != nil {
				_ = deleteByID(t, p.Id)
				return fmt.Errorf("page %s still exists", h)
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
	return fmt.Sprintf("resource \"shopify_page\" \"test\" {\n%s\n}\n", body)
}

func emptyPlan() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
}

func TestAccPage_basic(t *testing.T) {
	accSetup(t)
	handle := testHandle()
	renamed := handle + "-b"
	cleanup(t, handle, renamed)
	const broken = "<p>Hi<br>there<b>bold</p>"
	created := config(fmt.Sprintf(`
  handle          = %q
  title           = "tf-acc page"
  body            = %q
  published       = true
  template_suffix = "tf-acc"
`, handle, broken))
	updated := config(fmt.Sprintf(`
  handle          = %q
  title           = "tf-acc page 2"
  body            = "<p>two</p>"
  published       = false
  template_suffix = ""
`, handle))
	rename := config(fmt.Sprintf(`
  handle          = %q
  title           = "tf-acc page 2"
  body            = "<p>two</p>"
  published       = false
  template_suffix = ""
`, renamed))
	// body, published and template_suffix unset: unmanaged, so out-of-band changes must not plan.
	unmanaged := config(fmt.Sprintf(`
  handle = %q
  title  = "tf-acc page 2"
`, renamed))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle, renamed),
		Steps: []resource.TestStep{
			{Config: config(`handle = "x"` + "\n" + `title = ""`), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Attribute title string length`)},
			{Config: config(`handle = "X Y"` + "\n" + `title = "x"`), PlanOnly: true, ExpectError: regexp.MustCompile(`Invalid handle`)},
			{
				// Shopify repairs the body without a diff.
				Config: created,
				Check: func(*terraform.State) error {
					p := mustLookup(t, handle)
					if p.Body != "<p>Hi<br>there<b>bold</b></p>" || !p.IsPublished || p.TemplateSuffix == nil || *p.TemplateSuffix != "tf-acc" {
						return fmt.Errorf("live page = %+v, want repaired body, published, suffix tf-acc", p)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("id", knownvalue.StringRegexp(gidRe)),
					stateValue("handle", str(handle)),
					stateValue("body", str(broken)),
					stateValue("published", knownvalue.Bool(true)),
					stateValue("template_suffix", str("tf-acc")),
					stateValue("adopt_existing", knownvalue.Bool(false)),
				},
			},
			{Config: created, ConfigPlanChecks: emptyPlan()},
			{
				Config: updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				Check: func(*terraform.State) error {
					p := mustLookup(t, handle)
					if p.Title != "tf-acc page 2" || p.Body != "<p>two</p>" || p.IsPublished || p.TemplateSuffix == nil || *p.TemplateSuffix != "" {
						return fmt.Errorf("live page = %+v, want the updated values", p)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("title", str("tf-acc page 2")),
					stateValue("published", knownvalue.Bool(false)),
					stateValue("template_suffix", str("")),
				},
			},
			{Config: updated, ConfigPlanChecks: emptyPlan()},
			{
				// A new handle renames in place: same GID.
				Config: rename,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{stateValue("handle", str(renamed))},
			},
			{
				// Review Focus 2: template suffix, published and body changed through the client while unset in config.
				PreConfig: func() {
					p := mustLookup(t, renamed)
					suffix, published, body := "client", true, "<p>client</p>"
					mustUpdate(t, p.Id, client.PageUpdateInput{TemplateSuffix: &suffix, IsPublished: &published, Body: &body})
				},
				Config:           unmanaged,
				ConfigPlanChecks: emptyPlan(),
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("template_suffix", str("client")),
					stateValue("published", knownvalue.Bool(true)),
					stateValue("body", str("<p>client</p>")),
				},
			},
			{RefreshState: true},
			{Config: unmanaged, ConfigStateChecks: []statecheck.StateCheck{expectIdentity}},
			{Config: unmanaged, ResourceName: addr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				Config:                               unmanaged,
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        renamed,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "handle",
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

// Pattern A: an existing page is only taken over with adopt_existing, and destroy leaves it as Terraform left it.
func TestAccPage_adopt(t *testing.T) {
	accSetup(t)
	handle := testHandle()
	cleanup(t, handle)
	mustCreate(t, client.PageCreateInput{Title: "tf-acc adopt (client)", Handle: &handle, Body: ptr("<p>client</p>")})
	body := func(adopt bool) string {
		return config(fmt.Sprintf(`
  handle         = %q
  title          = "tf-acc adopt (terraform)"
  adopt_existing = %t
`, handle, adopt))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Review Focus 1: destroy only drops state, so the page is still there, unchanged.
		CheckDestroy: func(*terraform.State) error {
			p := lookup(t, handle)
			switch {
			case p == nil:
				return fmt.Errorf("adopted page %s was deleted", handle)
			case p.Title != "tf-acc adopt (terraform)" || p.Body != "<p>client</p>":
				return fmt.Errorf("adopted page changed on destroy: %+v", p)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: body(false), ExpectError: regexp.MustCompile(`Page already exists`)},
			{
				Config: body(true),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("title", str("tf-acc adopt (terraform)")),
					// Unset, so the client's value stays.
					stateValue("body", str("<p>client</p>")),
					stateValue("adopt_existing", knownvalue.Bool(true)),
				},
			},
		},
	})
}

func ptr[T any](v T) *T { return &v }

// The store's own contact page is adopted with its live values, so adopt and destroy leave it exactly as it was.
func TestAccPage_adoptContact(t *testing.T) {
	accSetup(t)
	const handle = "contact"
	before := lookup(t, handle)
	if before == nil {
		t.Skip("the dev store has no contact page")
	}
	snapshot := func(p *client.PageFields) string {
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	want := snapshot(before)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if got := snapshot(lookup(t, handle)); got != want {
				return fmt.Errorf("contact page changed:\n got %s\nwant %s", got, want)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: config(fmt.Sprintf(`
  handle         = %q
  title          = %q
  adopt_existing = true
`, handle, before.Title)),
			ConfigStateChecks: []statecheck.StateCheck{
				expectIdentity,
				stateValue("id", str(before.Id)),
				stateValue("adopt_existing", knownvalue.Bool(true)),
			},
		}},
	})
}

// A page deleted outside Terraform is planned for recreation, and a delete of one already gone succeeds.
func TestAccPage_deletedOutside(t *testing.T) {
	accSetup(t)
	handle := testHandle()
	cleanup(t, handle)
	cfg := config(fmt.Sprintf("  handle = %q\n  title  = \"tf-acc gone\"\n", handle))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					if err := deleteByID(t, mustLookup(t, handle).Id); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)},
				},
			},
			{
				// Gone again just before destroy.
				PreConfig: func() {
					if err := deleteByID(t, mustLookup(t, handle).Id); err != nil {
						t.Fatal(err)
					}
				},
				Config:   cfg,
				PlanOnly: true,
				// Refresh removes it from state, so the plan proposes a create.
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
