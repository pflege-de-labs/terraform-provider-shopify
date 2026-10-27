package discount

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func ptr[T any](v T) *T { return &v }

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstantEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2026-10-26T12:00:00+01:00", "2026-10-26T11:00:00Z", true},
		{"2026-11-01T00:00:00+01:00", "2026-10-31T23:00:00Z", true},
		{"2026-10-26T11:00:00Z", "2026-10-26T11:00:00Z", true},
		{"2026-10-26T12:00:00+01:00", "2026-10-26T12:00:00Z", false},
		{"not a time", "not a time", false},
		{"2026-10-26", "2026-10-26T00:00:00Z", false},
	}
	for _, c := range cases {
		if got := (instantEquality{}).Equal(c.a, c.b); got != c.want {
			t.Errorf("Equal(%q, %q) = %t, want %t", c.a, c.b, got, c.want)
		}
	}
}

func TestParseImportKey(t *testing.T) {
	cases := []struct {
		in      string
		code    bool
		key     string
		wantErr bool
	}{
		{"code/WILLKOMMEN10", true, "WILLKOMMEN10", false},
		{"automatic/Versandkostenfrei ab 50 €", false, "Versandkostenfrei ab 50 €", false},
		{"automatic/50/50 deal", false, "50/50 deal", false},
		{"code/A/B", true, "A/B", false},
		{"code/", false, "", true},
		{"WILLKOMMEN10", false, "", true},
		{"bxgy/x", false, "", true},
		{"gid://shopify/DiscountCodeNode/1", false, "", true},
	}
	for _, c := range cases {
		code, key, err := parseImportKey(c.in)
		if (err != nil) != c.wantErr || code != c.code || key != c.key {
			t.Errorf("parseImportKey(%q) = %t, %q, %v; want %t, %q, err %t", c.in, code, key, err, c.code, c.key, c.wantErr)
		}
	}
}

func TestCheckBasic(t *testing.T) {
	cases := []struct {
		typename string
		wantErr  string
	}{
		{"DiscountCodeBasic", ""},
		{"DiscountAutomaticBasic", ""},
		{"DiscountCodeBxgy", "is a bxgy discount; use shopify_discount_bxgy"},
		{"DiscountAutomaticFreeShipping", "is a free_shipping discount; use shopify_discount_free_shipping"},
		{"DiscountCodeApp", "belongs to an app"},
		{"DiscountSomethingNew", "unknown discount type DiscountSomethingNew"},
	}
	for _, c := range cases {
		err := checkBasic(c.typename)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.typename, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("%s: got %v, want %q", c.typename, err, c.wantErr)
		}
	}
}

func TestAddRemove(t *testing.T) {
	add, remove := addRemove([]string{"a", "b", "c"}, []string{"b", "d"})
	if !reflect.DeepEqual(add, []string{"a", "c"}) || !reflect.DeepEqual(remove, []string{"d"}) {
		t.Errorf("got add %v remove %v", add, remove)
	}
	add, remove = addRemove([]string{"a"}, []string{"a"})
	if add != nil || remove != nil {
		t.Errorf("no change: got add %v remove %v", add, remove)
	}
}

func segments(ids ...string) client.DiscountContextFields {
	s := &client.DiscountContextFieldsDiscountCustomerSegments{Typename: ptr("DiscountCustomerSegments")}
	for _, id := range ids {
		s.Segments = append(s.Segments, client.DiscountContextFieldsSegmentsSegment{Id: id})
	}
	return s
}

func markets(more bool, ids ...string) client.DiscountContextFields {
	m := &client.DiscountContextFieldsDiscountMarkets{Typename: ptr("DiscountMarkets")}
	for _, id := range ids {
		m.Markets.Nodes = append(m.Markets.Nodes, client.DiscountContextFieldsMarketsMarketConnectionNodesMarket{Id: id})
	}
	m.Markets.PageInfo.HasNextPage = more
	return m
}

