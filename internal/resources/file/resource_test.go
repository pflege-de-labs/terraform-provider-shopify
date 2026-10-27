package file_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

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

const (
	addr = "shopify_file.test"
	// A 1x1 PNG on Wikimedia Commons.
	imageURL = "https://upload.wikimedia.org/wikipedia/commons/c/ca/1x1.png"
	videoURL = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	// Shopify stores YouTube links in this form.
	videoOrigin = "https://youtu.be/dQw4w9WgXcQ"
)

var gidRe = regexp.MustCompile(`^gid://shopify/(MediaImage|GenericFile|ExternalVideo|Video|Model3d)/\d+$`)

var expectIdentity = statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(gidRe)})

func testFilename() string {
	return "tf-acc-file-" + strings.ToLower(sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlphaNum)) + ".png"
}

// accSetup skips without TF_ACC, because these tests call the API before resource.Test would skip them.
func accSetup(t *testing.T) {
	t.Helper()
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skip("Acceptance tests skipped unless env 'TF_ACC' set")
	}
	acctest.PreCheck(t)
}

// liveFile is the part of a File the tests look at; the client returns raw JSON.
type liveFile struct {
	Typename   string  `json:"__typename"`
	ID         string  `json:"id"`
	Alt        *string `json:"alt"`
	FileStatus string  `json:"fileStatus"`
	Image      *struct {
		URL string `json:"url"`
	} `json:"image"`
	URL       *string `json:"url"`
	OriginURL string  `json:"originUrl"`
}

func (f *liveFile) publicURL() string {
	switch {
	case f.Image != nil:
		return f.Image.URL
	case f.URL != nil:
		return *f.URL
	}
	return f.OriginURL
}

func (f *liveFile) filename() string {
	u := strings.SplitN(f.publicURL(), "?", 2)[0]
	return u[strings.LastIndex(u, "/")+1:]
}

