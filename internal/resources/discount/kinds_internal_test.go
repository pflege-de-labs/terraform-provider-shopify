package discount

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func countries(codes ...client.CountryCode) client.DiscountDestinationFields {
	return &client.DiscountDestinationFieldsDiscountCountries{Typename: ptr("DiscountCountries"), Countries: codes}
}

func TestDestination(t *testing.T) {
	all := &client.DiscountDestinationFieldsDiscountCountryAll{Typename: ptr("DiscountCountryAll"), AllCountries: true}
	got, err := destinationFromAPI(all)
	if err != nil || got.All == nil || !*got.All || got.CountryCodes != nil {
		t.Errorf("all: %+v, %v", got, err)
	}
	got, err = destinationFromAPI(countries("DE", "AT"))
	if err != nil || got.All != nil || len(got.CountryCodes) != 2 {
		t.Errorf("countries: %+v, %v", got, err)
	}
	// An empty list stays non-null, so state keeps the countries kind (no country) and generated config validates.
	got, err = destinationFromAPI(countries())
	if err != nil || got.CountryCodes == nil || len(got.CountryCodes) != 0 {
		t.Errorf("no countries: %+v, %v", got, err)
	}
	if _, err := destinationFromAPI(nil); err == nil {
		t.Error("an unknown destination must fail")
	}

	allSel, de, deAt := destinationSel{All: ptr(true)}, destinationSel{CountryCodes: []string{"DE"}}, destinationSel{CountryCodes: []string{"DE", "AT"}}
	cases := []struct {
		name string
		want destinationSel
		live *destinationSel
		json string
	}{
		{"create all", allSel, nil, `{"all":true}`},
		{"create countries", deAt, nil, `{"countries":{"add":["AT","DE"]}}`},
		{"all unchanged", allSel, &allSel, `null`},
		{"countries unchanged", de, &de, `null`},
		{"countries diff", de, &deAt, `{"countries":{"remove":["AT"]}}`},
		{"countries to all", allSel, &de, `{"all":true}`},
		{"all to countries", de, &allSel, `{"countries":{"add":["DE"]}}`},
	}
	for _, c := range cases {
		sameJSON(t, destinationInput(c.want, c.live), c.json)
	}
}

func freeShippingNode(code bool) *client.DiscountNodeFields {
	if !code {
		return &client.DiscountNodeFields{Id: "gid://shopify/DiscountAutomaticNode/5", Discount: &client.DiscountNodeFieldsDiscountDiscountAutomaticFreeShipping{
			Typename: ptr("DiscountAutomaticFreeShipping"), Title: "Versand", StartsAt: "2026-11-01T00:00:00Z", Status: client.DiscountStatusActive,
			AppliesOnOneTimePurchase: true, Context: &client.DiscountContextFieldsDiscountBuyerSelectionAll{Typename: ptr("DiscountBuyerSelectionAll")},
			DestinationSelection: &client.DiscountDestinationFieldsDiscountCountryAll{Typename: ptr("DiscountCountryAll"), AllCountries: true},
		}}
	}
	d := &client.DiscountNodeFieldsDiscountDiscountCodeFreeShipping{
		Typename: ptr("DiscountCodeFreeShipping"), Title: "Versand", StartsAt: "2026-11-01T00:00:00Z", Status: client.DiscountStatusActive,
		UsageLimit: ptr(5), AppliesOncePerCustomer: true, AppliesOnOneTimePurchase: true,
		CodesCount:           &client.DiscountNodeFieldsDiscountDiscountCodeFreeShippingCodesCount{Count: 1},
		MaximumShippingPrice: &client.DiscountNodeFieldsDiscountDiscountCodeFreeShippingMaximumShippingPriceMoneyV2{Amount: "20.0"},
		Context:              &client.DiscountContextFieldsDiscountBuyerSelectionAll{Typename: ptr("DiscountBuyerSelectionAll")},
		DestinationSelection: countries("DE"),
	}
	d.Codes.Nodes = []client.DiscountNodeFieldsDiscountDiscountCodeFreeShippingCodesDiscountRedeemCodeConnectionNodesDiscountRedeemCode{{Code: "FREE"}}
	return &client.DiscountNodeFields{Id: "gid://shopify/DiscountCodeNode/4", Discount: d}
}

