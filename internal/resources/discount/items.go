package discount

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/boolvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

// itemsSel is an item selection: all, products and/or variants, or collections. Empty lists read as null, as config leaves them out.
type itemsSel struct {
	All           *bool    `tfsdk:"all"`
	ProductIDs    []string `tfsdk:"product_ids"`
	VariantIDs    []string `tfsdk:"variant_ids"`
	CollectionIDs []string `tfsdk:"collection_ids"`
}

type amountSel struct {
	Amount            string `tfsdk:"amount"`
	AppliesOnEachItem bool   `tfsdk:"applies_on_each_item"`
}

type valueSel struct {
	Percentage *float64   `tfsdk:"percentage"`
	Amount     *amountSel `tfsdk:"amount"`
}

type customerGetsSel struct {
	Value valueSel `tfsdk:"value"`
	Items itemsSel `tfsdk:"items"`
}

// minimumSel is the minimum requirement; neither set means none.
type minimumSel struct {
	Subtotal *string `tfsdk:"subtotal"`
	Quantity *int64  `tfsdk:"quantity"`
}

func sibling(names ...string) []path.Expression {
	out := make([]path.Expression, len(names))
	for i, n := range names {
		out[i] = path.MatchRelative().AtParent().AtName(n)
	}
	return out
}

var decimalValidator = stringvalidator.RegexMatches(decimalRe, "must be a decimal number such as 30.00")

func itemsAttribute() schema.SingleNestedAttribute {
	ids := func(desc string) schema.SetAttribute {
		return schema.SetAttribute{MarkdownDescription: desc, ElementType: types.StringType, Optional: true, Validators: []validator.Set{setvalidator.SizeAtLeast(1)}}
	}
	collections := ids("Collection GIDs; reference `shopify_collection` IDs.")
	collections.Validators = append(collections.Validators, setvalidator.ConflictsWith(sibling("product_ids", "variant_ids")...))
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Which items are discounted: exactly one of `all`, `product_ids` and/or `variant_ids`, or `collection_ids`. " +
			"Changes are written as additions and removals against the live selection.",
		Required: true,
		Attributes: map[string]schema.Attribute{
			"all": schema.BoolAttribute{
				MarkdownDescription: "All items, which makes it an order discount. Must be `true`.",
				Optional:            true,
				Validators: []validator.Bool{
					boolvalidator.Equals(true),
					boolvalidator.ConflictsWith(sibling("product_ids", "variant_ids", "collection_ids")...),
					boolvalidator.AtLeastOneOf(sibling("product_ids", "variant_ids", "collection_ids")...),
				},
			},
			"product_ids":    ids("Product GIDs; reference `shopify_product` IDs."),
			"variant_ids":    ids("Product variant GIDs."),
			"collection_ids": collections,
		},
	}
}

func customerGetsAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "What the customer gets.",
		Required:            true,
		Attributes: map[string]schema.Attribute{
			"value": schema.SingleNestedAttribute{
				MarkdownDescription: "The discount value: exactly one of `percentage` or `amount`.",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"percentage": schema.Float64Attribute{
						MarkdownDescription: "A fraction greater than 0 and at most 1, such as `0.1` for 10 %.",
						Optional:            true,
						Validators: []validator.Float64{
							float64validator.Between(0, 1),
							float64validator.NoneOf(0),
							float64validator.ExactlyOneOf(sibling("amount")...),
						},
					},
					"amount": schema.SingleNestedAttribute{
						MarkdownDescription: "A fixed amount off.",
						Optional:            true,
						Attributes: map[string]schema.Attribute{
							"amount": schema.StringAttribute{
								MarkdownDescription: "The amount in the shop currency, as a decimal string. Compared numerically, so `\"5\"` matches Shopify's `\"5.0\"`.",
								CustomType:          decimalType{},
								Required:            true,
								Validators:          []validator.String{decimalValidator},
							},
							"applies_on_each_item": schema.BoolAttribute{
								MarkdownDescription: "Whether the amount comes off each selected item instead of once per order. Defaults to `false`; must be `false` when `items` is `all`.",
								Optional:            true,
								Computed:            true,
								Default:             booldefault.StaticBool(false),
							},
						},
					},
				},
			},
			"items": itemsAttribute(),
		},
	}
}

func minimumAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "The minimum the order must reach: at most one of `subtotal` and `quantity`; `{}` removes the minimum. Unset means unmanaged.",
		Optional:            true,
		Computed:            true,
		PlanModifiers:       []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
		Attributes: map[string]schema.Attribute{
			"subtotal": schema.StringAttribute{
				MarkdownDescription: "Minimum subtotal of the eligible items, as a decimal string. Compared numerically, so `\"50\"` matches Shopify's `\"50.0\"`.",
				CustomType:          decimalType{},
				Optional:            true,
				Validators:          []validator.String{decimalValidator, stringvalidator.ConflictsWith(sibling("quantity")...)},
			},
			"quantity": schema.Int64Attribute{
				MarkdownDescription: "Minimum number of eligible items.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.AtLeast(1)},
			},
		},
	}
}

func customerGetsAttrTypes() map[string]attr.Type { return attrTypesOf(customerGetsAttribute()) }
func minimumAttrTypes() map[string]attr.Type      { return attrTypesOf(minimumAttribute()) }

func nodeIDs[T any, P interface {
	*T
	GetId() string
}](nodes []T) []string {
	var out []string
	for i := range nodes {
		out = append(out, P(&nodes[i]).GetId())
	}
	return out
}

func itemsFromAPI(items client.DiscountItemsFields) (itemsSel, error) {
	// completeItems pages before mapping; this only guards against a partial read.
	const tooMany = "came back with an incomplete list of %s"
	switch it := items.(type) {
	case *client.DiscountItemsFieldsAllDiscountItems:
		all := true
		return itemsSel{All: &all}, nil
	case *client.DiscountItemsFieldsDiscountProducts:
		if it.Products.PageInfo.HasNextPage {
			return itemsSel{}, fmt.Errorf(tooMany, "products")
		}
		if it.ProductVariants.PageInfo.HasNextPage {
			return itemsSel{}, fmt.Errorf(tooMany, "variants")
		}
		sel := itemsSel{ProductIDs: nodeIDs(it.Products.Nodes), VariantIDs: nodeIDs(it.ProductVariants.Nodes)}
		if sel.ProductIDs == nil && sel.VariantIDs == nil {
			return itemsSel{}, fmt.Errorf("selects no products")
		}
		return sel, nil
	case *client.DiscountItemsFieldsDiscountCollections:
		if it.Collections.PageInfo.HasNextPage {
			return itemsSel{}, fmt.Errorf(tooMany, "collections")
		}
		sel := itemsSel{CollectionIDs: nodeIDs(it.Collections.Nodes)}
		if sel.CollectionIDs == nil {
			return itemsSel{}, fmt.Errorf("selects no collections")
		}
		return sel, nil
	}
	return itemsSel{}, fmt.Errorf("has an item selection of type %T, which this provider doesn't know", items)
}

// itemsInput writes want over live (nil on create): a difference within one kind, else the new kind plus removal of the old (§9p).
func itemsInput(want itemsSel, live *itemsSel) *client.DiscountItemsInput {
	liveProducts := live != nil && (live.ProductIDs != nil || live.VariantIDs != nil)
	switch {
	case want.ProductIDs != nil || want.VariantIDs != nil:
		if !liveProducts {
			in := &client.DiscountItemsInput{Products: &client.DiscountProductsInput{ProductsToAdd: want.ProductIDs, ProductVariantsToAdd: want.VariantIDs}}
			if live != nil && live.CollectionIDs != nil {
				in.Collections = &client.DiscountCollectionsInput{Remove: live.CollectionIDs}
			}
			return in
		}
		addP, removeP := patterns.DiffIDs(live.ProductIDs, want.ProductIDs)
		addV, removeV := patterns.DiffIDs(live.VariantIDs, want.VariantIDs)
		if addP == nil && removeP == nil && addV == nil && removeV == nil {
			return nil
		}
		return &client.DiscountItemsInput{Products: &client.DiscountProductsInput{
			ProductsToAdd: addP, ProductsToRemove: removeP, ProductVariantsToAdd: addV, ProductVariantsToRemove: removeV,
		}}
	case want.CollectionIDs != nil:
		if live == nil || live.CollectionIDs == nil {
			in := &client.DiscountItemsInput{Collections: &client.DiscountCollectionsInput{Add: want.CollectionIDs}}
			if liveProducts {
				in.Products = &client.DiscountProductsInput{ProductsToRemove: live.ProductIDs, ProductVariantsToRemove: live.VariantIDs}
			}
			return in
		}
		add, remove := patterns.DiffIDs(live.CollectionIDs, want.CollectionIDs)
		if add == nil && remove == nil {
			return nil
		}
		return &client.DiscountItemsInput{Collections: &client.DiscountCollectionsInput{Add: add, Remove: remove}}
	}
	if live != nil && live.All != nil {
		return nil
	}
	all := true
	return &client.DiscountItemsInput{All: &all}
}

