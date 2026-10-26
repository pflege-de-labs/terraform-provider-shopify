package product

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

// Shopify's single option and value for a product without options.
const (
	defaultOption = "Title"
	defaultValue  = "Default Title"
)

type optionModel struct {
	Name   types.String `tfsdk:"name"`
	Values types.List   `tfsdk:"values"`
}

type variantModel struct {
	OptionValues     types.Map    `tfsdk:"option_values"`
	ID               types.String `tfsdk:"id"`
	InventoryItemID  types.String `tfsdk:"inventory_item_id"`
	Price            types.String `tfsdk:"price"`
	CompareAtPrice   types.String `tfsdk:"compare_at_price"`
	SKU              types.String `tfsdk:"sku"`
	Barcodes         types.List   `tfsdk:"barcodes"`
	InventoryPolicy  types.String `tfsdk:"inventory_policy"`
	Taxable          types.Bool   `tfsdk:"taxable"`
	Tracked          types.Bool   `tfsdk:"tracked"`
	RequiresShipping types.Bool   `tfsdk:"requires_shipping"`
	Weight           types.Object `tfsdk:"weight"`
}

type barcodeModel struct {
	Value types.String `tfsdk:"value"`
	Type  types.String `tfsdk:"type"`
}

type weightModel struct {
	Value types.Float64 `tfsdk:"value"`
	Unit  types.String  `tfsdk:"unit"`
}

var (
	optionType       = types.ObjectType{AttrTypes: map[string]attr.Type{"name": types.StringType, "values": types.ListType{ElemType: types.StringType}}}
	barcodeType      = types.ObjectType{AttrTypes: map[string]attr.Type{"value": types.StringType, "type": types.StringType}}
	weightAttrTypes  = map[string]attr.Type{"value": types.Float64Type, "unit": types.StringType}
	variantAttrTypes = map[string]attr.Type{
		"option_values":     types.MapType{ElemType: types.StringType},
		"id":                types.StringType,
		"inventory_item_id": types.StringType,
		"price":             types.StringType,
		"compare_at_price":  types.StringType,
		"sku":               types.StringType,
		"barcodes":          types.ListType{ElemType: barcodeType},
		"inventory_policy":  types.StringType,
		"taxable":           types.BoolType,
		"tracked":           types.BoolType,
		"requires_shipping": types.BoolType,
		"weight":            types.ObjectType{AttrTypes: weightAttrTypes},
	}
	variantType = types.ObjectType{AttrTypes: variantAttrTypes}
)

// optionsOf returns the options in l, or false when l or a part of it is null or unknown.
func optionsOf(ctx context.Context, l types.List) ([]option, bool) {
	var ms []optionModel
	if l.IsNull() || l.IsUnknown() || l.ElementsAs(ctx, &ms, false).HasError() {
		return nil, false
	}
	out := make([]option, len(ms))
	for i, m := range ms {
		if m.Name.IsUnknown() || m.Values.IsUnknown() || m.Values.ElementsAs(ctx, &out[i].Values, false).HasError() {
			return nil, false
		}
		out[i].Name = m.Name.ValueString()
	}
	return out, true
}

// variantsOf returns the variants in l and their option values, or false when any of those is null or unknown.
func variantsOf(ctx context.Context, l types.List) ([]variantModel, []combo, bool) {
	var ms []variantModel
	if l.IsNull() || l.IsUnknown() || l.ElementsAs(ctx, &ms, false).HasError() {
		return nil, nil, false
	}
	cs := make([]combo, len(ms))
	for i, m := range ms {
		if m.OptionValues.IsNull() || m.OptionValues.IsUnknown() || m.OptionValues.ElementsAs(ctx, &cs[i], false).HasError() {
			return nil, nil, false
		}
	}
	return ms, cs, true
}

// liveShape returns the live options and variant option values, with Shopify's default variant as no options and an empty combination.
func liveShape(p *client.ProductFields) ([]option, []combo) {
	nodes := p.Variants.Nodes
	cs := make([]combo, len(nodes))
	if p.HasOnlyDefaultVariant {
		for i := range cs {
			cs[i] = combo{}
		}
		return []option{}, cs
	}
	opts := make([]option, len(p.Options))
	for i, o := range p.Options {
		opts[i].Name = o.Name
		for _, v := range o.OptionValues {
			opts[i].Values = append(opts[i].Values, v.Name)
		}
	}
	for i, n := range nodes {
		cs[i] = combo{}
		for _, so := range n.SelectedOptions {
			cs[i][so.Name] = so.Value
		}
	}
	return opts, cs
}

