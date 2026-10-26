// Package product implements the shopify_product resource.
package product

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const (
	what = "product"
	// writtenKey holds the description Shopify returned at the last write, to tell repaired HTML from an admin edit.
	writtenKey = "description_html_written"
)

var (
	_ resource.ResourceWithConfigure      = &Resource{}
	_ resource.ResourceWithImportState    = &Resource{}
	_ resource.ResourceWithIdentity       = &Resource{}
	_ resource.ResourceWithModifyPlan     = &Resource{}
	_ resource.ResourceWithValidateConfig = &Resource{}
)

var decimalRe = regexp.MustCompile(`^\d+(\.\d+)?$`)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one product.
type Resource struct {
	client *client.Client
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_product"
	// Read re-adopts a product recreated outside Terraform by its handle, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

// TODO(verify): whether any large product needs productSet with synchronous: false and productOperation polling.
func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	unmanaged := " Unset means unmanaged: the value set in the admin is kept."
	resp.Schema = schema.Schema{
		MarkdownDescription: "A product's core fields: title, handle, status, description, tags and SEO. " +
			"**A poor fit for Terraform unless you bootstrap a store or keep a small catalogue in a repository:** products are content that merchants and other systems (PIM, ERP, apps) edit in the admin all the time, " +
			"and every admin edit to a managed attribute is reverted on the next apply. Attributes you leave unset are not managed, which keeps that small. " +
			"Publishing and media are not managed yet; Shopify keeps whatever they hold. " +
			"Destroying a product deletes its variants, inventory, media and metafield values: archive it with `status = \"ARCHIVED\"` instead, or set `allow_data_deletion = true` first, and add `lifecycle { prevent_destroy = true }` to guard it. " +
			"A product that already has the handle needs `adopt_existing = true`, so it isn't overwritten silently. " +
			"Import with the handle.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Product GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"handle": schema.StringAttribute{
				MarkdownDescription: "The product's URL handle, unique per shop. Changing it renames the product in place and creates no URL redirect. Lower case, no whitespace: Shopify lower-cases anything else.",
				Required:            true,
				Validators:          []validator.String{handleValidator{}},
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "The product title.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "`ACTIVE`, `DRAFT`, `ARCHIVED` or `UNLISTED`. A new product is `ACTIVE` when this is unset." + unmanaged,
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.OneOf(statusNames()...)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"description_html": schema.StringAttribute{
				MarkdownDescription: "The description as HTML. Shopify repairs malformed HTML; the configured text is kept while Shopify still holds what it returned at the last apply, so the repair is no diff, but a later admin edit is." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"vendor": schema.StringAttribute{
				MarkdownDescription: "The vendor. Shopify uses the shop name when a new product has none." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"product_type": schema.StringAttribute{
				MarkdownDescription: "Free-text product type, an empty string when none is set." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"category_id": schema.StringAttribute{
				MarkdownDescription: "Standard product taxonomy category GID, such as `gid://shopify/TaxonomyCategory/aa-1`." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{patterns.UseStateForUnknownIncludingNull()},
			},
			"tags": schema.SetAttribute{
				MarkdownDescription: "Tags. Shopify trims them, drops case-insensitive duplicates and sorts them, so tags with surrounding whitespace or duplicates are rejected. `[]` clears the tags." + unmanaged,
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Validators:          []validator.Set{tagsValidator{}},
				PlanModifiers:       []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"seo_title": schema.StringAttribute{
				MarkdownDescription: "Search engine title. Null while Shopify uses the product title." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{patterns.UseStateForUnknownIncludingNull()},
			},
			"seo_description": schema.StringAttribute{
				MarkdownDescription: "Search engine description. Null while Shopify uses the product description." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{patterns.UseStateForUnknownIncludingNull()},
			},
			"template_suffix": schema.StringAttribute{
				MarkdownDescription: "Theme template suffix, such as `special` for `product.special.json`." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{patterns.UseStateForUnknownIncludingNull()},
			},
			"gift_card": schema.BoolAttribute{
				MarkdownDescription: "Whether this is a gift card product, a catalogue entry; issued gift cards are not managed. Shopify can't change it, so changing it replaces the product and deletes its data." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown(), boolplanmodifier.RequiresReplace()},
			},
			"options":             optionsAttribute(),
			"variants":            variantsAttribute(),
			"adopt_existing":      patterns.AdoptExistingAttribute(what),
			"allow_data_deletion": patterns.AllowDataDeletionAttribute(what + ", its variants, inventory and media"),
		},
	}
}

func optionsAttribute() schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		MarkdownDescription: "Options in position order, at most 3, each with its values in position order. Set together with `variants`; when both are unset they are unmanaged. " +
			"`[]` is a product without options, which has one default variant. " +
			"Renaming an option or value where it stands keeps its variants. A new name at another position counts as removing the old value and adding a new one, so rename and reorder in separate applies.",
		Optional:      true,
		Computed:      true,
		PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
		Validators:    []validator.List{listvalidator.SizeAtMost(maxOptions)},
		NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Option name, such as `Size`.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"values": schema.ListAttribute{
				MarkdownDescription: "Option values in position order. A value no variant uses is allowed.",
				ElementType:         types.StringType,
				Required:            true,
				Validators:          []validator.List{listvalidator.SizeAtLeast(1)},
			},
		}},
	}
}