func TestFreeShippingSetFromAPI(t *testing.T) {
	ctx := context.Background()
	n, err := nodeOf(freeShippingNode(true))
	if err != nil || n.Method != methodFreeShipping || !n.IsCode || *n.Code != "FREE" || n.CodesCount != 1 || n.MaximumShippingPrice == nil {
		t.Fatalf("node = %+v, %v", n, err)
	}
	var m freeShippingModel
	if _, d := m.setFromAPI(ctx, n); d.HasError() {
		t.Fatal(d)
	}
	if want := object(t, destinationAttrTypes(), destinationSel{CountryCodes: []string{"DE"}}); !m.Destination.Equal(want) {
		t.Errorf("destination = %s, want %s", m.Destination, want)
	}
	if m.MaximumShippingPrice.ValueString() != "20.0" || m.UsageLimit.ValueInt64() != 5 || !m.AppliesOnOneTimePurchase.ValueBool() || m.AppliesOnSubscription.ValueBool() {
		t.Errorf("price %s, limit %s, flags %s %s", m.MaximumShippingPrice, m.UsageLimit, m.AppliesOnOneTimePurchase, m.AppliesOnSubscription)
	}
	if want := object(t, minimumAttrTypes(), minimumSel{}); !m.MinimumRequirement.Equal(want) {
		t.Errorf("no minimum must read as {}, got %s", m.MinimumRequirement)
	}

	an, err := nodeOf(freeShippingNode(false))
	if err != nil || an.IsCode {
		t.Fatalf("automatic node = %+v, %v", an, err)
	}
	var am freeShippingModel
	if _, d := am.setFromAPI(ctx, an); d.HasError() {
		t.Fatal(d)
	}
	if !am.MaximumShippingPrice.IsNull() || !am.Code.IsNull() || !am.UsageLimit.IsNull() {
		t.Errorf("automatic: price %s, code %s, limit %s", am.MaximumShippingPrice, am.Code, am.UsageLimit)
	}
	if want := object(t, destinationAttrTypes(), destinationSel{All: ptr(true)}); !am.Destination.Equal(want) {
		t.Errorf("destination = %s, want %s", am.Destination, want)
	}
}

// unsetFreeShipping is a config with only the title set.
func unsetFreeShipping(code *string) freeShippingModel {
	return freeShippingModel{
		commonModel: commonModel{
			Title: types.StringValue("T"), Code: types.StringPointerValue(code), StartsAt: instantNull(), EndsAt: instantNull(),
			Context: types.ObjectNull(contextAttrTypes()), CombinesWith: types.ObjectNull(combinesAttrTypes()),
			UsageLimit: types.Int64Null(), AppliesOncePerCustomer: types.BoolNull(),
		},
		Destination: types.ObjectNull(destinationAttrTypes()), MaximumShippingPrice: decimalNull(), MinimumRequirement: types.ObjectNull(minimumAttrTypes()),
		AppliesOnSubscription: types.BoolNull(), AppliesOnOneTimePurchase: types.BoolNull(),
	}
}

func TestFreeShippingInput(t *testing.T) {
	fixedNow(t)
	ctx := context.Background()
	// Unset attributes are never sent, notably the subscription flags, which a shop without subscriptions refuses.
	m := unsetFreeShipping(nil)
	in, d := m.automaticInput(ctx, nil)
	if d.HasError() {
		t.Fatal(d)
	}
	sameJSON(t, in, `{"title":"T","startsAt":"2026-10-27T10:00:00Z"}`)

	m = unsetFreeShipping(ptr("FREE"))
	m.Destination = object(t, destinationAttrTypes(), destinationSel{CountryCodes: []string{"DE"}})
	m.MaximumShippingPrice = decimalString("20.00")
	m.AppliesOnSubscription, m.AppliesOnOneTimePurchase = types.BoolValue(true), types.BoolValue(false)
	m.MinimumRequirement = object(t, minimumAttrTypes(), minimumSel{Subtotal: ptr("50")})
	cin, d := m.codeInput(ctx, nil)
	if d.HasError() {
		t.Fatal(d)
	}
	sameJSON(t, cin, `{"title":"T","code":"FREE","startsAt":"2026-10-27T10:00:00Z","context":{"all":"ALL"},
		"destination":{"countries":{"add":["DE"]}},"maximumShippingPrice":"20.00","appliesOnSubscription":true,"appliesOnOneTimePurchase":false,
		"minimumRequirement":{"quantity":{"greaterThanOrEqualToQuantity":null},"subtotal":{"greaterThanOrEqualToSubtotal":"50"}}}`)

	// An update sends the destination difference against the live countries.
	live, _ := nodeOf(freeShippingNode(true))
	m.Destination = object(t, destinationAttrTypes(), destinationSel{CountryCodes: []string{"DE", "AT"}})
	uin, d := m.codeInput(ctx, live)
	if d.HasError() {
		t.Fatal(d)
	}
	sameJSON(t, uin.Destination, `{"countries":{"add":["AT"]}}`)
}

func bxgyItems(ids ...string) bxgyItemsSel { return bxgyItemsSel{ProductIDs: ids} }