func decode(t *testing.T, raw json.RawMessage) []liveFile {
	t.Helper()
	var out []liveFile
	if string(raw) != "null" {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func search(t *testing.T, query string) []liveFile {
	t.Helper()
	var out []liveFile
	var after *string
	for {
		res, err := client.ListFiles(context.Background(), acctest.Client(t), after, &query)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, decode(t, res.Files.Nodes)...)
		if !res.Files.PageInfo.HasNextPage {
			return out
		}
		after = res.Files.PageInfo.EndCursor
	}
}

// lookup returns nil when no file has the filename.
func lookup(t *testing.T, filename string) *liveFile {
	t.Helper()
	for _, f := range search(t, fmt.Sprintf("filename:%q", filename)) {
		if f.filename() == filename {
			return &f
		}
	}
	return nil
}

func byID(t *testing.T, id string) *liveFile {
	t.Helper()
	res, err := client.GetFile(context.Background(), acctest.Client(t), id)
	if err != nil {
		t.Fatal(err)
	}
	var f liveFile
	if string(res.Node) == "null" {
		return nil
	}
	if err := json.Unmarshal(res.Node, &f); err != nil {
		t.Fatal(err)
	}
	if f.ID == "" {
		return nil
	}
	return &f
}

// waitSearchable waits for the search index, which lags a few seconds behind new files.
func waitSearchable(t *testing.T, filename string) {
	t.Helper()
	for range 40 {
		if lookup(t, filename) != nil {
			return
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("file %q was not searchable after 2 minutes", filename)
}

// mustCreate makes a file through the client, outside Terraform, and waits until it is ready and searchable.
func mustCreate(t *testing.T, filename, alt string) string {
	t.Helper()
	id := startCreate(t, filename, alt)
	for range 30 {
		if f := byID(t, id); f != nil && f.FileStatus == "READY" {
			waitSearchable(t, filename)
			return id
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("file %q was not ready", filename)
	return ""
}

func startCreate(t *testing.T, filename, alt string) string {
	t.Helper()
	ct := client.FileContentTypeImage
	mode := client.FileCreateInputDuplicateResolutionModeRaiseError
	res, err := client.CreateFile(context.Background(), acctest.Client(t), []client.FileCreateInput{{
		Filename: &filename, ContentType: &ct, Alt: &alt, DuplicateResolutionMode: &mode, OriginalSource: imageURL,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res.FileCreate == nil || len(res.FileCreate.UserErrors) > 0 {
		t.Fatalf("fileCreate: %+v", res.FileCreate)
	}
	files := decode(t, res.FileCreate.Files)
	if len(files) != 1 {
		t.Fatalf("fileCreate returned %d files", len(files))
	}
	return files[0].ID
}

func mustUpdateAlt(t *testing.T, id, alt string) {
	t.Helper()
	res, err := client.UpdateFile(context.Background(), acctest.Client(t), []client.FileUpdateInput{{Id: id, Alt: &alt}})
	if err != nil {
		t.Fatal(err)
	}
	if res.FileUpdate == nil || len(res.FileUpdate.UserErrors) > 0 {
		t.Fatalf("fileUpdate: %+v", res.FileUpdate)
	}
}

// deleteByID treats a file that is already gone as deleted.
func deleteByID(t *testing.T, id string) error {
	t.Helper()
	res, err := client.DeleteFile(context.Background(), acctest.Client(t), []string{id})
	if err != nil {
		return err
	}
	if res.FileDelete == nil {
		return fmt.Errorf("fileDelete %s returned no result", id)
	}
	for _, e := range res.FileDelete.UserErrors {
		if e.Code == nil || *e.Code != client.FilesErrorCodeFileDoesNotExist {
			return fmt.Errorf("fileDelete %s: %+v", id, res.FileDelete.UserErrors)
		}
	}
	return nil
}

// tracker remembers the GIDs of a test's files, because the search can't find files created moments ago.
type tracker struct {
	mu   sync.Mutex
	ids  map[string]bool
	last string
}

func (tr *tracker) add(id string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.ids[id] = true
	tr.last = id
}

// check records the ID of addr, so cleanup deletes it whatever the test does.
func (tr *tracker) check(name string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if rs := s.RootModule().Resources[name]; rs != nil && rs.Primary != nil {
			tr.add(rs.Primary.ID)
		}
		return nil
	}
}

// live returns the file of the last recorded ID.
func (tr *tracker) live(t *testing.T) *liveFile {
	t.Helper()
	f := byID(t, tr.last)
	if f == nil {
		t.Fatalf("file %s does not exist", tr.last)
	}
	return f
}

// gone fails, and removes the leftovers, when a tracked file or a file with a name still exists.
func (tr *tracker) gone(t *testing.T, names ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		var left []string
		for id := range tr.ids {
			if byID(t, id) != nil {
				left = append(left, id)
				_ = deleteByID(t, id)
			}
		}
		for _, n := range names {
			if f := lookup(t, n); f != nil {
				left = append(left, f.ID)
				_ = deleteByID(t, f.ID)
			}
		}
		if len(left) > 0 {
			return fmt.Errorf("files still exist: %v", left)
		}
		return nil
	}
}

// newTracker deletes the tracked files and the files with these names when the test ends.
func newTracker(t *testing.T, names ...string) *tracker {
	t.Helper()
	tr := &tracker{ids: map[string]bool{}}
	t.Cleanup(func() {
		for id := range tr.ids {
			if err := deleteByID(t, id); err != nil {
				t.Error(err)
			}
		}
		for _, n := range names {
			if f := lookup(t, n); f != nil {
				if err := deleteByID(t, f.ID); err != nil {
					t.Error(err)
				}
			}
		}
	})
	return tr
}

func str(s string) knownvalue.Check { return knownvalue.StringExact(s) }

func stateValue(path string, check knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(addr, tfjsonpath.New(path), check)
}

func config(body string) string {
	return fmt.Sprintf("resource \"shopify_file\" \"test\" {\n%s\n}\n", body)
}

func emptyPlan() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
}

func planAction(a plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, a)}}
}

func TestAccFile_validation(t *testing.T) {
	accSetup(t)
	cases := map[string]struct{ body, err string }{
		"local path":      {`content_type = "IMAGE"` + "\n" + `filename = "a.png"` + "\n" + `source = "./a.png"`, `local files`},
		"video source":    {`content_type = "VIDEO"` + "\n" + `filename = "a.mp4"` + "\n" + `source = "https://example.com/a.mp4"`, `staged upload`},
		"bad filename":    {`content_type = "FILE"` + "\n" + `filename = "TF Probe ä.png"` + "\n" + `source = "https://example.com/a"`, `ASCII letters`},
		"no filename":     {`content_type = "IMAGE"` + "\n" + `source = "https://example.com/a.png"`, `filename\s+is\s+required`},
		"video filename":  {`content_type = "EXTERNAL_VIDEO"` + "\n" + `filename = "a.mp4"` + "\n" + `source = "` + videoURL + `"`, `no\s+filename`},
		"hash only":       {`content_type = "IMAGE"` + "\n" + `filename = "a.png"` + "\n" + `source_hash = "x"`, `needs\s+a\s+source`},
		"wrong extension": {`content_type = "IMAGE"` + "\n" + `filename = "a.jpg"` + "\n" + `source = "` + imageURL + `"`, `must\s+match\s+the\s+source`},
		"no source":       {`content_type = "IMAGE"` + "\n" + `filename = "tf-acc-file-nosource.png"`, `without\s+a\s+source`},
		"video no source": {`content_type = "VIDEO"` + "\n" + `filename = "tf-acc-file-nosource.mp4"`, `staged upload`},
		"bad timeout":     {`content_type = "IMAGE"` + "\n" + `filename = "a.png"` + "\n" + `source = "https://example.com/a.png"` + "\n" + `timeouts {` + "\n" + `create = "soon"` + "\n" + `}`, `invalid\s+duration`},
		"unknown content": {`content_type = "AUDIO"` + "\n" + `filename = "a.png"`, `must\s+be\s+one\s+of`},
	}
	var steps []resource.TestStep
	for _, c := range cases {
		steps = append(steps, resource.TestStep{Config: config(c.body), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)` + c.err)})
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps:                    steps,
	})
}

func TestAccFile_basic(t *testing.T) {
	accSetup(t)
	name := testFilename()
	renamed := strings.Replace(name, "tf-acc-file-", "tf-acc-file-r-", 1)
	tr := newTracker(t, name, renamed)
	id, url := statecheck.CompareValue(compare.ValuesSame()), statecheck.CompareValue(compare.ValuesDiffer())
	body := func(filename, extra string) string {
		return config(fmt.Sprintf("  content_type = \"IMAGE\"\n  filename = %q\n  source = %q\n%s", filename, imageURL, extra))
	}
	created := body(name, "  alt = \"tf-acc alt\"")
	// alt unset: unmanaged, so out-of-band changes must not plan.
	unmanaged := body(renamed, "  source_hash = \"v2\"\n  allow_data_deletion = true")
	// An imported file has no source, so a config with one would plan a content update.
	imported := config(fmt.Sprintf("  content_type = \"IMAGE\"\n  filename = %q", renamed))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             tr.gone(t, name, renamed),
		Steps: []resource.TestStep{
			{
				Config: created,
				Check: resource.ComposeTestCheckFunc(tr.check(addr), func(*terraform.State) error {
					f := tr.live(t)
					if f.FileStatus != "READY" || f.Alt == nil || *f.Alt != "tf-acc alt" {
						return fmt.Errorf("live file = %+v, want READY with the alt", f)
					}
					return nil
				}),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("id", knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/MediaImage/\d+$`))),
					stateValue("content_type", str("IMAGE")),
					stateValue("filename", str(name)),
					stateValue("source", str(imageURL)),
					stateValue("alt", str("tf-acc alt")),
					stateValue("url", knownvalue.StringRegexp(regexp.MustCompile(`/files/`+regexp.QuoteMeta(name)+`\?v=`))),
					stateValue("adopt_existing", knownvalue.Bool(false)),
					stateValue("allow_data_deletion", knownvalue.Bool(false)),
					id.AddStateValue(addr, tfjsonpath.New("id")),
					url.AddStateValue(addr, tfjsonpath.New("url")),
				},
			},
			{Config: created, ConfigPlanChecks: emptyPlan()},
			{
				Config:           body(name, "  alt = \"tf-acc alt 2\""),
				ConfigPlanChecks: planAction(plancheck.ResourceActionUpdate),
				Check: func(*terraform.State) error {
					if f := tr.live(t); f.Alt == nil || *f.Alt != "tf-acc alt 2" {
						return fmt.Errorf("live alt = %v", f.Alt)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{id.AddStateValue(addr, tfjsonpath.New("id"))},
			},
			{
				// "" clears the alt text.
				Config: body(name, "  alt = \"\""),
				Check: func(*terraform.State) error {
					if f := tr.live(t); f.Alt != nil && *f.Alt != "" {
						return fmt.Errorf("live alt = %q, want empty", *f.Alt)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{stateValue("alt", str(""))},
			},
			{Config: body(name, "  alt = \"\""), ConfigPlanChecks: emptyPlan()},
			{
				// Rename: same GID, new URL.
				Config:           body(renamed, "  alt = \"\""),
				ConfigPlanChecks: planAction(plancheck.ResourceActionUpdate),
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("filename", str(renamed)),
					stateValue("url", knownvalue.StringRegexp(regexp.MustCompile(`/files/`+regexp.QuoteMeta(renamed)+`\?v=`))),
					id.AddStateValue(addr, tfjsonpath.New("id")),
					url.AddStateValue(addr, tfjsonpath.New("url")),
				},
			},
			{
				// Content change through source_hash, and alt unset now. Review Focus 2: alt set before, changed through the client in between.
				PreConfig:        func() { mustUpdateAlt(t, tr.last, "client alt") },
				Config:           unmanaged,
				ConfigPlanChecks: planAction(plancheck.ResourceActionUpdate),
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("alt", str("client alt")),
					id.AddStateValue(addr, tfjsonpath.New("id")),
					url.AddStateValue(addr, tfjsonpath.New("url")),
				},
			},
			{Config: unmanaged, ConfigPlanChecks: emptyPlan()},
			{RefreshState: true},
			{Config: imported, ResourceName: addr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				Config:                               imported,
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        renamed,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "filename",
				ImportStateVerifyIgnore:              []string{"adopt_existing", "allow_data_deletion", "source", "source_hash"},
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

// Pattern B: deleting or replacing a file destroys it and breaks its references, used or not.
func TestAccFile_dataDeletion(t *testing.T) {
	accSetup(t)
	name := testFilename()
	tr := newTracker(t, name)
	body := func(contentType, allow string) string {
		return config(fmt.Sprintf("  content_type = %q\n  filename = %q\n  source = %q\n  allow_data_deletion = %s", contentType, name, imageURL, allow))
	}
	var firstID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             tr.gone(t, name),
		Steps: []resource.TestStep{
			{Config: body("IMAGE", "false"), Check: tr.check(addr), ConfigStateChecks: []statecheck.StateCheck{stateValue("allow_data_deletion", knownvalue.Bool(false))}},
			{Config: body("IMAGE", "false"), Destroy: true, ExpectError: regexp.MustCompile(`(?s)would delete its data.*allow_data_deletion = true`)},
			{
				// A replace is a delete: refused while allow_data_deletion is false in state, and the file stays.
				Config:           body("FILE", "false"),
				ConfigPlanChecks: planAction(plancheck.ResourceActionDestroyBeforeCreate),
				ExpectError:      regexp.MustCompile(`(?s)would delete its data`),
				PreConfig:        func() { firstID = tr.last },
			},
			{
				Config: body("IMAGE", "true"),
				Check: func(*terraform.State) error {
					if byID(t, firstID) == nil {
						return fmt.Errorf("file %s was deleted by the refused replace", firstID)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{stateValue("allow_data_deletion", knownvalue.Bool(true))},
			},
			{
				// Replace: a generic file gets a GenericFile GID, and the filename still comes from its URL.
				Config:           body("FILE", "true"),
				ConfigPlanChecks: planAction(plancheck.ResourceActionDestroyBeforeCreate),
				Check:            tr.check(addr),
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("id", knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/GenericFile/\d+$`))),
					stateValue("content_type", str("FILE")),
					stateValue("filename", str(name)),
				},
			},
			{Config: body("FILE", "true"), ConfigPlanChecks: emptyPlan()},
			{
				// The replace made a new file, which the filename search finds only seconds later.
				PreConfig:                            func() { waitSearchable(t, name) },
				Config:                               body("FILE", "true"),
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        name,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "filename",
				ImportStateVerifyIgnore:              []string{"adopt_existing", "source", "source_hash", "allow_data_deletion"},
			},
		},
	})
}

// A different extension (here only in case, since the source fixes the extension) can't be changed in place, so it plans a replace, which Pattern B refuses without allow_data_deletion.
func TestAccFile_extensionReplaces(t *testing.T) {
	accSetup(t)
	name := testFilename()
	renamed := strings.TrimSuffix(name, ".png") + ".PNG"
	tr := newTracker(t, name, renamed)
	body := func(filename, allow string) string {
		return config(fmt.Sprintf("  content_type = \"IMAGE\"\n  filename = %q\n  source = %q\n  allow_data_deletion = %s", filename, imageURL, allow))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             tr.gone(t, name, renamed),
		Steps: []resource.TestStep{
			{Config: body(name, "false"), Check: tr.check(addr)},
			{
				Config:           body(renamed, "false"),
				ConfigPlanChecks: planAction(plancheck.ResourceActionDestroyBeforeCreate),
				ExpectError:      regexp.MustCompile(`(?s)would delete its data`),
			},
			{
				// The create half of a replace is planned again with no prior state, so a replace without a source fails before the delete.
				Config:      config(fmt.Sprintf("  content_type = \"IMAGE\"\n  filename = %q\n  allow_data_deletion = true", renamed)),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)without\s+a\s+source`),
			},
			{
				Config: body(name, "true"),
				Check: func(*terraform.State) error {
					if tr.live(t).FileStatus != "READY" {
						return fmt.Errorf("the file did not survive the refused replace")
					}
					return nil
				},
			},
		},
	})
}

// Pattern A: an existing file is only taken over with adopt_existing, and destroy leaves it as Terraform left it.
func TestAccFile_adopt(t *testing.T) {
	accSetup(t)
	name := testFilename()
	tr := newTracker(t, name)
	id := mustCreate(t, name, "client alt")
	tr.add(id)
	before := byID(t, id)
	body := func(adopt bool, alt string) string {
		return config(fmt.Sprintf("  content_type = \"IMAGE\"\n  filename = %q\n  adopt_existing = %t\n%s", name, adopt, alt))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Destroy only drops state, so the file is still there with Terraform's alt text and its original content.
		CheckDestroy: func(*terraform.State) error {
			f := byID(t, id)
			switch {
			case f == nil:
				return fmt.Errorf("adopted file %s was deleted", name)
			case f.Alt == nil || *f.Alt != "terraform alt" || f.publicURL() != before.publicURL():
				return fmt.Errorf("adopted file changed on destroy: %+v", f)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: body(false, "  source = \""+imageURL+"\""), ExpectError: regexp.MustCompile(`Failed to create file`)},
			{
				Config: body(true, "  alt = \"terraform alt\""),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("id", str(id)),
					stateValue("alt", str("terraform alt")),
					stateValue("adopt_existing", knownvalue.Bool(true)),
					stateValue("url", str(before.publicURL())),
				},
			},
			{Config: body(true, "  alt = \"terraform alt\""), ConfigPlanChecks: emptyPlan()},
		},
	})
}

// A file that fails processing is deleted again, so its filename isn't blocked.
func TestAccFile_failedProcessing(t *testing.T) {
	accSetup(t)
	name := testFilename()
	tr := newTracker(t, name)
	// A failed file has no URL, so no filename to search by: compare the whole library instead.
	before := map[string]bool{}
	for _, f := range search(t, "") {
		before[f.ID] = true
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			var left []string
			for _, f := range search(t, "") {
				if !before[f.ID] {
					left = append(left, f.ID)
					tr.add(f.ID)
				}
			}
			if len(left) > 0 {
				return fmt.Errorf("the failed file was not deleted: %v", left)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config:      config(fmt.Sprintf("  content_type = \"IMAGE\"\n  filename = %q\n  source = \"https://upload.wikimedia.org/wikipedia/commons/c/ca/tf-acc-does-not-exist.png\"", name)),
			ExpectError: regexp.MustCompile(`(?s)Shopify could not process the file.*404`),
		}},
	})
}