// comboKey identifies a combination independent of map order.
func comboKey(c combo) string {
	parts := make([]string, 0, len(c))
	for k, v := range c {
		parts = append(parts, k+"\x00"+v)
	}
	slices.Sort(parts)
	return strings.Join(parts, "\x01")
}

// linkedOptionError says why options and variants of a product with a linked option can't be written: without linkedMetafield it would be unlinked.
func linkedOptionError(p *client.ProductFields) error {
	// TODO(verify): how many staging/production products have linked options; Read, list and this refusal are unit-tested only, the dev store has none.
	for _, o := range p.Options {
		if lm := o.LinkedMetafield; lm != nil {
			ns, key := "", ""
			if lm.Namespace != nil {
				ns = *lm.Namespace
			}
			if lm.Key != nil {
				key = *lm.Key
			}
			return fmt.Errorf("option %q of product %s is linked to the metafield %s.%s, and linked options can't be managed yet. "+
				"Leave options and variants unset for this product; Shopify then keeps them as they are", o.Name, p.Handle, ns, key)
		}
	}
	return nil
}

// setVariantsFromAPI sets options and variants from the live product; m.Variants, if known, supplies the spellings to keep.
func setVariantsFromAPI(ctx context.Context, m *model, p *client.ProductFields) diag.Diagnostics {
	var diags diag.Diagnostics
	prior := map[string]variantModel{}
	if ms, cs, ok := variantsOf(ctx, m.Variants); ok {
		for i := range ms {
			prior[comboKey(cs[i])] = ms[i]
		}
	}
	opts, cs := liveShape(p)
	var d diag.Diagnostics
	m.Options, d = types.ListValueFrom(ctx, optionType, toOptionModels(opts))
	diags.Append(d...)
	vs := make([]variantModel, len(cs))
	for i, n := range p.Variants.Nodes {
		vs[i], d = variantFromAPI(ctx, &n.VariantFields, cs[i], prior[comboKey(cs[i])])
		diags.Append(d...)
	}
	m.Variants, d = types.ListValueFrom(ctx, variantType, vs)
	diags.Append(d...)
	return diags
}

func toOptionModels(opts []option) []optionModel {
	out := make([]optionModel, len(opts))
	for i, o := range opts {
		vals := make([]attr.Value, len(o.Values))
		for k, v := range o.Values {
			vals[k] = types.StringValue(v)
		}
		out[i] = optionModel{Name: types.StringValue(o.Name), Values: types.ListValueMust(types.StringType, vals)}
	}
	return out
}

// variantFromAPI maps a live variant, keeping prior's decimal spellings and empty SKU where Shopify returns the same value differently.
func variantFromAPI(ctx context.Context, v *client.VariantFields, c combo, prior variantModel) (variantModel, diag.Diagnostics) {
	var diags, d diag.Diagnostics
	out := variantModel{
		ID:               types.StringValue(v.Id),
		InventoryItemID:  types.StringValue(v.InventoryItem.Id),
		Price:            keepDecimal(prior.Price, &v.Price),
		CompareAtPrice:   keepDecimal(prior.CompareAtPrice, v.CompareAtPrice),
		SKU:              types.StringPointerValue(v.Sku),
		InventoryPolicy:  types.StringValue(string(v.InventoryPolicy)),
		Taxable:          types.BoolValue(v.Taxable),
		Tracked:          types.BoolValue(v.InventoryItem.Tracked),
		RequiresShipping: types.BoolValue(v.InventoryItem.RequiresShipping),
		Weight:           types.ObjectNull(weightAttrTypes),
	}
	if v.Sku == nil && prior.SKU.ValueString() == "" && !prior.SKU.IsNull() && !prior.SKU.IsUnknown() {
		out.SKU = prior.SKU
	}
	out.OptionValues, d = types.MapValueFrom(ctx, types.StringType, map[string]string(c))
	diags.Append(d...)
	barcodes := make([]barcodeModel, len(v.Barcodes.Nodes))
	for i, b := range v.Barcodes.Nodes {
		barcodes[i] = barcodeModel{Value: types.StringValue(b.Value), Type: types.StringNull()}
		if b.Type != nil {
			barcodes[i].Type = types.StringValue(string(*b.Type))
		}
	}
	out.Barcodes, d = types.ListValueFrom(ctx, barcodeType, barcodes)
	diags.Append(d...)
	if w := v.InventoryItem.Measurement.Weight; w != nil {
		out.Weight, d = types.ObjectValueFrom(ctx, weightAttrTypes, weightModel{Value: types.Float64Value(w.Value), Unit: types.StringValue(string(w.Unit))})
		diags.Append(d...)
	}
	return out, diags
}