func bxgyNode(code bool) *client.DiscountNodeFields {
	buys := client.DiscountCustomerBuysFields{Value: &client.DiscountCustomerBuysFieldsValueDiscountQuantity{Quantity: "2"}, Items: products(false, []string{"p1"}, nil)}
	gets := client.DiscountCustomerGetsFields{
		Value: &client.DiscountCustomerGetsFieldsValueDiscountOnQuantity{
			Quantity: client.DiscountCustomerGetsFieldsValueDiscountOnQuantityQuantityDiscountQuantity{Quantity: "1"},
			Effect:   &client.DiscountCustomerGetsFieldsValueDiscountOnQuantityEffectDiscountPercentage{Percentage: 1},
		},
		Items: collections("c1"),
	}
	all := &client.DiscountContextFieldsDiscountBuyerSelectionAll{Typename: ptr("DiscountBuyerSelectionAll")}
	if !code {
		return &client.DiscountNodeFields{Id: "gid://shopify/DiscountAutomaticNode/7", Discount: &client.DiscountNodeFieldsDiscountDiscountAutomaticBxgy{
			Typename: ptr("DiscountAutomaticBxgy"), Title: "2 for 1", StartsAt: "2026-11-01T00:00:00Z", Status: client.DiscountStatusActive,
			UsesPerOrderLimit: ptr(3), Context: all, CustomerBuys: buys, CustomerGets: gets,
		}}
	}
	d := &client.DiscountNodeFieldsDiscountDiscountCodeBxgy{
		Typename: ptr("DiscountCodeBxgy"), Title: "2 for 1", StartsAt: "2026-11-01T00:00:00Z", Status: client.DiscountStatusActive, UsageLimit: ptr(7),
		CodesCount: &client.DiscountNodeFieldsDiscountDiscountCodeBxgyCodesCount{Count: 1}, Context: all, CustomerBuys: buys, CustomerGets: gets,
	}
	d.Codes.Nodes = []client.DiscountNodeFieldsDiscountDiscountCodeBxgyCodesDiscountRedeemCodeConnectionNodesDiscountRedeemCode{{Code: "BX"}}
	return &client.DiscountNodeFields{Id: "gid://shopify/DiscountCodeNode/6", Discount: d}
}

func autoBxgy(t *testing.T, n *client.DiscountNodeFields) *client.DiscountNodeFieldsDiscountDiscountAutomaticBxgy {
	t.Helper()
	d, ok := n.Discount.(*client.DiscountNodeFieldsDiscountDiscountAutomaticBxgy)
	if !ok {
		t.Fatalf("discount is %T", n.Discount)
	}
	return d
}

func TestBxgySetFromAPI(t *testing.T) {
	ctx := context.Background()
	for _, code := range []bool{true, false} {
		n, err := nodeOf(bxgyNode(code))
		if err != nil || n.Method != methodBxgy || n.IsCode != code || n.CustomerBuys == nil || n.CustomerGets == nil {
			t.Fatalf("node = %+v, %v", n, err)
		}
		var m bxgyModel
		if _, d := m.setFromAPI(ctx, n); d.HasError() {
			t.Fatal(d)
		}
		q := int64(2)
		if want := object(t, bxgyBuysAttrTypes(), bxgyBuysSel{Quantity: &q, Items: bxgyItems("p1")}); !m.CustomerBuys.Equal(want) {
			t.Errorf("buys = %s, want %s", m.CustomerBuys, want)
		}
		if want := object(t, bxgyGetsAttrTypes(), bxgyGetsSel{Quantity: 1, Percentage: ptr(1.0), Items: bxgyItemsSel{CollectionIDs: []string{"c1"}}}); !m.CustomerGets.Equal(want) {
			t.Errorf("gets = %s, want %s", m.CustomerGets, want)
		}
		if code && (m.UsageLimit.ValueInt64() != 7 || !m.UsesPerOrderLimit.IsNull()) || !code && m.UsesPerOrderLimit.ValueInt64() != 3 {
			t.Errorf("code %t: usage limit %s, per order %s", code, m.UsageLimit, m.UsesPerOrderLimit)
		}
	}

	// Amount variants read as amount, not quantity or percentage.
	amt := bxgyNode(false)
	d := autoBxgy(t, amt)
	d.CustomerBuys.Value = &client.DiscountCustomerBuysFieldsValueDiscountPurchaseAmount{Amount: "30.0"}
	d.CustomerGets.Value = &client.DiscountCustomerGetsFieldsValueDiscountOnQuantity{
		Quantity: client.DiscountCustomerGetsFieldsValueDiscountOnQuantityQuantityDiscountQuantity{Quantity: "2"},
		Effect: &client.DiscountCustomerGetsFieldsValueDiscountOnQuantityEffectDiscountAmount{
			Amount: client.DiscountCustomerGetsFieldsValueDiscountOnQuantityEffectDiscountAmountAmountMoneyV2{Amount: "5.0"},
		},
	}
	n, _ := nodeOf(amt)
	buys, err1 := buysFromAPI(n.CustomerBuys)
	gets, err2 := getsFromAPI(n.CustomerGets)
	if err1 != nil || err2 != nil || buys.Quantity != nil || *buys.Amount != "30.0" || gets.Percentage != nil || *gets.Amount != "5.0" || gets.Quantity != 2 {
		t.Errorf("amounts: %+v %+v, %v %v", buys, gets, err1, err2)
	}

	// Shapes the schema can't hold fail the mapping, so import errors and list skips.
	all := bxgyNode(false)
	autoBxgy(t, all).CustomerBuys.Items = &client.DiscountItemsFieldsAllDiscountItems{}
	an, _ := nodeOf(all)
	if _, d := (&bxgyModel{}).setFromAPI(ctx, an); !d.HasError() || !strings.Contains(d[0].Detail(), "all items") {
		t.Errorf("all items: got %v", d)
	}
	plain := bxgyNode(false)
	autoBxgy(t, plain).CustomerGets.Value = &client.DiscountCustomerGetsFieldsValueDiscountPercentage{Percentage: 0.1}
	pn, _ := nodeOf(plain)
	if _, err := getsFromAPI(pn.CustomerGets); err == nil {
		t.Error("a customer gets that is not a quantity discount must fail")
	}
}

