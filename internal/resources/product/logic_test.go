package product

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestValidateHandle(t *testing.T) {
	cases := []struct {
		handle  string
		wantErr bool
	}{
		{"bettschutzeinlage", false},
		{"tf-design-probe_3", false},
		{"größe-m", false},
		{"TF-Design", true},
		{"has space", true},
		{"tab\there", true},
		{"Ä-upper", true},
		{"", true},
	}
	for _, c := range cases {
		if err := validateHandle(c.handle); (err != nil) != c.wantErr {
			t.Errorf("validateHandle(%q) = %v, want error: %t", c.handle, err, c.wantErr)
		}
	}
}

func TestValidateTags(t *testing.T) {
	cases := []struct {
		name    string
		tags    []string
		wantErr string
	}{
		{"plain", []string{"a", "b-c", "Zeta"}, ""},
		{"none", nil, ""},
		{"leading space", []string{" spaced"}, "whitespace"},
		{"trailing space", []string{"spaced\n"}, "whitespace"},
		{"empty", []string{""}, "empty"},
		{"case-insensitive duplicate", []string{"Zeta", "zeta"}, "duplicate"},
	}
	for _, c := range cases {
		err := validateTags(c.tags)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("%s: got %v, want error containing %q", c.name, err, c.wantErr)
		}
	}
}

