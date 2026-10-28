// Package discount implements the discount resources; the method-independent parts live here.
package discount

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/boolvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const (
	methodBasic        = "basic"
	methodBxgy         = "bxgy"
	methodFreeShipping = "free_shipping"
	methodApp          = "app"

	// privateUnsupportedContext holds the live context type Read couldn't map, for ModifyPlan's error.
	privateUnsupportedContext = "unsupported_context"
)

// typenames maps the Discount union's members to method and code-or-automatic.
var typenames = map[string]struct {
	method string
	code   bool
}{
	"DiscountCodeBasic":             {methodBasic, true},
	"DiscountAutomaticBasic":        {methodBasic, false},
	"DiscountCodeBxgy":              {methodBxgy, true},
	"DiscountAutomaticBxgy":         {methodBxgy, false},
	"DiscountCodeFreeShipping":      {methodFreeShipping, true},
	"DiscountAutomaticFreeShipping": {methodFreeShipping, false},
	"DiscountCodeApp":               {methodApp, true},
	"DiscountAutomaticApp":          {methodApp, false},
}

// checkBasic fails unless typename is a basic discount, naming the resource that manages it.
func checkBasic(typename string) error {
	const want = methodBasic
	t, ok := typenames[typename]
	switch {
	case !ok:
		return fmt.Errorf("unknown discount type %s", typename)
	case t.method == methodApp:
		return fmt.Errorf("the discount belongs to an app (%s): its Function's app manages it", typename)
	case t.method != want:
		return fmt.Errorf("the discount is a %s discount; use shopify_discount_%s", t.method, t.method)
	}
	return nil
}

// parseImportKey splits code/<code> or automatic/<title> at the first slash, so codes and titles may contain slashes.
func parseImportKey(id string) (isCode bool, key string, err error) {
	kind, key, ok := strings.Cut(id, "/")
	if !ok || key == "" || (kind != "code" && kind != "automatic") {
		return false, "", fmt.Errorf("import key %q must be code/<code> or automatic/<title>", id)
	}
	return kind == "code", key, nil
}

// node is a live discount with the parts every method shares; method-specific parts stay in their generated types.
type node struct {
	ID, Typename, Method, Title string
	IsCode                      bool
	// Code is the first code, or the state's code while it still belongs to this discount (resolveCode).
	Code                   *string
	CodesCount             int
	StartsAt               string
	EndsAt                 *string
	Status                 string
	UsageLimit             *int
	AppliesOncePerCustomer bool
	CombinesWith           client.DiscountCombinesWithFields
	Context                client.DiscountContextFields
	CustomerGets           *client.DiscountCustomerGetsFields
	Minimum                *client.DiscountMinimumFields
}

func nodeOf(n *client.DiscountNodeFields) (*node, error) {
	if n.Discount == nil {
		return nil, fmt.Errorf("discount %s came back without its details", n.Id)
	}
	out := &node{ID: n.Id}
	if tn := n.Discount.GetTypename(); tn != nil {
		out.Typename = *tn
	}
	t := typenames[out.Typename]
	out.Method, out.IsCode = t.method, t.code
	switch d := n.Discount.(type) {
	case *client.DiscountNodeFieldsDiscountDiscountCodeBasic:
		out.Title, out.StartsAt, out.EndsAt, out.Status = d.Title, d.StartsAt, d.EndsAt, string(d.Status)
		out.UsageLimit, out.AppliesOncePerCustomer = d.UsageLimit, d.AppliesOncePerCustomer
		out.CombinesWith, out.Context, out.CustomerGets, out.Minimum = d.CombinesWith, d.Context, &d.CustomerGets, d.MinimumRequirement
		if len(d.Codes.Nodes) > 0 {
			out.Code = &d.Codes.Nodes[0].Code
			out.CodesCount = 1
		}
		if d.CodesCount != nil {
			out.CodesCount = d.CodesCount.Count
		}
	case *client.DiscountNodeFieldsDiscountDiscountAutomaticBasic:
		out.Title, out.StartsAt, out.EndsAt, out.Status = d.Title, d.StartsAt, d.EndsAt, string(d.Status)
		out.CombinesWith, out.Context, out.CustomerGets, out.Minimum = d.CombinesWith, d.Context, &d.CustomerGets, d.MinimumRequirement
	case *client.DiscountNodeFieldsDiscountDiscountAutomaticBxgy:
		out.Title = d.Title
	case *client.DiscountNodeFieldsDiscountDiscountAutomaticFreeShipping:
		out.Title = d.Title
	case *client.DiscountNodeFieldsDiscountDiscountAutomaticApp:
		out.Title = d.Title
	}
	return out, nil
}

