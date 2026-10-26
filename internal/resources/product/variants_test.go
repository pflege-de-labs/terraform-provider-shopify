package product

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

type liveOption struct {
	id, name string
	values   [][2]string // id, name
}

type liveVariant struct {
	id, price string
	sku       *string
	opts      [][2]string // option name, value
}

func liveProduct(defaultOnly bool, opts []liveOption, vs []liveVariant) *client.ProductFields {
	p := &client.ProductFields{Id: "gid://shopify/Product/1", Handle: "h", HasOnlyDefaultVariant: defaultOnly}
	for _, o := range opts {
		po := client.ProductFieldsOptionsProductOption{Id: o.id, Name: o.name}
		for _, v := range o.values {
			po.OptionValues = append(po.OptionValues, client.ProductFieldsOptionsProductOptionOptionValuesProductOptionValue{Id: v[0], Name: v[1]})
		}
		p.Options = append(p.Options, po)
	}
	for _, v := range vs {
		vf := client.VariantFields{Id: v.id, Price: v.price, Sku: v.sku, InventoryPolicy: "DENY", Taxable: true}
		vf.InventoryItem.Id = strings.Replace(v.id, "ProductVariant", "InventoryItem", 1)
		for _, so := range v.opts {
			vf.SelectedOptions = append(vf.SelectedOptions, client.VariantFieldsSelectedOptionsSelectedOption{Name: so[0], Value: so[1]})
		}
		p.Variants.Nodes = append(p.Variants.Nodes, client.ProductFieldsVariantsProductVariantConnectionNodesProductVariant{VariantFields: vf})
	}
	return p
}

// sizeSM is a live product with option Size (S, M) and one variant per value.
func sizeSM() *client.ProductFields {
	return liveProduct(false,
		[]liveOption{{"opt-size", "Size", [][2]string{{"val-s", "S"}, {"val-m", "M"}}}},
		[]liveVariant{
			{id: "gid://shopify/ProductVariant/1", price: "10.00", opts: [][2]string{{"Size", "S"}}},
			{id: "gid://shopify/ProductVariant/2", price: "12.00", sku: ptr("ADMIN"), opts: [][2]string{{"Size", "M"}}},
		})
}

func optionsList(t *testing.T, opts ...option) types.List {
	t.Helper()
	l, d := types.ListValueFrom(context.Background(), optionType, toOptionModels(opts))
	if d.HasError() {
		t.Fatal(d)
	}
	return l
}

// cfgVariant is a configured variant: only option values and the given fields set.
func cfgVariant(c combo, set func(*variantModel)) variantModel {
	v := variantModel{
		OptionValues: types.MapValueMust(types.StringType, map[string]attr.Value{}),
		ID:           types.StringNull(), InventoryItemID: types.StringNull(), Price: types.StringNull(), CompareAtPrice: types.StringNull(),
		SKU: types.StringNull(), Barcodes: types.ListNull(barcodeType), InventoryPolicy: types.StringNull(), Taxable: types.BoolNull(),
		Tracked: types.BoolNull(), RequiresShipping: types.BoolNull(), Weight: types.ObjectNull(weightAttrTypes),
	}
	vals := map[string]attr.Value{}
	for k, val := range c {
		vals[k] = types.StringValue(val)
	}
	v.OptionValues = types.MapValueMust(types.StringType, vals)
	if set != nil {
		set(&v)
	}
	return v
}

func variantsList(t *testing.T, vs ...variantModel) types.List {
	t.Helper()
	l, d := types.ListValueFrom(context.Background(), variantType, vs)
	if d.HasError() {
		t.Fatal(d)
	}
	return l
}

// inputFor builds the productSet input for config options and variants against live, as JSON.
func inputFor(t *testing.T, live *client.ProductFields, opts types.List, vs ...variantModel) (string, string, []string) {
	t.Helper()
	m := unsetModel()
	m.Options, m.Variants = opts, variantsList(t, vs...)
	in, removed, diags := buildInput(context.Background(), &m, live)
	if diags.HasError() {
		t.Fatal(diags)
	}
	o, _ := json.Marshal(in.ProductOptions)
	v, _ := json.Marshal(in.Variants)
	return string(o), string(v), removed
}