func TestKeepOrTake(t *testing.T) {
	cfg := types.StringValue("<p>Hi<br>there<b>bold</p>")
	repaired := "<p>Hi<br>there<b>bold</b></p>"
	edited := "<p>edited in the admin</p>"
	cases := []struct {
		name       string
		configured types.String
		written    *string
		live       string
		want       types.String
	}{
		{"live is what Shopify returned at write: keep config", cfg, &repaired, repaired, cfg},
		{"admin edit: take live", cfg, &repaired, edited, types.StringValue(edited)},
		{"no stored write (import): take live", cfg, nil, repaired, types.StringValue(repaired)},
		{"unset: take live", types.StringNull(), &repaired, repaired, types.StringValue(repaired)},
		{"unknown (create without config): take live", types.StringUnknown(), nil, repaired, types.StringValue(repaired)},
	}
	for _, c := range cases {
		if got := keepOrTake(c.configured, c.written, c.live); !got.Equal(c.want) {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func inputJSON(t *testing.T, m model) map[string]json.RawMessage {
	t.Helper()
	in, diags := buildInput(context.Background(), &m)
	if diags.HasError() {
		t.Fatal(diags)
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func unsetModel() model {
	return model{
		ID: types.StringUnknown(), Handle: types.StringValue("h"), Title: types.StringValue("T"),
		Status: types.StringNull(), DescriptionHTML: types.StringNull(), Vendor: types.StringNull(),
		ProductType: types.StringNull(), CategoryID: types.StringNull(), Tags: types.SetNull(types.StringType),
		SEOTitle: types.StringNull(), SEODescription: types.StringNull(), TemplateSuffix: types.StringNull(),
		GiftCard: types.BoolNull(),
	}
}

func TestBuildInputSendsOnlySetAttributes(t *testing.T) {
	got := inputJSON(t, unsetModel())
	if len(got) != 2 || string(got["handle"]) != `"h"` || string(got["title"]) != `"T"` {
		t.Errorf("got %v, want only handle and title", got)
	}
}

func TestBuildInputAllAttributes(t *testing.T) {
	m := unsetModel()
	m.Status = types.StringValue("DRAFT")
	m.DescriptionHTML = types.StringValue("<p>x</p>")
	m.Vendor = types.StringValue("v")
	m.ProductType = types.StringValue("pt")
	m.CategoryID = types.StringValue("gid://shopify/TaxonomyCategory/aa-1")
	m.Tags = types.SetValueMust(types.StringType, []attr.Value{types.StringValue("b"), types.StringValue("a")})
	m.SEOTitle = types.StringValue("st")
	m.SEODescription = types.StringValue("sd")
	m.TemplateSuffix = types.StringValue("alt")
	m.GiftCard = types.BoolValue(false)
	got := inputJSON(t, m)
	want := map[string]string{
		"handle": `"h"`, "title": `"T"`, "status": `"DRAFT"`, "descriptionHtml": `"\u003cp\u003ex\u003c/p\u003e"`, "vendor": `"v"`,
		"productType": `"pt"`, "category": `"gid://shopify/TaxonomyCategory/aa-1"`, "tags": `["a","b"]`,
		"seo": `{"title":"st","description":"sd"}`, "templateSuffix": `"alt"`, "giftCard": `false`,
	}
	if len(got) != len(want) {
		t.Errorf("got keys %v, want %d keys", got, len(want))
	}
	for k, v := range want {
		if string(got[k]) != v {
			t.Errorf("%s = %s, want %s", k, got[k], v)
		}
	}
}

func TestBuildInputPartialAndEmpty(t *testing.T) {
	m := unsetModel()
	m.Tags = types.SetValueMust(types.StringType, nil)
	m.SEOTitle = types.StringValue("st")
	m.DescriptionHTML = types.StringValue("")
	got := inputJSON(t, m)
	if string(got["tags"]) != `[]` {
		t.Errorf("tags = %s, want [] so the tags are cleared", got["tags"])
	}
	if string(got["seo"]) != `{"title":"st"}` {
		t.Errorf("seo = %s, want only the title", got["seo"])
	}
	if string(got["descriptionHtml"]) != `""` {
		t.Errorf("descriptionHtml = %s, want an empty string to clear it", got["descriptionHtml"])
	}
}

func TestBuildInputNeverSendsUnownedKeys(t *testing.T) {
	m := unsetModel()
	m.Status = types.StringValue("ACTIVE")
	got := inputJSON(t, m)
	for _, k := range []string{"collections", "metafields", "files", "productOptions", "variants", "requiresSellingPlan",
		"claimOwnership", "combinedListingRole", "redirectNewHandle", "giftCardTemplateSuffix"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s was sent, want it left out so the admin value stays", k)
		}
	}
}

type fakeError struct {
	field   []string
	message string
}

func (e *fakeError) GetField() []string { return e.field }
func (e *fakeError) GetMessage() string { return e.message }

func TestErrorPath(t *testing.T) {
	cases := []struct {
		field []string
		want  string
	}{
		{[]string{"input", "handle"}, "handle"},
		{[]string{"input", "title"}, "title"},
		{[]string{"input", "descriptionHtml"}, "description_html"},
		{[]string{"input", "productType"}, "product_type"},
		{[]string{"input", "templateSuffix"}, "template_suffix"},
		{[]string{"input", "giftCard"}, "gift_card"},
		{[]string{"input", "category"}, "category_id"},
		{[]string{"input", "seo", "title"}, "seo_title"},
		{[]string{"input", "seo", "description"}, "seo_description"},
		{[]string{"input", "tags", "2"}, "tags"},
		{[]string{"input", "variants", "0", "price"}, ""},
		{[]string{"input"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		p, ok := errorPath(c.field)
		if c.want == "" {
			if ok {
				t.Errorf("errorPath(%v) = %s, want no attribute", c.field, p)
			}
			continue
		}
		if !ok || !p.Equal(path.Root(c.want)) {
			t.Errorf("errorPath(%v) = %s (%t), want %s", c.field, p, ok, c.want)
		}
	}
}

func TestUserErrorDiags(t *testing.T) {
	errs := []fakeError{
		{[]string{"input", "handle"}, "Handle has already been taken"},
		{[]string{"input", "variants", "0", "price"}, "Price is invalid"},
	}
	diags := userErrorDiags("Failed to write product", errs)
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want 2", len(diags))
	}
	for i, wantScoped := range []bool{true, false} {
		d, scoped := diags[i].(interface{ Path() path.Path })
		if scoped != wantScoped {
			t.Errorf("diagnostic %d scoped = %t, want %t", i, scoped, wantScoped)
		}
		if scoped && !d.Path().Equal(path.Root("handle")) {
			t.Errorf("diagnostic %d path = %s, want handle", i, d.Path())
		}
	}
}
