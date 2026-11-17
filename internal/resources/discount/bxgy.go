package discount

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const whatBxgy = "buy X get Y discount"

var (
	_ resource.ResourceWithConfigure   = &BxgyResource{}
	_ resource.ResourceWithImportState = &BxgyResource{}
	_ resource.ResourceWithIdentity    = &BxgyResource{}
	_ resource.ResourceWithModifyPlan  = &BxgyResource{}
)

func NewBxgyResource() resource.Resource {
	return &BxgyResource{discountResource[bxgyModel, *bxgyModel]{method: methodBxgy, what: whatBxgy}}
}

// BxgyResource manages one buy X get Y discount, as a code or an automatic discount.
type BxgyResource struct {
	discountResource[bxgyModel, *bxgyModel]
}

type bxgyModel struct {
	commonModel
	CustomerBuys      types.Object `tfsdk:"customer_buys"`
	CustomerGets      types.Object `tfsdk:"customer_gets"`
	UsesPerOrderLimit types.Int64  `tfsdk:"uses_per_order_limit"`
}

func (m *bxgyModel) common() *commonModel { return &m.commonModel }

// bxgyItemsSel is itemsSel without `all`, which Shopify refuses on both sides of a buy X get Y discount.
type bxgyItemsSel struct {
	ProductIDs    []string `tfsdk:"product_ids"`
	VariantIDs    []string `tfsdk:"variant_ids"`
	CollectionIDs []string `tfsdk:"collection_ids"`
}

func (s bxgyItemsSel) sel() itemsSel {
	return itemsSel{ProductIDs: s.ProductIDs, VariantIDs: s.VariantIDs, CollectionIDs: s.CollectionIDs}
}

// bxgyItemsFromAPI maps the live items; all items can't occur, so it is an error.
func bxgyItemsFromAPI(items client.DiscountItemsFields) (bxgyItemsSel, error) {
	s, err := itemsFromAPI(items)
	if err == nil && s.All != nil {
		err = fmt.Errorf("selects all items, which buy X get Y discounts don't support")
	}
	return bxgyItemsSel{ProductIDs: s.ProductIDs, VariantIDs: s.VariantIDs, CollectionIDs: s.CollectionIDs}, err
}

type bxgyBuysSel struct {
	Quantity *int64       `tfsdk:"quantity"`
	Amount   *string      `tfsdk:"amount"`
	Items    bxgyItemsSel `tfsdk:"items"`
}

type bxgyGetsSel struct {
	Quantity   int64        `tfsdk:"quantity"`
	Percentage *float64     `tfsdk:"percentage"`
	Amount     *string      `tfsdk:"amount"`
	Items      bxgyItemsSel `tfsdk:"items"`
}

func bxgyItemsAttribute(side string) schema.SingleNestedAttribute {
	a := itemsAttribute()
	delete(a.Attributes, "all")
	// Without `all`, an empty object (also what a dropped `all = true` leaves) would only fail at the API.
	p, _ := a.Attributes["product_ids"].(schema.SetAttribute)
	p.Validators = []validator.Set{setvalidator.AtLeastOneOf(sibling("variant_ids", "collection_ids")...)}
	a.Attributes["product_ids"] = p
	a.MarkdownDescription = "The items the customer " + side + ": `product_ids` and/or `variant_ids`, or `collection_ids`. Shopify refuses all items here. " +
		"Changes are written as additions and removals against the live selection. " +
		"A selection whose items are all gone (for example deleted products) reads as `product_ids = []` or `collection_ids = []`; " +
		"Shopify rejects creating such a discount, and any update to it unless the same change adds items."
	return a
}

func quantityAttribute(desc string) schema.Int64Attribute {
	return schema.Int64Attribute{MarkdownDescription: desc, Validators: []validator.Int64{int64validator.AtLeast(1)}}
}

