package page

import (
	"encoding/json"
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
		Body: types.StringNull(), Published: types.BoolNull(), TemplateSuffix: types.StringNull(),
	}
}

// Unset attributes are unmanaged, so the write inputs never carry them, whatever state holds.
func TestCreateInputSendsOnlyConfigured(t *testing.T) {
	cfg := unsetModel("h", "T")
	if got, want := asJSON(t, cfg.createInput()), `{"handle":"h","title":"T"}`; got != want {
		t.Errorf("unset: got %s, want %s", got, want)
	}
	cfg.Body, cfg.Published, cfg.TemplateSuffix = types.StringValue("text"), types.BoolValue(false), types.StringValue("")
	got := asJSON(t, cfg.createInput())
	want := `{"handle":"h","body":"text","isPublished":false,"templateSuffix":"","title":"T"}`
	if got != want {
		t.Errorf("set: got %s, want %s", got, want)
	}
}

func TestUpdateInputSendsOnlyChanges(t *testing.T) {
	prior := model{
		Handle: types.StringValue("h"), Title: types.StringValue("T"), Body: types.StringValue("<p>x</p>"),
		Published: types.BoolValue(true), TemplateSuffix: types.StringNull(),
	}
	cases := []struct {
		name string
		cfg  model
		want string
		ok   bool
	}{
		{"nothing changed", model{Handle: prior.Handle, Title: prior.Title, Body: prior.Body, Published: prior.Published, TemplateSuffix: types.StringNull()}, `{}`, false},
		{"unset stays unmanaged", unsetModel("h", "T"), `{}`, false},
		{"title and handle", model{Handle: types.StringValue("h2"), Title: types.StringValue("T2"), Body: types.StringNull(), Published: types.BoolNull(), TemplateSuffix: types.StringNull()},
			`{"handle":"h2","title":"T2"}`, true},
		{"published off", model{Handle: prior.Handle, Title: prior.Title, Body: types.StringNull(), Published: types.BoolValue(false), TemplateSuffix: types.StringNull()},
			`{"isPublished":false}`, true},
		{"template suffix emptied", model{Handle: prior.Handle, Title: prior.Title, Body: types.StringNull(), Published: types.BoolNull(), TemplateSuffix: types.StringValue("")},
			`{"templateSuffix":""}`, true},
		{"body", model{Handle: prior.Handle, Title: prior.Title, Body: types.StringValue("other"), Published: types.BoolNull(), TemplateSuffix: types.StringNull()},
			`{"body":"other"}`, true},
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
	cfg := unsetModel("contact", "Kontakt")
	cfg.Published = types.BoolValue(true)
	in, ok := cfg.updateInput(&model{})
	if got, want := asJSON(t, in), `{"handle":"contact","isPublished":true,"title":"Kontakt"}`; got != want || !ok {
		t.Errorf("got %s, %t; want %s, true", got, ok, want)
	}
}

func TestErrorPath(t *testing.T) {
	cases := []struct {
		field []string
		want  path.Path
		ok    bool
	}{
		{[]string{"page", "handle"}, path.Root("handle"), true},
		{[]string{"page", "title"}, path.Root("title"), true},
		{[]string{"page", "body"}, path.Root("body"), true},
		{[]string{"page", "templateSuffix"}, path.Root("template_suffix"), true},
		{[]string{"page", "isPublished"}, path.Root("published"), true},
		{[]string{"page", "publishDate"}, path.Empty(), false},
		{[]string{"page"}, path.Empty(), false},
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
		"impressum":   `handle:"impressum"`,
		`a"b\c`:       `handle:"a\"b\\c"`,
		"tf-acc_page": `handle:"tf-acc_page"`,
	} {
		if got := handleQuery(in); got != want {
			t.Errorf("handleQuery(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestMatchHandle(t *testing.T) {
	node := func(h string) client.FindPagesPagesPageConnectionNodesPage {
		return client.FindPagesPagesPageConnectionNodesPage{PageFields: client.PageFields{Id: "gid://" + h, Handle: h}}
	}
	nodes := []client.FindPagesPagesPageConnectionNodesPage{node("impressum-2"), node("impressum")}
	if p := matchHandle(nodes, "impressum"); p == nil || p.Id != "gid://impressum" {
		t.Errorf("exact match: got %+v", p)
	}
	if p := matchHandle(nodes, "imp"); p != nil {
		t.Errorf("prefix must not match: got %+v", p)
	}
}

func TestGoneOnDelete(t *testing.T) {
	type e = client.DeletePagePageDeletePageDeletePayloadUserErrorsPageDeleteUserError
	notFound, other := client.PageDeleteUserErrorCodeNotFound, client.PageDeleteUserErrorCode("OTHER")
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

// The configured body is kept while Shopify holds what it returned at our write; template suffix reads back as given.
func TestSetFromAPI(t *testing.T) {
	suffix := ""
	live := &client.PageFields{Id: "gid://1", Handle: "h", Title: "T", Body: "<p>a<b>x</b></p>", IsPublished: false, TemplateSuffix: &suffix}
	m := unsetModel("h", "T")
	m.Body = types.StringValue("<p>a<b>x</p>")
	written := live.Body
	setFromAPI(&m, live, &written)
	if m.Body.ValueString() != "<p>a<b>x</p>" || m.Published.ValueBool() || m.TemplateSuffix.IsNull() || m.TemplateSuffix.ValueString() != "" || m.ID.ValueString() != "gid://1" {
		t.Errorf("repaired body kept: got %+v", m)
	}
	live.Body = "<p>edited in the admin</p>"
	setFromAPI(&m, live, &written)
	if m.Body.ValueString() != live.Body {
		t.Errorf("drifted body taken: got %q", m.Body.ValueString())
	}
	live.TemplateSuffix = nil
	setFromAPI(&m, live, nil)
	if !m.TemplateSuffix.IsNull() {
		t.Errorf("template suffix: got %v, want null", m.TemplateSuffix)
	}
}
