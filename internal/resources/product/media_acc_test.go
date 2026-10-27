package product_test

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/compare"
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

// A 1x1 PNG on Wikimedia Commons, as in the file tests.
const imageURL = "https://upload.wikimedia.org/wikipedia/commons/c/ca/1x1.png"

func testFilename() string {
	return "tf-acc-file-" + strings.ToLower(sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlphaNum)) + ".png"
}

func fileConfig(name, filename string) string {
	return fmt.Sprintf(`
resource "shopify_file" %q {
  filename            = %q
  content_type        = "IMAGE"
  source              = %q
  allow_data_deletion = true
}
`, name, filename, imageURL)
}

// files records the GIDs of a test's shopify_file resources and deletes them when the test ends.
type files struct {
	mu  sync.Mutex
	ids map[string]string
}

func newFiles(t *testing.T) *files {
	t.Helper()
	f := &files{ids: map[string]string{}}
	t.Cleanup(func() {
		for _, id := range f.ids {
			res, err := client.DeleteFile(context.Background(), acctest.Client(t), []string{id})
			if err != nil {
				t.Error(err)
				continue
			}
			for _, e := range res.FileDelete.UserErrors {
				if e.Code == nil || *e.Code != client.FilesErrorCodeFileDoesNotExist {
					t.Errorf("fileDelete %s: %s", id, e.Message)
				}
			}
		}
	})
	return f
}

// track records the IDs of these shopify_file resources from state.
func (f *files) track(names ...string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, n := range names {
			if rs := s.RootModule().Resources["shopify_file."+n]; rs != nil && rs.Primary != nil {
				f.ids[n] = rs.Primary.ID
			}
		}
		return nil
	}
}

func (f *files) id(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ids[name]
}

func fileExists(t *testing.T, id string) bool {
	t.Helper()
	res, err := client.GetFile(context.Background(), acctest.Client(t), id)
	if err != nil {
		t.Fatal(err)
	}
	return string(res.Node) != "null"
}

func liveMedia(t *testing.T, handle string) []string {
	t.Helper()
	var ids []string
	for _, n := range mustLookup(t, handle).Media.Nodes {
		ids = append(ids, n.GetId())
	}
	return ids
}

// checkMedia fails unless the product's media are these files in this order.
func checkMedia(t *testing.T, handle string, f *files, names ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		want := make([]string, len(names))
		for i, n := range names {
			want[i] = f.id(n)
		}
		if got := liveMedia(t, handle); !slices.Equal(got, want) {
			return fmt.Errorf("product %s has media %v, want %v", handle, got, want)
		}
		return nil
	}
}

// checkFiles fails unless each named file exists (true) or is gone (false).
func checkFiles(t *testing.T, f *files, want map[string]bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for n, exists := range want {
			if got := fileExists(t, f.id(n)); got != exists {
				return fmt.Errorf("file %s (%s) exists = %t, want %t", n, f.id(n), got, exists)
			}
		}
		return nil
	}
}

// mediaState checks media_ids against the IDs of the named shopify_file resources, in order.
func mediaState(names ...string) []statecheck.StateCheck {
	checks := []statecheck.StateCheck{
		statecheck.ExpectKnownValue(addr, tfjsonpath.New("media_ids"), knownvalue.ListSizeExact(len(names))),
	}
	for i, n := range names {
		checks = append(checks, statecheck.CompareValuePairs(addr, tfjsonpath.New("media_ids").AtSliceIndex(i), "shopify_file."+n, tfjsonpath.New("id"), compare.ValuesSame()))
	}
	return checks
}