func TestBuildInputRenameSendsLiveIDs(t *testing.T) {
	o, v, removed := inputFor(t, sizeSM(), optionsList(t, option{"Größe", []string{"S", "Medium", "L"}}),
		cfgVariant(combo{"Größe": "S"}, func(v *variantModel) { v.Price = types.StringValue("10") }),
		cfgVariant(combo{"Größe": "Medium"}, nil),
		cfgVariant(combo{"Größe": "L"}, nil),
	)
	wantO := `[{"id":"opt-size","name":"Größe","position":1,"values":[{"id":"val-s","name":"S"},{"id":"val-m","name":"Medium"},{"name":"L"}]}]`
	if o != wantO {
		t.Errorf("productOptions = %s\nwant %s", o, wantO)
	}
	wantV := `[{"id":"gid://shopify/ProductVariant/1","optionValues":[{"name":"S","optionName":"Größe"}],"price":"10","position":1},` +
		`{"id":"gid://shopify/ProductVariant/2","optionValues":[{"name":"Medium","optionName":"Größe"}],"position":2},` +
		`{"optionValues":[{"name":"L","optionName":"Größe"}],"position":3}]`
	if v != wantV {
		t.Errorf("variants = %s\nwant %s", v, wantV)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}
}

func TestBuildInputNamesRemovedVariants(t *testing.T) {
	_, _, removed := inputFor(t, sizeSM(), optionsList(t, option{"Size", []string{"S"}}), cfgVariant(combo{"Size": "S"}, nil))
	if len(removed) != 1 || removed[0] != "Size = M" {
		t.Errorf("removed = %v, want [Size = M]", removed)
	}
}

func TestBuildInputDefaultVariant(t *testing.T) {
	live := liveProduct(true,
		[]liveOption{{"opt-title", "Title", [][2]string{{"val-dt", "Default Title"}}}},
		[]liveVariant{{id: "gid://shopify/ProductVariant/9", price: "0.00", opts: [][2]string{{"Title", "Default Title"}}}})
	o, v, removed := inputFor(t, live, optionsList(t), cfgVariant(combo{}, func(v *variantModel) { v.Price = types.StringValue("5") }))
	if want := `[{"name":"Title","values":[{"name":"Default Title"}]}]`; o != want {
		t.Errorf("productOptions = %s, want %s", o, want)
	}
	if want := `[{"id":"gid://shopify/ProductVariant/9","optionValues":[{"name":"Default Title","optionName":"Title"}],"price":"5","position":1}]`; v != want {
		t.Errorf("variants = %s, want %s", v, want)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v", removed)
	}
	// Adding options keeps the default variant.
	_, v, _ = inputFor(t, live, optionsList(t, option{"Size", []string{"S", "M"}}), cfgVariant(combo{"Size": "S"}, nil), cfgVariant(combo{"Size": "M"}, nil))
	if !strings.HasPrefix(v, `[{"id":"gid://shopify/ProductVariant/9",`) || strings.Count(v, `"id"`) != 1 {
		t.Errorf("variants = %s, want the default variant's id on S only", v)
	}
}

func TestBuildInputCreateHasNoIDs(t *testing.T) {
	_, v, _ := inputFor(t, nil, optionsList(t, option{"Size", []string{"S"}}), cfgVariant(combo{"Size": "S"}, nil))
	if strings.Contains(v, `"id"`) {
		t.Errorf("variants = %s, want no ids on create", v)
	}
}