func bxgyBuysAttribute() schema.SingleNestedAttribute {
	qty := quantityAttribute("Number of items the customer buys.")
	qty.Optional = true
	qty.Validators = append(qty.Validators, int64validator.ExactlyOneOf(sibling("amount")...))
	return schema.SingleNestedAttribute{
		MarkdownDescription: "What the customer buys: `items` and exactly one of `quantity` or `amount`.",
		Required:            true,
		Attributes: map[string]schema.Attribute{
			"quantity": qty,
			"amount": schema.StringAttribute{
				MarkdownDescription: "Purchase amount of the items in the shop currency, as a decimal string. Compared numerically, so `\"30\"` matches Shopify's `\"30.0\"`.",
				CustomType:          decimalType{},
				Optional:            true,
				Validators:          []validator.String{decimalValidator},
			},
			"items": bxgyItemsAttribute("buys"),
		},
	}
}

func bxgyGetsAttribute() schema.SingleNestedAttribute {
	qty := quantityAttribute("Number of items that get the discount.")
	qty.Required = true
	return schema.SingleNestedAttribute{
		MarkdownDescription: "What the customer gets: `items`, the `quantity` that is discounted and exactly one of `percentage` or `amount`.",
		Required:            true,
		Attributes: map[string]schema.Attribute{
			"quantity": qty,
			"percentage": schema.Float64Attribute{
				MarkdownDescription: "A fraction greater than 0 and at most 1 off each discounted item, such as `1` for free.",
				Optional:            true,
				Validators: []validator.Float64{
					float64validator.Between(0, 1),
					float64validator.NoneOf(0),
					float64validator.ExactlyOneOf(sibling("amount")...),
				},
			},
			"amount": schema.StringAttribute{
				MarkdownDescription: "A fixed amount off each discounted item in the shop currency, as a decimal string. Compared numerically.",
				CustomType:          decimalType{},
				Optional:            true,
				Validators:          []validator.String{decimalValidator},
			},
			"items": bxgyItemsAttribute("gets"),
		},
	}
}

func bxgyBuysAttrTypes() map[string]attr.Type { return attrTypesOf(bxgyBuysAttribute()) }
func bxgyGetsAttrTypes() map[string]attr.Type { return attrTypesOf(bxgyGetsAttribute()) }

func (r *BxgyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_discount_bxgy"
	// Read re-adopts a discount recreated outside Terraform by its code or title, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *BxgyResource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *BxgyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := commonAttributes()
	attrs["customer_buys"] = bxgyBuysAttribute()
	attrs["customer_gets"] = bxgyGetsAttribute()
	attrs["uses_per_order_limit"] = schema.Int64Attribute{
		MarkdownDescription: "How often the discount can apply to one order. A limit set in the admin can't be removed here: set a high one instead. Unset means unmanaged.",
		Optional:            true,
		Computed:            true,
		Validators:          []validator.Int64{int64validator.AtLeast(1)},
		PlanModifiers:       []planmodifier.Int64{patterns.UseStateForUnknownIncludingNullInt64()},
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A buy X get Y discount: customers who buy a quantity or an amount of some items get a quantity of other items at a percentage or an amount off, " +
			"as a code discount (`code` set) or an automatic one (`code` unset). " +
			"**Volatile, usually a poor fit for Terraform:** promotions are created, changed and ended in the admin by marketing, and every admin edit to a managed attribute is reverted on the next apply. " +
			"Destroying a discount deletes it; orders keep their discount applications, but a recreated discount counts its uses from zero. " +
			"Bulk codes, tags, subscription settings and app discounts are not managed. " +
			"Generated config holds product, collection, segment and market GIDs, which tie it to the store: replace them with references. " +
			"An existing discount with the same code, or an automatic one with the same title, needs `adopt_existing = true`; destroy then leaves it in place. " +
			"Import with `code/<code>` or `automatic/<title>`. A selection of more than 250 items per list is not read yet.",
		Attributes: attrs,
	}
}