func valueFromAPI(v client.DiscountCustomerGetsFieldsValueDiscountCustomerGetsValue) (valueSel, error) {
	switch v := v.(type) {
	case *client.DiscountCustomerGetsFieldsValueDiscountPercentage:
		p := v.Percentage
		return valueSel{Percentage: &p}, nil
	case *client.DiscountCustomerGetsFieldsValueDiscountAmount:
		return valueSel{Amount: &amountSel{Amount: v.Amount.Amount, AppliesOnEachItem: v.AppliesOnEachItem}}, nil
	}
	return valueSel{}, fmt.Errorf("has a value of type %T, which only buy X get Y discounts use", v)
}

func valueInput(v valueSel) *client.DiscountCustomerGetsValueInput {
	if v.Amount != nil {
		return &client.DiscountCustomerGetsValueInput{DiscountAmount: &client.DiscountAmountInput{Amount: &v.Amount.Amount, AppliesOnEachItem: &v.Amount.AppliesOnEachItem}}
	}
	return &client.DiscountCustomerGetsValueInput{Percentage: v.Percentage}
}

// customerGetsInput sends the whole value and the items as a difference against live (nil on create).
func customerGetsInput(want customerGetsSel, live *client.DiscountCustomerGetsFields) (*client.DiscountCustomerGetsInput, error) {
	var have *itemsSel
	if live != nil {
		h, err := itemsFromAPI(live.Items)
		if err != nil {
			return nil, err
		}
		have = &h
	}
	return &client.DiscountCustomerGetsInput{Value: valueInput(want.Value), Items: itemsInput(want.Items, have)}, nil
}

func minimumFromAPI(m *client.DiscountMinimumFields) (minimumSel, error) {
	if m == nil || *m == nil {
		return minimumSel{}, nil
	}
	switch v := (*m).(type) {
	case *client.DiscountMinimumFieldsDiscountMinimumSubtotal:
		s := v.GreaterThanOrEqualToSubtotal.Amount
		return minimumSel{Subtotal: &s}, nil
	case *client.DiscountMinimumFieldsDiscountMinimumQuantity:
		q, err := strconv.ParseInt(v.GreaterThanOrEqualToQuantity, 10, 64)
		if err != nil {
			return minimumSel{}, fmt.Errorf("has a minimum quantity %q that is not a number", v.GreaterThanOrEqualToQuantity)
		}
		return minimumSel{Quantity: &q}, nil
	}
	return minimumSel{}, fmt.Errorf("has a minimum requirement of type %T, which this provider doesn't know", *m)
}

// minimumInput sends both kinds, the unused one as null: minimumRequirement: null is ignored, and Shopify refuses both kinds set at once.
func minimumInput(m minimumSel) *client.DiscountMinimumRequirementInput {
	in := &client.DiscountMinimumRequirementInput{
		Subtotal: &client.DiscountMinimumSubtotalInput{GreaterThanOrEqualToSubtotal: m.Subtotal},
		Quantity: &client.DiscountMinimumQuantityInput{},
	}
	if m.Quantity != nil {
		q := strconv.FormatInt(*m.Quantity, 10)
		in.Quantity.GreaterThanOrEqualToQuantity = &q
	}
	return in
}