func TestBuildInputVariantFields(t *testing.T) {
	ctx := context.Background()
	_, v, _ := inputFor(t, nil, optionsList(t, option{"Size", []string{"S", "M"}}),
		cfgVariant(combo{"Size": "S"}, func(v *variantModel) {
			v.CompareAtPrice = types.StringValue("15")
			v.SKU = types.StringValue("")
			v.Barcodes = types.ListValueMust(barcodeType, []attr.Value{types.ObjectValueMust(barcodeType.AttrTypes, map[string]attr.Value{
				"value": types.StringValue("4006381333931"), "type": types.StringNull(),
			})})
			v.InventoryPolicy = types.StringValue("CONTINUE")
			v.Taxable = types.BoolValue(false)
			v.Tracked = types.BoolValue(true)
			v.Weight, _ = types.ObjectValueFrom(ctx, weightAttrTypes, weightModel{Value: types.Float64Value(0.4), Unit: types.StringValue("KILOGRAMS")})
		}),
		cfgVariant(combo{"Size": "M"}, func(v *variantModel) { v.Barcodes = types.ListValueMust(barcodeType, nil) }),
	)
	want := `[{"optionValues":[{"name":"S","optionName":"Size"}],"sku":"","barcodes":[{"value":"4006381333931"}],"position":1,"compareAtPrice":"15",` +
		`"inventoryPolicy":"CONTINUE","inventoryItem":{"tracked":true,"measurement":{"weight":{"value":0.4,"unit":"KILOGRAMS"}}},"taxable":false},` +
		`{"optionValues":[{"name":"M","optionName":"Size"}],"barcodes":[],"position":2}]`
	if v != want {
		t.Errorf("variants = %s\nwant %s", v, want)
	}
}

func TestSetVariantsFromAPI(t *testing.T) {
	ctx := context.Background()
	m := unsetModel()
	m.Variants = variantsList(t,
		cfgVariant(combo{"Size": "S"}, func(v *variantModel) { v.Price, v.SKU = types.StringValue("10"), types.StringValue("") }),
		cfgVariant(combo{"Size": "M"}, func(v *variantModel) { v.Price = types.StringValue("11") }),
	)
	live := sizeSM()
	live.Variants.Nodes[0].Sku = nil
	if d := setVariantsFromAPI(ctx, &m, live); d.HasError() {
		t.Fatal(d)
	}
	vs, cs, ok := variantsOf(ctx, m.Variants)
	if !ok || len(vs) != 2 {
		t.Fatalf("variants = %s", m.Variants)
	}
	if got := vs[0].Price.ValueString(); got != "10" {
		t.Errorf("S price = %q, want the prior spelling 10 kept for 10.00", got)
	}
	if got := vs[1].Price.ValueString(); got != "12.00" {
		t.Errorf("M price = %q, want the live 12.00 since 11 differs", got)
	}
	if !vs[0].SKU.Equal(types.StringValue("")) || vs[1].SKU.ValueString() != "ADMIN" {
		t.Errorf("skus = %s, %s, want the configured empty SKU kept for Shopify's null, and ADMIN", vs[0].SKU, vs[1].SKU)
	}
	if cs[1]["Size"] != "M" || vs[1].ID.ValueString() != "gid://shopify/ProductVariant/2" || vs[1].InventoryItemID.ValueString() != "gid://shopify/InventoryItem/2" {
		t.Errorf("variant 1 = %+v", vs[1])
	}
	if want := optionsList(t, option{"Size", []string{"S", "M"}}); !m.Options.Equal(want) {
		t.Errorf("options = %s, want %s", m.Options, want)
	}
}

func TestSetVariantsFromAPIDefaultVariant(t *testing.T) {
	ctx := context.Background()
	m := unsetModel()
	live := liveProduct(true,
		[]liveOption{{"opt-title", "Title", [][2]string{{"val-dt", "Default Title"}}}},
		[]liveVariant{{id: "gid://shopify/ProductVariant/9", price: "0.00", opts: [][2]string{{"Title", "Default Title"}}}})
	if d := setVariantsFromAPI(ctx, &m, live); d.HasError() {
		t.Fatal(d)
	}
	if !m.Options.Equal(optionsList(t)) {
		t.Errorf("options = %s, want []", m.Options)
	}
	_, cs, ok := variantsOf(ctx, m.Variants)
	if !ok || len(cs) != 1 || len(cs[0]) != 0 {
		t.Errorf("variants = %s, want one with option_values = {}", m.Variants)
	}
}

