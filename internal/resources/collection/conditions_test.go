package collection

import (
	"encoding/json"
	"strings"
	"testing"
)

// One output condition per kind, as CollectionFields reads it, with the input it maps to.
var conditionCases = []struct{ kind, output, input string }{
	{"productTag",
		`{"__typename":"CollectionSourceInclusionConditionProductTag","id":"gid://shopify/C/1","relation__ProductTag":"TAGGED_WITH","values":["a"],"matchType":"ALL"}`,
		`{"productTag":{"relation":"TAGGED_WITH","values":["a"],"matchType":"ALL"}}`},
	{"productTitle",
		`{"__typename":"CollectionSourceInclusionConditionProductTitle","id":"gid://shopify/C/2","relation__ProductTitle":"CONTAINS","values":["Bett"],"matchType":"ANY"}`,
		`{"productTitle":{"relation":"CONTAINS","values":["Bett"],"matchType":"ANY"}}`},
	{"productType",
		`{"__typename":"CollectionSourceInclusionConditionProductType","id":"gid://shopify/C/3","relation__ProductType":"EQUALS","values":["Hilfe"],"matchType":"ANY"}`,
		`{"productType":{"relation":"EQUALS","values":["Hilfe"],"matchType":"ANY"}}`},
	{"productVendor",
		`{"__typename":"CollectionSourceInclusionConditionProductVendor","id":"gid://shopify/C/4","relation__ProductVendor":"EQUALS","values":["V"],"matchType":"ANY"}`,
		`{"productVendor":{"relation":"EQUALS","values":["V"],"matchType":"ANY"}}`},
	{"productStatus",
		`{"__typename":"CollectionSourceInclusionConditionProductStatus","id":"gid://shopify/C/5","relation__ProductStatus":"EQUALS","values__ProductStatus":["ACTIVE"],"matchType":"ANY"}`,
		`{"productStatus":{"relation":"EQUALS","values":["ACTIVE"],"matchType":"ANY"}}`},
	{"productCategory",
		`{"__typename":"CollectionSourceInclusionConditionProductCategory","id":"gid://shopify/C/6","relation__ProductCategory":"EQUALS","values__ProductCategory":[{"category":{"id":"gid://shopify/TaxonomyCategory/hb-1"},"includeDescendants":true}],"matchType":"ANY"}`,
		`{"productCategory":{"relation":"EQUALS","values":[{"categoryId":"gid://shopify/TaxonomyCategory/hb-1","includeDescendants":true}],"matchType":"ANY"}}`},
	{"variantTitle",
		`{"__typename":"CollectionSourceInclusionConditionVariantTitle","id":"gid://shopify/C/7","relation__VariantTitle":"EQUALS","values":["M"],"matchType":"ANY"}`,
		`{"variantTitle":{"relation":"EQUALS","values":["M"],"matchType":"ANY"}}`},
	{"variantPrice",
		`{"__typename":"CollectionSourceInclusionConditionVariantPrice","id":"gid://shopify/C/8","relation__VariantPrice":"GREATER_THAN","value__VariantPrice":{"amount":"5.0","currencyCode":"EUR"}}`,
		`{"variantPrice":{"relation":"GREATER_THAN","value":{"amount":"5.0","currencyCode":"EUR"}}}`},
	{"variantCompareAtPrice",
		`{"__typename":"CollectionSourceInclusionConditionVariantCompareAtPrice","id":"gid://shopify/C/9","relation__VariantCompareAtPrice":"IS_EMPTY","value__VariantCompareAtPrice":null}`,
		`{"variantCompareAtPrice":{"relation":"IS_EMPTY"}}`},
	{"variantInventory",
		`{"__typename":"CollectionSourceInclusionConditionVariantInventory","id":"gid://shopify/C/10","relation__VariantInventory":"GREATER_THAN","value__VariantInventory":3}`,
		`{"variantInventory":{"relation":"GREATER_THAN","value":3}}`},
	{"variantWeight",
		`{"__typename":"CollectionSourceInclusionConditionVariantWeight","id":"gid://shopify/C/11","relation__VariantWeight":"LESS_THAN","value__VariantWeight":{"value":1.5,"unit":"KILOGRAMS"}}`,
		`{"variantWeight":{"relation":"LESS_THAN","value":{"value":1.5,"unit":"KILOGRAMS"}}}`},
	{"metafieldBoolean",
		`{"__typename":"CollectionSourceInclusionConditionMetafieldBoolean","id":"gid://shopify/C/12","definition":{"id":"gid://shopify/MetafieldDefinition/1"},"relation__MetafieldBoolean":"EQUALS","value__MetafieldBoolean":true}`,
		`{"metafieldBoolean":{"definitionId":"gid://shopify/MetafieldDefinition/1","relation":"EQUALS","value":true}}`},
	{"metafieldInt",
		`{"__typename":"CollectionSourceInclusionConditionMetafieldInt","id":"gid://shopify/C/13","definition":{"id":"gid://shopify/MetafieldDefinition/2"},"relation__MetafieldInt":"GREATER_THAN","value__MetafieldInt":"4"}`,
		`{"metafieldInt":{"definitionId":"gid://shopify/MetafieldDefinition/2","relation":"GREATER_THAN","value":"4"}}`},
	{"metafieldDecimal",
		`{"__typename":"CollectionSourceInclusionConditionMetafieldDecimal","id":"gid://shopify/C/14","definition":{"id":"gid://shopify/MetafieldDefinition/3"},"relation__MetafieldDecimal":"LESS_THAN","value__MetafieldDecimal":"2.5"}`,
		`{"metafieldDecimal":{"definitionId":"gid://shopify/MetafieldDefinition/3","relation":"LESS_THAN","value":"2.5"}}`},
	{"metafieldString",
		`{"__typename":"CollectionSourceInclusionConditionMetafieldString","id":"gid://shopify/C/15","definition":{"id":"gid://shopify/MetafieldDefinition/4"},"relation__MetafieldString":"EQUALS","values":["x"],"matchType":"ANY"}`,
		`{"metafieldString":{"definitionId":"gid://shopify/MetafieldDefinition/4","relation":"EQUALS","values":["x"],"matchType":"ANY"}}`},
	{"metafieldStringList",
		`{"__typename":"CollectionSourceInclusionConditionMetafieldStringList","id":"gid://shopify/C/16","definition":{"id":"gid://shopify/MetafieldDefinition/5"},"relation__MetafieldStringList":"CONTAINS","values":["y"],"matchType":"ALL"}`,
		`{"metafieldStringList":{"definitionId":"gid://shopify/MetafieldDefinition/5","relation":"CONTAINS","values":["y"],"matchType":"ALL"}}`},
	{"metafieldMetaobject",
		`{"__typename":"CollectionSourceInclusionConditionMetafieldMetaobject","id":"gid://shopify/C/17","definition":{"id":"gid://shopify/MetafieldDefinition/6"},"relation__MetafieldMetaobject":"EQUALS","value__MetafieldMetaobject":{"id":"gid://shopify/Metaobject/1"}}`,
		`{"metafieldMetaobject":{"definitionId":"gid://shopify/MetafieldDefinition/6","relation":"EQUALS","value":"gid://shopify/Metaobject/1"}}`},
	{"metafieldMetaobjectList",
		`{"__typename":"CollectionSourceInclusionConditionMetafieldMetaobjectList","id":"gid://shopify/C/18","definition":{"id":"gid://shopify/MetafieldDefinition/7"},"relation__MetafieldMetaobjectList":"CONTAINS","values__MetafieldMetaobjectList":[{"id":"gid://shopify/Metaobject/2"},{"id":"gid://shopify/Metaobject/3"}],"matchType":"ANY"}`,
		`{"metafieldMetaobjectList":{"definitionId":"gid://shopify/MetafieldDefinition/7","relation":"CONTAINS","values":["gid://shopify/Metaobject/2","gid://shopify/Metaobject/3"],"matchType":"ANY"}}`},
}