func displayName(n *node) string {
	switch {
	case !n.IsCode:
		return n.Title + " (automatic)"
	case n.Code == nil:
		return n.Title + " (code)"
	case n.CodesCount > 1:
		return fmt.Sprintf("%s (code %s, +%d codes)", n.Title, *n.Code, n.CodesCount-1)
	}
	return fmt.Sprintf("%s (code %s)", n.Title, *n.Code)
}

// contextSel is the context attribute; exactly one field is set.
type contextSel struct {
	All                *bool    `tfsdk:"all"`
	CustomerSegmentIDs []string `tfsdk:"customer_segment_ids"`
	MarketIDs          []string `tfsdk:"market_ids"`
}

// contextFromAPI returns the context, or the name of a context type the schema can't hold.
func contextFromAPI(c client.DiscountContextFields) (*contextSel, string, error) {
	// TODO(verify): whether reading segment and market contexts needs read_customers / read_markets; checked only with every scope granted.
	switch c := c.(type) {
	case *client.DiscountContextFieldsDiscountBuyerSelectionAll:
		all := true
		return &contextSel{All: &all}, "", nil
	case *client.DiscountContextFieldsDiscountCustomerSegments:
		ids := []string{}
		for _, s := range c.Segments {
			ids = append(ids, s.Id)
		}
		return &contextSel{CustomerSegmentIDs: ids}, "", nil
	case *client.DiscountContextFieldsDiscountMarkets:
		// shortcut: the first 250 markets only, page them if a store ever has more.
		if c.Markets.PageInfo.HasNextPage {
			return nil, "", fmt.Errorf("the discount's context has more than 250 markets, which this provider can't read yet")
		}
		ids := []string{}
		for _, m := range c.Markets.Nodes {
			ids = append(ids, m.Id)
		}
		return &contextSel{MarketIDs: ids}, "", nil
	case *client.DiscountContextFieldsDiscountCustomers:
		return nil, "DiscountCustomers", nil
	case *client.DiscountContextFieldsDiscountContextUnknown:
		return nil, fmt.Sprintf("DiscountContextUnknown (%s)", c.ContextType), nil
	}
	return nil, fmt.Sprintf("%T", c), nil
}

// contextInput writes want over live (nil on create): a difference within one kind, otherwise the new kind, which replaces the old.
func contextInput(want contextSel, live *contextSel) *client.DiscountContextInput {
	switch {
	case want.CustomerSegmentIDs != nil:
		if live != nil && live.CustomerSegmentIDs != nil {
			add, remove := patterns.DiffIDs(live.CustomerSegmentIDs, want.CustomerSegmentIDs)
			if add == nil && remove == nil {
				return nil
			}
			return &client.DiscountContextInput{CustomerSegments: &client.DiscountCustomerSegmentsInput{Add: add, Remove: remove}}
		}
		return &client.DiscountContextInput{CustomerSegments: &client.DiscountCustomerSegmentsInput{Add: want.CustomerSegmentIDs}}
	case want.MarketIDs != nil:
		if live != nil && live.MarketIDs != nil {
			add, remove := patterns.DiffIDs(live.MarketIDs, want.MarketIDs)
			if add == nil && remove == nil {
				return nil
			}
			return &client.DiscountContextInput{Markets: &client.DiscountMarketsInput{Add: add, Remove: remove}}
		}
		return &client.DiscountContextInput{Markets: &client.DiscountMarketsInput{Add: want.MarketIDs}}
	}
	if live != nil && live.All != nil {
		return nil
	}
	all := client.DiscountBuyerSelectionAll
	return &client.DiscountContextInput{All: &all}
}

type combinesSel struct {
	ProductDiscounts  bool `tfsdk:"product_discounts"`
	OrderDiscounts    bool `tfsdk:"order_discounts"`
	ShippingDiscounts bool `tfsdk:"shipping_discounts"`
}

func contextAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Who can use the discount: exactly one of `all`, `customer_segment_ids` or `market_ids`. " +
			"A code discount created without it is open to all buyers. Reference `shopify_customer_segment` IDs: deleting a discount's only segment expires the discount and resets it to all buyers, " +
			"and the reference makes Terraform update the discount first. A discount for individual customers, or with a context Shopify doesn't describe, reads as null and can't be managed here. " +
			"Unset means unmanaged.",
		Optional:      true,
		Computed:      true,
		PlanModifiers: []planmodifier.Object{patterns.UseStateForUnknownIncludingNullObject()},
		Attributes: map[string]schema.Attribute{
			"all": schema.BoolAttribute{
				MarkdownDescription: "All buyers. Must be `true`.",
				Optional:            true,
				Validators: []validator.Bool{
					boolvalidator.Equals(true),
					boolvalidator.ExactlyOneOf(sibling("customer_segment_ids", "market_ids")...),
				},
			},
			"customer_segment_ids": schema.SetAttribute{
				MarkdownDescription: "Customer segment GIDs.",
				ElementType:         types.StringType,
				Optional:            true,
				Validators:          []validator.Set{setvalidator.SizeAtLeast(1)},
			},
			"market_ids": schema.SetAttribute{
				MarkdownDescription: "Market GIDs; reference `shopify_market` IDs.",
				ElementType:         types.StringType,
				Optional:            true,
				Validators:          []validator.Set{setvalidator.SizeAtLeast(1)},
			},
		},
	}
}

func combinesAttribute() schema.SingleNestedAttribute {
	flag := func(what string) schema.BoolAttribute {
		return schema.BoolAttribute{
			MarkdownDescription: "Whether it combines with " + what + ". Defaults to `false` when `combines_with` is set.",
			Optional:            true,
			Computed:            true,
			Default:             booldefault.StaticBool(false),
		}
	}
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Which other discount classes it combines with. All `false` when created without it. Unset means unmanaged.",
		Optional:            true,
		Computed:            true,
		PlanModifiers:       []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
		Attributes: map[string]schema.Attribute{
			"product_discounts":  flag("product discounts"),
			"order_discounts":    flag("order discounts"),
			"shipping_discounts": flag("shipping discounts"),
		},
	}
}

func attrTypesOf(a schema.SingleNestedAttribute) map[string]attr.Type {
	t, _ := a.GetType().(types.ObjectType)
	return t.AttrTypes
}

func contextAttrTypes() map[string]attr.Type  { return attrTypesOf(contextAttribute()) }
func combinesAttrTypes() map[string]attr.Type { return attrTypesOf(combinesAttribute()) }

// commonModel holds the attributes every discount method has.
type commonModel struct {
	ID                     types.String `tfsdk:"id"`
	Title                  types.String `tfsdk:"title"`
	Code                   types.String `tfsdk:"code"`
	StartsAt               instantValue `tfsdk:"starts_at"`
	EndsAt                 instantValue `tfsdk:"ends_at"`
	Status                 types.String `tfsdk:"status"`
	Context                types.Object `tfsdk:"context"`
	CombinesWith           types.Object `tfsdk:"combines_with"`
	UsageLimit             types.Int64  `tfsdk:"usage_limit"`
	AppliesOncePerCustomer types.Bool   `tfsdk:"applies_once_per_customer"`
	AdoptExisting          types.Bool   `tfsdk:"adopt_existing"`
}