func TestAccProduct_media(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	handle := testHandle()
	cleanup(t, handle)
	f := newFiles(t)
	names := map[string]string{"a": testFilename(), "b": testFilename(), "c": testFilename()}
	cfg := func(fileNames []string, allow bool, title, media string) string {
		var b strings.Builder
		for _, n := range fileNames {
			b.WriteString(fileConfig(n, names[n]))
		}
		b.WriteString(config(fmt.Sprintf(`
  handle              = %q
  title               = %q
  status              = "DRAFT"
  allow_data_deletion = %t
%s
`, handle, title, allow, media)))
		return b.String()
	}
	const title = "tf-acc media"
	abc := []string{"a", "b", "c"}
	ac := []string{"a", "c"}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             resource.ComposeTestCheckFunc(checkGone(t, handle), checkFiles(t, f, map[string]bool{"a": false, "c": false})),
		Steps: []resource.TestStep{
			{
				Config:      cfg(nil, true, title, `media_ids = ["gid://shopify/GenericFile/1"]`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`3D\s+model\s+or\s+external\s+video`),
			},
			{
				// c stays unattached until the admin-side change below.
				Config:            cfg(abc, false, title, `media_ids = [shopify_file.a.id, shopify_file.b.id]`),
				Check:             resource.ComposeTestCheckFunc(f.track(abc...), checkMedia(t, handle, f, "a", "b")),
				ConfigStateChecks: append(mediaState("a", "b"), expectIdentity),
			},
			{
				Config: cfg(abc, false, title, `media_ids = [shopify_file.b.id, shopify_file.a.id]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				Check:             resource.ComposeTestCheckFunc(checkMedia(t, handle, f, "b", "a"), checkFiles(t, f, map[string]bool{"a": true, "b": true})),
				ConfigStateChecks: mediaState("b", "a"),
			},
			{
				Config:           cfg(abc, false, title, `media_ids = [shopify_file.b.id, shopify_file.a.id]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				// Detaching keeps the file: no flag needed, and shopify_file.b still finds it.
				Config:            cfg(abc, false, title, `media_ids = [shopify_file.a.id]`),
				Check:             resource.ComposeTestCheckFunc(checkMedia(t, handle, f, "a"), checkFiles(t, f, map[string]bool{"a": true, "b": true, "c": true})),
				ConfigStateChecks: mediaState("a"),
			},
			{
				Config:            cfg(abc, false, title, `media_ids = [shopify_file.a.id, shopify_file.b.id]`),
				Check:             checkMedia(t, handle, f, "a", "b"),
				ConfigStateChecks: mediaState("a", "b"),
			},
			{
				// Removing a file and its ID together: whichever runs first, only shopify_file.b deletes b, under its own flag.
				Config:            cfg(ac, false, title, `media_ids = [shopify_file.a.id]`),
				Check:             resource.ComposeTestCheckFunc(checkMedia(t, handle, f, "a"), checkFiles(t, f, map[string]bool{"a": true, "b": false, "c": true})),
				ConfigStateChecks: mediaState("a"),
			},
			{
				// Review focus 2: unset means unmanaged, so media attached elsewhere are no diff and survive other updates.
				PreConfig: func() {
					raw, _ := json.Marshal([]map[string]string{{"id": f.id("a")}, {"id": f.id("c")}})
					id := mustLookup(t, handle).Id
					mustSet(t, &id, client.ProductSetInput{Files: raw})
				},
				Config:            cfg(ac, false, title, ""),
				ConfigPlanChecks:  resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				ConfigStateChecks: mediaState("a", "c"),
			},
			{
				// Also sets the flag in state for the final destroy.
				Config:            cfg(ac, true, title+" (renamed)", ""),
				Check:             checkMedia(t, handle, f, "a", "c"),
				ConfigStateChecks: mediaState("a", "c"),
			},
			{
				// c was attached outside Terraform and isn't listed, so it is detached and kept.
				Config:            cfg(ac, true, title, `media_ids = [shopify_file.a.id]`),
				Check:             resource.ComposeTestCheckFunc(checkMedia(t, handle, f, "a"), checkFiles(t, f, map[string]bool{"a": true, "c": true})),
				ConfigStateChecks: mediaState("a"),
			},
			{
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        handle,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "handle",
				ImportStateVerifyIgnore:              []string{"adopt_existing", "allow_data_deletion", "media_managed"},
			},
			{
				// Generated config carries media_ids as file GIDs and must plan as import-only.
				Config:          ` `,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
		},
	})
}