// keepDecimal keeps prior while it is numerically equal to live, so "10" doesn't diff against Shopify's "10.00".
func keepDecimal(prior types.String, live *string) types.String {
	if live == nil {
		return types.StringNull()
	}
	if !prior.IsNull() && !prior.IsUnknown() && decimalEqual(prior.ValueString(), *live) {
		return prior
	}
	return types.StringValue(*live)
}

// optionsAndVariantsInput maps the configured options and variants, with the IDs of the live ones they keep, and names the live variants they remove.
func optionsAndVariantsInput(ctx context.Context, cfg *model, live *client.ProductFields) ([]client.OptionSetInput, []client.ProductVariantSetInput, []string, diag.Diagnostics) {
	var diags diag.Diagnostics
	opts, ok1 := optionsOf(ctx, cfg.Options)
	vs, cs, ok2 := variantsOf(ctx, cfg.Variants)
	if !ok1 || !ok2 {
		diags.AddError("Invalid options or variants", "options and variants must both be set and known.")
		return nil, nil, nil, diags
	}
	liveOpts, liveCombos := []option{}, []combo{}
	if live != nil {
		if err := linkedOptionError(live); err != nil {
			diags.AddAttributeError(path.Root("options"), "Linked product option", err.Error())
			return nil, nil, nil, diags
		}
		liveOpts, liveCombos = liveShape(live)
	}
	keep, removed := matchVariants(liveOpts, liveCombos, opts, cs)
	m := matchOptions(liveOpts, opts)

	var optIn []client.OptionSetInput
	for j, o := range opts {
		oi := client.OptionSetInput{Name: &o.Name, Position: ptr(j + 1)}
		if i := m.Options[j]; i >= 0 {
			oi.Id = &live.Options[i].Id
		}
		for k := range o.Values {
			vi := client.OptionValueSetInput{Name: &o.Values[k]}
			if i, pk := m.Options[j], m.Values[j][k]; i >= 0 && pk >= 0 {
				vi.Id = &live.Options[i].OptionValues[pk].Id
			}
			oi.Values = append(oi.Values, vi)
		}
		optIn = append(optIn, oi)
	}
	if len(opts) == 0 {
		optIn = []client.OptionSetInput{{Name: ptr(defaultOption), Values: []client.OptionValueSetInput{{Name: ptr(defaultValue)}}}}
	}

	varIn := make([]client.ProductVariantSetInput, len(vs))
	for i, v := range vs {
		vi, d := variantInput(ctx, v, cs[i], opts)
		diags.Append(d...)
		vi.Position = ptr(i + 1)
		if keep[i] >= 0 {
			vi.Id = &live.Variants.Nodes[keep[i]].Id
		}
		varIn[i] = vi
	}
	gone := make([]string, len(removed))
	for i, k := range removed {
		gone[i] = describe(liveCombos[k], liveOpts)
	}
	return optIn, varIn, gone, diags
}