// commonAttributes is the schema of commonModel.
func commonAttributes() map[string]schema.Attribute {
	unmanaged := " Unset means unmanaged."
	codeOnly := []path.Expression{path.MatchRoot("code")}
	return map[string]schema.Attribute{
		"id": schema.StringAttribute{
			MarkdownDescription: "`DiscountCodeNode` or `DiscountAutomaticNode` GID.",
			Computed:            true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"title": schema.StringAttribute{
			MarkdownDescription: "The discount's name in the admin and at checkout. Unique among automatic discounts; code discounts may share titles.",
			Required:            true,
			Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
		},
		"code": schema.StringAttribute{
			MarkdownDescription: "The code customers enter; unset makes an automatic discount. Unique per shop, ignoring case; Shopify keeps the case as given. " +
				"Changing the code renames it in place, but setting or removing it replaces the discount, which restarts its usage count. " +
				"Discounts with several codes can be imported by any of them, but their code can't be changed here.",
			Optional:   true,
			Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(
				func(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
					resp.RequiresReplace = req.StateValue.IsNull() != req.ConfigValue.IsNull()
				},
				"Switching between a code and an automatic discount replaces it.",
				"Switching between a code and an automatic discount replaces it.",
			)},
		},
		"starts_at": schema.StringAttribute{
			MarkdownDescription: "When the discount starts, as an RFC 3339 time such as `2026-11-01T00:00:00+01:00`. Shopify returns UTC; the instant is compared, so the configured spelling stays. " +
				"Created as now when unset." + unmanaged,
			CustomType:    instantType{},
			Optional:      true,
			Computed:      true,
			Validators:    []validator.String{rfc3339Validator{}},
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"ends_at": schema.StringAttribute{
			MarkdownDescription: "When the discount ends, as an RFC 3339 time; null means no end. Setting it in the past ends the discount. " +
				"An end date set in the admin can't be removed here: set a far end date instead." + unmanaged,
			CustomType:    instantType{},
			Optional:      true,
			Computed:      true,
			Validators:    []validator.String{rfc3339Validator{}},
			PlanModifiers: []planmodifier.String{patterns.UseStateForUnknownIncludingNull()},
		},
		"status": schema.StringAttribute{
			MarkdownDescription: "`ACTIVE`, `SCHEDULED` or `EXPIRED`, from the dates.",
			Computed:            true,
		},
		"context":       contextAttribute(),
		"combines_with": combinesAttribute(),
		"usage_limit": schema.Int64Attribute{
			MarkdownDescription: "How often the code can be used in total; null means unlimited. Code discounts only. " +
				"A limit set in the admin can't be removed here: set a high limit instead." + unmanaged,
			Optional:      true,
			Computed:      true,
			Validators:    []validator.Int64{int64validator.AtLeast(1), int64validator.AlsoRequires(codeOnly...)},
			PlanModifiers: []planmodifier.Int64{patterns.UseStateForUnknownIncludingNullInt64()},
		},
		"applies_once_per_customer": schema.BoolAttribute{
			MarkdownDescription: "Whether each customer can use the code only once. Code discounts only." + unmanaged,
			Optional:            true,
			Computed:            true,
			Validators:          []validator.Bool{boolvalidator.AlsoRequires(codeOnly...)},
			PlanModifiers:       []planmodifier.Bool{patterns.UseStateForUnknownIncludingNullBool()},
		},
		"adopt_existing": patterns.AdoptExistingAttribute("discount"),
	}
}

// takeDefaults copies objects whose nested defaults only the plan holds; config still decides whether combines_with is managed.
func (m *commonModel) takeDefaults(plan *commonModel) {
	if !m.CombinesWith.IsNull() {
		m.CombinesWith = plan.CombinesWith
	}
}

// commonInput is the part of every create and update input that all methods share.
type commonInput struct {
	Title, StartsAt, EndsAt, Code *string
	UsageLimit                    *int
	AppliesOncePerCustomer        *bool
	CombinesWith                  *client.DiscountCombinesWithInput
	Context                       *client.DiscountContextInput
}

// input maps the configured attributes against live (nil on create); m is the config, so unset attributes are never sent.
func (m *commonModel) input(ctx context.Context, live *node) (commonInput, diag.Diagnostics) {
	var diags diag.Diagnostics
	in := commonInput{
		Title:                  m.Title.ValueStringPointer(),
		StartsAt:               patterns.ConfiguredString(m.StartsAt.StringValue),
		EndsAt:                 patterns.ConfiguredString(m.EndsAt.StringValue),
		AppliesOncePerCustomer: patterns.ConfiguredBool(m.AppliesOncePerCustomer),
	}
	// Shopify requires a start on create.
	if live == nil && in.StartsAt == nil {
		s := now().UTC().Format(time.RFC3339)
		in.StartsAt = &s
	}
	if !m.UsageLimit.IsNull() && !m.UsageLimit.IsUnknown() {
		n := int(m.UsageLimit.ValueInt64())
		in.UsageLimit = &n
	}
	code := patterns.ConfiguredString(m.Code)
	switch {
	case code == nil || live == nil || live.Code == nil:
		in.Code = code
	case *code == *live.Code:
	case live.CodesCount > 1:
		// Shopify refuses it anyway ("Cannot update the code of a bulk discount."), even for one of the discount's own codes.
		diags.AddAttributeError(path.Root("code"), "Code can't be changed",
			fmt.Sprintf("The discount has %d codes, and Shopify doesn't change the code of a discount with several codes. Change the codes in the admin.", live.CodesCount))
	default:
		in.Code = code
	}
	if !m.Context.IsNull() && !m.Context.IsUnknown() {
		var want contextSel
		diags.Append(m.Context.As(ctx, &want, basetypes.ObjectAsOptions{})...)
		var have *contextSel
		if live != nil {
			h, unsupported, err := contextFromAPI(live.Context)
			switch {
			case err != nil:
				diags.AddAttributeError(path.Root("context"), "Context can't be managed", err.Error())
			case unsupported != "":
				diags.AddAttributeError(path.Root("context"), "Context can't be managed", unsupportedContextDetail(unsupported))
			}
			have = h
		}
		in.Context = contextInput(want, have)
	} else if live == nil && code != nil {
		// Shopify requires a context for code discounts.
		all := client.DiscountBuyerSelectionAll
		in.Context = &client.DiscountContextInput{All: &all}
	}
	if !m.CombinesWith.IsNull() && !m.CombinesWith.IsUnknown() {
		// All three, since an omitted one defaults to false and would overwrite the admin's value.
		var c combinesSel
		diags.Append(m.CombinesWith.As(ctx, &c, basetypes.ObjectAsOptions{})...)
		in.CombinesWith = &client.DiscountCombinesWithInput{ProductDiscounts: &c.ProductDiscounts, OrderDiscounts: &c.OrderDiscounts, ShippingDiscounts: &c.ShippingDiscounts}
	}
	return in, diags
}

func unsupportedContextDetail(kind string) string {
	return fmt.Sprintf("The discount's context is %s, which this provider doesn't manage (individual customers are records, not configuration). "+
		"Remove context from the configuration, or change the context in the admin first.", kind)
}

// setFromAPI copies the shared attributes of n into m; it returns the live context type when the schema can't hold it.
func (m *commonModel) setFromAPI(ctx context.Context, n *node) (string, diag.Diagnostics) {
	var diags diag.Diagnostics
	m.ID = types.StringValue(n.ID)
	m.Title = types.StringValue(n.Title)
	m.Status = types.StringValue(n.Status)
	m.StartsAt = instantString(n.StartsAt)
	m.EndsAt = instantNull()
	if n.EndsAt != nil {
		m.EndsAt = instantString(*n.EndsAt)
	}
	m.Code, m.UsageLimit, m.AppliesOncePerCustomer = types.StringNull(), types.Int64Null(), types.BoolNull()
	if n.IsCode {
		if n.Code == nil {
			diags.AddError("Unsupported discount", fmt.Sprintf("Code discount %q has no code.", n.Title))
		}
		m.Code = types.StringPointerValue(n.Code)
		if n.UsageLimit != nil {
			m.UsageLimit = types.Int64Value(int64(*n.UsageLimit))
		}
		m.AppliesOncePerCustomer = types.BoolValue(n.AppliesOncePerCustomer)
	}
	var d diag.Diagnostics
	m.CombinesWith, d = types.ObjectValueFrom(ctx, combinesAttrTypes(), combinesSel{
		ProductDiscounts: n.CombinesWith.ProductDiscounts, OrderDiscounts: n.CombinesWith.OrderDiscounts, ShippingDiscounts: n.CombinesWith.ShippingDiscounts,
	})
	diags.Append(d...)
	m.Context = types.ObjectNull(contextAttrTypes())
	sel, unsupported, err := contextFromAPI(n.Context)
	switch {
	case err != nil:
		diags.AddError("Unsupported discount", fmt.Sprintf("Discount %q: %s.", n.Title, err))
	case sel != nil:
		m.Context, d = types.ObjectValueFrom(ctx, contextAttrTypes(), sel)
		diags.Append(d...)
	}
	return unsupported, diags
}

// errorPath maps a DiscountUserError to an attribute; field is relative to the input, such as ["automaticBasicDiscount","title"].
func errorPath(e client.DiscountUserErrorFields) (path.Path, bool) {
	if e.Code != nil {
		switch *e.Code {
		case client.DiscountErrorCodeEndDateBeforeStartDate:
			return path.Root("ends_at"), true
		case client.DiscountErrorCodeMinimumSubtotalAndQuantityRangeBothPresent:
			return path.Root("minimum_requirement"), true
		case client.DiscountErrorCodeInvalidCombinesWithForDiscountClass:
			return path.Root("combines_with"), true
		}
	}
	if len(e.Field) < 2 {
		return path.Empty(), false
	}
	name, ok := inputAttributes[e.Field[1]]
	if !ok {
		return path.Empty(), false
	}
	p := path.Root(name)
	// Deeper input fields (add/remove lists) have no attribute of their own.
	if name == "customer_gets" && len(e.Field) > 2 && (e.Field[2] == "value" || e.Field[2] == "items") {
		p = p.AtName(e.Field[2])
	}
	return p, true
}

var inputAttributes = map[string]string{
	"title": "title", "code": "code", "startsAt": "starts_at", "endsAt": "ends_at", "context": "context",
	"combinesWith": "combines_with", "usageLimit": "usage_limit", "appliesOncePerCustomer": "applies_once_per_customer",
	"customerGets": "customer_gets", "minimumRequirement": "minimum_requirement",
}

// userErrorDiags keeps each message, with extraInfo appended.
func userErrorDiags(summary string, errs []client.DiscountUserErrorFields) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, e := range errs {
		msg := e.Message
		if e.ExtraInfo != nil && *e.ExtraInfo != "" {
			msg += " (" + *e.ExtraInfo + ")"
		}
		if p, ok := errorPath(e); ok {
			diags.AddAttributeError(p, summary, msg)
		} else {
			diags.AddError(summary, msg)
		}
	}
	return diags
}