// linkedSizeSM is sizeSM with Size linked to a category metafield.
func linkedSizeSM() *client.ProductFields {
	p := sizeSM()
	p.Options[0].LinkedMetafield = &client.ProductFieldsOptionsProductOptionLinkedMetafield{Namespace: ptr("shopify"), Key: ptr("size")}
	return p
}

func TestSetVariantsFromAPILinkedOption(t *testing.T) {
	m := unsetModel()
	if d := setVariantsFromAPI(context.Background(), &m, linkedSizeSM()); d.HasError() {
		t.Fatalf("diags = %v, want a linked option read without error", d)
	}
	if want := optionsList(t, option{"Size", []string{"S", "M"}}); !m.Options.Equal(want) {
		t.Errorf("options = %s, want %s", m.Options, want)
	}
}

func TestBuildInputRefusesLinkedOption(t *testing.T) {
	m := unsetModel()
	m.Options = optionsList(t, option{"Size", []string{"S", "M"}})
	m.Variants = variantsList(t, cfgVariant(combo{"Size": "S"}, nil), cfgVariant(combo{"Size": "M"}, nil))
	in, _, d := buildInput(context.Background(), &m, linkedSizeSM())
	if !d.HasError() || !strings.Contains(d[0].Detail(), `"Size"`) || !strings.Contains(d[0].Detail(), "shopify.size") || !strings.Contains(d[0].Detail(), "unset") {
		t.Fatalf("diags = %v, want an error naming the linked option and saying to leave options and variants unset", d)
	}
	if in.ProductOptions != nil || in.Variants != nil {
		t.Errorf("input carries options or variants for a linked product: %+v", in)
	}
}

func TestBuildInputLinkedOptionCoreOnly(t *testing.T) {
	m := unsetModel()
	m.Vendor = types.StringValue("v")
	in, _, d := buildInput(context.Background(), &m, linkedSizeSM())
	if d.HasError() {
		t.Fatalf("diags = %v, want core fields written to a linked product", d)
	}
	if in.ProductOptions != nil || in.Variants != nil || in.Vendor == nil {
		t.Errorf("input = %+v, want only the core fields", in)
	}
}

func TestListModelLinkedOption(t *testing.T) {
	ctx := context.Background()
	m, d := listModel(ctx, linkedSizeSM())
	if d.HasError() {
		t.Fatal(d)
	}
	if !m.Options.IsNull() || !m.Variants.IsNull() || m.Handle.ValueString() != "h" {
		t.Errorf("linked product listed with options %s, variants %s, handle %s; want options and variants null", m.Options, m.Variants, m.Handle)
	}
	m, _ = listModel(ctx, sizeSM())
	if m.Options.IsNull() || m.Variants.IsNull() {
		t.Errorf("unlinked product listed without options or variants")
	}
}

func TestCarryForward(t *testing.T) {
	prior := cfgVariant(combo{"Size": "S"}, func(v *variantModel) {
		v.ID, v.InventoryItemID = types.StringValue("v1"), types.StringValue("i1")
		v.Price, v.SKU, v.CompareAtPrice = types.StringValue("10.00"), types.StringValue("ADMIN"), types.StringNull()
		v.Taxable = types.BoolValue(true)
	})
	cfg := cfgVariant(combo{"Size": "S"}, func(v *variantModel) { v.Price = types.StringValue("11") })
	got := carryForward(cfg, prior)
	if got.ID.ValueString() != "v1" || got.InventoryItemID.ValueString() != "i1" || got.Price.ValueString() != "11" ||
		got.SKU.ValueString() != "ADMIN" || !got.CompareAtPrice.IsNull() || !got.Taxable.ValueBool() {
		t.Errorf("kept variant = %+v", got)
	}
	got = carryForward(cfg, unknownVariant)
	if !got.ID.IsUnknown() || !got.SKU.IsUnknown() || !got.Weight.IsUnknown() || got.Price.ValueString() != "11" {
		t.Errorf("new variant = %+v, want unknowns except the configured price", got)
	}
}
