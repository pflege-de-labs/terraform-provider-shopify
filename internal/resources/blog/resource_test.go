package blog_test

import (
	"context"
	"encoding/json"
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

const addr = "shopify_blog.test"

var gidRe = regexp.MustCompile(`^gid://shopify/Blog/\d+$`)

var expectIdentity = statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(gidRe)})

// testHandle keeps test blogs apart from real ones.
func testHandle() string {
	return "tf-acc-blog-" + strings.ToLower(sdkacctest.RandStringFromCharSet(6, sdkacctest.CharSetAlphaNum))
}

// lookup returns nil when no blog has the handle.
func lookup(t *testing.T, handle string) *client.BlogFields {
	t.Helper()
	res, err := client.FindBlogs(context.Background(), acctest.Client(t), fmt.Sprintf("handle:%q", handle))
	if err != nil {
		t.Fatal(err)
	}
	for i := range res.Blogs.Nodes {
		if res.Blogs.Nodes[i].Handle == handle {
			return &res.Blogs.Nodes[i].BlogFields
		}
	}
	return nil
}

func mustLookup(t *testing.T, handle string) *client.BlogFields {
	t.Helper()
	b := lookup(t, handle)
	if b == nil {
		t.Fatalf("blog %q does not exist", handle)
	}
	return b
}

// mustCreate makes a blog through the client, outside Terraform.
func mustCreate(t *testing.T, in client.BlogCreateInput) {
	t.Helper()
	res, err := client.CreateBlog(context.Background(), acctest.Client(t), in)
	if err != nil {
		t.Fatal(err)
	}
	if res.BlogCreate == nil || res.BlogCreate.Blog == nil || len(res.BlogCreate.UserErrors) > 0 {
		t.Fatalf("blogCreate: %+v", res.BlogCreate)
	}
}

// mustUpdate changes a blog through the client, outside Terraform.
func mustUpdate(t *testing.T, id string, in client.BlogUpdateInput) {
	t.Helper()
	res, err := client.UpdateBlog(context.Background(), acctest.Client(t), id, in)
	if err != nil {
		t.Fatal(err)
	}
	if res.BlogUpdate == nil || len(res.BlogUpdate.UserErrors) > 0 {
		t.Fatalf("blogUpdate: %+v", res.BlogUpdate)
	}
}

// mustCreateArticle gives a blog one article, through the client.
func mustCreateArticle(t *testing.T, blogID string) string {
	t.Helper()
	name := "tf-acc"
	res, err := client.CreateArticle(context.Background(), acctest.Client(t), client.ArticleCreateInput{
		BlogId: &blogID, Title: "tf-acc article", Author: client.AuthorInput{Name: &name},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ArticleCreate == nil || res.ArticleCreate.Article == nil || len(res.ArticleCreate.UserErrors) > 0 {
		t.Fatalf("articleCreate: %+v", res.ArticleCreate)
	}
	return res.ArticleCreate.Article.Id
}

func articleExists(t *testing.T, id string) bool {
	t.Helper()
	res, err := client.GetArticle(context.Background(), acctest.Client(t), id)
	if err != nil {
		t.Fatal(err)
	}
	return res.Article != nil
}

func deleteByID(t *testing.T, id string) error {
	t.Helper()
	res, err := client.DeleteBlog(context.Background(), acctest.Client(t), id)
	if err != nil {
		return err
	}
	if res.BlogDelete == nil || len(res.BlogDelete.UserErrors) > 0 {
		return fmt.Errorf("blogDelete %s: %+v", id, res.BlogDelete)
	}
	return nil
}

// withSuffixes adds the handle Shopify would give a taken one, so a wrongly created copy is cleaned up too.
func withSuffixes(handles []string) []string {
	out := append([]string(nil), handles...)
	for _, h := range handles {
		out = append(out, h+"-1")
	}
	return out
}

// cleanup deletes the test blogs with these handles (and their -1 variants) when the test ends, whatever state they are in.
func cleanup(t *testing.T, handles ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, h := range withSuffixes(handles) {
			if b := lookup(t, h); b != nil {
				if err := deleteByID(t, b.Id); err != nil {
					t.Error(err)
				}
			}
		}
	})
}

// checkGone fails, and removes the leftovers, when a test blog or a suffixed copy still exists.
func checkGone(t *testing.T, handles ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for _, h := range withSuffixes(handles) {
			if b := lookup(t, h); b != nil {
				_ = deleteByID(t, b.Id)
				return fmt.Errorf("blog %s still exists", h)
			}
		}
		return nil
	}
}

