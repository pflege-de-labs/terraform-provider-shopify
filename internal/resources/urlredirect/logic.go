package urlredirect

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// errorPath maps a userError field, relative to `urlRedirect`, to an attribute.
func errorPath(field []string) (path.Path, bool) {
	if len(field) < 2 || field[0] != "urlRedirect" {
		return path.Empty(), false
	}
	switch field[1] {
	case "path", "target":
		return path.Root(field[1]), true
	}
	return path.Empty(), false
}

// searchQuery is the urlRedirects(query:) filter for a path; the caller still compares exactly.
func searchQuery(p string) string {
	return "path:" + strconv.Quote(p)
}

func matchPath(nodes []client.ListUrlRedirectsUrlRedirectsUrlRedirectConnectionNodesUrlRedirect, p string) *client.UrlRedirectFields {
	for i := range nodes {
		if nodes[i].Path == p {
			return &nodes[i].UrlRedirectFields
		}
	}
	return nil
}

// goneOnDelete reports whether urlRedirectDelete failed only because the redirect no longer exists.
func goneOnDelete(errs []client.DeleteUrlRedirectUrlRedirectDeleteUrlRedirectDeletePayloadUserErrorsUrlRedirectUserError) bool {
	for _, e := range errs {
		if e.Code == nil || *e.Code != client.UrlRedirectErrorCodeDoesNotExist {
			return false
		}
	}
	return len(errs) > 0
}

var lowerPercent = regexp.MustCompile(`%[0-9a-f]{2}`)

// printable reports whether s has only ASCII characters from "!" to "~"; Shopify percent-encodes anything else.
func printable(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '!' || s[i] > '~' {
			return false
		}
	}
	return true
}

// validatePath accepts only the form Shopify stores, because it rewrites anything else after apply.
func validatePath(p string) error {
	if !strings.HasPrefix(p, "/") {
		return errors.New("must start with /")
	}
	if !printable(p) {
		return errors.New("must not contain whitespace, control or non-ASCII characters; percent-encode them in lower case, e.g. %20")
	}
	if strings.ContainsAny(p, "#ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		return errors.New("must be lower case and without a #fragment: Shopify lower-cases the path and drops the fragment")
	}
	for i := 0; i < len(p); i++ {
		if p[i] == '%' && !lowerPercent.MatchString(p[i:min(i+3, len(p))]) {
			return errors.New("% must start a percent-encoded byte in lower case, such as %20 (a bare % is %25)")
		}
	}
	base, _, _ := strings.Cut(p, "?")
	if base == "/" || strings.HasSuffix(base, "/") {
		return errors.New("must not end with / (Shopify strips it) and can't be / alone")
	}
	for _, seg := range strings.Split(base, "/") {
		if seg == "." || seg == ".." {
			return errors.New(`must not contain "." or ".." segments: Shopify resolves them`)
		}
	}
	return nil
}

// validateTarget accepts a path or an absolute URL in the form Shopify stores.
func validateTarget(t string) error {
	if !printable(t) {
		return errors.New("must be non-empty, without whitespace, control or non-ASCII characters; percent-encode them")
	}
	if strings.HasPrefix(t, "/") {
		return nil
	}
	scheme, rest, ok := strings.Cut(t, "://")
	if !ok || scheme == "" {
		return errors.New("must be a path starting with / or an absolute URL such as https://example.com/x")
	}
	host, _, hasPath := strings.Cut(rest, "/")
	if host == "" || !hasPath || strings.ContainsAny(host, "?#") {
		return errors.New("an absolute URL needs a host and a / after it: Shopify rewrites https://example.com to https://example.com/")
	}
	if scheme != strings.ToLower(scheme) || host != strings.ToLower(host) {
		return errors.New("scheme and host must be lower case: Shopify lower-cases them")
	}
	if (scheme == "https" && strings.HasSuffix(host, ":443")) || (scheme == "http" && strings.HasSuffix(host, ":80")) {
		return errors.New("must not name the default port: Shopify drops it")
	}
	return nil
}

type stringValidator struct {
	what string
	fn   func(string) error
}

func pathValidator() validator.String   { return stringValidator{"path", validatePath} }
func targetValidator() validator.String { return stringValidator{"target", validateTarget} }

func (v stringValidator) Description(context.Context) string {
	return "the " + v.what + " must be in the canonical form Shopify stores"
}
func (v stringValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (v stringValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := v.fn(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, fmt.Sprintf("Invalid %s", v.what), fmt.Sprintf("%q %s.", req.ConfigValue.ValueString(), err))
	}
}
