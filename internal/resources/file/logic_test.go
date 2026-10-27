package file

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

func TestValidateFilename(t *testing.T) {
	for name, wantErr := range map[string]bool{
		"logo.png": false, "my-logo_2.final.png": false, "A.JPG": false,
		"": true, "logo": true, "logo.": true, ".png": true, "TF Probe ä.png": true, "a/b.png": true, "a b.png": true, "ä.png": true,
	} {
		if err := validateFilename(name); (err != nil) != wantErr {
			t.Errorf("validateFilename(%q) = %v, want error: %t", name, err, wantErr)
		}
	}
}

func TestFilenameFromURL(t *testing.T) {
	cases := []struct {
		url, want string
		ok        bool
	}{
		{"https://cdn.shopify.com/s/files/1/0/1/files/tf-probe.png?v=1700000000", "tf-probe.png", true},
		{"https://cdn.shopify.com/s/files/1/0/1/files/doc.pdf", "doc.pdf", true},
		{"https://cdn.shopify.com/s/files/1/0/1/files/", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got, ok := filenameFromURL(c.url); got != c.want || ok != c.ok {
			t.Errorf("filenameFromURL(%q) = %q, %t; want %q, %t", c.url, got, ok, c.want, c.ok)
		}
	}
}

func TestParseImportKey(t *testing.T) {
	if k := parseImportKey("logo.png"); k.filename != "logo.png" || k.externalURL != "" {
		t.Errorf("filename key: %+v", k)
	}
	if k := parseImportKey("https://www.youtube.com/watch?v=x"); k.externalURL != "https://www.youtube.com/watch?v=x" || k.filename != "" {
		t.Errorf("url key: %+v", k)
	}
}

func TestCheckSource(t *testing.T) {
	cases := []struct {
		contentType, source, wantErr string
	}{
		{"IMAGE", "https://example.com/a.png", ""},
		{"FILE", "http://example.com/a.pdf", ""},
		{"EXTERNAL_VIDEO", "https://www.youtube.com/watch?v=x", ""},
		{"IMAGE", "./assets/logo.png", "not supported yet"},
		{"IMAGE", "/abs/logo.png", "not supported yet"},
		{"EXTERNAL_VIDEO", "assets/video.mp4", "not supported yet"},
		{"IMAGE", "https://", "host"},
		{"VIDEO", "https://example.com/a.mp4", "staged upload"},
		{"MODEL_3D", "https://example.com/a.glb", "staged upload"},
	}
	for _, c := range cases {
		err := checkSource(c.contentType, c.source)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("checkSource(%s, %q) = %v, want ok", c.contentType, c.source, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("checkSource(%s, %q) = %v, want %q", c.contentType, c.source, err, c.wantErr)
		}
	}
}

func TestDecodeFile(t *testing.T) {
	img := `{"__typename":"MediaImage","id":"gid://shopify/MediaImage/1","alt":null,"fileStatus":"READY","fileErrors":[],"image":{"url":"https://cdn.shopify.com/s/files/1/files/a.png?v=1"}}`
	f, err := decodeFile([]byte(img))
	if err != nil || f == nil {
		t.Fatalf("decode: %v, %v", f, err)
	}
	if ct, _ := f.contentType(); ct != "IMAGE" {
		t.Errorf("contentType = %q", ct)
	}
	if name, ok := f.filename(); !ok || name != "a.png" {
		t.Errorf("filename = %q, %t", name, ok)
	}
	if u := f.publicURL(); u == nil || !strings.HasSuffix(*u, "a.png?v=1") {
		t.Errorf("url = %v", u)
	}

	video := `{"__typename":"Video","id":"gid://shopify/Video/1","filename":"v.mp4","fileStatus":"READY","fileErrors":[],"sources":[{"url":"https://cdn/v.mp4"}]}`
	if f, _ = decodeFile([]byte(video)); f == nil {
		t.Fatal("video not decoded")
	} else if name, _ := f.filename(); name != "v.mp4" || *f.publicURL() != "https://cdn/v.mp4" {
		t.Errorf("video: %q, %v", name, f.publicURL())
	}

	ext := `{"__typename":"ExternalVideo","id":"gid://shopify/ExternalVideo/1","fileStatus":"READY","fileErrors":[],"originUrl":"https://youtu.be/x"}`
	if f, _ = decodeFile([]byte(ext)); f == nil {
		t.Fatal("external video not decoded")
	} else if _, ok := f.filename(); ok || *f.publicURL() != "https://youtu.be/x" {
		t.Errorf("external: %v", f.publicURL())
	}

	// A processing image has no URL yet.
	proc := `{"__typename":"MediaImage","id":"gid://shopify/MediaImage/2","fileStatus":"UPLOADED","fileErrors":[],"image":null}`
	if f, _ = decodeFile([]byte(proc)); f == nil {
		t.Fatal("processing image not decoded")
	} else if _, ok := f.filename(); ok || f.publicURL() != nil {
		t.Error("a processing image has no filename or URL")
	}

	for _, gone := range []string{`null`, `{}`, `{"__typename":"Product","id":"gid://shopify/Product/1"}`} {
		if f, err := decodeFile([]byte(gone)); f != nil || err != nil {
			t.Errorf("decodeFile(%s) = %v, %v; want nil, nil", gone, f, err)
		}
	}
}

func TestDecodeFiles(t *testing.T) {
	fs, err := decodeFiles([]byte(`[{"__typename":"GenericFile","id":"g1","url":"https://cdn/x/doc.pdf?v=2","fileStatus":"READY"},{"__typename":"Other","id":"o"}]`))
	if err != nil || len(fs) != 1 || fs[0].Id != "g1" {
		t.Fatalf("got %v, %v", fs, err)
	}
	if fs, err = decodeFiles([]byte(`null`)); err != nil || len(fs) != 0 {
		t.Errorf("null: %v, %v", fs, err)
	}
}

func TestMatchFiles(t *testing.T) {
	mk := func(id, url string) apiFile {
		return apiFile{Typename: "MediaImage", Id: id, Image: &urlRef{url}}
	}
	files := []apiFile{
		mk("1", "https://cdn/files/logo-2.png?v=1"),
		mk("2", "https://cdn/files/logo.png?v=1"),
		mk("3", "https://cdn/files/Logo.png?v=1"),
		{Typename: "ExternalVideo", Id: "4", OriginUrl: "https://youtu.be/x"},
		{Typename: "ExternalVideo", Id: "5", OriginUrl: "https://youtu.be/y"},
	}
	ids := func(fs []apiFile) []string {
		var out []string
		for _, f := range fs {
			out = append(out, f.Id)
		}
		return out
	}
	if got := ids(matchFiles(files, key{filename: "logo.png"})); !slices.Equal(got, []string{"2"}) {
		t.Errorf("filename match = %v, want exact and case-sensitive", got)
	}
	if got := ids(matchFiles(files, key{externalURL: "https://youtu.be/x"})); !slices.Equal(got, []string{"4"}) {
		t.Errorf("url match = %v", got)
	}
	if got := matchFiles(files, key{filename: "nope.png"}); len(got) != 0 {
		t.Errorf("no match = %v", got)
	}
}

func TestFindQuery(t *testing.T) {
	if got := findQuery(key{filename: "logo.png"}); got != `filename:"logo.png"` {
		t.Errorf("filename query = %s", got)
	}
	if got := findQuery(key{externalURL: "https://youtu.be/x"}); got != "media_type:EXTERNAL_VIDEO" {
		t.Errorf("url query = %s", got)
	}
}

func TestErrorPath(t *testing.T) {
	for field, want := range map[string]string{
		"files.0.filename": "filename", "files.0.originalSource": "source", "files.0.alt": "alt", "files.0.contentType": "content_type",
	} {
		p, ok := errorPath(strings.Split(field, "."))
		if !ok || !p.Equal(path.Root(want)) {
			t.Errorf("errorPath(%s) = %v, %t; want %s", field, p, ok, want)
		}
	}
	for _, field := range [][]string{nil, {"files"}, {"files", "0"}, {"files", "0", "referencesToAdd"}, {"other", "0", "alt"}} {
		if _, ok := errorPath(field); ok {
			t.Errorf("errorPath(%v) should not map", field)
		}
	}
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func s(v string) types.String { return types.StringValue(v) }

func TestReplaceReasons(t *testing.T) {
	base := model{ContentType: s("IMAGE"), Filename: s("a.png"), Source: s("https://x/a.png"), SourceHash: s("h1")}
	with := func(f func(m *model)) *model {
		m := base
		f(&m)
		return &m
	}
	cases := []struct {
		name string
		plan *model
		want []string
	}{
		{"same", &base, nil},
		{"rename in place", with(func(m *model) { m.Filename = s("b.png") }), nil},
		{"alt only", with(func(m *model) { m.Alt = s("x") }), nil},
		{"image content change in place", with(func(m *model) { m.SourceHash = s("h2") }), nil},
		{"image new source in place", with(func(m *model) { m.Source = s("https://x/b.png") }), nil},
		{"extension replaces", with(func(m *model) { m.Filename = s("a.jpg") }), []string{"filename extension"}},
		{"extension case replaces", with(func(m *model) { m.Filename = s("a.PNG") }), []string{"filename extension"}},
		{"content type replaces", with(func(m *model) { m.ContentType = s("FILE") }), []string{"content_type"}},
		{"unknown filename is not a change", with(func(m *model) { m.Filename = types.StringUnknown() }), nil},
		{"hash set to null is not a content change", with(func(m *model) { m.SourceHash = types.StringNull() }), nil},
	}
	for _, c := range cases {
		if got := replaceReasons(&base, c.plan); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	// Videos, 3D models and external videos can't take new content in place.
	for _, ct := range []string{"VIDEO", "MODEL_3D", "EXTERNAL_VIDEO"} {
		prior := model{ContentType: s(ct), Source: s("https://x/v"), SourceHash: types.StringNull()}
		plan := prior
		plan.Source = s("https://x/w")
		if got := replaceReasons(&prior, &plan); !slices.Equal(got, []string{"source"}) {
			t.Errorf("%s source change: got %v", ct, got)
		}
		plan = prior
		plan.SourceHash = s("h")
		if got := replaceReasons(&prior, &plan); !slices.Equal(got, []string{"source_hash"}) {
			t.Errorf("%s hash change: got %v", ct, got)
		}
	}
}

func TestContentChanged(t *testing.T) {
	prior := model{Source: s("u"), SourceHash: s("h")}
	if contentChanged(&prior, &prior) {
		t.Error("same values changed")
	}
	if !contentChanged(&prior, &model{Source: s("u2"), SourceHash: s("h")}) || !contentChanged(&prior, &model{Source: s("u"), SourceHash: s("h2")}) {
		t.Error("changed values not noticed")
	}
	if !contentChanged(&model{Source: types.StringNull()}, &model{Source: s("u")}) {
		t.Error("adding a source to an imported file is a content change")
	}
	if contentChanged(&prior, &model{Source: types.StringNull(), SourceHash: types.StringNull()}) {
		t.Error("unset means unmanaged")
	}
}

// pollClock is a fake clock: sleeping advances it.
type pollClock struct{ t time.Time }

func (c *pollClock) now() time.Time { return c.t }
func (c *pollClock) sleep(_ context.Context, d time.Duration) error {
	c.t = c.t.Add(d)
	return nil
}

func scripted(statuses ...string) (func(context.Context) (*apiFile, error), *int) {
	calls := 0
	return func(context.Context) (*apiFile, error) {
		st := statuses[min(calls, len(statuses)-1)]
		calls++
		return &apiFile{Id: "gid", FileStatus: st}, nil
	}, &calls
}

func TestPollReady(t *testing.T) {
	clock := &pollClock{}
	fetch, calls := scripted("UPLOADED", "PROCESSING", "READY")
	p := poller{fetch: fetch, sleep: clock.sleep, now: clock.now, interval: 2 * time.Second}
	f, err := p.wait(context.Background(), time.Minute)
	if err != nil || f.FileStatus != "READY" || *calls != 3 {
		t.Fatalf("got %v, %v after %d calls", f, err, *calls)
	}
	if clock.t != (time.Time{}).Add(4*time.Second) {
		t.Errorf("waited %v, want 4s (every 2 s)", clock.t.Sub(time.Time{}))
	}
}

func TestPollFailed(t *testing.T) {
	clock := &pollClock{}
	p := poller{sleep: clock.sleep, now: clock.now, interval: 2 * time.Second, fetch: func(context.Context) (*apiFile, error) {
		d := "404"
		return &apiFile{Id: "gid", FileStatus: "FAILED", FileErrors: []fileError{{Code: "IMAGE_DOWNLOAD_FAILURE", Message: "download failed", Details: &d}}}, nil
	}}
	f, err := p.wait(context.Background(), time.Minute)
	var failed *failedError
	if !errors.As(err, &failed) || f == nil || failed.file.Id != "gid" {
		t.Fatalf("got %v, %v; want failedError", f, err)
	}
	for _, want := range []string{"IMAGE_DOWNLOAD_FAILURE", "download failed", "404"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestPollTimeout(t *testing.T) {
	clock := &pollClock{}
	fetch, calls := scripted("PROCESSING")
	p := poller{fetch: fetch, sleep: clock.sleep, now: clock.now, interval: 2 * time.Second}
	f, err := p.wait(context.Background(), 10*time.Second)
	var timeout *timeoutError
	if !errors.As(err, &timeout) || f == nil || timeout.file.Id != "gid" {
		t.Fatalf("got %v, %v; want timeoutError", f, err)
	}
	if *calls < 5 || *calls > 7 {
		t.Errorf("%d calls in 10 s at 2 s intervals", *calls)
	}
	if !strings.Contains(err.Error(), "still processing") {
		t.Errorf("error %q must say the file is still processing", err)
	}
}

func TestPollGoneAndFetchError(t *testing.T) {
	clock := &pollClock{}
	gone := poller{sleep: clock.sleep, now: clock.now, interval: time.Second, fetch: func(context.Context) (*apiFile, error) { return nil, nil }}
	if _, err := gone.wait(context.Background(), time.Minute); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("gone: %v", err)
	}
	boom := errors.New("boom")
	failing := poller{sleep: clock.sleep, now: clock.now, interval: time.Second, fetch: func(context.Context) (*apiFile, error) { return nil, boom }}
	if _, err := failing.wait(context.Background(), time.Minute); !errors.Is(err, boom) {
		t.Errorf("fetch error: %v", err)
	}
}

func TestCreateInput(t *testing.T) {
	cfg := model{ContentType: s("IMAGE"), Filename: s("a.png"), Source: s("https://x/a.png"), Alt: types.StringNull()}
	if got, want := asJSON(t, cfg.createInput()), `{"filename":"a.png","contentType":"IMAGE","duplicateResolutionMode":"RAISE_ERROR","originalSource":"https://x/a.png"}`; got != want {
		t.Errorf("image: got %s, want %s", got, want)
	}
	cfg.Alt = s("Logo")
	if got := asJSON(t, cfg.createInput()); !strings.Contains(got, `"alt":"Logo"`) {
		t.Errorf("alt not sent: %s", got)
	}
	ext := model{ContentType: s("EXTERNAL_VIDEO"), Filename: types.StringNull(), Source: s("https://youtu.be/x"), Alt: types.StringNull()}
	if got, want := asJSON(t, ext.createInput()), `{"contentType":"EXTERNAL_VIDEO","originalSource":"https://youtu.be/x"}`; got != want {
		t.Errorf("external: got %s, want %s", got, want)
	}
}

// Unset attributes are unmanaged, so the update only carries changes; "" clears alt and is sent.
func TestUpdateInput(t *testing.T) {
	prior := model{ContentType: s("IMAGE"), Filename: s("a.png"), Source: s("u"), SourceHash: s("h"), Alt: s("old")}
	same := prior
	cases := []struct {
		name string
		cfg  func(m *model)
		want string
		ok   bool
	}{
		{"nothing", func(m *model) {}, `{"id":"gid"}`, false},
		{"alt unset is unmanaged", func(m *model) { m.Alt = types.StringNull() }, `{"id":"gid"}`, false},
		{"alt cleared", func(m *model) { m.Alt = s("") }, `{"id":"gid","alt":""}`, true},
		{"rename", func(m *model) { m.Filename = s("b.png") }, `{"id":"gid","filename":"b.png"}`, true},
		{"hash change re-sends the source", func(m *model) { m.SourceHash = s("h2") }, `{"id":"gid","originalSource":"u"}`, true},
		{"new source", func(m *model) { m.Source = s("u2") }, `{"id":"gid","originalSource":"u2"}`, true},
	}
	for _, c := range cases {
		cfg := same
		c.cfg(&cfg)
		in, ok := cfg.updateInput("gid", &prior)
		if got := asJSON(t, in); got != c.want || ok != c.ok {
			t.Errorf("%s: got %s, %t; want %s, %t", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestSetFromAPI(t *testing.T) {
	alt := ""
	img := &apiFile{Typename: "MediaImage", Id: "gid://shopify/MediaImage/1", Alt: &alt, FileStatus: "READY", Image: &urlRef{"https://cdn/files/a.png?v=1"}}
	m := model{Source: s("https://x/a.png"), SourceHash: s("h"), Alt: s("")}
	setFromAPI(&m, img, nil)
	if m.ContentType.ValueString() != "IMAGE" || m.Filename.ValueString() != "a.png" || m.URL.ValueString() != "https://cdn/files/a.png?v=1" {
		t.Errorf("image: %+v", m)
	}
	if m.Source.ValueString() != "https://x/a.png" || m.SourceHash.ValueString() != "h" {
		t.Error("hosted files keep source and source_hash from state")
	}

	// A cleared alt reads back as null or "" and must keep the configured "".
	img.Alt = nil
	setFromAPI(&m, img, nil)
	if m.Alt.IsNull() || m.Alt.ValueString() != "" {
		t.Errorf("alt = %v, want the configured empty string kept", m.Alt)
	}
	m.Alt = types.StringNull()
	setFromAPI(&m, img, nil)
	if !m.Alt.IsNull() {
		t.Errorf("alt = %v, want null", m.Alt)
	}

	// An imported file has no source; a processing one has no URL, which keeps the known filename.
	imported := model{}
	setFromAPI(&imported, &apiFile{Typename: "GenericFile", Id: "g", FileStatus: "UPLOADED"}, nil)
	if !imported.Source.IsNull() || !imported.URL.IsNull() || !imported.Filename.IsNull() {
		t.Errorf("imported: %+v", imported)
	}
	known := model{Filename: s("keep.pdf")}
	setFromAPI(&known, &apiFile{Typename: "GenericFile", Id: "g", FileStatus: "UPLOADED"}, nil)
	if known.Filename.ValueString() != "keep.pdf" {
		t.Errorf("filename = %v, want the known one kept", known.Filename)
	}

	ext := model{}
	video := &apiFile{Typename: "ExternalVideo", Id: "e", OriginUrl: "https://youtu.be/x"}
	setFromAPI(&ext, video, nil)
	if ext.ContentType.ValueString() != "EXTERNAL_VIDEO" || ext.Source.ValueString() != "https://youtu.be/x" || !ext.Filename.IsNull() || ext.URL.ValueString() != "https://youtu.be/x" {
		t.Errorf("external: %+v", ext)
	}

	// Shopify rewrites video URLs: the configured form stays while the live URL is what it returned at our write.
	cfg := model{Source: s("https://www.youtube.com/watch?v=x")}
	written := "https://youtu.be/x"
	setFromAPI(&cfg, video, &written)
	if cfg.Source.ValueString() != "https://www.youtube.com/watch?v=x" || cfg.URL.ValueString() != "https://youtu.be/x" {
		t.Errorf("configured form not kept: %+v", cfg)
	}
	other := "https://youtu.be/other"
	setFromAPI(&cfg, video, &other)
	if cfg.Source.ValueString() != "https://youtu.be/x" {
		t.Errorf("a changed live URL must be taken: %+v", cfg)
	}
}

func TestConfigErrors(t *testing.T) {
	null, unknown := types.StringNull(), types.StringUnknown()
	attrs := func(m model) []string {
		var out []string
		for _, e := range configErrors(&m) {
			out = append(out, e.attr)
		}
		return out
	}
	cases := []struct {
		name string
		m    model
		want []string
	}{
		{"image", model{ContentType: s("IMAGE"), Filename: s("a.png"), Source: s("https://x/a.png"), SourceHash: null}, nil},
		{"image needs filename", model{ContentType: s("IMAGE"), Filename: null, Source: s("https://x/a.png"), SourceHash: null}, []string{"filename"}},
		{"bad filename", model{ContentType: s("FILE"), Filename: s("TF Probe ä.png"), Source: null, SourceHash: null}, []string{"filename"}},
		{"adopt without source", model{ContentType: s("IMAGE"), Filename: s("a.png"), Source: null, SourceHash: null}, nil},
		{"external video", model{ContentType: s("EXTERNAL_VIDEO"), Filename: null, Source: s("https://youtu.be/x"), SourceHash: null}, nil},
		{"external video with filename", model{ContentType: s("EXTERNAL_VIDEO"), Filename: s("v.mp4"), Source: s("https://youtu.be/x"), SourceHash: null}, []string{"filename"}},
		{"external video needs source", model{ContentType: s("EXTERNAL_VIDEO"), Filename: null, Source: null, SourceHash: null}, []string{"source"}},
		{"local path", model{ContentType: s("IMAGE"), Filename: s("a.png"), Source: s("./a.png"), SourceHash: null}, []string{"source"}},
		{"video source", model{ContentType: s("VIDEO"), Filename: s("v.mp4"), Source: s("https://x/v.mp4"), SourceHash: null}, []string{"source"}},
		{"video adopted", model{ContentType: s("VIDEO"), Filename: s("v.mp4"), Source: null, SourceHash: null}, nil},
		{"hash without source", model{ContentType: s("IMAGE"), Filename: s("a.png"), Source: null, SourceHash: s("h")}, []string{"source_hash"}},
		{"unknowns are skipped", model{ContentType: unknown, Filename: unknown, Source: unknown, SourceHash: unknown}, nil},
		{"unknown filename", model{ContentType: s("IMAGE"), Filename: unknown, Source: s("https://x/a.png"), SourceHash: null}, nil},
		{"extension differs from the source", model{ContentType: s("IMAGE"), Filename: s("a.jpg"), Source: s("https://x/a.png?v=1"), SourceHash: null}, []string{"filename"}},
		{"extension case differs only", model{ContentType: s("FILE"), Filename: s("a.PDF"), Source: s("https://x/doc.pdf"), SourceHash: null}, nil},
		{"source without extension", model{ContentType: s("FILE"), Filename: s("a.pdf"), Source: s("https://x/download?id=3"), SourceHash: null}, nil},
		{"unknown source", model{ContentType: s("IMAGE"), Filename: s("a.jpg"), Source: unknown, SourceHash: null}, nil},
	}
	for _, c := range cases {
		if got := attrs(c.m); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestExtensionMismatch(t *testing.T) {
	for _, c := range []struct {
		filename, source string
		wantErr          bool
	}{
		{"a.png", "https://x/dir/b.png", false},
		{"a.PNG", "https://x/b.png", false},
		{"a.jpg", "https://x/b.png", true},
		{"a.jpg", "https://x/b.png?v=1#f", true},
		{"a.jpg", "https://x/b", false},
		{"a.jpg", "https://x/dir.d/b", false},
		{"a.jpg", "https://x/", false},
	} {
		if err := extensionMismatch(c.filename, c.source); (err != nil) != c.wantErr {
			t.Errorf("extensionMismatch(%q, %q) = %v, want error: %t", c.filename, c.source, err, c.wantErr)
		}
	}
}

func TestFilterListable(t *testing.T) {
	mk := func(id, filename string) apiFile {
		return apiFile{Typename: "Video", Id: id, Filename: &filename}
	}
	files := []apiFile{
		mk("1", "no extension"),
		{Typename: "GenericFile", Id: "2", Url: new(string)},
		mk("3", "v a.mp4"),
		mk("4", "ok.mp4"),
		{Typename: "ExternalVideo", Id: "5", OriginUrl: "https://youtu.be/x"},
		mk("6", "also-ok.mp4"),
	}
	var ids []string
	for _, f := range filterListable(files) {
		ids = append(ids, f.Id)
	}
	if !slices.Equal(ids, []string{"4", "5", "6"}) {
		t.Errorf("listable = %v, want 4, 5, 6", ids)
	}
}

// Skipped files must not use up the list limit.
func TestListLimitCountsListedFilesOnly(t *testing.T) {
	bad := func(id string) apiFile { n := "bad name"; return apiFile{Typename: "Video", Id: id, Filename: &n} }
	good := func(id string) apiFile { n := id + ".mp4"; return apiFile{Typename: "Video", Id: id, Filename: &n} }
	pages := [][]apiFile{{bad("b1"), bad("b2"), good("g1")}, {bad("b3"), good("g2"), good("g3")}}
	page := func(after *string) ([]apiFile, *string, error) {
		i := 0
		if after != nil {
			i = 1
		}
		var next *string
		if i == 0 {
			next = new(string)
		}
		return filterListable(pages[i]), next, nil
	}
	var got []string
	err := patterns.Paginate(context.Background(), 2, page, func(f apiFile) bool { got = append(got, f.Id); return true })
	if err != nil || !slices.Equal(got, []string{"g1", "g2"}) {
		t.Errorf("got %v, %v; want g1, g2", got, err)
	}
}