// A create that outlives its timeout keeps the file: state has its ID, and the next plan replaces the tainted resource.
func TestAccFile_timeout(t *testing.T) {
	accSetup(t)
	name := testFilename()
	tr := newTracker(t, name)
	body := func(timeouts string) string {
		return config(fmt.Sprintf("  content_type = \"IMAGE\"\n  filename = %q\n  source = %q\n  allow_data_deletion = true\n%s", name, imageURL, timeouts))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             tr.gone(t, name),
		Steps: []resource.TestStep{
			{Config: body("  timeouts {\n    create = \"1ns\"\n  }"), ExpectError: regexp.MustCompile(`(?s)still processing|The file is not ready`)},
			{
				// A file deleted before the search index has it keeps its name taken for longer, so the replace waits for the index.
				PreConfig:        func() { waitSearchable(t, name) },
				Config:           body(""),
				ConfigPlanChecks: planAction(plancheck.ResourceActionDestroyBeforeCreate),
				Check:            tr.check(addr),
			},
		},
	})
}

func TestAccFile_externalVideo(t *testing.T) {
	accSetup(t)
	tr := newTracker(t)
	// External videos have no filename to sweep by, and a failed apply is not tracked.
	t.Cleanup(func() {
		for _, f := range search(t, "media_type:EXTERNAL_VIDEO") {
			if f.OriginURL == videoOrigin {
				if err := deleteByID(t, f.ID); err != nil {
					t.Error(err)
				}
			}
		}
	})
	body := func(alt string) string {
		return config(fmt.Sprintf("  content_type = \"EXTERNAL_VIDEO\"\n  source = %q\n  allow_data_deletion = true\n%s", videoURL, alt))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             tr.gone(t),
		Steps: []resource.TestStep{
			{
				Config: body(""),
				Check:  tr.check(addr),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("id", knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/ExternalVideo/\d+$`))),
					stateValue("filename", knownvalue.Null()),
					// The configured form stays, though Shopify rewrote it.
					stateValue("source", str(videoURL)),
					stateValue("url", str(videoOrigin)),
				},
			},
			{Config: body(""), ConfigPlanChecks: emptyPlan()},
			{Config: body("  alt = \"tf-acc video\""), ConfigPlanChecks: planAction(plancheck.ResourceActionUpdate), ConfigStateChecks: []statecheck.StateCheck{stateValue("alt", str("tf-acc video"))}},
			{
				// Another resource with the same URL would create a second video, so it needs adopt_existing.
				Config: body("  alt = \"tf-acc video\"") + "\n" + `resource "shopify_file" "dup" {
  content_type = "EXTERNAL_VIDEO"
  source       = "` + videoOrigin + `"
  depends_on   = [shopify_file.test]
}
`,
				ExpectError: regexp.MustCompile(`File already exists`),
			},
			{Config: body("  alt = \"tf-acc video\""), ResourceName: addr, ImportState: true, ImportStateId: videoOrigin, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"adopt_existing", "allow_data_deletion", "source"}},
		},
	})
}

// A file deleted outside Terraform is planned for recreation.
func TestAccFile_deletedOutside(t *testing.T) {
	accSetup(t)
	name := testFilename()
	tr := newTracker(t, name)
	cfg := config(fmt.Sprintf("  content_type = \"IMAGE\"\n  filename = %q\n  source = %q\n  allow_data_deletion = true", name, imageURL))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             tr.gone(t, name),
		Steps: []resource.TestStep{
			{Config: cfg, Check: tr.check(addr)},
			{
				PreConfig: func() {
					waitSearchable(t, name)
					if err := deleteByID(t, tr.last); err != nil {
						t.Fatal(err)
					}
				},
				Config:           cfg,
				ConfigPlanChecks: planAction(plancheck.ResourceActionCreate),
				Check:            tr.check(addr),
			},
		},
	})
}
