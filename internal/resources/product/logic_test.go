package product

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

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

func inputJSON(t *testing.T, m model) map[string]json.RawMessage {
	t.Helper()
	in, _, diags := buildInput(context.Background(), &m, nil)
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
		GiftCard: types.BoolNull(), Options: types.ListNull(optionType), Variants: types.ListNull(variantType),
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
	variant := path.Root("variants").AtListIndex(1)
	cases := []struct {
		field []string
		want  *path.Path
	}{
		{[]string{"input", "handle"}, ptr(path.Root("handle"))},
		{[]string{"input", "title"}, ptr(path.Root("title"))},
		{[]string{"input", "descriptionHtml"}, ptr(path.Root("description_html"))},
		{[]string{"input", "productType"}, ptr(path.Root("product_type"))},
		{[]string{"input", "templateSuffix"}, ptr(path.Root("template_suffix"))},
		{[]string{"input", "giftCard"}, ptr(path.Root("gift_card"))},
		{[]string{"input", "category"}, ptr(path.Root("category_id"))},
		{[]string{"input", "seo", "title"}, ptr(path.Root("seo_title"))},
		{[]string{"input", "seo", "description"}, ptr(path.Root("seo_description"))},
		{[]string{"input", "tags", "2"}, ptr(path.Root("tags"))},
		{[]string{"input", "variants"}, ptr(path.Root("variants"))},
		{[]string{"input", "variants", "1"}, ptr(variant)},
		{[]string{"input", "variants", "1", "price"}, ptr(variant.AtName("price"))},
		{[]string{"input", "variants", "1", "compareAtPrice"}, ptr(variant.AtName("compare_at_price"))},
		{[]string{"input", "variants", "1", "barcodes"}, ptr(variant.AtName("barcodes"))},
		{[]string{"input", "variants", "1", "optionValues", "0"}, ptr(variant.AtName("option_values"))},
		{[]string{"input", "variants", "1", "inventoryItem", "requiresShipping"}, ptr(variant.AtName("requires_shipping"))},
		{[]string{"input", "variants", "1", "inventoryItem", "measurement", "weight"}, ptr(variant.AtName("weight"))},
		{[]string{"input", "variants", "1", "inventoryQuantities"}, ptr(variant)},
		{[]string{"input", "productOptions", "0", "values", "1"}, ptr(path.Root("options").AtListIndex(0).AtName("values"))},
		{[]string{"input", "productOptions", "x"}, ptr(path.Root("options"))},
		{[]string{"input", "metafields", "0"}, nil},
		{[]string{"input"}, nil},
		{nil, nil},
	}
	for _, c := range cases {
		p, ok := errorPath(c.field)
		if c.want == nil {
			if ok {
				t.Errorf("errorPath(%v) = %s, want no attribute", c.field, p)
			}
			continue
		}
		if !ok || !p.Equal(*c.want) {
			t.Errorf("errorPath(%v) = %s (%t), want %s", c.field, p, ok, *c.want)
		}
	}
}

func TestUserErrorDiags(t *testing.T) {
	errs := []fakeError{
		{[]string{"input", "handle"}, "Handle has already been taken"},
		{[]string{"input", "metafields", "0"}, "Metafield is invalid"},
	}
	diags := client.UserErrorDiagsWith("Failed to write product", errs, errorPath)
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