// buysFromAPI and getsFromAPI map the live sides; shapes the schema can't hold are errors.
func buysFromAPI(b *client.DiscountCustomerBuysFields) (bxgyBuysSel, error) {
	if b == nil {
		return bxgyBuysSel{}, fmt.Errorf("has no customer buys")
	}
	items, err := bxgyItemsFromAPI(b.Items)
	if err != nil {
		return bxgyBuysSel{}, err
	}
	switch v := b.Value.(type) {
	case *client.DiscountCustomerBuysFieldsValueDiscountQuantity:
		q, err := strconv.ParseInt(v.Quantity, 10, 64)
		if err != nil {
			return bxgyBuysSel{}, fmt.Errorf("buys a quantity %q that is not a number", v.Quantity)
		}
		return bxgyBuysSel{Quantity: &q, Items: items}, nil
	case *client.DiscountCustomerBuysFieldsValueDiscountPurchaseAmount:
		return bxgyBuysSel{Amount: &v.Amount, Items: items}, nil
	}
	return bxgyBuysSel{}, fmt.Errorf("buys a value of type %T, which this provider doesn't know", b.Value)
}

func getsFromAPI(g *client.DiscountCustomerGetsFields) (bxgyGetsSel, error) {
	if g == nil {
		return bxgyGetsSel{}, fmt.Errorf("has no customer gets")
	}
	items, err := bxgyItemsFromAPI(g.Items)
	if err != nil {
		return bxgyGetsSel{}, err
	}
	v, ok := g.Value.(*client.DiscountCustomerGetsFieldsValueDiscountOnQuantity)
	if !ok {
		return bxgyGetsSel{}, fmt.Errorf("gets a value of type %T, which is not a quantity discount", g.Value)
	}
	q, err := strconv.ParseInt(v.Quantity.Quantity, 10, 64)
	if err != nil {
		return bxgyGetsSel{}, fmt.Errorf("gets a quantity %q that is not a number", v.Quantity.Quantity)
	}
	out := bxgyGetsSel{Quantity: q, Items: items}
	switch e := v.Effect.(type) {
	case *client.DiscountCustomerGetsFieldsValueDiscountOnQuantityEffectDiscountPercentage:
		out.Percentage = &e.Percentage
	case *client.DiscountCustomerGetsFieldsValueDiscountOnQuantityEffectDiscountAmount:
		out.Amount = &e.Amount.Amount
	default:
		return bxgyGetsSel{}, fmt.Errorf("gets an effect of type %T, which this provider doesn't know", v.Effect)
	}
	return out, nil
}

// buysInput sends the whole value, the unused kind as an explicit null (Shopify refuses both, and a switch needs the old one cleared), and the items as a difference.
func buysInput(want bxgyBuysSel, live *client.DiscountCustomerBuysFields) (*client.DiscountCustomerBuysInput, error) {
	var have *itemsSel
	if live != nil {
		h, err := bxgyItemsFromAPI(live.Items)
		if err != nil {
			return nil, err
		}
		s := h.sel()
		have = &s
	}
	value := &client.DiscountCustomerBuysValueInput{Amount: want.Amount}
	if want.Quantity != nil {
		q := strconv.FormatInt(*want.Quantity, 10)
		value.Quantity = &q
	}
	return &client.DiscountCustomerBuysInput{Value: value, Items: itemsInput(want.Items.sel(), have)}, nil
}

// getsInput sends the whole value and the items as a difference.
func getsInput(want bxgyGetsSel, live *client.DiscountCustomerGetsFields) (*client.DiscountCustomerGetsInput, error) {
	var have *itemsSel
	if live != nil {
		h, err := bxgyItemsFromAPI(live.Items)
		if err != nil {
			return nil, err
		}
		s := h.sel()
		have = &s
	}
	q := strconv.FormatInt(want.Quantity, 10)
	return &client.DiscountCustomerGetsInput{
		Value: &client.DiscountCustomerGetsValueInput{DiscountOnQuantity: &client.DiscountOnQuantityInput{
			Quantity: &q, Effect: &client.DiscountEffectInput{Percentage: want.Percentage, Amount: want.Amount},
		}},
		Items: itemsInput(want.Items.sel(), have),
	}, nil
}

