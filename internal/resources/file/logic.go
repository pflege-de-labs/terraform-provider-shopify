package file

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const (
	typeImage         = "IMAGE"
	typeFile          = "FILE"
	typeVideo         = "VIDEO"
	typeExternalVideo = "EXTERNAL_VIDEO"
	typeModel3d       = "MODEL_3D"
)

type urlRef struct {
	Url string `json:"url"`
}

type fileError struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Details *string `json:"details"`
}

// apiFile is the union of the five File types; each type fills only its own fields.
type apiFile struct {
	Typename   string      `json:"__typename"`
	Id         string      `json:"id"`
	Alt        *string     `json:"alt"`
	FileStatus string      `json:"fileStatus"`
	FileErrors []fileError `json:"fileErrors"`
	Image      *urlRef     `json:"image"`
	Url        *string     `json:"url"`
	Filename   *string     `json:"filename"`
	Sources    []urlRef    `json:"sources"`
	OriginUrl  string      `json:"originUrl"`
}

func (f *apiFile) contentType() (string, bool) {
	switch f.Typename {
	case "MediaImage":
		return typeImage, true
	case "GenericFile":
		return typeFile, true
	case "Video":
		return typeVideo, true
	case "ExternalVideo":
		return typeExternalVideo, true
	case "Model3d":
		return typeModel3d, true
	}
	return "", false
}

// filename is what Shopify calls the file; images and generic files have no such field, so it comes from the URL.
func (f *apiFile) filename() (string, bool) {
	if f.Filename != nil && *f.Filename != "" {
		return *f.Filename, true
	}
	if u := f.publicURL(); u != nil && f.Typename != "ExternalVideo" {
		return filenameFromURL(*u)
	}
	return "", false
}

// publicURL is nil while a file is still processing.
// TODO(verify): that sources[0].url of a Video or Model3d is the public URL.
func (f *apiFile) publicURL() *string {
	switch f.Typename {
	case "MediaImage":
		if f.Image != nil && f.Image.Url != "" {
			return &f.Image.Url
		}
	case "GenericFile":
		if f.Url != nil && *f.Url != "" {
			return f.Url
		}
	case "Video", "Model3d":
		if len(f.Sources) > 0 && f.Sources[0].Url != "" {
			return &f.Sources[0].Url
		}
	case "ExternalVideo":
		if f.OriginUrl != "" {
			return &f.OriginUrl
		}
	}
	return nil
}

// filenameFromURL takes the last path segment without the query.
func filenameFromURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	name := u.Path[strings.LastIndex(u.Path, "/")+1:]
	return name, name != ""
}

// decodeFile returns nil for a null node and for a node that is not a file.
func decodeFile(raw json.RawMessage) (*apiFile, error) {
	var f apiFile
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	if _, ok := f.contentType(); !ok {
		return nil, nil
	}
	return &f, nil
}

func decodeFiles(raw json.RawMessage) ([]apiFile, error) {
	var all []apiFile
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(f apiFile) bool { _, ok := f.contentType(); return !ok }), nil
}

// key is the natural key: a filename, or the URL of an external video.
type key struct {
	filename    string
	externalURL string
}

// parseImportKey tells a URL from a filename: filenames never contain "://".
func parseImportKey(s string) key {
	if strings.Contains(s, "://") {
		return key{externalURL: s}
	}
	return key{filename: s}
}

// findQuery is the files(query:) filter for a key; the search is token-based, so matchFiles compares exactly.
func findQuery(k key) string {
	if k.externalURL != "" {
		return "media_type:EXTERNAL_VIDEO"
	}
	return "filename:" + strconv.Quote(k.filename)
}

// matchFiles keeps the exact matches; Shopify treats names that differ only in case as taken, but keeps the case it was given.
func matchFiles(files []apiFile, k key) []apiFile {
	var out []apiFile
	for _, f := range files {
		if k.externalURL != "" {
			if f.Typename == "ExternalVideo" && f.OriginUrl == k.externalURL {
				out = append(out, f)
			}
		} else if name, ok := f.filename(); ok && name == k.filename {
			out = append(out, f)
		}
	}
	return out
}

