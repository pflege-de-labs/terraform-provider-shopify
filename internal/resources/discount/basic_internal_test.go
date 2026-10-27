package discount

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func object(t *testing.T, at map[string]attr.Type, v any) types.Object {
	t.Helper()
	o, d := types.ObjectValueFrom(context.Background(), at, v)
	if d.HasError() {
		t.Fatal(d)
	}
	return o
}

// unsetBasic is a config with only the required attributes set.
func unsetBasic(t *testing.T, code *string) basicModel {
	t.Helper()
	return basicModel{
		commonModel: commonModel{
			ID: types.StringNull(), Title: types.StringValue("T"), Code: types.StringPointerValue(code),
			StartsAt: instantNull(), EndsAt: instantNull(), Status: types.StringNull(),
			Context: types.ObjectNull(contextAttrTypes()), CombinesWith: types.ObjectNull(combinesAttrTypes()),
			UsageLimit: types.Int64Null(), AppliesOncePerCustomer: types.BoolNull(), AdoptExisting: types.BoolNull(),
		},
		CustomerGets: object(t, customerGetsAttrTypes(), customerGetsSel{
			Value: valueSel{Percentage: ptr(0.1)},
			Items: itemsSel{All: ptr(true)},
		}),
		MinimumRequirement: types.ObjectNull(minimumAttrTypes()),
	}
}

func sameJSON(t *testing.T, got any, want string) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal([]byte(jsonOf(t, got)), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("got %s\nwant %s", jsonOf(t, got), want)
	}
}

func fixedNow(t *testing.T) {
	t.Helper()
	old := now
	now = func() time.Time { return time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = old })
}

func TestCreateInputSendsOnlyConfigured(t *testing.T) {
	fixedNow(t)
	ctx := context.Background()
	m := unsetBasic(t, ptr("WILLKOMMEN10"))
	in, d := m.codeInput(ctx, nil)
	if d.HasError() {
		t.Fatal(d)
	}
	// starts_at and a code discount's context are required on create, so they get now and all buyers.
	sameJSON(t, in, `{"title":"T","code":"WILLKOMMEN10","startsAt":"2026-10-27T10:00:00Z","context":{"all":"ALL"},
		"customerGets":{"value":{"percentage":0.1},"items":{"all":true}}}`)

	auto := unsetBasic(t, nil)
	ain, d := auto.automaticInput(ctx, nil)
	if d.HasError() {
		t.Fatal(d)
	}
	sameJSON(t, ain, `{"title":"T","startsAt":"2026-10-27T10:00:00Z","customerGets":{"value":{"percentage":0.1},"items":{"all":true}}}`)
}

func TestCreateInputSendsEveryConfiguredAttribute(t *testing.T) {
	ctx := context.Background()
	m := unsetBasic(t, ptr("C"))
	m.StartsAt = instantString("2026-11-01T00:00:00+01:00")
	m.EndsAt = instantString("2026-12-31T23:59:59+01:00")
	m.Context = object(t, contextAttrTypes(), contextSel{CustomerSegmentIDs: []string{"s1"}})
	m.CombinesWith = object(t, combinesAttrTypes(), combinesSel{ShippingDiscounts: true})
	m.UsageLimit = types.Int64Value(1000)
	m.AppliesOncePerCustomer = types.BoolValue(true)
	m.MinimumRequirement = object(t, minimumAttrTypes(), minimumSel{Subtotal: ptr("30.00")})
	m.CustomerGets = object(t, customerGetsAttrTypes(), customerGetsSel{
		Value: valueSel{Amount: &amountSel{Amount: "5", AppliesOnEachItem: false}},
		Items: itemsSel{CollectionIDs: []string{"c1"}},
	})
	in, d := m.codeInput(ctx, nil)
	if d.HasError() {
		t.Fatal(d)
	}
	sameJSON(t, in, `{"title":"T","code":"C","startsAt":"2026-11-01T00:00:00+01:00","endsAt":"2026-12-31T23:59:59+01:00",
		"context":{"customerSegments":{"add":["s1"]}},
		"combinesWith":{"productDiscounts":false,"orderDiscounts":false,"shippingDiscounts":true},
		"usageLimit":1000,"appliesOncePerCustomer":true,
		"minimumRequirement":{"quantity":{"greaterThanOrEqualToQuantity":null},"subtotal":{"greaterThanOrEqualToSubtotal":"30.00"}},
		"customerGets":{"value":{"discountAmount":{"amount":"5","appliesOnEachItem":false}},"items":{"collections":{"add":["c1"]}}}}`)
}

func liveNode() *node {
	return &node{
		ID: "gid://shopify/DiscountCodeNode/1", Typename: "DiscountCodeBasic", Method: methodBasic, IsCode: true,
		Title: "T", Code: ptr("C"), CodesCount: 1, StartsAt: "2026-10-26T11:00:00Z", Status: "ACTIVE",
		Context:      segments("s1"),
		CustomerGets: &client.DiscountCustomerGetsFields{Value: &client.DiscountCustomerGetsFieldsValueDiscountPercentage{Percentage: 0.1}, Items: collections("c1")},
	}
}

