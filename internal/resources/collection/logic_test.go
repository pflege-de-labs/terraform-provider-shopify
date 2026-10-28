package collection

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const (
	tagCondition = `{"__typename":"CollectionSourceInclusionConditionProductTag","id":"gid://shopify/C/1","relation__ProductTag":"TAGGED_WITH","values":["a"],"matchType":"ALL"}`
	tagInput     = `{"productTag":{"relation":"TAGGED_WITH","values":["a"],"matchType":"ALL"}}`
	typeInput    = `{"productType":{"relation":"EQUALS","values":["b"],"matchType":"ANY"}}`
)

// conditionsSource is one source as CollectionFields reads it.
func conditionsSource(matchType, conditions, selections string) string {
	return `{"__typename":"CollectionConditionsSource","id":"S1","app":null,"shareable":false,"targetType":"PRODUCTS"` +
		`,"inclusion":{"matchType":` + matchType + `,"conditions":[` + conditions + `],"selections":{"nodes":[` + selections +
		`],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`
}

func selection(id string) string { return `{"product":{"id":"` + id + `"},"variantIds":null}` }

func ptr[T any](v T) *T { return &v }

func TestMembershipOf(t *testing.T) {
	cases := []struct {
		name    string
		sources string
		want    *membership
		wantErr string
	}{
		{"no source", `[]`, nil, ""},
		{"empty source (frontpage)", "[" + conditionsSource("null", "", "") + "]",
			&membership{SourceID: "S1", Conditions: []any{}, ProductIDs: []string{}, ShopifyMade: true}, ""},
		{"manual", "[" + conditionsSource("null", "", selection("P2")+","+selection("P1")) + "]",
			&membership{SourceID: "S1", Conditions: []any{}, ProductIDs: []string{"P1", "P2"}, ShopifyMade: true}, ""},
		{"smart", "[" + conditionsSource(`"ALL"`, tagCondition, "") + "]",
			&membership{SourceID: "S1", MatchType: ptr("ALL"), Conditions: mustInput(tagInput), ConditionIDs: []string{"gid://shopify/C/1"}, ProductIDs: []string{}, ShopifyMade: true}, ""},
		{"two sources", "[" + conditionsSource("null", "", "") + `,{"__typename":"CollectionSubCollectionsSource","id":"S2","app":null}]`,
			nil, "CollectionConditionsSource, CollectionSubCollectionsSource"},
		{"sub-collection source", `[{"__typename":"CollectionSubCollectionsSource","id":"S2","app":null}]`, nil, "CollectionSubCollectionsSource"},
		// S13-8: a non-shareable source created by an app, this provider's own included, is managed.
		{"app-created", `[` + strings.Replace(conditionsSource("null", "", selection("P1")), `"app":null`, `"app":{"id":"gid://shopify/App/1"}`, 1) + `]`,
			&membership{SourceID: "S1", Conditions: []any{}, ProductIDs: []string{"P1"}}, ""},
		{"exclusions", `[` + strings.Replace(conditionsSource("null", "", selection("P1")), `"targetType":"PRODUCTS"`, `"targetType":"PRODUCTS","exclusion":{"conditions":[{"__typename":"CollectionSourceExclusionConditionProductTag","id":"X1"}],"selections":{"nodes":[]}}`, 1) + `]`,
			&membership{SourceID: "S1", Conditions: []any{}, ProductIDs: []string{"P1"}, ShopifyMade: true, Exclusions: true}, ""},
		{"empty exclusion", `[` + strings.Replace(conditionsSource("null", "", selection("P1")), `"targetType":"PRODUCTS"`, `"targetType":"PRODUCTS","exclusion":{"conditions":[],"selections":{"nodes":[]}}`, 1) + `]`,
			&membership{SourceID: "S1", Conditions: []any{}, ProductIDs: []string{"P1"}, ShopifyMade: true}, ""},
		{"shareable", `[` + strings.Replace(conditionsSource("null", "", ""), `"shareable":false`, `"shareable":true`, 1) + `]`, nil, "shareable"},
		{"variant target", `[` + strings.Replace(conditionsSource("null", "", ""), `"PRODUCTS"`, `"VARIANTS"`, 1) + `]`, nil, "VARIANTS"},
		{"variant selection", "[" + conditionsSource("null", "", `{"product":{"id":"P1"},"variantIds":["V1"]}`) + "]", nil, "variant"},
		{"unknown condition", "[" + conditionsSource(`"ALL"`, `{"__typename":"CollectionSourceInclusionConditionUnknown","id":"C9"}`, "") + "]", nil, "CollectionSourceInclusionConditionUnknown"},
	}
	for _, c := range cases {
		sources, err := decodeSources(json.RawMessage(c.sources))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got, err := membershipOf(sources)
		switch {
		case c.wantErr != "":
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: got %v, want error containing %q", c.name, err, c.wantErr)
			}
		case err != nil:
			t.Errorf("%s: %v", c.name, err)
		case !reflect.DeepEqual(got, c.want):
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func mustInput(s ...string) []any {
	v, err := parseConditions("[" + strings.Join(s, ",") + "]")
	if err != nil {
		panic(err)
	}
	return v
}

func sel(ids ...string) []client.CollectionInclusionProductSelectionInput {
	out := make([]client.CollectionInclusionProductSelectionInput, len(ids))
	for i, id := range ids {
		out[i].ProductId = id
	}
	return out
}

func TestPlanSource(t *testing.T) {
	all := client.CollectionConditionMatchTypeAll
	anyOf := client.CollectionConditionMatchTypeAny
	smart := &membership{SourceID: "S1", MatchType: ptr("ALL"), Conditions: mustInput(tagInput), ConditionIDs: []string{"C1"}, ProductIDs: []string{}}
	manual := &membership{SourceID: "S1", Conditions: []any{}, ProductIDs: []string{"P1", "P2"}}
	empty := &membership{SourceID: "S1", Conditions: []any{}, ProductIDs: []string{}, ShopifyMade: true}
	shopifyManual := &membership{SourceID: "S1", Conditions: []any{}, ProductIDs: []string{"P1"}, ShopifyMade: true}
	excluding := &membership{SourceID: "S1", Conditions: []any{}, ProductIDs: []string{"P1"}, Exclusions: true}
	smartExcluding := &membership{SourceID: "S1", MatchType: ptr("ALL"), Conditions: mustInput(tagInput), ConditionIDs: []string{"C1"}, ProductIDs: []string{}, Exclusions: true}
	shopifyExcluding := &membership{SourceID: "S1", Conditions: []any{}, ProductIDs: []string{"P1"}, ShopifyMade: true, Exclusions: true}
	emptyExcluding := &membership{SourceID: "S1", MatchType: ptr("ALL"), Conditions: []any{}, ProductIDs: []string{}, Exclusions: true}
	cases := []struct {
		name    string
		live    *membership
		want    desired
		expect  sourceChanges
		wantErr string
	}{
		{"unmanaged", smart, desired{}, sourceChanges{}, ""},
		{"no source, nothing to add", nil, desired{ProductIDs: []string{}, Conditions: json.RawMessage(`[]`)}, sourceChanges{}, ""},
		{"no source, match type alone", nil, desired{MatchType: ptr("ALL"), ProductIDs: []string{}}, sourceChanges{}, "match_type"},
		{"no source, products", nil, desired{ProductIDs: []string{"P2", "P1"}},
			sourceChanges{Create: &client.CollectionCreateSourceInclusionInput{Selections: sel("P1", "P2")}}, ""},
		{"no source, conditions", nil, desired{MatchType: ptr("ALL"), Conditions: json.RawMessage(`[` + tagInput + `]`)},
			sourceChanges{Create: &client.CollectionCreateSourceInclusionInput{MatchType: &all, Conditions: json.RawMessage(`[` + tagInput + `]`)}}, ""},
		{"unchanged", smart, desired{MatchType: ptr("ALL"), Conditions: json.RawMessage(`[` + tagInput + `]`), ProductIDs: []string{}}, sourceChanges{}, ""},
		{"match type", smart, desired{MatchType: ptr("ANY")},
			sourceChanges{Update: &client.CollectionUpdateSourceInclusionInput{MatchType: &anyOf}}, ""},
		{"replace conditions", smart, desired{Conditions: json.RawMessage(`[` + typeInput + `]`)},
			sourceChanges{Update: &client.CollectionUpdateSourceInclusionInput{ConditionsToDelete: []string{"C1"}, ConditionsToCreate: json.RawMessage(`[` + typeInput + `]`)}}, ""},
		{"products added and removed", manual, desired{ProductIDs: []string{"P2", "P3"}},
			sourceChanges{Update: &client.CollectionUpdateSourceInclusionInput{SelectionsToAdd: sel("P3"), SelectionsToRemove: sel("P1")}}, ""},
		{"conditions swapped for products", smart, desired{Conditions: json.RawMessage(`[]`), ProductIDs: []string{"P1"}},
			sourceChanges{Update: &client.CollectionUpdateSourceInclusionInput{ConditionsToDelete: []string{"C1"}, SelectionsToAdd: sel("P1")}}, ""},
		{"emptied: delete the source", manual, desired{ProductIDs: []string{}}, sourceChanges{Delete: true}, ""},
		{"emptied Shopify-made source: refused (S13-9)", shopifyManual, desired{ProductIDs: []string{}}, sourceChanges{}, "Shopify made"},
		{"emptied source with exclusions: products removed in place (S13-9)", excluding, desired{ProductIDs: []string{}},
			sourceChanges{Update: &client.CollectionUpdateSourceInclusionInput{SelectionsToRemove: sel("P1")}}, ""},
		{"emptied source with exclusions: conditions deleted in place (S13-9)", smartExcluding, desired{Conditions: json.RawMessage(`[]`)},
			sourceChanges{Update: &client.CollectionUpdateSourceInclusionInput{ConditionsToDelete: []string{"C1"}}}, ""},
		{"emptied Shopify-made source with exclusions: updated, not refused", shopifyExcluding, desired{ProductIDs: []string{}},
			sourceChanges{Update: &client.CollectionUpdateSourceInclusionInput{SelectionsToRemove: sel("P1")}}, ""},
		{"emptied source with exclusions keeps match type", smartExcluding, desired{MatchType: ptr("ALL"), Conditions: json.RawMessage(`[]`)},
			sourceChanges{Update: &client.CollectionUpdateSourceInclusionInput{ConditionsToDelete: []string{"C1"}}}, ""},
		{"empty source with exclusions stays", emptyExcluding, desired{Conditions: json.RawMessage(`[]`), ProductIDs: []string{}}, sourceChanges{}, ""},
		{"empty source with exclusions: match type changed", emptyExcluding, desired{MatchType: ptr("ANY"), Conditions: json.RawMessage(`[]`)},
			sourceChanges{Update: &client.CollectionUpdateSourceInclusionInput{MatchType: &anyOf}}, ""},
		{"Shopify-made source keeps an entry", shopifyManual, desired{ProductIDs: []string{"P2"}},
			sourceChanges{Update: &client.CollectionUpdateSourceInclusionInput{SelectionsToAdd: sel("P2"), SelectionsToRemove: sel("P1")}}, ""},
		{"emptied, conditions unmanaged but live", smart, desired{ProductIDs: []string{}}, sourceChanges{}, ""},
		{"emptied with match type", smart, desired{MatchType: ptr("ALL"), Conditions: json.RawMessage(`[]`)}, sourceChanges{}, "match_type"},
		{"empty source stays (frontpage)", empty, desired{ProductIDs: []string{}}, sourceChanges{}, ""},
		{"empty source gains products", empty, desired{ProductIDs: []string{"P1"}},
			sourceChanges{Update: &client.CollectionUpdateSourceInclusionInput{SelectionsToAdd: sel("P1")}}, ""},
	}
	for _, c := range cases {
		got, err := planSource(c.live, c.want)
		switch {
		case c.wantErr != "":
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: got %v, want error containing %q", c.name, err, c.wantErr)
			}
		case err != nil:
			t.Errorf("%s: %v", c.name, err)
		default:
			gb, _ := json.Marshal(got)
			wb, _ := json.Marshal(c.expect)
			if string(gb) != string(wb) {
				t.Errorf("%s:\n got %s\nwant %s", c.name, gb, wb)
			}
		}
	}
}

type userError struct {
	field   []string
	message string
}

func (e userError) GetField() []string { return e.field }
func (e userError) GetMessage() string { return e.message }

func TestErrorPath(t *testing.T) {
	cases := []struct {
		field   []string
		message string
		want    path.Path
		ok      bool
	}{
		{[]string{"collection", "title"}, "Title can't be blank", path.Root("title"), true},
		{[]string{"collection", "descriptionHtml"}, "x", path.Root("description_html"), true},
		{[]string{"collection", "sortOrder"}, "x", path.Root("sort_order"), true},
		{[]string{"collection", "templateSuffix"}, "x", path.Root("template_suffix"), true},
		{[]string{"collection", "seo", "description"}, "x", path.Root("seo_description"), true},
		{[]string{"collection"}, "Handle has already been taken", path.Root("handle"), true},
		{[]string{"collection"}, "Something else", path.Empty(), false},
		{[]string{"collection", "sources", "0", "source", "inclusion", "conditions", "1", "productTag"}, "x", path.Root("conditions"), true},
		{[]string{"collection", "sourcesToUpdate", "0", "condition", "inclusion", "conditionsToCreate", "0"}, "x", path.Root("conditions"), true},
		{[]string{"collection", "sourcesToUpdate", "0", "condition", "inclusion", "selectionsToAdd", "0"}, "x", path.Root("product_ids"), true},
		{[]string{"collection", "sources", "0", "source", "inclusion", "matchType"}, "x", path.Root("match_type"), true},
		{[]string{"collection", "sources", "0", "source", "inclusion"}, "Match type is required when conditions are provided", path.Empty(), false},
		{[]string{"id"}, "Collection does not exist", path.Empty(), false},
		{nil, "x", path.Empty(), false},
	}
	for _, c := range cases {
		got, ok := errorPath(userError{c.field, c.message})
		if ok != c.ok || !got.Equal(c.want) {
			t.Errorf("errorPath(%v, %q) = %s, %t; want %s, %t", c.field, c.message, got, ok, c.want, c.ok)
		}
	}
}

// Unset attributes are unmanaged, so the write inputs never carry them, whatever state holds.
func TestInputsSendOnlyConfigured(t *testing.T) {
	cfg := model{
		Handle: types.StringValue("h"), Title: types.StringValue("T"),
		DescriptionHTML: types.StringNull(), SortOrder: types.StringNull(), SEOTitle: types.StringNull(), SEODescription: types.StringNull(),
		TemplateSuffix: types.StringNull(), MatchType: types.StringNull(), Conditions: conditionsNull(), ProductIDs: types.SetNull(types.StringType),
	}
	asJSON := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := asJSON(cfg.createInput()); got != `{"handle":"h","title":"T"}` {
		t.Errorf("createInput = %s", got)
	}
	if got := asJSON(cfg.updateInput("C1")); got != `{"handle":"h","id":"C1","title":"T"}` {
		t.Errorf("updateInput = %s", got)
	}
	if cfg.membershipConfigured() {
		t.Error("no membership attribute is configured")
	}
	want, d := cfg.desired(context.Background())
	if d.HasError() || want.MatchType != nil || want.Conditions != nil || want.ProductIDs != nil {
		t.Errorf("desired = %+v %v, want nothing managed", want, d)
	}
	cfg.SortOrder, cfg.ProductIDs = types.StringValue("MANUAL"), types.SetValueMust(types.StringType, nil)
	if got := asJSON(cfg.updateInput("C1")); got != `{"handle":"h","id":"C1","sortOrder":"MANUAL","title":"T"}` {
		t.Errorf("updateInput with sort_order = %s", got)
	}
	if want, _ := cfg.desired(context.Background()); want.ProductIDs == nil || len(want.ProductIDs) != 0 {
		t.Errorf("product_ids = [] must be managed and empty, got %#v", want.ProductIDs)
	}
}
