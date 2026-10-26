package blog

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func unsetModel(handle, title string) model {
	return model{
		Handle: types.StringValue(handle), Title: types.StringValue(title),
		CommentPolicy: types.StringNull(), TemplateSuffix: types.StringNull(),
	}
}

// Unset attributes are unmanaged, so the write inputs never carry them.
func TestCreateInputSendsOnlyConfigured(t *testing.T) {
	cfg := unsetModel("h", "T")
	if got, want := asJSON(t, cfg.createInput()), `{"handle":"h","title":"T"}`; got != want {
		t.Errorf("unset: got %s, want %s", got, want)
	}
	cfg.CommentPolicy, cfg.TemplateSuffix = types.StringValue("MODERATED"), types.StringValue("")
	got := asJSON(t, cfg.createInput())
	want := `{"handle":"h","templateSuffix":"","commentPolicy":"MODERATED","title":"T"}`
	if got != want {
		t.Errorf("set: got %s, want %s", got, want)
	}
}

func TestUpdateInputSendsOnlyChanges(t *testing.T) {
	prior := model{
		Handle: types.StringValue("h"), Title: types.StringValue("T"),
		CommentPolicy: types.StringValue("CLOSED"), TemplateSuffix: types.StringNull(),
	}
	with := func(f func(m *model)) model {
		m := unsetModel("h", "T")
		f(&m)
		return m
	}
	cases := []struct {
		name string
		cfg  model
		want string
		ok   bool
	}{
		{"nothing changed", model{Handle: prior.Handle, Title: prior.Title, CommentPolicy: prior.CommentPolicy, TemplateSuffix: types.StringNull()}, `{}`, false},
		{"unset stays unmanaged", unsetModel("h", "T"), `{}`, false},
		{"title and handle", with(func(m *model) { m.Handle, m.Title = types.StringValue("h2"), types.StringValue("T2") }), `{"handle":"h2","title":"T2"}`, true},
		{"comment policy", with(func(m *model) { m.CommentPolicy = types.StringValue("AUTO_PUBLISHED") }), `{"commentPolicy":"AUTO_PUBLISHED"}`, true},
		{"template suffix emptied", with(func(m *model) { m.TemplateSuffix = types.StringValue("") }), `{"templateSuffix":""}`, true},
	}
	for _, c := range cases {
		in, ok := c.cfg.updateInput(&prior)
		if got := asJSON(t, in); got != c.want || ok != c.ok {
			t.Errorf("%s: got %s, %t; want %s, %t", c.name, got, ok, c.want, c.ok)
		}
	}
}

// Adopting sends everything configured: the prior is an empty model.
func TestUpdateInputWithoutPriorSendsAllConfigured(t *testing.T) {
	cfg := unsetModel("news", "Ratgeber")
	cfg.CommentPolicy = types.StringValue("MODERATED")
	in, ok := cfg.updateInput(&model{})
	if got, want := asJSON(t, in), `{"handle":"news","commentPolicy":"MODERATED","title":"Ratgeber"}`; got != want || !ok {
		t.Errorf("got %s, %t; want %s, true", got, ok, want)
	}
}

func TestErrorPath(t *testing.T) {
	cases := []struct {
		field []string
		want  path.Path
		ok    bool
	}{
		{[]string{"blog", "handle"}, path.Root("handle"), true},
		{[]string{"blog", "title"}, path.Root("title"), true},
		{[]string{"blog", "commentPolicy"}, path.Root("comment_policy"), true},
		{[]string{"blog", "templateSuffix"}, path.Root("template_suffix"), true},
		{[]string{"blog", "metafields"}, path.Empty(), false},
		{[]string{"blog"}, path.Empty(), false},
		{[]string{"id"}, path.Empty(), false},
		{nil, path.Empty(), false},
	}
	for _, c := range cases {
		got, ok := errorPath(c.field)
		if ok != c.ok || !got.Equal(c.want) {
			t.Errorf("errorPath(%v) = %s, %t; want %s, %t", c.field, got, ok, c.want, c.ok)
		}
	}
}