func TestUpdateInputSendsDifferences(t *testing.T) {
	ctx := context.Background()
	m := unsetBasic(t, ptr("C"))
	m.Context = object(t, contextAttrTypes(), contextSel{All: ptr(true)})
	m.CustomerGets = object(t, customerGetsAttrTypes(), customerGetsSel{
		Value: valueSel{Percentage: ptr(0.2)},
		Items: itemsSel{CollectionIDs: []string{"c1"}},
	})
	in, d := m.codeInput(ctx, liveNode())
	if d.HasError() {
		t.Fatal(d)
	}
	// Same code and same collections: neither is sent; starts_at stays unmanaged on update.
	sameJSON(t, in, `{"title":"T","context":{"all":"ALL"},"customerGets":{"value":{"percentage":0.2}}}`)

	m.Code = types.StringValue("RENAMED")
	in, d = m.codeInput(ctx, liveNode())
	if d.HasError() || in.Code == nil || *in.Code != "RENAMED" {
		t.Errorf("rename: got %v, %v", in.Code, d)
	}
	multi := liveNode()
	multi.CodesCount = 3
	if _, d = m.codeInput(ctx, multi); !d.HasError() || !strings.Contains(d[0].Detail(), "3 codes") {
		t.Errorf("rename with several codes must fail, got %v", d)
	}
}

func TestUpdateInputRefusesUnsupportedLiveContext(t *testing.T) {
	m := unsetBasic(t, ptr("C"))
	m.Context = object(t, contextAttrTypes(), contextSel{All: ptr(true)})
	live := liveNode()
	live.Context = &client.DiscountContextFieldsDiscountCustomers{Typename: ptr("DiscountCustomers")}
	if _, d := m.codeInput(context.Background(), live); !d.HasError() || !strings.Contains(d[0].Detail(), "DiscountCustomers") {
		t.Errorf("want an error naming the context type, got %v", d)
	}
	// Unset context leaves the unsupported one alone.
	m.Context = types.ObjectNull(contextAttrTypes())
	if _, d := m.codeInput(context.Background(), live); d.HasError() {
		t.Errorf("unset context must not fail: %v", d)
	}
}

func codeBasicNode() *client.DiscountNodeFields {
	var minimum client.DiscountMinimumFields = &client.DiscountMinimumFieldsDiscountMinimumSubtotal{
		GreaterThanOrEqualToSubtotal: client.DiscountMinimumFieldsGreaterThanOrEqualToSubtotalMoneyV2{Amount: "30.0"},
	}
	d := &client.DiscountNodeFieldsDiscountDiscountCodeBasic{
		Typename: ptr("DiscountCodeBasic"), Title: "Newsletter", StartsAt: "2026-10-31T23:00:00Z", EndsAt: ptr("2026-12-31T22:59:59Z"),
		Status: client.DiscountStatusScheduled, UsageLimit: ptr(1000), AppliesOncePerCustomer: true,
		CodesCount:         &client.DiscountNodeFieldsDiscountDiscountCodeBasicCodesCount{Count: 1},
		CombinesWith:       client.DiscountCombinesWithFields{ShippingDiscounts: true},
		Context:            segments("s1"),
		CustomerGets:       client.DiscountCustomerGetsFields{Value: &client.DiscountCustomerGetsFieldsValueDiscountPercentage{Percentage: 0.1}, Items: collections("c1")},
		MinimumRequirement: &minimum,
	}
	d.Codes.Nodes = []client.DiscountNodeFieldsDiscountDiscountCodeBasicCodesDiscountRedeemCodeConnectionNodesDiscountRedeemCode{{Code: "WILLKOMMEN10"}}
	return &client.DiscountNodeFields{Id: "gid://shopify/DiscountCodeNode/1", Discount: d}
}

func codeBasic(t *testing.T, n *client.DiscountNodeFields) *client.DiscountNodeFieldsDiscountDiscountCodeBasic {
	t.Helper()
	d, ok := n.Discount.(*client.DiscountNodeFieldsDiscountDiscountCodeBasic)
	if !ok {
		t.Fatalf("discount is %T", n.Discount)
	}
	return d
}

func TestNodeOf(t *testing.T) {
	n, err := nodeOf(codeBasicNode())
	if err != nil {
		t.Fatal(err)
	}
	if !n.IsCode || n.Method != methodBasic || *n.Code != "WILLKOMMEN10" || n.CodesCount != 1 || n.Title != "Newsletter" {
		t.Errorf("code node = %+v", n)
	}
	noCode := codeBasicNode()
	codeBasic(t, noCode).Codes.Nodes = nil
	// Delete still needs the node, so only the mapping refuses a code discount without codes.
	nc, err := nodeOf(noCode)
	if err != nil || nc.Code != nil {
		t.Errorf("code discount without codes: got %+v, %v", nc, err)
	}
	if _, d := (&basicModel{}).setFromAPI(context.Background(), nc); !d.HasError() || !strings.Contains(d[0].Detail(), "no code") {
		t.Errorf("mapping a code discount without codes: got %v", d)
	}
	bxgy, err := nodeOf(&client.DiscountNodeFields{Id: "x", Discount: &client.DiscountNodeFieldsDiscountDiscountAutomaticBxgy{Typename: ptr("DiscountAutomaticBxgy"), Title: "B"}})
	if err != nil || bxgy.Method != "bxgy" || bxgy.IsCode || bxgy.Title != "B" {
		t.Errorf("bxgy node = %+v, %v", bxgy, err)
	}
	if _, err := nodeOf(&client.DiscountNodeFields{Id: "x"}); err == nil {
		t.Error("a node without discount must fail")
	}
}

