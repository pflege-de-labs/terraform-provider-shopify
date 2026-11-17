package discount

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/boolvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const whatFreeShipping = "free shipping discount"

var (
	_ resource.ResourceWithConfigure   = &FreeShippingResource{}
	_ resource.ResourceWithImportState = &FreeShippingResource{}
	_ resource.ResourceWithIdentity    = &FreeShippingResource{}
	_ resource.ResourceWithModifyPlan  = &FreeShippingResource{}

	countryCodeRe = regexp.MustCompile(`^[A-Z]{2}$`)
)

func NewFreeShippingResource() resource.Resource {
	return &FreeShippingResource{discountResource[freeShippingModel, *freeShippingModel]{method: methodFreeShipping, what: whatFreeShipping}}
}

// FreeShippingResource manages one free shipping discount, as a code or an automatic discount.
type FreeShippingResource struct {
	discountResource[freeShippingModel, *freeShippingModel]
}

type freeShippingModel struct {
	commonModel
	Destination              types.Object `tfsdk:"destination"`
	MaximumShippingPrice     decimalValue `tfsdk:"maximum_shipping_price"`
	MinimumRequirement       types.Object `tfsdk:"minimum_requirement"`
	AppliesOnSubscription    types.Bool   `tfsdk:"applies_on_subscription"`
	AppliesOnOneTimePurchase types.Bool   `tfsdk:"applies_on_one_time_purchase"`
}

func (m *freeShippingModel) common() *commonModel { return &m.commonModel }

// destinationSel is the destination attribute; exactly one field is set. An empty country list stays [] (no country), as for items.
type destinationSel struct {
	All          *bool    `tfsdk:"all"`
	CountryCodes []string `tfsdk:"country_codes"`
}

func destinationAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Where the discount applies: exactly one of `all` or `country_codes`. Changes are written as additions and removals against the live countries. " +
			"Shopify rejects a country that is in none of the store's shipping zones, and silently drops one that is in a zone but not offered (a plan then keeps showing it). Unset means unmanaged.",
		Optional:      true,
		Computed:      true,
		PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
		Attributes: map[string]schema.Attribute{
			"all": schema.BoolAttribute{
				MarkdownDescription: "All countries. Must be `true`.",
				Optional:            true,
				Validators: []validator.Bool{
					boolvalidator.Equals(true),
					boolvalidator.ExactlyOneOf(sibling("country_codes")...),
				},
			},
			"country_codes": schema.SetAttribute{
				MarkdownDescription: "ISO 3166-1 alpha-2 country codes such as `DE`.",
				ElementType:         types.StringType,
				Optional:            true,
				Validators:          []validator.Set{setvalidator.ValueStringsAre(stringvalidator.RegexMatches(countryCodeRe, "must be an upper-case ISO country code such as DE"))},
			},
		},
	}
}

func destinationAttrTypes() map[string]attr.Type { return attrTypesOf(destinationAttribute()) }

func (r *FreeShippingResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_discount_free_shipping"
	// Read re-adopts a discount recreated outside Terraform by its code or title, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *FreeShippingResource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *FreeShippingResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := commonAttributes()
	attrs["destination"] = destinationAttribute()
	attrs["maximum_shipping_price"] = schema.StringAttribute{
		MarkdownDescription: "Only shipping rates up to this price are free, as a decimal string in the shop currency; null means all rates. Compared numerically, so `\"20\"` matches Shopify's `\"20.0\"`. " +
			"A limit set in the admin can't be removed here: set a high one instead. Unset means unmanaged.",
		CustomType:    decimalType{},
		Optional:      true,
		Computed:      true,
		Validators:    []validator.String{decimalValidator},
		PlanModifiers: []planmodifier.String{patterns.UseStateForUnknownIncludingNull()},
	}
	attrs["minimum_requirement"] = minimumAttribute()
	flag := func(what string) schema.BoolAttribute {
		return schema.BoolAttribute{
			MarkdownDescription: "Whether the discount applies on " + what + ". Shopify accepts these only for shops that use subscriptions, and then refuses even the default values, " +
				"so leave them unset elsewhere (unset means unmanaged).",
			Optional:      true,
			Computed:      true,
			PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
		}
	}
	attrs["applies_on_subscription"] = flag("subscription items")
	attrs["applies_on_one_time_purchase"] = flag("one-time purchases")
	resp.Schema = schema.Schema{
		MarkdownDescription: "A free shipping discount: shipping is free to all countries or selected ones, optionally up to a maximum shipping price, " +
			"as a code discount (`code` set) or an automatic one (`code` unset). " +
			"**Volatile, usually a poor fit for Terraform:** promotions are created, changed and ended in the admin by marketing, and every admin edit to a managed attribute is reverted on the next apply. " +
			"Long-running rules, such as free shipping above a threshold, fit Terraform; attributes you leave unset are not managed. " +
			"Destroying a discount deletes it; orders keep their discount applications, but a recreated discount counts its uses from zero. " +
			"Bulk codes, tags, recurring cycle limits and app discounts are not managed. " +
			"Generated config holds segment and market GIDs, which tie it to the store: replace them with references. " +
			"An existing discount with the same code, or an automatic one with the same title, needs `adopt_existing = true`; destroy then leaves it in place. " +
			"Import with `code/<code>` or `automatic/<title>`. A free shipping discount can't combine with other shipping discounts.",
		Attributes: attrs,
	}
}