func TestBxgyInputs(t *testing.T) {
	// Switching the buys value sends the unused kind as an explicit null: Shopify rejects quantity to amount otherwise.
	live, _ := nodeOf(bxgyNode(false))
	amount := "20"
	in, err := buysInput(bxgyBuysSel{Amount: &amount, Items: bxgyItems("p1")}, live.CustomerBuys)
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, in, `{"value":{"quantity":null,"amount":"20"}}`)
	q := int64(3)
	in, _ = buysInput(bxgyBuysSel{Quantity: &q, Items: bxgyItems("p1", "p2")}, live.CustomerBuys)
	sameJSON(t, in, `{"value":{"quantity":"3","amount":null},"items":{"products":{"productsToAdd":["p2"]}}}`)
	in, _ = buysInput(bxgyBuysSel{Quantity: &q, Items: bxgyItems("p1")}, nil)
	sameJSON(t, in, `{"value":{"quantity":"3","amount":null},"items":{"products":{"productsToAdd":["p1"]}}}`)

	pct := 1.0
	gin, err := getsInput(bxgyGetsSel{Quantity: 1, Percentage: &pct, Items: bxgyItemsSel{CollectionIDs: []string{"c1"}}}, live.CustomerGets)
	if err != nil {
		t.Fatal(err)
	}
	// Items unchanged: none are sent.
	sameJSON(t, gin, `{"value":{"discountOnQuantity":{"quantity":"1","effect":{"percentage":1}}}}`)
	off := "5"
	gin, _ = getsInput(bxgyGetsSel{Quantity: 2, Amount: &off, Items: bxgyItemsSel{CollectionIDs: []string{"c1"}}}, live.CustomerGets)
	sameJSON(t, gin, `{"value":{"discountOnQuantity":{"quantity":"2","effect":{"amount":"5"}}}}`)
	// Products to collections replaces the kind, as for basic.
	gin, _ = getsInput(bxgyGetsSel{Quantity: 1, Percentage: &pct, Items: bxgyItems("p9")}, live.CustomerGets)
	sameJSON(t, gin.Items, `{"products":{"productsToAdd":["p9"]},"collections":{"remove":["c1"]}}`)

	// The code input takes the per-order limit as an int, the automatic one as a string.
	ctx := context.Background()
	m := bxgyModel{
		commonModel:       unsetFreeShipping(ptr("BX")).commonModel,
		CustomerBuys:      object(t, bxgyBuysAttrTypes(), bxgyBuysSel{Quantity: &q, Items: bxgyItems("p1")}),
		CustomerGets:      object(t, bxgyGetsAttrTypes(), bxgyGetsSel{Quantity: 1, Percentage: &pct, Items: bxgyItems("p1")}),
		UsesPerOrderLimit: types.Int64Value(2),
	}
	fixedNow(t)
	cin, d := m.codeInput(ctx, nil)
	if d.HasError() {
		t.Fatal(d)
	}
	if cin.UsesPerOrderLimit == nil || *cin.UsesPerOrderLimit != 2 {
		t.Errorf("code per order limit = %v", cin.UsesPerOrderLimit)
	}
	m.Code = types.StringNull()
	ain, d := m.automaticInput(ctx, nil)
	if d.HasError() {
		t.Fatal(d)
	}
	if ain.UsesPerOrderLimit == nil || *ain.UsesPerOrderLimit != "2" {
		t.Errorf("automatic per order limit = %v", ain.UsesPerOrderLimit)
	}
}