// Destroying a product must not delete a file shopify_file guards with allow_data_deletion = false.
func TestAccProduct_destroyKeepsManagedFile(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	handle := testHandle()
	cleanup(t, handle)
	f := newFiles(t)
	filename := testFilename()
	file := func(allow bool) string {
		return strings.Replace(fileConfig("a", filename), "allow_data_deletion = true", fmt.Sprintf("allow_data_deletion = %t", allow), 1)
	}
	product := config(fmt.Sprintf(`
  handle              = %q
  title               = "tf-acc destroy keeps media"
  status              = "DRAFT"
  allow_data_deletion = true
  media_ids           = [shopify_file.a.id]
`, handle))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             resource.ComposeTestCheckFunc(checkGone(t, handle), checkFiles(t, f, map[string]bool{"a": false})),
		Steps: []resource.TestStep{
			{
				Config:            file(false) + product,
				Check:             resource.ComposeTestCheckFunc(f.track("a"), checkMedia(t, handle, f, "a")),
				ConfigStateChecks: mediaState("a"),
			},
			{
				Config: file(false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroy)},
				},
				Check: resource.ComposeTestCheckFunc(checkGone(t, handle), checkFiles(t, f, map[string]bool{"a": true})),
			},
			{
				// Lets the final destroy remove the file.
				Config: file(true),
			},
		},
	})
}

// With media_ids unset, destroy keeps Shopify's behaviour: the unused file goes, and its own destroy then skips the guard.
func TestAccProduct_destroyUnmanagedMedia(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	handle := testHandle()
	cleanup(t, handle)
	f := newFiles(t)
	cfg := strings.Replace(fileConfig("a", testFilename()), "allow_data_deletion = true", "allow_data_deletion = false", 1) + config(fmt.Sprintf(`
  handle              = %q
  title               = "tf-acc destroy unmanaged media"
  status              = "DRAFT"
  allow_data_deletion = true
  depends_on          = [shopify_file.a]
`, handle))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             resource.ComposeTestCheckFunc(checkGone(t, handle), checkFiles(t, f, map[string]bool{"a": false})),
		Steps: []resource.TestStep{
			{Config: cfg, Check: f.track("a")},
			{
				PreConfig: func() {
					raw, _ := json.Marshal([]map[string]string{{"id": f.id("a")}})
					id := mustLookup(t, handle).Id
					mustSet(t, &id, client.ProductSetInput{Files: raw})
				},
				Config:           cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            checkMedia(t, handle, f, "a"),
			},
		},
	})
}

// Issue 32: media_ids going from unset to the live list must record the media as managed, or a destroy deletes the guarded file.
func TestAccProduct_mediaUnsetToSetKeepsFile(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	handle := testHandle()
	cleanup(t, handle)
	f := newFiles(t)
	file := func(allow bool) string {
		return strings.Replace(fileConfig("a", testFilename()), "allow_data_deletion = true", fmt.Sprintf("allow_data_deletion = %t", allow), 1)
	}
	product := func(media string) string {
		return file(false) + config(fmt.Sprintf(`
  handle              = %q
  title               = "tf-acc media transition"
  status              = "DRAFT"
  allow_data_deletion = true
  %s
`, handle, media))
	}
	unmanaged := product("depends_on = [shopify_file.a]")
	managed := product("media_ids = [shopify_file.a.id]")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             resource.ComposeTestCheckFunc(checkGone(t, handle), checkFiles(t, f, map[string]bool{"a": false})),
		Steps: []resource.TestStep{
			{Config: unmanaged, Check: f.track("a")},
			{
				PreConfig: func() {
					raw, _ := json.Marshal([]map[string]string{{"id": f.id("a")}})
					id := mustLookup(t, handle).Id
					mustSet(t, &id, client.ProductSetInput{Files: raw})
				},
				Config: unmanaged,
				Check:  checkMedia(t, handle, f, "a"),
			},
			{
				Config: managed,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				Check:             checkMedia(t, handle, f, "a"),
				ConfigStateChecks: mediaState("a"),
			},
			{
				Config:           managed,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: file(false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroy)},
				},
				Check: resource.ComposeTestCheckFunc(checkGone(t, handle), checkFiles(t, f, map[string]bool{"a": true})),
			},
			{Config: file(true)},
		},
	})
}