func decodeConditions(t *testing.T, s string) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInputKindsCoverEveryCase(t *testing.T) {
	if len(conditionCases) != len(inputKinds) {
		t.Fatalf("%d cases for %d input kinds", len(conditionCases), len(inputKinds))
	}
	for _, c := range conditionCases {
		if !inputKinds[c.kind] {
			t.Errorf("%s is not an input kind", c.kind)
		}
	}
}

func TestLiveConditionsEveryKind(t *testing.T) {
	for _, c := range conditionCases {
		got, ids, err := liveConditions(decodeConditions(t, "["+c.output+"]"))
		if err != nil {
			t.Errorf("%s: %v", c.kind, err)
			continue
		}
		b, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if !conditionsEqual(string(b), "["+c.input+"]") || !strings.Contains(string(b), `"`+c.kind+`"`) {
			t.Errorf("%s: got %s, want [%s]", c.kind, b, c.input)
		}
		if len(ids) != 1 || !strings.HasPrefix(ids[0], "gid://shopify/C/") {
			t.Errorf("%s: ids = %v", c.kind, ids)
		}
	}
}

func TestLiveConditionsKeepsOrder(t *testing.T) {
	got, ids, err := liveConditions(decodeConditions(t, "["+conditionCases[7].output+","+conditionCases[0].output+"]"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	want := "[" + conditionCases[7].input + "," + conditionCases[0].input + "]"
	if !conditionsEqual(string(b), want) || len(ids) != 2 || ids[0] != "gid://shopify/C/8" {
		t.Errorf("got %s %v, want %s", b, ids, want)
	}
}

func TestLiveConditionsRejectsUnknownKinds(t *testing.T) {
	for _, typename := range []string{
		"CollectionSourceInclusionConditionUnknown",
		"CollectionSourceInclusionConditionMetafieldInteger",
		"SomethingNew",
	} {
		_, _, err := liveConditions(decodeConditions(t, `[{"__typename":"`+typename+`","id":"x"}]`))
		if err == nil || !strings.Contains(err.Error(), typename) {
			t.Errorf("%s: got %v, want an error naming it", typename, err)
		}
	}
}

func TestConditionsEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"same", `[{"productTag":{"relation":"TAGGED_WITH","values":["a"],"matchType":"ALL"}}]`, `[{"productTag":{"matchType":"ALL","values":["a"],"relation":"TAGGED_WITH"}}]`, true},
		{"decimal string amount", `[{"variantPrice":{"relation":"GREATER_THAN","value":{"amount":"5","currencyCode":"EUR"}}}]`, `[{"variantPrice":{"relation":"GREATER_THAN","value":{"amount":"5.0","currencyCode":"EUR"}}}]`, true},
		{"number vs decimal string", `[{"variantPrice":{"value":{"amount":5}}}]`, `[{"variantPrice":{"value":{"amount":"5.00"}}}]`, true},
		{"decimal value", `[{"metafieldDecimal":{"value":"2.50"}}]`, `[{"metafieldDecimal":{"value":"2.5"}}]`, true},
		{"different amount", `[{"variantPrice":{"value":{"amount":"5"}}}]`, `[{"variantPrice":{"value":{"amount":"6"}}}]`, false},
		{"numeric strings in values stay exact", `[{"productTag":{"values":["1"]}}]`, `[{"productTag":{"values":["1.0"]}}]`, false},
		{"order matters", `[{"productTag":{"values":["a"]}},{"productType":{"values":["b"]}}]`, `[{"productType":{"values":["b"]}},{"productTag":{"values":["a"]}}]`, false},
		{"values order matters", `[{"productTag":{"values":["a","b"]}}]`, `[{"productTag":{"values":["b","a"]}}]`, false},
		{"null equals missing", `[{"variantCompareAtPrice":{"relation":"IS_EMPTY","value":null}}]`, `[{"variantCompareAtPrice":{"relation":"IS_EMPTY"}}]`, true},
		{"extra key", `[{"productTag":{"values":["a"]}}]`, `[{"productTag":{"values":["a"],"matchType":"ALL"}}]`, false},
		{"case matters", `[{"productTag":{"values":["a"]}}]`, `[{"productTag":{"values":["A"]}}]`, false},
		{"empty lists", `[]`, ` [ ] `, true},
		{"not JSON compares exactly", `[`, `[`, true},
		{"not JSON differs", `[`, `[]`, false},
	}
	for _, c := range cases {
		if got := conditionsEqual(c.a, c.b); got != c.want {
			t.Errorf("%s: conditionsEqual = %t, want %t", c.name, got, c.want)
		}
		if got := conditionsEqual(c.b, c.a); got != c.want {
			t.Errorf("%s (swapped): conditionsEqual = %t, want %t", c.name, got, c.want)
		}
	}
}

func TestValidateConditions(t *testing.T) {
	cases := []struct {
		in      string
		wantErr string
	}{
		{`[]`, ""},
		{conditionCasesInput(), ""},
		{`{}`, "JSON list"},
		{`[`, "JSON list"},
		{`[1]`, "condition 0 must be an object"},
		{`[{}]`, "exactly one key"},
		{`[{"productTag":{},"productType":{}}]`, "exactly one key"},
	}
	for _, c := range cases {
		_, err := parseConditions(c.in)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.in, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("%s: got %v, want error containing %q", c.in, err, c.wantErr)
		}
	}
}

func conditionCasesInput() string {
	parts := make([]string, len(conditionCases))
	for i, c := range conditionCases {
		parts[i] = c.input
	}
	return "[" + strings.Join(parts, ",") + "]"
}