func variantsAttribute() schema.ListNestedAttribute {
	decimal := []validator.String{stringvalidator.RegexMatches(decimalRe, "must be a decimal number such as 19.90")}
	unmanaged := " Unset means unmanaged."
	return schema.ListNestedAttribute{
		MarkdownDescription: "Variants in position order, each identified by its option values (not its SKU). Required when `options` is set. " +
			"**Removing a variant, or an option or option value its variants use, deletes those variants with their inventory items and stock;** such a change fails unless `allow_data_deletion = true`. " +
			"Inventory quantities, cost, unit prices, variant metafields and media are not managed.",
		Optional:      true,
		Computed:      true,
		PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
		NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"option_values": schema.MapAttribute{
				MarkdownDescription: "Option name to value, one entry per option. `{}` for the default variant of a product without options.",
				ElementType:         types.StringType,
				Required:            true,
			},
			"id":                schema.StringAttribute{MarkdownDescription: "Variant GID.", Computed: true},
			"inventory_item_id": schema.StringAttribute{MarkdownDescription: "Inventory item GID, for inventory levels.", Computed: true},
			"price": schema.StringAttribute{
				MarkdownDescription: "Price as a decimal string. Compared numerically, so `\"10\"` matches Shopify's `\"10.00\"`." + unmanaged,
				Optional:            true, Computed: true, Validators: decimal,
			},
			"compare_at_price": schema.StringAttribute{
				MarkdownDescription: "Compare-at price as a decimal string, null when there is none." + unmanaged,
				Optional:            true, Computed: true, Validators: decimal,
			},
			"sku": schema.StringAttribute{MarkdownDescription: "SKU, null when there is none. Not an identity: SKUs can be empty or repeated." + unmanaged, Optional: true, Computed: true},
			"barcodes": schema.ListNestedAttribute{
				MarkdownDescription: "Barcodes; `[]` clears them." + unmanaged,
				Optional:            true,
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"value": schema.StringAttribute{MarkdownDescription: "Barcode value.", Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
					"type": schema.StringAttribute{
						MarkdownDescription: "`UPC`, `EAN`, `ISBN`, `GTIN`, `ASIN` or `NS_PID`; null when not set.",
						Optional:            true,
						Validators:          []validator.String{stringvalidator.OneOf(enumNames(client.AllBarcodeType)...)},
					},
				}},
			},
			"inventory_policy": schema.StringAttribute{
				MarkdownDescription: "`DENY` stops selling when out of stock, `CONTINUE` keeps selling." + unmanaged,
				Optional:            true, Computed: true,
				Validators: []validator.String{stringvalidator.OneOf(enumNames(client.AllProductVariantInventoryPolicy)...)},
			},
			"taxable":           schema.BoolAttribute{MarkdownDescription: "Whether taxes are charged." + unmanaged, Optional: true, Computed: true},
			"tracked":           schema.BoolAttribute{MarkdownDescription: "Whether Shopify tracks inventory for the variant." + unmanaged, Optional: true, Computed: true},
			"requires_shipping": schema.BoolAttribute{MarkdownDescription: "Whether the variant is shipped." + unmanaged, Optional: true, Computed: true},
			"weight": schema.SingleNestedAttribute{
				MarkdownDescription: "Shipping weight, used by weight-based shipping rates." + unmanaged,
				Optional:            true,
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"value": schema.Float64Attribute{MarkdownDescription: "Weight value.", Required: true},
					"unit": schema.StringAttribute{
						MarkdownDescription: "`KILOGRAMS`, `GRAMS`, `POUNDS` or `OUNCES`.",
						Required:            true,
						Validators:          []validator.String{stringvalidator.OneOf(enumNames(client.AllWeightUnit)...)},
					},
				},
			},
		}},
	}
}

func enumNames[T ~string](all []T) []string {
	out := make([]string, len(all))
	for i, v := range all {
		out[i] = string(v)
	}
	return out
}

func statusNames() []string {
	out := make([]string, len(client.AllProductStatus))
	for i, s := range client.AllProductStatus {
		out[i] = string(s)
	}
	return out
}

func (r *Resource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	r.client = c
}