// destinationFromAPI maps the live destination; Shopify only describes all countries or a list.
func destinationFromAPI(d client.DiscountDestinationFields) (destinationSel, error) {
	switch d := d.(type) {
	case *client.DiscountDestinationFieldsDiscountCountryAll:
		all := true
		return destinationSel{All: &all}, nil
	case *client.DiscountDestinationFieldsDiscountCountries:
		codes := []string{}
		for _, c := range d.Countries {
			codes = append(codes, string(c))
		}
		return destinationSel{CountryCodes: codes}, nil
	}
	return destinationSel{}, fmt.Errorf("has a destination of type %T, which this provider doesn't know", d)
}

// destinationInput writes want over live (nil on create): countries as a difference, `all` replacing them; nil when nothing changes.
func destinationInput(want destinationSel, live *destinationSel) *client.DiscountShippingDestinationSelectionInput {
	if want.CountryCodes == nil {
		if live != nil && live.All != nil {
			return nil
		}
		all := true
		return &client.DiscountShippingDestinationSelectionInput{All: &all}
	}
	var liveCodes []string
	if live != nil {
		liveCodes = live.CountryCodes
	}
	add, remove := patterns.DiffIDs(liveCodes, want.CountryCodes)
	if add == nil && remove == nil && live != nil && live.CountryCodes != nil {
		return nil
	}
	return &client.DiscountShippingDestinationSelectionInput{Countries: &client.DiscountCountriesInput{Add: countryCodes(add), Remove: countryCodes(remove)}}
}

func countryCodes(in []string) []client.CountryCode {
	var out []client.CountryCode
	for _, c := range in {
		out = append(out, client.CountryCode(c))
	}
	return out
}

// freeShippingInputs are the method-specific parts of both create and update inputs.
type freeShippingInputs struct {
	common                                          commonInput
	destination                                     *client.DiscountShippingDestinationSelectionInput
	maximum                                         *string
	minimum                                         *client.DiscountMinimumRequirementInput
	appliesOnSubscription, appliesOnOneTimePurchase *bool
}

// inputs maps the configured attributes against live (nil on create).
func (m *freeShippingModel) inputs(ctx context.Context, live *node) (freeShippingInputs, diag.Diagnostics) {
	common, diags := m.input(ctx, live)
	in := freeShippingInputs{
		common:                   common,
		maximum:                  patterns.ConfiguredString(m.MaximumShippingPrice.StringValue),
		appliesOnSubscription:    patterns.ConfiguredBool(m.AppliesOnSubscription),
		appliesOnOneTimePurchase: patterns.ConfiguredBool(m.AppliesOnOneTimePurchase),
	}
	if !m.Destination.IsNull() && !m.Destination.IsUnknown() {
		var want destinationSel
		diags.Append(m.Destination.As(ctx, &want, basetypes.ObjectAsOptions{})...)
		var have *destinationSel
		if live != nil {
			h, err := destinationFromAPI(live.Destination)
			if err != nil {
				diags.AddAttributeError(path.Root("destination"), "Destination can't be managed", fmt.Sprintf("The discount %s.", err))
			}
			have = &h
		}
		in.destination = destinationInput(want, have)
	}
	if !m.MinimumRequirement.IsNull() && !m.MinimumRequirement.IsUnknown() {
		var ms minimumSel
		diags.Append(m.MinimumRequirement.As(ctx, &ms, basetypes.ObjectAsOptions{})...)
		in.minimum = minimumInput(ms)
	}
	return in, diags
}

func (m *freeShippingModel) codeInput(ctx context.Context, live *node) (client.DiscountCodeFreeShippingInput, diag.Diagnostics) {
	i, diags := m.inputs(ctx, live)
	c := i.common
	return client.DiscountCodeFreeShippingInput{
		Title: c.Title, StartsAt: c.StartsAt, EndsAt: c.EndsAt, Code: c.Code, UsageLimit: c.UsageLimit, AppliesOncePerCustomer: c.AppliesOncePerCustomer,
		CombinesWith: c.CombinesWith, Context: c.Context, MinimumRequirement: i.minimum, Destination: i.destination, MaximumShippingPrice: i.maximum,
		AppliesOnSubscription: i.appliesOnSubscription, AppliesOnOneTimePurchase: i.appliesOnOneTimePurchase,
	}, diags
}