var filenameRe = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]*\.[A-Za-z0-9]+$`)

// validateFilename rejects what Shopify rewrites, which would plan forever.
func validateFilename(name string) error {
	if !filenameRe.MatchString(name) {
		return fmt.Errorf("%q must consist of ASCII letters, digits, '.', '_' and '-' and end in an extension, because Shopify rewrites anything else", name)
	}
	return nil
}

// checkSource accepts what this provider can send: an http(s) URL, and only for types Shopify fetches from a URL.
func checkSource(contentType, source string) error {
	if contentType == typeVideo || contentType == typeModel3d {
		return errors.New("videos and 3D models need a staged upload, which this provider does not support yet (roadmap). " +
			"They can be imported or adopted with adopt_existing")
	}
	u, err := url.Parse(source)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("only http:// and https:// URLs are supported; uploads of local files are not supported yet (roadmap), so serve the file from a URL")
	}
	if u.Host == "" {
		return errors.New("the URL has no host")
	}
	return nil
}

// extensionMismatch applies Shopify's rule (MISMATCHED_FILENAME_AND_ORIGINAL_SOURCE) before anything is deleted or uploaded; a URL without an extension is not checked.
func extensionMismatch(filename, source string) error {
	u, err := url.Parse(source)
	if err != nil {
		return err
	}
	base := u.Path[strings.LastIndex(u.Path, "/")+1:]
	i := strings.LastIndex(base, ".")
	if i < 0 {
		return nil
	}
	if want := base[i:]; !strings.EqualFold(want, extension(filename)) {
		return fmt.Errorf("the extension of %q must match the source URL, which ends in %q: Shopify refuses the file otherwise", filename, want)
	}
	return nil
}

type configError struct {
	attr   string
	detail string
}

// configErrors checks attribute combinations that depend on content_type; unknown values are skipped.
func configErrors(m *model) []configError {
	var errs []configError
	if m.ContentType.IsUnknown() || m.ContentType.IsNull() {
		return nil
	}
	ct := m.ContentType.ValueString()
	switch {
	case m.Filename.IsUnknown():
	case ct == typeExternalVideo && !m.Filename.IsNull():
		errs = append(errs, configError{"filename", "External videos have no filename; leave it unset."})
	case ct != typeExternalVideo && m.Filename.IsNull():
		errs = append(errs, configError{"filename", "The filename is required: it is the key of the file."})
	case ct != typeExternalVideo:
		if err := validateFilename(m.Filename.ValueString()); err != nil {
			errs = append(errs, configError{"filename", err.Error()})
		}
	}
	switch {
	case m.Source.IsUnknown():
	case m.Source.IsNull():
		if ct == typeExternalVideo {
			errs = append(errs, configError{"source", "The video URL is required: it is the key of an external video."})
		}
		if !m.SourceHash.IsNull() && !m.SourceHash.IsUnknown() {
			errs = append(errs, configError{"source_hash", "source_hash needs a source."})
		}
	default:
		if err := checkSource(ct, m.Source.ValueString()); err != nil {
			errs = append(errs, configError{"source", err.Error()})
		} else if ct != typeExternalVideo && !m.Filename.IsNull() && !m.Filename.IsUnknown() && validateFilename(m.Filename.ValueString()) == nil {
			if err := extensionMismatch(m.Filename.ValueString(), m.Source.ValueString()); err != nil {
				errs = append(errs, configError{"filename", err.Error()})
			}
		}
	}
	return errs
}

func differs(prior, plan types.String) bool { return !plan.IsNull() && !plan.Equal(prior) }

// contentChanged reports a new source or hash; unset means unmanaged, so it is no change.
func contentChanged(prior, plan *model) bool {
	return differs(prior.Source, plan.Source) || differs(prior.SourceHash, plan.SourceHash)
}

func extension(name string) string { return name[strings.LastIndex(name, "."):] }

// replaceReasons lists why the plan replaces the file: the API can't change these in place, and an extension that differs only in case counts.
func replaceReasons(prior, plan *model) []string {
	var out []string
	if !plan.ContentType.IsUnknown() && !plan.ContentType.Equal(prior.ContentType) {
		out = append(out, "content_type")
	}
	known := func(v types.String) bool {
		return !v.IsNull() && !v.IsUnknown() && strings.Contains(v.ValueString(), ".")
	}
	if known(prior.Filename) && known(plan.Filename) && extension(prior.Filename.ValueString()) != extension(plan.Filename.ValueString()) {
		out = append(out, "filename extension")
	}
	if ct := plan.ContentType.ValueString(); ct != typeImage && ct != typeFile {
		if differs(prior.Source, plan.Source) {
			out = append(out, "source")
		}
		if differs(prior.SourceHash, plan.SourceHash) {
			out = append(out, "source_hash")
		}
	}
	return out
}

// errorPath maps a userError field such as ["files","0","filename"] to an attribute.
func errorPath(field []string) (path.Path, bool) {
	if len(field) < 3 || field[0] != "files" {
		return path.Empty(), false
	}
	if a, ok := map[string]string{"filename": "filename", "originalSource": "source", "alt": "alt", "contentType": "content_type"}[field[2]]; ok {
		return path.Root(a), true
	}
	return path.Empty(), false
}

// failedError is a file Shopify could not process.
type failedError struct{ file *apiFile }

func (e *failedError) Error() string {
	parts := make([]string, len(e.file.FileErrors))
	for i, fe := range e.file.FileErrors {
		parts[i] = fe.String()
	}
	return fmt.Sprintf("Shopify could not process file %s: %s", e.file.Id, strings.Join(parts, "; "))
}

func (fe fileError) String() string {
	s := fe.Code + ": " + fe.Message
	if fe.Details != nil && *fe.Details != "" {
		s += " (" + *fe.Details + ")"
	}
	return s
}

type timeoutError struct {
	file  *apiFile
	after time.Duration
}

func (e *timeoutError) Error() string {
	return fmt.Sprintf("file %s is still processing after %s; it was kept", e.file.Id, e.after)
}

// poller waits for processing; fetch, sleep and now are injected so the loop is testable.
type poller struct {
	fetch    func(context.Context) (*apiFile, error)
	sleep    func(context.Context, time.Duration) error
	now      func() time.Time
	interval time.Duration
}

// wait returns the READY file. It returns the last file seen with a *failedError or *timeoutError.
func (p poller) wait(ctx context.Context, timeout time.Duration) (*apiFile, error) {
	deadline := p.now().Add(timeout)
	for {
		f, err := p.fetch(ctx)
		if err != nil {
			return nil, err
		}
		if f == nil {
			return nil, errors.New("the file no longer exists")
		}
		switch f.FileStatus {
		case "READY":
			return f, nil
		case "FAILED":
			return f, &failedError{f}
		}
		if !p.now().Before(deadline) {
			return f, &timeoutError{f, timeout}
		}
		if err := p.sleep(ctx, p.interval); err != nil {
			return f, err
		}
	}
}

// decodeOne takes the single file of a mutation payload.
func decodeOne(raw json.RawMessage) (*apiFile, error) {
	fs, err := decodeFiles(raw)
	if err != nil {
		return nil, err
	}
	if len(fs) != 1 {
		return nil, fmt.Errorf("expected 1 file in the response, got %d", len(fs))
	}
	return &fs[0], nil
}

// gone reports whether fileDelete failed only because the file no longer exists.
func gone[E any, P interface {
	*E
	userError
}](errs []E) bool {
	for i := range errs {
		if c := P(&errs[i]).GetCode(); c == nil || *c != client.FilesErrorCodeFileDoesNotExist {
			return false
		}
	}
	return len(errs) > 0
}

// userError is the shape of the userErrors the file mutations return.
type userError interface {
	client.UserError
	GetCode() *client.FilesErrorCode
}

// retryHints tell the user to apply again for errors that clear by themselves.
var retryHints = map[client.FilesErrorCode]string{
	client.FilesErrorCodeFileLocked:    "Another operation is still running on the file; apply again in a moment.",
	client.FilesErrorCodeNonReadyState: "The file is not ready yet; apply again in a moment.",
}

// userErrorDiags maps userErrors to attributes; hints add advice per error code.
func userErrorDiags[E any, P interface {
	*E
	userError
}](summary string, errs []E, hints map[client.FilesErrorCode]string) diag.Diagnostics {
	var diags diag.Diagnostics
	for i := range errs {
		e := P(&errs[i])
		detail := e.GetMessage()
		if c := e.GetCode(); c != nil {
			if h, ok := hints[*c]; ok {
				detail += " " + h
			} else if h, ok := retryHints[*c]; ok {
				detail += " " + h
			}
		}
		if p, ok := errorPath(e.GetField()); ok {
			diags.AddAttributeError(p, summary, detail)
		} else {
			diags.AddError(summary, detail)
		}
	}
	return diags
}