// ValidateConfig checks options and variants together: every variant needs a known value for every option.
func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var opts, vars types.List
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("options"), &opts)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("variants"), &vars)...)
	if resp.Diagnostics.HasError() {
		return
	}
	switch {
	case vars.IsNull() && !opts.IsNull():
		resp.Diagnostics.AddAttributeError(path.Root("variants"), "Missing variants", "Set variants together with options: every product has at least one variant.")
		return
	case !vars.IsNull() && opts.IsNull():
		resp.Diagnostics.AddAttributeError(path.Root("options"), "Missing options", "Set options together with variants; use options = [] for a product without options.")
		return
	}
	o, ok1 := optionsOf(ctx, opts)
	_, cs, ok2 := variantsOf(ctx, vars)
	if !ok1 || !ok2 {
		return
	}
	if err := validateOptionsAndVariants(o, cs); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("variants"), "Invalid options or variants", err.Error())
	}
}

// ModifyPlan carries variant IDs and unset fields forward by option values, and warns about deleting changes.
func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	r.planVariants(ctx, req, resp)
	r.warnReplace(ctx, req, resp)
}

// planVariants matches configured variants to the ones in state, so the plan shows which survive and which are deleted.
func (r *Resource) planVariants(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var cfgOpts, cfgVars, stateOpts, stateVars types.List
	var allow types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("options"), &cfgOpts)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("variants"), &cfgVars)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("options"), &stateOpts)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("variants"), &stateVars)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("allow_data_deletion"), &allow)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plannedOpts, ok1 := optionsOf(ctx, cfgOpts)
	planned, cs, ok2 := variantsOf(ctx, cfgVars)
	priorOpts, ok3 := optionsOf(ctx, stateOpts)
	prior, priorCs, ok4 := variantsOf(ctx, stateVars)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return
	}
	keep, removed := matchVariants(priorOpts, priorCs, plannedOpts, cs)
	for i := range planned {
		from := unknownVariant
		if keep[i] >= 0 {
			from = prior[keep[i]]
		}
		planned[i] = carryForward(planned[i], from)
	}
	list, d := types.ListValueFrom(ctx, variantType, planned)
	resp.Diagnostics.Append(d...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("variants"), list)...)
	if len(removed) == 0 {
		return
	}
	gone := make([]string, len(removed))
	for i, k := range removed {
		gone[i] = describe(priorCs[k], priorOpts)
	}
	detail := fmt.Sprintf("This change deletes the variants %s with their inventory items and stock.", strings.Join(gone, "; "))
	if !allow.ValueBool() {
		detail += " Apply will fail unless allow_data_deletion = true."
	}
	resp.Diagnostics.AddWarning("This change deletes product variants", detail)
}

// warnReplace warns about a replace, which deletes the product; attribute-level RequiresReplace isn't visible here.
func (r *Resource) warnReplace(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var before, after types.Bool
	var allow types.Bool
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("gift_card"), &before)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("gift_card"), &after)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("allow_data_deletion"), &allow)...)
	if resp.Diagnostics.HasError() || after.IsUnknown() || before.Equal(after) {
		return
	}
	detail := "Changing gift_card replaces the product and deletes its variants, inventory and media."
	if !allow.ValueBool() {
		detail += " Apply will fail because allow_data_deletion is not true in state."
	}
	resp.Diagnostics.AddWarning("This change replaces the "+what, detail)
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, cfg model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// productSet without an identifier also fails on a taken handle, but this says how to take the product over.
	existing, err := r.getByHandle(ctx, plan.Handle.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
		return
	}
	var id *string
	if existing != nil {
		if !plan.AdoptExisting.ValueBool() {
			resp.Diagnostics.AddAttributeError(path.Root("handle"), "Product already exists",
				"A product with this handle exists. Set adopt_existing = true to take it over, or import it.")
			return
		}
		id = &existing.Id
	}
	r.write(ctx, id, existing, &plan, &cfg, &resp.Diagnostics, resp.Private)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GID first; fall back to the handle so a product recreated in the admin is found again.
	var p *client.ProductFields
	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		var err error
		if p, err = r.getByID(ctx, state.ID.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if p == nil && !state.Handle.IsNull() {
		var err error
		if p, err = r.getByHandle(ctx, state.Handle.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if p == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	// Set on import, where only the ID is known.
	if state.AdoptExisting.IsNull() {
		state.AdoptExisting = types.BoolValue(false)
	}
	if state.AllowDataDeletion.IsNull() {
		state.AllowDataDeletion = types.BoolValue(false)
	}
	written, d := readWritten(ctx, req.Private)
	resp.Diagnostics.Append(d...)
	resp.Diagnostics.Append(setFromAPI(ctx, &state, p, written)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, cfg model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := plan.ID.ValueString()
	// Option, value and variant IDs come from the live product, so renames keep them.
	var live *client.ProductFields
	if !cfg.Variants.IsNull() {
		var err error
		if live, err = r.getByID(ctx, id); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what+" before the update", err.Error())
			return
		}
		if live == nil {
			resp.Diagnostics.AddError("Failed to update "+what, fmt.Sprintf("Product %s no longer exists; refresh to recreate it.", id))
			return
		}
	}
	r.write(ctx, &id, live, &plan, &cfg, &resp.Diagnostics, resp.Private)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.AdoptExisting.ValueBool() {
		patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, what, "adopt_existing is true")
		return
	}
	resp.Diagnostics.Append(patterns.GuardDataDeletion(state.AllowDataDeletion, what)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A product already gone is a plain UserError from productDelete, so look first instead of matching its message.
	p, err := r.getByID(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	if p == nil {
		return
	}
	res, err := client.DeleteProduct(ctx, r.client, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to delete "+what, res.ProductDelete.UserErrors)...)
}

// ImportState takes the handle from `terraform import`, or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		p, err := r.getByHandle(ctx, req.ID)
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+what, err.Error())
			return
		}
		if p == nil {
			resp.Diagnostics.AddError("Product not found", fmt.Sprintf("No product has the handle %q.", req.ID))
			return
		}
		// Read fills in the rest by GID.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), p.Id)...)
		return
	}
	var id patterns.IDIdentity
	resp.Diagnostics.Append(req.Identity.Get(ctx, &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id.ID)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, id)...)
}