func TestHandleQuery(t *testing.T) {
	for in, want := range map[string]string{
		"news":        `handle:"news"`,
		`a"b\c`:       `handle:"a\"b\\c"`,
		"tf-acc-blog": `handle:"tf-acc-blog"`,
	} {
		if got := handleQuery(in); got != want {
			t.Errorf("handleQuery(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestMatchHandle(t *testing.T) {
	node := func(h string) client.FindBlogsBlogsBlogConnectionNodesBlog {
		return client.FindBlogsBlogsBlogConnectionNodesBlog{BlogFields: client.BlogFields{Id: "gid://" + h, Handle: h}}
	}
	nodes := []client.FindBlogsBlogsBlogConnectionNodesBlog{node("news-1"), node("news")}
	if b := matchHandle(nodes, "news"); b == nil || b.Id != "gid://news" {
		t.Errorf("exact match: got %+v", b)
	}
	if b := matchHandle(nodes, "new"); b != nil {
		t.Errorf("prefix must not match: got %+v", b)
	}
}

// Shopify silently suffixes a taken handle; that must surface as an error naming both handles.
func TestHandleDrift(t *testing.T) {
	if err := handleDrift("news", "news"); err != nil {
		t.Errorf("same handle: got %v", err)
	}
	err := handleDrift("news", "news-1")
	if err == nil || !strings.Contains(err.Error(), `"news"`) || !strings.Contains(err.Error(), `"news-1"`) {
		t.Errorf("suffixed handle: got %v, want an error naming both", err)
	}
}

func TestGoneOnDelete(t *testing.T) {
	type e = client.DeleteBlogBlogDeleteBlogDeletePayloadUserErrorsBlogDeleteUserError
	notFound, other := client.BlogDeleteUserErrorCodeNotFound, client.BlogDeleteUserErrorCode("OTHER")
	cases := []struct {
		name string
		errs []e
		want bool
	}{
		{"none", nil, false},
		{"not found", []e{{Code: &notFound}}, true},
		{"not found and another", []e{{Code: &notFound}, {Code: &other}}, false},
		{"no code", []e{{}}, false},
	}
	for _, c := range cases {
		if got := goneOnDelete(c.errs); got != c.want {
			t.Errorf("%s: got %t, want %t", c.name, got, c.want)
		}
	}
}

func TestSetFromAPI(t *testing.T) {
	suffix := ""
	live := &client.BlogFields{Id: "gid://1", Handle: "h", Title: "T", CommentPolicy: client.CommentPolicyModerated, TemplateSuffix: &suffix}
	m := unsetModel("h", "T")
	setFromAPI(&m, live)
	if m.CommentPolicy.ValueString() != "MODERATED" || m.TemplateSuffix.IsNull() || m.TemplateSuffix.ValueString() != "" || m.ID.ValueString() != "gid://1" {
		t.Errorf("got %+v", m)
	}
	live.TemplateSuffix = nil
	setFromAPI(&m, live)
	if !m.TemplateSuffix.IsNull() {
		t.Errorf("template suffix: got %v, want null", m.TemplateSuffix)
	}
}

// The data-loss guard fails closed: only a known count of 0 lets a delete through.
func TestArticlesAtRisk(t *testing.T) {
	cases := []struct {
		name   string
		count  *client.GetBlogArticlesCountBlogArticlesCount
		risk   bool
		detail string
	}{
		{"empty", &client.GetBlogArticlesCountBlogArticlesCount{Count: 0}, false, ""},
		{"known", &client.GetBlogArticlesCountBlogArticlesCount{Count: 3}, true, "It holds 3 articles, which are deleted with it."},
		{"unknown", nil, true, "Shopify did not return the article count, so the blog may hold articles, which are deleted with it."},
	}
	for _, c := range cases {
		risk, detail := articlesAtRisk(c.count)
		if risk != c.risk || detail != c.detail {
			t.Errorf("%s: got %t, %q; want %t, %q", c.name, risk, detail, c.risk, c.detail)
		}
	}
}