func TestContextFromAPI(t *testing.T) {
	cases := []struct {
		name            string
		in              client.DiscountContextFields
		want            *contextSel
		wantUnsupported string
		wantErr         string
	}{
		{"all", &client.DiscountContextFieldsDiscountBuyerSelectionAll{Typename: ptr("DiscountBuyerSelectionAll")}, &contextSel{All: ptr(true)}, "", ""},
		{"segments", segments("s1", "s2"), &contextSel{CustomerSegmentIDs: []string{"s1", "s2"}}, "", ""},
		{"markets", markets(false, "m1"), &contextSel{MarketIDs: []string{"m1"}}, "", ""},
		{"too many markets", markets(true, "m1"), nil, "", "more than 250 markets"},
		{"customers", &client.DiscountContextFieldsDiscountCustomers{Typename: ptr("DiscountCustomers")}, nil, "DiscountCustomers", ""},
		{"unknown", &client.DiscountContextFieldsDiscountContextUnknown{Typename: ptr("DiscountContextUnknown"), ContextType: "LOYALTY"}, nil, "DiscountContextUnknown (LOYALTY)", ""},
	}
	for _, c := range cases {
		got, unsupported, err := contextFromAPI(c.in)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: got err %v, want %q", c.name, err, c.wantErr)
			}
			continue
		}
		if err != nil || unsupported != c.wantUnsupported || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, %q, %v; want %+v, %q", c.name, got, unsupported, err, c.want, c.wantUnsupported)
		}
	}
}

func TestContextInput(t *testing.T) {
	all := contextSel{All: ptr(true)}
	segs := func(ids ...string) contextSel { return contextSel{CustomerSegmentIDs: ids} }
	cases := []struct {
		name string
		want contextSel
		live *contextSel
		json string
	}{
		{"create all", all, nil, `{"all":"ALL"}`},
		{"create segments", segs("a", "b"), nil, `{"customerSegments":{"add":["a","b"]}}`},
		{"all unchanged", all, &all, `null`},
		{"segments diff", segs("a", "c"), ptr(segs("a", "b")), `{"customerSegments":{"add":["c"],"remove":["b"]}}`},
		{"segments unchanged", segs("a"), ptr(segs("a")), `null`},
		{"segments to all", all, ptr(segs("a")), `{"all":"ALL"}`},
		{"all to segments", segs("a"), &all, `{"customerSegments":{"add":["a"]}}`},
		{"segments to markets", contextSel{MarketIDs: []string{"m"}}, ptr(segs("a")), `{"markets":{"add":["m"]}}`},
		{"markets diff", contextSel{MarketIDs: []string{"m2"}}, &contextSel{MarketIDs: []string{"m1"}}, `{"markets":{"add":["m2"],"remove":["m1"]}}`},
	}
	for _, c := range cases {
		if got := jsonOf(t, contextInput(c.want, c.live)); got != c.json {
			t.Errorf("%s: got %s, want %s", c.name, got, c.json)
		}
	}
}

func products(more bool, products, variants []string) client.DiscountItemsFields {
	p := &client.DiscountItemsFieldsDiscountProducts{Typename: ptr("DiscountProducts")}
	for _, id := range products {
		p.Products.Nodes = append(p.Products.Nodes, client.DiscountItemsFieldsProductsProductConnectionNodesProduct{Id: id})
	}
	for _, id := range variants {
		p.ProductVariants.Nodes = append(p.ProductVariants.Nodes, client.DiscountItemsFieldsProductVariantsProductVariantConnectionNodesProductVariant{Id: id})
	}
	p.ProductVariants.PageInfo.HasNextPage = more
	return p
}

func collections(ids ...string) client.DiscountItemsFields {
	c := &client.DiscountItemsFieldsDiscountCollections{Typename: ptr("DiscountCollections")}
	for _, id := range ids {
		c.Collections.Nodes = append(c.Collections.Nodes, client.DiscountItemsFieldsCollectionsCollectionConnectionNodesCollection{Id: id})
	}
	return c
}

func TestItemsFromAPI(t *testing.T) {
	cases := []struct {
		name    string
		in      client.DiscountItemsFields
		want    itemsSel
		wantErr string
	}{
		{"all", &client.DiscountItemsFieldsAllDiscountItems{Typename: ptr("AllDiscountItems")}, itemsSel{All: ptr(true)}, ""},
		{"products only", products(false, []string{"p1"}, nil), itemsSel{ProductIDs: []string{"p1"}}, ""},
		{"variants only", products(false, nil, []string{"v1"}), itemsSel{VariantIDs: []string{"v1"}}, ""},
		{"both", products(false, []string{"p1"}, []string{"v1"}), itemsSel{ProductIDs: []string{"p1"}, VariantIDs: []string{"v1"}}, ""},
		{"collections", collections("c1"), itemsSel{CollectionIDs: []string{"c1"}}, ""},
		{"no products", products(false, nil, nil), itemsSel{}, "selects no products"},
		{"no collections", collections(), itemsSel{}, "selects no collections"},
		{"too many", products(true, []string{"p1"}, nil), itemsSel{}, "more than 250"},
	}
	for _, c := range cases {
		got, err := itemsFromAPI(c.in)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: got err %v, want %q", c.name, err, c.wantErr)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, %v; want %+v", c.name, got, err, c.want)
		}
	}
}

