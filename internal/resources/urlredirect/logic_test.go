package urlredirect

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func TestErrorPath(t *testing.T) {
	tests := []struct {
		field []string
		want  path.Path
		ok    bool
	}{
		{[]string{"urlRedirect", "path"}, path.Root("path"), true},
		{[]string{"urlRedirect", "target"}, path.Root("target"), true},
		{[]string{"urlRedirect"}, path.Empty(), false},
		{[]string{"id"}, path.Empty(), false},
		{[]string{"urlRedirect", "other"}, path.Empty(), false},
		{nil, path.Empty(), false},
	}
	for _, tt := range tests {
		got, ok := errorPath(tt.field)
		if ok != tt.ok || !got.Equal(tt.want) {
			t.Errorf("errorPath(%v) = %v, %v; want %v, %v", tt.field, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSearchQuery(t *testing.T) {
	tests := map[string]string{
		"/a":         `path:"/a"`,
		"/a?x=1":     `path:"/a?x=1"`,
		`/a"b`:       `path:"/a\"b"`,
		`/a\b`:       `path:"/a\\b"`,
		"/a%20b":     `path:"/a%20b"`,
		"/a b:c (d)": `path:"/a b:c (d)"`,
	}
	for in, want := range tests {
		if got := searchQuery(in); got != want {
			t.Errorf("searchQuery(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestMatchPath(t *testing.T) {
	nodes := []client.ListUrlRedirectsUrlRedirectsUrlRedirectConnectionNodesUrlRedirect{
		{UrlRedirectFields: client.UrlRedirectFields{Id: "1", Path: "/a-b"}},
		{UrlRedirectFields: client.UrlRedirectFields{Id: "2", Path: "/a"}},
	}
	if got := matchPath(nodes, "/a"); got == nil || got.Id != "2" {
		t.Errorf("matchPath(/a) = %+v, want id 2", got)
	}
	if got := matchPath(nodes, "/A"); got != nil {
		t.Errorf("matchPath(/A) = %+v, want nil: exact compare", got)
	}
	if got := matchPath(nil, "/a"); got != nil {
		t.Errorf("matchPath(nil) = %+v, want nil", got)
	}
}

func TestGoneOnDelete(t *testing.T) {
	type e = client.DeleteUrlRedirectUrlRedirectDeleteUrlRedirectDeletePayloadUserErrorsUrlRedirectUserError
	code := func(c client.UrlRedirectErrorCode) *client.UrlRedirectErrorCode { return &c }
	tests := []struct {
		name string
		errs []e
		want bool
	}{
		{"none", nil, false},
		{"does not exist", []e{{Code: code(client.UrlRedirectErrorCodeDoesNotExist)}}, true},
		{"delete failed", []e{{Code: code(client.UrlRedirectErrorCodeDeleteFailed)}}, false},
		{"mixed", []e{{Code: code(client.UrlRedirectErrorCodeDoesNotExist)}, {Code: code(client.UrlRedirectErrorCodeDeleteFailed)}}, false},
		{"no code", []e{{}}, false},
	}
	for _, tt := range tests {
		if got := goneOnDelete(tt.errs); got != tt.want {
			t.Errorf("%s: goneOnDelete = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestValidatePath(t *testing.T) {
	ok := []string{"/a", "/a/b", "/a-b_c.html", "/a?x=1", "/a?x=b&y=c", "/a%20b", "/prob%c3%a9", "/a//b", "/a+b", "/.hidden", "/a.b/c", "/tf-acc-redirect-x1"}
	bad := []string{"", "/", "a", "a/b", "https://example.com/a", "/A", "/a/", "/a/?x=1", "/a b", " /a", "/a ", "/probé", "/a#f", "/a/../b", "/a/./b", "/a%C3%A9", "/a%zz", "/a%2", "/a%", "/a\tb", "/?x=1", "/a/.."}
	for _, p := range ok {
		if err := validatePath(p); err != nil {
			t.Errorf("validatePath(%q) = %v, want ok", p, err)
		}
	}
	for _, p := range bad {
		if err := validatePath(p); err == nil {
			t.Errorf("validatePath(%q) accepted", p)
		}
	}
}

func TestValidateTarget(t *testing.T) {
	ok := []string{"/", "/a", "/Y", "/y/", "/a?x=B", "/a#f", "/a%C3%A9", "//example.com/x", "https://example.com/", "https://example.com/X/", "https://example.com/y?a=1", "http://shop.example.com/a%20b", "https://example.com/?x=1", "ftp://example.com/x", "https://example.com:8443/x"}
	bad := []string{"", "foo", "foo/bar", "example.com/x", "mailto:a@b.c", "/a b", " /x", "/x ", "/é", "https://example.com", "https://example.com?x=1", "https://example.com#f", "https://Example.com/x", "HTTPS://example.com/x", "https:///x", "https://", "/a\tb", "https://exa mple.com/x", "https://example.com:443/x", "http://example.com:80/x"}
	for _, p := range ok {
		if err := validateTarget(p); err != nil {
			t.Errorf("validateTarget(%q) = %v, want ok", p, err)
		}
	}
	for _, p := range bad {
		if err := validateTarget(p); err == nil {
			t.Errorf("validateTarget(%q) accepted", p)
		}
	}
}