func str(s string) knownvalue.Check { return knownvalue.StringExact(s) }

func ptr[T any](v T) *T { return &v }

func stateValue(path string, check knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(addr, tfjsonpath.New(path), check)
}

func config(body string) string {
	return fmt.Sprintf("resource \"shopify_blog\" \"test\" {\n%s\n}\n", body)
}

func emptyPlan() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
}

func updatePlan() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)}}
}

func TestAccBlog_basic(t *testing.T) {
	acctest.Setup(t)
	handle := testHandle()
	renamed := handle + "-b"
	cleanup(t, handle, renamed)
	created := config(fmt.Sprintf(`
  handle          = %q
  title           = "tf-acc blog"
  template_suffix = "tf-acc"
`, handle))
	updated := config(fmt.Sprintf(`
  handle          = %q
  title           = "tf-acc blog 2"
  comment_policy  = "MODERATED"
  template_suffix = ""
`, handle))
	rename := config(fmt.Sprintf(`
  handle          = %q
  title           = "tf-acc blog 2"
  comment_policy  = "MODERATED"
  template_suffix = ""
`, renamed))
	// comment_policy and template_suffix unset: unmanaged, so out-of-band changes must not plan.
	unmanaged := config(fmt.Sprintf(`
  handle = %q
  title  = "tf-acc blog 2"
`, renamed))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle, renamed),
		Steps: []resource.TestStep{
			{Config: config(`handle = "x"` + "\n" + `title = ""`), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Attribute title string length`)},
			{Config: config(`handle = "X Y"` + "\n" + `title = "x"`), PlanOnly: true, ExpectError: regexp.MustCompile(`Invalid handle`)},
			{Config: config(`handle = "x"` + "\n" + `title = "x"` + "\n" + `comment_policy = "OPEN"`), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)comment_policy value must be one of`)},
			{
				Config: created,
				Check: func(*terraform.State) error {
					b := mustLookup(t, handle)
					if b.CommentPolicy != client.CommentPolicyClosed || b.TemplateSuffix == nil || *b.TemplateSuffix != "tf-acc" {
						return fmt.Errorf("live blog = %+v, want CLOSED and suffix tf-acc", b)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("id", knownvalue.StringRegexp(gidRe)),
					stateValue("handle", str(handle)),
					stateValue("comment_policy", str("CLOSED")),
					stateValue("template_suffix", str("tf-acc")),
					stateValue("adopt_existing", knownvalue.Bool(false)),
					stateValue("allow_data_deletion", knownvalue.Bool(false)),
				},
			},
			{Config: created, ConfigPlanChecks: emptyPlan()},
			{
				Config:           updated,
				ConfigPlanChecks: updatePlan(),
				Check: func(*terraform.State) error {
					b := mustLookup(t, handle)
					if b.Title != "tf-acc blog 2" || b.CommentPolicy != client.CommentPolicyModerated || b.TemplateSuffix == nil || *b.TemplateSuffix != "" {
						return fmt.Errorf("live blog = %+v, want the updated values", b)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("title", str("tf-acc blog 2")),
					stateValue("comment_policy", str("MODERATED")),
					stateValue("template_suffix", str("")),
				},
			},
			{Config: updated, ConfigPlanChecks: emptyPlan()},
			{
				// A new handle renames in place: same GID.
				Config:            rename,
				ConfigPlanChecks:  updatePlan(),
				ConfigStateChecks: []statecheck.StateCheck{stateValue("handle", str(renamed))},
			},
			{
				// Review Focus 2: comment_policy was set above and is left out now; changed through the client in between.
				PreConfig: func() {
					b := mustLookup(t, renamed)
					mustUpdate(t, b.Id, client.BlogUpdateInput{CommentPolicy: ptr(client.CommentPolicyAutoPublished), TemplateSuffix: ptr("client")})
				},
				Config:           unmanaged,
				ConfigPlanChecks: emptyPlan(),
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("comment_policy", str("AUTO_PUBLISHED")),
					stateValue("template_suffix", str("client")),
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

// Pattern A: an existing blog is only taken over with adopt_existing, and destroy leaves it as Terraform left it.
func TestAccBlog_adopt(t *testing.T) {
	acctest.Setup(t)
	handle := testHandle()
	cleanup(t, handle)
	mustCreate(t, client.BlogCreateInput{Title: "tf-acc adopt (client)", Handle: &handle, CommentPolicy: ptr(client.CommentPolicyModerated)})
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
		// Destroy only drops state, so the blog is still there with Terraform's title and the client's comment policy.
		CheckDestroy: func(*terraform.State) error {
			b := lookup(t, handle)
			switch {
			case b == nil:
				return fmt.Errorf("adopted blog %s was deleted", handle)
			case b.Title != "tf-acc adopt (terraform)" || b.CommentPolicy != client.CommentPolicyModerated:
				return fmt.Errorf("adopted blog changed on destroy: %+v", b)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: body(false), ExpectError: regexp.MustCompile(`Blog already exists`)},
			{
				Config: body(true),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("title", str("tf-acc adopt (terraform)")),
					// Unset, so the client's value stays.
					stateValue("comment_policy", str("MODERATED")),
					stateValue("adopt_existing", knownvalue.Bool(true)),
				},
			},
		},
	})
}