func TestItemsInput(t *testing.T) {
	all := itemsSel{All: ptr(true)}
	cases := []struct {
		name string
		want itemsSel
		live *itemsSel
		json string
	}{
		{"create all", all, nil, `{"all":true}`},
		{"create products", itemsSel{ProductIDs: []string{"p"}, VariantIDs: []string{"v"}}, nil, `{"products":{"productsToAdd":["p"],"productVariantsToAdd":["v"]}}`},
		{"create collections", itemsSel{CollectionIDs: []string{"c"}}, nil, `{"collections":{"add":["c"]}}`},
		{"all unchanged", all, &all, `null`},
		{"collections diff", itemsSel{CollectionIDs: []string{"c2"}}, &itemsSel{CollectionIDs: []string{"c1"}}, `{"collections":{"add":["c2"],"remove":["c1"]}}`},
		{"collections unchanged", itemsSel{CollectionIDs: []string{"c1"}}, &itemsSel{CollectionIDs: []string{"c1"}}, `null`},
		{"products diff, variants dropped", itemsSel{ProductIDs: []string{"p1", "p2"}}, &itemsSel{ProductIDs: []string{"p1"}, VariantIDs: []string{"v1"}}, `{"products":{"productsToAdd":["p2"],"productVariantsToRemove":["v1"]}}`},
		{"collections to all", all, &itemsSel{CollectionIDs: []string{"c1"}}, `{"all":true}`},
		// Shopify refuses collections together with products, so a switch removes the old kind in the same call.
		{"products to collections", itemsSel{CollectionIDs: []string{"c"}}, &itemsSel{ProductIDs: []string{"p"}, VariantIDs: []string{"v"}}, `{"products":{"productsToRemove":["p"],"productVariantsToRemove":["v"]},"collections":{"add":["c"]}}`},
		{"collections to products", itemsSel{ProductIDs: []string{"p"}}, &itemsSel{CollectionIDs: []string{"c"}}, `{"products":{"productsToAdd":["p"]},"collections":{"remove":["c"]}}`},
		{"all to products", itemsSel{ProductIDs: []string{"p"}}, &all, `{"products":{"productsToAdd":["p"]}}`},
	}
	for _, c := range cases {
		if got := jsonOf(t, itemsInput(c.want, c.live)); got != c.json {
			t.Errorf("%s: got %s, want %s", c.name, got, c.json)
		}
	}
}

func TestMinimum(t *testing.T) {
	inputs := []struct {
		in   minimumSel
		json string
	}{
		// The other kind is always cleared, since Shopify refuses a subtotal and a quantity together.
		{minimumSel{}, `{"quantity":{"greaterThanOrEqualToQuantity":null},"subtotal":{"greaterThanOrEqualToSubtotal":null}}`},
		{minimumSel{Subtotal: ptr("30.00")}, `{"quantity":{"greaterThanOrEqualToQuantity":null},"subtotal":{"greaterThanOrEqualToSubtotal":"30.00"}}`},
		{minimumSel{Quantity: ptr(int64(3))}, `{"quantity":{"greaterThanOrEqualToQuantity":"3"},"subtotal":{"greaterThanOrEqualToSubtotal":null}}`},
	}
	for _, c := range inputs {
		if got := jsonOf(t, minimumInput(c.in)); got != c.json {
			t.Errorf("minimumInput(%+v) = %s, want %s", c.in, got, c.json)
		}
	}
	var subtotal client.DiscountMinimumFields = &client.DiscountMinimumFieldsDiscountMinimumSubtotal{
		GreaterThanOrEqualToSubtotal: client.DiscountMinimumFieldsGreaterThanOrEqualToSubtotalMoneyV2{Amount: "50.0"},
	}
	var quantity client.DiscountMinimumFields = &client.DiscountMinimumFieldsDiscountMinimumQuantity{GreaterThanOrEqualToQuantity: "3"}
	reads := []struct {
		in   *client.DiscountMinimumFields
		want minimumSel
	}{
		{nil, minimumSel{}},
		{&subtotal, minimumSel{Subtotal: ptr("50.0")}},
		{&quantity, minimumSel{Quantity: ptr(int64(3))}},
	}
	for _, c := range reads {
		got, err := minimumFromAPI(c.in)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("minimumFromAPI = %+v, %v; want %+v", got, err, c.want)
		}
	}
}