// getNode returns nil when no discount has the GID.
func getNode(ctx context.Context, c *client.Client, id string) (*node, error) {
	res, err := client.GetDiscount(ctx, c, id)
	if err != nil || res.DiscountNode == nil {
		return nil, err
	}
	n, err := nodeOf(&res.DiscountNode.DiscountNodeFields)
	if err != nil {
		return nil, err
	}
	return n, completeItems(ctx, c, n)
}

// findByKey looks a discount up by its code, or an automatic one by its exact title; nil when there is none.
func findByKey(ctx context.Context, c *client.Client, isCode bool, key string) (*node, error) {
	if !isCode {
		return findAutomaticByTitle(ctx, c, key)
	}
	res, err := client.GetDiscountByCode(ctx, c, key)
	if err != nil || res.CodeDiscountNodeByCode == nil {
		return nil, err
	}
	n, err := getNode(ctx, c, res.CodeDiscountNodeByCode.Id)
	if err != nil || n == nil {
		return n, err
	}
	return n, resolveCode(ctx, c, n, &key)
}

// findAutomaticByTitle pages the search results, since search matches words, not the exact title.
func findAutomaticByTitle(ctx context.Context, c *client.Client, title string) (*node, error) {
	q := fmt.Sprintf(`method:automatic AND title:"%s"`, strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(title))
	var found *node
	var nodeErr error
	err := patterns.Paginate(ctx, 0, listPage(ctx, c, &q), func(n client.DiscountNodeFields) bool {
		nn, err := nodeOf(&n)
		if err != nil {
			nodeErr = err
			return false
		}
		if !nn.IsCode && nn.Title == title {
			found = nn
			return false
		}
		return true
	})
	if err == nil {
		err = nodeErr
	}
	if err == nil && found != nil {
		err = completeItems(ctx, c, found)
	}
	return found, err
}