// write sends the configured attributes to the product with the given ID (nil creates one) and writes the result into plan.
// live is that product as it is now, nil on create.
func (r *Resource) write(ctx context.Context, id *string, live *client.ProductFields, plan, cfg *model, diags *diag.Diagnostics, private privateState) {
	in, removed, d := buildInput(ctx, cfg, live)
	diags.Append(d...)
	if diags.HasError() {
		return
	}
	// The planned flag counts, unlike for destroy: the variants go in this same apply.
	if len(removed) > 0 && !plan.AllowDataDeletion.ValueBool() {
		diags.AddAttributeError(path.Root("variants"), "Removing variants would delete their stock",
			fmt.Sprintf("This change deletes the variants %s with their inventory items and stock. Set allow_data_deletion = true to allow it.", strings.Join(removed, "; ")))
		return
	}
	var ident *client.ProductSetIdentifiers
	if id != nil {
		ident = &client.ProductSetIdentifiers{Id: id}
	}
	res, err := client.SetProduct(ctx, r.client, ident, in)
	if err != nil {
		diags.AddError("Failed to write "+what, err.Error())
		return
	}
	diags.Append(client.UserErrorDiagsWith("Failed to write "+what, res.ProductSet.UserErrors, errorPath)...)
	if diags.HasError() {
		return
	}
	if res.ProductSet.Product == nil {
		diags.AddError("Failed to write "+what, "Shopify returned no product.")
		return
	}
	p := &res.ProductSet.Product.ProductFields
	if err := fetchAllVariants(ctx, r.client, p); err != nil {
		diags.AddError("Failed to read the written "+what, err.Error())
		return
	}
	diags.Append(setFromAPI(ctx, plan, p, &p.DescriptionHtml)...)
	raw, err := json.Marshal(p.DescriptionHtml)
	if err != nil {
		diags.AddError("Failed to remember the description", err.Error())
		return
	}
	diags.Append(private.SetKey(ctx, writtenKey, raw)...)
}

// privateState is the part of the private state the resource writes.
type privateState interface {
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

// readWritten returns the stored description, or nil when none was stored (import).
func readWritten(ctx context.Context, private interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
}) (*string, diag.Diagnostics) {
	raw, diags := private.GetKey(ctx, writtenKey)
	if diags.HasError() || len(raw) == 0 {
		return nil, diags
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		diags.AddError("Failed to read the remembered description", err.Error())
		return nil, diags
	}
	return &s, diags
}

// getByID returns the product with all its variants, or nil when it doesn't exist.
func (r *Resource) getByID(ctx context.Context, id string) (*client.ProductFields, error) {
	res, err := client.GetProduct(ctx, r.client, id)
	if err != nil || res.Product == nil {
		return nil, err
	}
	return &res.Product.ProductFields, fetchAllVariants(ctx, r.client, &res.Product.ProductFields)
}

// getByHandle returns the product with all its variants, or nil when no product has the handle.
func (r *Resource) getByHandle(ctx context.Context, handle string) (*client.ProductFields, error) {
	res, err := client.GetProductByHandle(ctx, r.client, handle)
	if err != nil || res.ProductByIdentifier == nil {
		return nil, err
	}
	return &res.ProductByIdentifier.ProductFields, fetchAllVariants(ctx, r.client, &res.ProductByIdentifier.ProductFields)
}