func TestValue(t *testing.T) {
	inputs := []struct {
		in   valueSel
		json string
	}{
		{valueSel{Percentage: ptr(0.1)}, `{"percentage":0.1}`},
		{valueSel{Amount: &amountSel{Amount: "5.00", AppliesOnEachItem: true}}, `{"discountAmount":{"amount":"5.00","appliesOnEachItem":true}}`},
	}
	for _, c := range inputs {
		if got := jsonOf(t, valueInput(c.in)); got != c.json {
			t.Errorf("valueInput(%+v) = %s, want %s", c.in, got, c.json)
		}
	}
	got, err := valueFromAPI(&client.DiscountCustomerGetsFieldsValueDiscountPercentage{Percentage: 0.1})
	if err != nil || !reflect.DeepEqual(got, valueSel{Percentage: ptr(0.1)}) {
		t.Errorf("percentage: got %+v, %v", got, err)
	}
	got, err = valueFromAPI(&client.DiscountCustomerGetsFieldsValueDiscountAmount{
		Amount: client.DiscountCustomerGetsFieldsValueDiscountAmountAmountMoneyV2{Amount: "5.0"}, AppliesOnEachItem: true,
	})
	if err != nil || !reflect.DeepEqual(got, valueSel{Amount: &amountSel{Amount: "5.0", AppliesOnEachItem: true}}) {
		t.Errorf("amount: got %+v, %v", got, err)
	}
	if _, err := valueFromAPI(&client.DiscountCustomerGetsFieldsValueDiscountOnQuantity{Typename: ptr("DiscountOnQuantity")}); err == nil {
		t.Error("DiscountOnQuantity must be unsupported for basic discounts")
	}
}

func TestErrorPath(t *testing.T) {
	code := func(c client.DiscountErrorCode) *client.DiscountErrorCode { return &c }
	cases := []struct {
		e    client.DiscountUserErrorFields
		want path.Path
		ok   bool
	}{
		{client.DiscountUserErrorFields{Field: []string{"automaticBasicDiscount", "title"}, Code: code(client.DiscountErrorCodeTaken)}, path.Root("title"), true},
		{client.DiscountUserErrorFields{Field: []string{"basicCodeDiscount", "code"}}, path.Root("code"), true},
		{client.DiscountUserErrorFields{Field: []string{"basicCodeDiscount", "startsAt"}}, path.Root("starts_at"), true},
		{client.DiscountUserErrorFields{Field: []string{"basicCodeDiscount", "customerGets", "value", "percentage"}}, path.Root("customer_gets").AtName("value"), true},
		{client.DiscountUserErrorFields{Field: []string{"basicCodeDiscount", "customerGets", "items", "collections", "add", "0"}}, path.Root("customer_gets").AtName("items"), true},
		{client.DiscountUserErrorFields{Field: []string{"basicCodeDiscount", "context", "customerSegments", "add"}}, path.Root("context"), true},
		{client.DiscountUserErrorFields{Field: []string{"basicCodeDiscount", "minimumRequirement", "subtotal"}}, path.Root("minimum_requirement"), true},
		{client.DiscountUserErrorFields{Code: code(client.DiscountErrorCodeEndDateBeforeStartDate)}, path.Root("ends_at"), true},
		{client.DiscountUserErrorFields{Code: code(client.DiscountErrorCodeMinimumSubtotalAndQuantityRangeBothPresent)}, path.Root("minimum_requirement"), true},
		{client.DiscountUserErrorFields{Code: code(client.DiscountErrorCodeInvalidCombinesWithForDiscountClass)}, path.Root("combines_with"), true},
		{client.DiscountUserErrorFields{Field: []string{"basicCodeDiscount", "recurringCycleLimit"}}, path.Empty(), false},
		{client.DiscountUserErrorFields{Field: []string{"id"}}, path.Empty(), false},
		{client.DiscountUserErrorFields{}, path.Empty(), false},
	}
	for _, c := range cases {
		got, ok := errorPath(c.e)
		if ok != c.ok || (ok && !got.Equal(c.want)) {
			t.Errorf("errorPath(%v) = %s, %t; want %s, %t", c.e.Field, got, ok, c.want, c.ok)
		}
	}
}

func TestUserErrorDiagsKeepMessageAndExtraInfo(t *testing.T) {
	diags := userErrorDiags("Failed", []client.DiscountUserErrorFields{
		{Field: []string{"automaticBasicDiscount", "title"}, Message: "Title must be unique for automatic discount."},
		{Message: "Something", ExtraInfo: ptr("more detail")},
	})
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics", len(diags))
	}
	if d := diags[0].Detail(); d != "Title must be unique for automatic discount." {
		t.Errorf("first detail = %q", d)
	}
	if d := diags[1].Detail(); d != "Something (more detail)" {
		t.Errorf("second detail = %q", d)
	}
}