// listPage is one page of discountNodes, optionally filtered by a search query.
func listPage(ctx context.Context, c *client.Client, query *string) func(after *string) ([]client.DiscountNodeFields, *string, error) {
	return func(after *string) ([]client.DiscountNodeFields, *string, error) {
		res, err := client.ListDiscounts(ctx, c, after, query)
		if err != nil {
			return nil, nil, err
		}
		conn := res.DiscountNodes
		out := make([]client.DiscountNodeFields, len(conn.Nodes))
		for i := range conn.Nodes {
			out[i] = conn.Nodes[i].DiscountNodeFields
		}
		return out, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}

// resolveCode keeps want as the code while it still belongs to n, so a discount with several codes keeps the one it was imported by.
func resolveCode(ctx context.Context, c *client.Client, n *node, want *string) error {
	if !n.IsCode || want == nil || n.Code == nil || *want == *n.Code || n.CodesCount < 2 {
		return nil
	}
	res, err := client.GetDiscountByCode(ctx, c, *want)
	if err != nil {
		return err
	}
	if res.CodeDiscountNodeByCode != nil && res.CodeDiscountNodeByCode.Id == n.ID {
		n.Code = want
	}
	return nil
}

// deleteDiscount deletes the discount with the GID; one already gone is no error.
func deleteDiscount(ctx context.Context, c *client.Client, id string) diag.Diagnostics {
	var diags diag.Diagnostics
	summary := "Failed to delete discount"
	// Look first instead of matching the "does not exist" messages, one of which is unverified.
	n, err := getNode(ctx, c, id)
	if err != nil {
		diags.AddError(summary, err.Error())
		return diags
	}
	if n == nil {
		return diags
	}
	var errs []client.DiscountUserErrorFields
	if n.IsCode {
		res, err := client.DeleteDiscountCode(ctx, c, id)
		switch {
		case err != nil:
			diags.AddError(summary, err.Error())
		case res.DiscountCodeDelete == nil:
			diags.AddError(summary, "Shopify returned no result.")
		default:
			errs = res.DiscountCodeDelete.UserErrors
		}
	} else {
		res, err := client.DeleteDiscountAutomatic(ctx, c, id)
		switch {
		case err != nil:
			diags.AddError(summary, err.Error())
		case res.DiscountAutomaticDelete == nil:
			diags.AddError(summary, "Shopify returned no result.")
		default:
			errs = res.DiscountAutomaticDelete.UserErrors
		}
	}
	diags.Append(userErrorDiags(summary, errs)...)
	return diags
}

// rememberContext stores the unsupported live context type for ModifyPlan, or clears it.
func rememberContext(ctx context.Context, private patterns.PrivateSetter, unsupported string) diag.Diagnostics {
	var b []byte
	if unsupported != "" {
		b, _ = json.Marshal(unsupported)
	}
	return private.SetKey(ctx, privateUnsupportedContext, b)
}

// modifyPlan warns on a replace and fails early when config sets a context the live discount can't take.
func modifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	// Resource-level ModifyPlan runs after the attribute modifiers, so the plan is final here.
	resp.Diagnostics.Append(keepStatus(ctx, req.State, &resp.Plan)...)
	var stateCode, planCode types.String
	var stateContext, cfgContext types.Object
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("code"), &stateCode)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("code"), &planCode)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("context"), &stateContext)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("context"), &cfgContext)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if stateCode.IsNull() != planCode.IsNull() {
		resp.Diagnostics.AddWarning("This change replaces the discount",
			"Setting or removing code deletes the discount and creates a new one. Its usage count restarts: usage_limit and applies_once_per_customer count from zero again, "+
				"so customers can use a once-per-customer code again. Orders keep their discount applications.")
	}
	// Read leaves context null only when the live context type is unsupported.
	if stateContext.IsNull() && !cfgContext.IsNull() {
		kind := "of a type this provider doesn't manage"
		b, d := req.Private.GetKey(ctx, privateUnsupportedContext)
		resp.Diagnostics.Append(d...)
		var s string
		if len(b) > 0 && json.Unmarshal(b, &s) == nil {
			kind = s
		}
		resp.Diagnostics.AddAttributeError(path.Root("context"), "Context can't be managed", unsupportedContextDetail(kind))
	}
}

// keepStatus takes the state's status only when the plan then equals the state, so no-op plans stay empty and real updates leave it unknown.
func keepStatus(ctx context.Context, state tfsdk.State, plan *tfsdk.Plan) diag.Diagnostics {
	var status types.String
	diags := state.GetAttribute(ctx, path.Root("status"), &status)
	candidate := *plan
	diags.Append(candidate.SetAttribute(ctx, path.Root("status"), status)...)
	if !diags.HasError() && candidate.Raw.Equal(state.Raw) {
		*plan = candidate
	}
	return diags
}