// variantInput maps one configured variant; unset fields stay out so Shopify keeps their values.
func variantInput(ctx context.Context, v variantModel, c combo, opts []option) (client.ProductVariantSetInput, diag.Diagnostics) {
	var diags diag.Diagnostics
	in := client.ProductVariantSetInput{
		Price:          patterns.ConfiguredString(v.Price),
		CompareAtPrice: patterns.ConfiguredString(v.CompareAtPrice),
		Sku:            patterns.ConfiguredString(v.SKU),
		Taxable:        patterns.ConfiguredBool(v.Taxable),
	}
	for _, o := range opts {
		in.OptionValues = append(in.OptionValues, client.VariantOptionValueInput{OptionName: ptr(o.Name), Name: ptr(c[o.Name])})
	}
	if len(opts) == 0 {
		in.OptionValues = []client.VariantOptionValueInput{{OptionName: ptr(defaultOption), Name: ptr(defaultValue)}}
	}
	if p := patterns.ConfiguredString(v.InventoryPolicy); p != nil {
		ip := client.ProductVariantInventoryPolicy(*p)
		in.InventoryPolicy = &ip
	}
	if !v.Barcodes.IsNull() && !v.Barcodes.IsUnknown() {
		var bs []barcodeModel
		diags.Append(v.Barcodes.ElementsAs(ctx, &bs, false)...)
		out := make([]barcodeInput, len(bs))
		for i, b := range bs {
			out[i] = barcodeInput{Value: b.Value.ValueString(), Type: patterns.ConfiguredString(b.Type)}
		}
		raw, err := json.Marshal(out)
		if err != nil {
			diags.AddError("Failed to encode barcodes", err.Error())
		}
		in.Barcodes = raw
	}
	item := client.InventoryItemInput{Tracked: patterns.ConfiguredBool(v.Tracked), RequiresShipping: patterns.ConfiguredBool(v.RequiresShipping)}
	if !v.Weight.IsNull() && !v.Weight.IsUnknown() {
		var w weightModel
		diags.Append(v.Weight.As(ctx, &w, basetypes.ObjectAsOptions{})...)
		item.Measurement = &client.InventoryItemMeasurementInput{Weight: &client.WeightInput{Value: w.Value.ValueFloat64(), Unit: client.WeightUnit(w.Unit.ValueString())}}
	}
	if item.Tracked != nil || item.RequiresShipping != nil || item.Measurement != nil {
		in.InventoryItem = &item
	}
	return in, diags
}

// barcodeInput is BarcodeInput, sent as raw JSON so that [] clears the barcodes.
type barcodeInput struct {
	Value string  `json:"value"`
	Type  *string `json:"type,omitempty"`
}

func ptr[T any](v T) *T { return &v }

// fetchAllVariants appends the variants after the first page, so p holds all of them.
func fetchAllVariants(ctx context.Context, c *client.Client, p *client.ProductFields) error {
	page := p.Variants.PageInfo
	for page.HasNextPage {
		res, err := client.GetProductVariants(ctx, c, p.Id, page.EndCursor)
		if err != nil {
			return err
		}
		if res.Product == nil {
			return fmt.Errorf("product %s was deleted while its variants were read", p.Id)
		}
		conn := res.Product.Variants
		for _, n := range conn.Nodes {
			p.Variants.Nodes = append(p.Variants.Nodes, client.ProductFieldsVariantsProductVariantConnectionNodesProductVariant(n))
		}
		page.HasNextPage, page.EndCursor = conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor
	}
	p.Variants.PageInfo = page
	return nil
}

// unknownVariant is what a new variant gets for what config leaves unset.
var unknownVariant = variantModel{
	ID:               types.StringUnknown(),
	InventoryItemID:  types.StringUnknown(),
	Price:            types.StringUnknown(),
	CompareAtPrice:   types.StringUnknown(),
	SKU:              types.StringUnknown(),
	Barcodes:         types.ListUnknown(barcodeType),
	InventoryPolicy:  types.StringUnknown(),
	Taxable:          types.BoolUnknown(),
	Tracked:          types.BoolUnknown(),
	RequiresShipping: types.BoolUnknown(),
	Weight:           types.ObjectUnknown(weightAttrTypes),
}

// carryForward fills computed and unset fields of a configured variant from the variant it keeps (or unknownVariant).
func carryForward(v, from variantModel) variantModel {
	v.ID, v.InventoryItemID = from.ID, from.InventoryItemID
	v.Price = orElse(v.Price, from.Price)
	v.CompareAtPrice = orElse(v.CompareAtPrice, from.CompareAtPrice)
	v.SKU = orElse(v.SKU, from.SKU)
	v.Barcodes = orElse(v.Barcodes, from.Barcodes)
	v.InventoryPolicy = orElse(v.InventoryPolicy, from.InventoryPolicy)
	v.Taxable = orElse(v.Taxable, from.Taxable)
	v.Tracked = orElse(v.Tracked, from.Tracked)
	v.RequiresShipping = orElse(v.RequiresShipping, from.RequiresShipping)
	v.Weight = orElse(v.Weight, from.Weight)
	return v
}

func orElse[T attr.Value](v, fallback T) T {
	if v.IsNull() {
		return fallback
	}
	return v
}