// The store's own news blog is adopted with its live values, so adopt and destroy leave it exactly as it was (Review Focus 1).
func TestAccBlog_adoptNews(t *testing.T) {
	acctest.Setup(t)
	const handle = "news"
	before := lookup(t, handle)
	if before == nil {
		t.Skip("the dev store has no news blog")
	}
	snapshot := func(b *client.BlogFields) string {
		out, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	want := snapshot(before)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if got := snapshot(lookup(t, handle)); got != want {
				return fmt.Errorf("news blog changed:\n got %s\nwant %s", got, want)
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

// Shopify suffixes a taken handle instead of failing; neither create nor update may leave a suffixed blog behind.
func TestAccBlog_takenHandle(t *testing.T) {
	acctest.Setup(t)
	taken, mine := testHandle(), testHandle()
	cleanup(t, taken, mine)
	mustCreate(t, client.BlogCreateInput{Title: "tf-acc taken", Handle: &taken})
	noSuffix := func(*terraform.State) error {
		for _, h := range []string{taken + "-1", mine + "-1"} {
			if lookup(t, h) != nil {
				return fmt.Errorf("blog %s was created", h)
			}
		}
		return nil
	}
	create := func(handle string) string {
		return config(fmt.Sprintf("  handle = %q\n  title  = \"tf-acc mine\"\n", handle))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(s *terraform.State) error {
			if err := noSuffix(s); err != nil {
				return err
			}
			return checkGone(t, mine)(s)
		},
		Steps: []resource.TestStep{
			{Config: create(taken), ExpectError: regexp.MustCompile(`Blog already exists`)},
			{Config: create(mine)},
			{
				// The rename is refused before blogUpdate, which would silently suffix it.
				Config:      create(taken),
				ExpectError: regexp.MustCompile(`Handle already taken`),
			},
		},
	})
}

// Pattern B: deleting a blog deletes its articles, so a blog with articles needs allow_data_deletion; an empty one never does.
func TestAccBlog_dataDeletion(t *testing.T) {
	acctest.Setup(t)
	handle := testHandle()
	cleanup(t, handle)
	var articleID string
	guarded := config(fmt.Sprintf("  handle = %q\n  title  = \"tf-acc guarded\"\n", handle))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(s *terraform.State) error {
			if err := checkGone(t, handle)(s); err != nil {
				return err
			}
			if articleExists(t, articleID) {
				return fmt.Errorf("article %s survived its blog", articleID)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: guarded, ConfigStateChecks: []statecheck.StateCheck{stateValue("allow_data_deletion", knownvalue.Bool(false))}},
			{
				PreConfig:   func() { articleID = mustCreateArticle(t, mustLookup(t, handle).Id) },
				Config:      guarded,
				Destroy:     true,
				ExpectError: regexp.MustCompile(`(?s)holds 1 articles.*allow_data_deletion = true`),
			},
			{
				// Opt in, so the destroy at the end of the test deletes the blog and its article.
				Config: config(fmt.Sprintf("  handle              = %q\n  title               = \"tf-acc guarded\"\n  allow_data_deletion = true\n", handle)),
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("allow_data_deletion", knownvalue.Bool(true)),
				},
			},
		},
	})
}

// A blog deleted outside Terraform is planned for recreation.
func TestAccBlog_deletedOutside(t *testing.T) {
	acctest.Setup(t)
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
		},
	})
}