func TestSetFromAPI(t *testing.T) {
	ctx := context.Background()
	n, err := nodeOf(codeBasicNode())
	if err != nil {
		t.Fatal(err)
	}
	var m basicModel
	unsupported, d := m.setFromAPI(ctx, n)
	if d.HasError() || unsupported != "" {
		t.Fatal(d, unsupported)
	}
	checks := map[string][2]attr.Value{
		"id":         {m.ID, types.StringValue("gid://shopify/DiscountCodeNode/1")},
		"code":       {m.Code, types.StringValue("WILLKOMMEN10")},
		"starts_at":  {m.StartsAt, instantString("2026-10-31T23:00:00Z")},
		"ends_at":    {m.EndsAt, instantString("2026-12-31T22:59:59Z")},
		"status":     {m.Status, types.StringValue("SCHEDULED")},
		"limit":      {m.UsageLimit, types.Int64Value(1000)},
		"once":       {m.AppliesOncePerCustomer, types.BoolValue(true)},
		"context":    {m.Context, object(t, contextAttrTypes(), contextSel{CustomerSegmentIDs: []string{"s1"}})},
		"combines":   {m.CombinesWith, object(t, combinesAttrTypes(), combinesSel{ShippingDiscounts: true})},
		"minimum":    {m.MinimumRequirement, object(t, minimumAttrTypes(), minimumSel{Subtotal: ptr("30.0")})},
		"customer":   {m.CustomerGets, object(t, customerGetsAttrTypes(), customerGetsSel{Value: valueSel{Percentage: ptr(0.1)}, Items: itemsSel{CollectionIDs: []string{"c1"}}})},
		"adopt_kept": {m.AdoptExisting, types.BoolNull()},
	}
	for name, c := range checks {
		if !c[0].Equal(c[1]) {
			t.Errorf("%s = %s, want %s", name, c[0], c[1])
		}
	}

	// Automatic: code-only attributes are null, no minimum reads as {}.
	auto := &client.DiscountNodeFields{Id: "gid://shopify/DiscountAutomaticNode/2", Discount: &client.DiscountNodeFieldsDiscountDiscountAutomaticBasic{
		Typename: ptr("DiscountAutomaticBasic"), Title: "Auto", StartsAt: "2026-10-26T11:00:00Z", Status: client.DiscountStatusActive,
		Context:      &client.DiscountContextFieldsDiscountCustomers{Typename: ptr("DiscountCustomers")},
		CustomerGets: client.DiscountCustomerGetsFields{Value: &client.DiscountCustomerGetsFieldsValueDiscountPercentage{Percentage: 0.5}, Items: &client.DiscountItemsFieldsAllDiscountItems{}},
	}}
	an, err := nodeOf(auto)
	if err != nil {
		t.Fatal(err)
	}
	var am basicModel
	unsupported, d = am.setFromAPI(ctx, an)
	if d.HasError() {
		t.Fatal(d)
	}
	if unsupported != "DiscountCustomers" || !am.Context.IsNull() {
		t.Errorf("unsupported context: got %q, %s", unsupported, am.Context)
	}
	if !am.Code.IsNull() || !am.UsageLimit.IsNull() || !am.AppliesOncePerCustomer.IsNull() || !am.EndsAt.IsNull() {
		t.Errorf("automatic: code %s, limit %s, once %s, ends %s", am.Code, am.UsageLimit, am.AppliesOncePerCustomer, am.EndsAt)
	}
	if want := object(t, minimumAttrTypes(), minimumSel{}); !am.MinimumRequirement.Equal(want) {
		t.Errorf("minimum = %s, want %s", am.MinimumRequirement, want)
	}

	// Shapes the schema can't hold are errors, so list and import skip or refuse them.
	bad := codeBasicNode()
	codeBasic(t, bad).CustomerGets.Items = collections()
	bn, _ := nodeOf(bad)
	if _, d := (&basicModel{}).setFromAPI(ctx, bn); !d.HasError() {
		t.Error("a selection of no collections must be an error")
	}
}

func TestDisplayName(t *testing.T) {
	n := &node{Title: "T", IsCode: true, Code: ptr("C"), CodesCount: 1}
	if got := displayName(n); got != "T (code C)" {
		t.Errorf("got %q", got)
	}
	n.CodesCount = 4
	if got := displayName(n); got != "T (code C, +3 codes)" {
		t.Errorf("got %q", got)
	}
	if got := displayName(&node{Title: "A"}); got != "A (automatic)" {
		t.Errorf("got %q", got)
	}
}