// bxgyInputs are the method-specific parts of both create and update inputs.
type bxgyInputs struct {
	common commonInput
	buys   *client.DiscountCustomerBuysInput
	gets   *client.DiscountCustomerGetsInput
	// perOrder is the limit for the code input (int) and the automatic one (string).
	perOrder *int
}

// inputs maps the configured attributes against live (nil on create).
func (m *bxgyModel) inputs(ctx context.Context, live *node) (bxgyInputs, diag.Diagnostics) {
	common, diags := m.input(ctx, live)
	in := bxgyInputs{common: common}
	var liveBuys *client.DiscountCustomerBuysFields
	var liveGets *client.DiscountCustomerGetsFields
	if live != nil {
		liveBuys, liveGets = live.CustomerBuys, live.CustomerGets
	}
	var buys bxgyBuysSel
	var gets bxgyGetsSel
	diags.Append(m.CustomerBuys.As(ctx, &buys, basetypes.ObjectAsOptions{})...)
	diags.Append(m.CustomerGets.As(ctx, &gets, basetypes.ObjectAsOptions{})...)
	var err error
	if in.buys, err = buysInput(buys, liveBuys); err != nil {
		diags.AddAttributeError(path.Root("customer_buys").AtName("items"), "Items can't be managed", fmt.Sprintf("The discount %s.", err))
	}
	if in.gets, err = getsInput(gets, liveGets); err != nil {
		diags.AddAttributeError(path.Root("customer_gets").AtName("items"), "Items can't be managed", fmt.Sprintf("The discount %s.", err))
	}
	if !m.UsesPerOrderLimit.IsNull() && !m.UsesPerOrderLimit.IsUnknown() {
		n := int(m.UsesPerOrderLimit.ValueInt64())
		in.perOrder = &n
	}
	return in, diags
}

func (m *bxgyModel) codeInput(ctx context.Context, live *node) (client.DiscountCodeBxgyInput, diag.Diagnostics) {
	i, diags := m.inputs(ctx, live)
	c := i.common
	return client.DiscountCodeBxgyInput{
		Title: c.Title, StartsAt: c.StartsAt, EndsAt: c.EndsAt, Code: c.Code, UsageLimit: c.UsageLimit, AppliesOncePerCustomer: c.AppliesOncePerCustomer,
		CombinesWith: c.CombinesWith, Context: c.Context, CustomerBuys: i.buys, CustomerGets: i.gets, UsesPerOrderLimit: i.perOrder,
	}, diags
}

func (m *bxgyModel) automaticInput(ctx context.Context, live *node) (client.DiscountAutomaticBxgyInput, diag.Diagnostics) {
	i, diags := m.inputs(ctx, live)
	c := i.common
	in := client.DiscountAutomaticBxgyInput{
		Title: c.Title, StartsAt: c.StartsAt, EndsAt: c.EndsAt,
		CombinesWith: c.CombinesWith, Context: c.Context, CustomerBuys: i.buys, CustomerGets: i.gets,
	}
	if i.perOrder != nil {
		s := strconv.Itoa(*i.perOrder)
		in.UsesPerOrderLimit = &s
	}
	return in, diags
}