// itemsCursors returns where each item list continues, and whether any has more; a complete list gets its end cursor, so its next page is empty.
func itemsCursors(items client.DiscountItemsFields) (products, variants, collections *string, more bool) {
	switch it := items.(type) {
	case *client.DiscountItemsFieldsDiscountProducts:
		return it.Products.PageInfo.EndCursor, it.ProductVariants.PageInfo.EndCursor, nil,
			it.Products.PageInfo.HasNextPage || it.ProductVariants.PageInfo.HasNextPage
	case *client.DiscountItemsFieldsDiscountCollections:
		return nil, nil, it.Collections.PageInfo.EndCursor, it.Collections.PageInfo.HasNextPage
	}
	return nil, nil, nil, false
}

type rawConnection struct {
	Nodes []struct {
		ID string `json:"id"`
	}
	PageInfo struct {
		HasNextPage bool
		EndCursor   *string
	}
}

// mergeItemsPage appends one page of GetDiscountItemsPage to items.
func mergeItemsPage(items client.DiscountItemsFields, raw json.RawMessage) error {
	var page struct {
		Typename                               string `json:"__typename"`
		Products, ProductVariants, Collections rawConnection
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return err
	}
	changed := fmt.Errorf("the discount's items changed while they were read; try again")
	switch it := items.(type) {
	case *client.DiscountItemsFieldsDiscountProducts:
		if page.Typename != "DiscountProducts" {
			return changed
		}
		for _, n := range page.Products.Nodes {
			it.Products.Nodes = append(it.Products.Nodes, client.DiscountItemsFieldsProductsProductConnectionNodesProduct{Id: n.ID})
		}
		for _, n := range page.ProductVariants.Nodes {
			it.ProductVariants.Nodes = append(it.ProductVariants.Nodes, client.DiscountItemsFieldsProductVariantsProductVariantConnectionNodesProductVariant{Id: n.ID})
		}
		it.Products.PageInfo.HasNextPage, it.Products.PageInfo.EndCursor = page.Products.PageInfo.HasNextPage, keepCursor(page.Products, it.Products.PageInfo.EndCursor)
		it.ProductVariants.PageInfo.HasNextPage, it.ProductVariants.PageInfo.EndCursor = page.ProductVariants.PageInfo.HasNextPage, keepCursor(page.ProductVariants, it.ProductVariants.PageInfo.EndCursor)
	case *client.DiscountItemsFieldsDiscountCollections:
		if page.Typename != "DiscountCollections" {
			return changed
		}
		for _, n := range page.Collections.Nodes {
			it.Collections.Nodes = append(it.Collections.Nodes, client.DiscountItemsFieldsCollectionsCollectionConnectionNodesCollection{Id: n.ID})
		}
		it.Collections.PageInfo.HasNextPage, it.Collections.PageInfo.EndCursor = page.Collections.PageInfo.HasNextPage, keepCursor(page.Collections, it.Collections.PageInfo.EndCursor)
	default:
		return changed
	}
	return nil
}

// keepCursor keeps the old end cursor when a page is empty, which has none.
func keepCursor(page rawConnection, old *string) *string {
	if page.PageInfo.EndCursor == nil {
		return old
	}
	return page.PageInfo.EndCursor
}

// completeItems pages the item lists of n after the first 250 that DiscountNodeFields carries.
func completeItems(ctx context.Context, c *client.Client, n *node) error {
	if n.CustomerGets == nil {
		return nil
	}
	for {
		products, variants, collections, more := itemsCursors(n.CustomerGets.Items)
		if !more {
			return nil
		}
		res, err := client.GetDiscountItemsPage(ctx, c, n.ID, products, variants, collections)
		if err != nil {
			return err
		}
		var raw json.RawMessage
		if res.DiscountNode != nil {
			switch d := res.DiscountNode.Discount.(type) {
			case *client.GetDiscountItemsPageDiscountNodeDiscountDiscountCodeBasic:
				raw = d.CustomerGets.Items
			case *client.GetDiscountItemsPageDiscountNodeDiscountDiscountAutomaticBasic:
				raw = d.CustomerGets.Items
			}
		}
		if raw == nil {
			return fmt.Errorf("discount %s disappeared or changed while its items were read", n.ID)
		}
		if err := mergeItemsPage(n.CustomerGets.Items, raw); err != nil {
			return err
		}
	}
}