func (m *freeShippingModel) automaticInput(ctx context.Context, live *node) (client.DiscountAutomaticFreeShippingInput, diag.Diagnostics) {
	i, diags := m.inputs(ctx, live)
	c := i.common
	return client.DiscountAutomaticFreeShippingInput{
		Title: c.Title, StartsAt: c.StartsAt, EndsAt: c.EndsAt,
		CombinesWith: c.CombinesWith, Context: c.Context, MinimumRequirement: i.minimum, Destination: i.destination, MaximumShippingPrice: i.maximum,
		AppliesOnSubscription: i.appliesOnSubscription, AppliesOnOneTimePurchase: i.appliesOnOneTimePurchase,
	}, diags
}

// write: see kindModel.
func (cfg *freeShippingModel) write(ctx context.Context, c *client.Client, plan *freeShippingModel, live *node, diags *diag.Diagnostics) string {
	cfg.takeDefaults(&plan.commonModel)
	isCode := !cfg.Code.IsNull()
	if live != nil {
		isCode = live.IsCode
	}
	var id *string
	var errs []client.DiscountUserErrorFields
	var err error
	if isCode {
		in, d := cfg.codeInput(ctx, live)
		if diags.Append(d...); diags.HasError() {
			return ""
		}
		if live == nil {
			var res *client.CreateDiscountCodeFreeShippingResponse
			if res, err = client.CreateDiscountCodeFreeShipping(ctx, c, in); err == nil && res.DiscountCodeFreeShippingCreate != nil {
				errs = res.DiscountCodeFreeShippingCreate.UserErrors
				if n := res.DiscountCodeFreeShippingCreate.CodeDiscountNode; n != nil {
					id = &n.Id
				}
			}
		} else {
			var res *client.UpdateDiscountCodeFreeShippingResponse
			if res, err = client.UpdateDiscountCodeFreeShipping(ctx, c, live.ID, in); err == nil && res.DiscountCodeFreeShippingUpdate != nil {
				errs = res.DiscountCodeFreeShippingUpdate.UserErrors
				if n := res.DiscountCodeFreeShippingUpdate.CodeDiscountNode; n != nil {
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
			var res *client.CreateDiscountAutomaticFreeShippingResponse
			if res, err = client.CreateDiscountAutomaticFreeShipping(ctx, c, in); err == nil && res.DiscountAutomaticFreeShippingCreate != nil {
				errs = res.DiscountAutomaticFreeShippingCreate.UserErrors
				if n := res.DiscountAutomaticFreeShippingCreate.AutomaticDiscountNode; n != nil {
					id = &n.Id
				}
			}
		} else {
			var res *client.UpdateDiscountAutomaticFreeShippingResponse
			if res, err = client.UpdateDiscountAutomaticFreeShipping(ctx, c, live.ID, in); err == nil && res.DiscountAutomaticFreeShippingUpdate != nil {
				errs = res.DiscountAutomaticFreeShippingUpdate.UserErrors
				if n := res.DiscountAutomaticFreeShippingUpdate.AutomaticDiscountNode; n != nil {
					id = &n.Id
				}
			}
		}
	}
	return writeOutcome(diags, writeSummary(whatFreeShipping, live), err, errs, id)
}

// setFromAPI copies n into m; shapes the schema can't hold are errors, so import fails and list skips them.
//
//nolint:unparam // the context result is part of kindModel; only basic reads it so far
func (m *freeShippingModel) setFromAPI(ctx context.Context, n *node) (string, diag.Diagnostics) {
	unsupported, diags := m.commonModel.setFromAPI(ctx, n)
	m.Destination, m.MinimumRequirement = types.ObjectNull(destinationAttrTypes()), types.ObjectNull(minimumAttrTypes())
	m.MaximumShippingPrice = decimalNull()
	m.AppliesOnSubscription, m.AppliesOnOneTimePurchase = types.BoolValue(n.AppliesOnSubscription), types.BoolValue(n.AppliesOnOneTimePurchase)
	if n.MaximumShippingPrice != nil {
		m.MaximumShippingPrice = decimalString(*n.MaximumShippingPrice)
	}
	dest, err1 := destinationFromAPI(n.Destination)
	minimum, err2 := minimumFromAPI(n.Minimum)
	for _, err := range []error{err1, err2} {
		if err != nil {
			diags.AddError("Unsupported discount", fmt.Sprintf("Discount %q %s.", n.Title, err))
		}
	}
	if diags.HasError() {
		return unsupported, diags
	}
	var d diag.Diagnostics
	m.Destination, d = types.ObjectValueFrom(ctx, destinationAttrTypes(), dest)
	diags.Append(d...)
	m.MinimumRequirement, d = types.ObjectValueFrom(ctx, minimumAttrTypes(), minimum)
	diags.Append(d...)
	return unsupported, diags
}