// write: see kindModel.
func (cfg *bxgyModel) write(ctx context.Context, c *client.Client, plan *bxgyModel, live *node, diags *diag.Diagnostics) string {
	cfg.takeDefaults(&plan.commonModel)
	cfg.CustomerBuys, cfg.CustomerGets = plan.CustomerBuys, plan.CustomerGets
	isCode := !cfg.Code.IsNull()
	if live != nil {
		isCode = live.IsCode
	}
	summary := writeSummary(whatBxgy, live)
	var id *string
	var errs []client.DiscountUserErrorFields
	var err error
	if isCode {
		in, d := cfg.codeInput(ctx, live)
		if diags.Append(d...); diags.HasError() {
			return ""
		}
		if live == nil {
			var res *client.CreateDiscountCodeBxgyResponse
			if res, err = client.CreateDiscountCodeBxgy(ctx, c, in); err == nil && res.DiscountCodeBxgyCreate != nil {
				errs = res.DiscountCodeBxgyCreate.UserErrors
				if n := res.DiscountCodeBxgyCreate.CodeDiscountNode; n != nil {
					id = &n.Id
				}
			}
		} else {
			var res *client.UpdateDiscountCodeBxgyResponse
			if res, err = client.UpdateDiscountCodeBxgy(ctx, c, live.ID, in); err == nil && res.DiscountCodeBxgyUpdate != nil {
				errs = res.DiscountCodeBxgyUpdate.UserErrors
				if n := res.DiscountCodeBxgyUpdate.CodeDiscountNode; n != nil {
					id = &n.Id
				}
			}
		}
	} else {
		in, d := cfg.automaticInput(ctx, live)
		if diags.Append(d...); diags.HasError() {
			return ""
		}
		if live == nil {
			var res *client.CreateDiscountAutomaticBxgyResponse
			if res, err = client.CreateDiscountAutomaticBxgy(ctx, c, in); err == nil && res.DiscountAutomaticBxgyCreate != nil {
				errs = res.DiscountAutomaticBxgyCreate.UserErrors
				if n := res.DiscountAutomaticBxgyCreate.AutomaticDiscountNode; n != nil {
					id = &n.Id
				}
			}
		} else {
			var res *client.UpdateDiscountAutomaticBxgyResponse
			if res, err = client.UpdateDiscountAutomaticBxgy(ctx, c, live.ID, in); err == nil && res.DiscountAutomaticBxgyUpdate != nil {
				errs = res.DiscountAutomaticBxgyUpdate.UserErrors
				if n := res.DiscountAutomaticBxgyUpdate.AutomaticDiscountNode; n != nil {
					id = &n.Id
				}
			}
		}
	}
	out := writeOutcome(diags, summary, err, errs, id)
	if out == "" && err == nil {
		var buys bxgyBuysSel
		var gets bxgyGetsSel
		if plan.CustomerBuys.As(ctx, &buys, basetypes.ObjectAsOptions{}).HasError() || plan.CustomerGets.As(ctx, &gets, basetypes.ObjectAsOptions{}).HasError() {
			return out
		}
		if buys.Items.sel().empty() {
			diags.AddAttributeError(path.Root("customer_buys").AtName("items"), summary, emptySelectionHint)
		}
		if gets.Items.sel().empty() {
			diags.AddAttributeError(path.Root("customer_gets").AtName("items"), summary, emptySelectionHint)
		}
	}
	return out
}

// setFromAPI copies n into m; shapes the schema can't hold are errors, so import fails and list skips them.
//
//nolint:unparam // the context result is part of kindModel; only basic reads it so far
func (m *bxgyModel) setFromAPI(ctx context.Context, n *node) (string, diag.Diagnostics) {
	unsupported, diags := m.commonModel.setFromAPI(ctx, n)
	m.CustomerBuys, m.CustomerGets = types.ObjectNull(bxgyBuysAttrTypes()), types.ObjectNull(bxgyGetsAttrTypes())
	m.UsesPerOrderLimit = types.Int64Null()
	if n.UsesPerOrderLimit != nil {
		m.UsesPerOrderLimit = types.Int64Value(int64(*n.UsesPerOrderLimit))
	}
	buys, err1 := buysFromAPI(n.CustomerBuys)
	gets, err2 := getsFromAPI(n.CustomerGets)
	for _, err := range []error{err1, err2} {
		if err != nil {
			diags.AddError("Unsupported discount", fmt.Sprintf("Discount %q %s.", n.Title, err))
		}
	}
	if diags.HasError() {
		return unsupported, diags
	}
	var d diag.Diagnostics
	m.CustomerBuys, d = types.ObjectValueFrom(ctx, bxgyBuysAttrTypes(), buys)
	diags.Append(d...)
	m.CustomerGets, d = types.ObjectValueFrom(ctx, bxgyGetsAttrTypes(), gets)
	diags.Append(d...)
	return unsupported, diags
}
