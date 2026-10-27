package discount

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const whatBasic = "basic discount"

var (
	_ resource.ResourceWithConfigure   = &BasicResource{}
	_ resource.ResourceWithImportState = &BasicResource{}
	_ resource.ResourceWithIdentity    = &BasicResource{}
	_ resource.ResourceWithModifyPlan  = &BasicResource{}
)

func NewBasicResource() resource.Resource {
	return &BasicResource{}
}

// BasicResource manages one amount-off discount on products or the order, as a code or an automatic discount.
type BasicResource struct {
	client *client.Client
}

type basicModel struct {
	commonModel
	CustomerGets       types.Object `tfsdk:"customer_gets"`
	MinimumRequirement types.Object `tfsdk:"minimum_requirement"`
}

func (r *BasicResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_discount_basic"
	// Read re-adopts a discount recreated outside Terraform by its code or title, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *BasicResource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *BasicResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := commonAttributes()
	attrs["customer_gets"] = customerGetsAttribute()
	attrs["minimum_requirement"] = minimumAttribute()
	resp.Schema = schema.Schema{
		MarkdownDescription: "An amount-off discount: a percentage or a fixed amount off selected products, collections or the whole order, " +
			"as a code discount (`code` set) or an automatic one (`code` unset). " +
			"**Volatile, usually a poor fit for Terraform:** promotions are created, changed and ended in the admin by marketing, and every admin edit to a managed attribute is reverted on the next apply. " +
			"Long-running rules, such as a newsletter code, fit Terraform; attributes you leave unset are not managed. " +
			"Destroying a discount deletes it; orders keep their discount applications, but a recreated discount counts its uses from zero. " +
			"Bulk codes, tags, subscription settings and app discounts are not managed. " +
			"Generated config holds product, collection, segment and market GIDs, which tie it to the store: replace them with references. " +
			"An existing discount with the same code, or an automatic one with the same title, needs `adopt_existing = true`; destroy then leaves it in place. " +
			"Import with `code/<code>` or `automatic/<title>`.",
		Attributes: attrs,
	}
}

func (r *BasicResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *BasicResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	modifyPlan(ctx, req, resp)
}

func (r *BasicResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, cfg basicModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var live *node
	if plan.AdoptExisting.ValueBool() {
		isCode, key := naturalKey(&cfg.commonModel)
		n, err := findByKey(ctx, r.client, isCode, key)
		if err != nil {
			resp.Diagnostics.AddError("Failed to look up "+whatBasic, err.Error())
			return
		}
		if n != nil {
			if err := checkBasic(n.Typename); err != nil {
				resp.Diagnostics.AddAttributeError(path.Root("adopt_existing"), "Can't adopt the discount", err.Error())
				return
			}
			live = n
		}
	}
	id := r.write(ctx, &cfg, &plan, live, &resp.Diagnostics)
	if id == "" {
		return
	}
	if !r.finish(ctx, id, &cfg, &plan, &resp.Diagnostics, resp.Private) {
		// The discount exists, so its ID goes into state: Terraform taints it instead of orphaning it, and Read fills the rest.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("adopt_existing"), plan.AdoptExisting)...)
		resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(id)})...)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *BasicResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state basicModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// GID first; fall back to the natural key so a discount recreated in the admin is found again.
	var n *node
	var err error
	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		if n, err = getNode(ctx, r.client, state.ID.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+whatBasic, err.Error())
			return
		}
	}
	importing := state.Title.IsNull()
	if n == nil && !importing {
		isCode, key := naturalKey(&state.commonModel)
		if n, err = findByKey(ctx, r.client, isCode, key); err != nil {
			resp.Diagnostics.AddError("Failed to read "+whatBasic, err.Error())
			return
		}
	}
	if n == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	if err := checkBasic(n.Typename); err != nil {
		resp.Diagnostics.AddError("Wrong discount type", fmt.Sprintf("Discount %s: %s.", n.ID, err))
		return
	}
	if err := resolveCode(ctx, r.client, n, patterns.ConfiguredString(state.Code)); err != nil {
		resp.Diagnostics.AddError("Failed to read "+whatBasic, err.Error())
		return
	}
	// Set on import, where only the ID is known.
	if state.AdoptExisting.IsNull() {
		state.AdoptExisting = types.BoolValue(false)
	}
	wasManaged := !state.Context.IsNull()
	unsupported, d := state.setFromAPI(ctx, n)
	resp.Diagnostics.Append(d...)
	// Warn once: on import, or when the context stops being manageable; ModifyPlan errors if config still sets it.
	if unsupported != "" && (importing || wasManaged) {
		resp.Diagnostics.AddWarning("Context is not managed", unsupportedContextDetail(unsupported))
	}
	resp.Diagnostics.Append(rememberContext(ctx, resp.Private, unsupported)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

func (r *BasicResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, cfg, state basicModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Items, context and codes are diffed against the live discount, never against state.
	live, err := getNode(ctx, r.client, state.ID.ValueString())
	if err == nil && live != nil {
		err = resolveCode(ctx, r.client, live, patterns.ConfiguredString(state.Code))
	}
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Failed to read "+whatBasic+" before the update", err.Error())
		return
	case live == nil:
		resp.Diagnostics.AddError("Failed to update "+whatBasic, fmt.Sprintf("Discount %s no longer exists; refresh to recreate it.", state.ID.ValueString()))
		return
	}
	if err := checkBasic(live.Typename); err != nil {
		resp.Diagnostics.AddError("Wrong discount type", fmt.Sprintf("Discount %s: %s.", live.ID, err))
		return
	}
	if r.write(ctx, &cfg, &plan, live, &resp.Diagnostics) == "" || !r.finish(ctx, live.ID, &cfg, &plan, &resp.Diagnostics, resp.Private) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *BasicResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state basicModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.AdoptExisting.ValueBool() {
		patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, "discount", "adopt_existing is true")
		return
	}
	resp.Diagnostics.Append(deleteDiscount(ctx, r.client, state.ID.ValueString())...)
}

// ImportState takes code/<code> or automatic/<title> from `terraform import`, or the GID from an import block with identity.
func (r *BasicResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		isCode, key, err := parseImportKey(req.ID)
		if err != nil {
			resp.Diagnostics.AddError("Invalid import key", err.Error())
			return
		}
		n, err := findByKey(ctx, r.client, isCode, key)
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+whatBasic, err.Error())
			return
		}
		if n == nil {
			resp.Diagnostics.AddError("Discount not found", fmt.Sprintf("No discount matches %q.", req.ID))
			return
		}
		// Read fills in the rest by GID; the code lets it keep the one imported by when the discount has several.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), n.ID)...)
		if isCode {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("code"), key)...)
		}
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

// naturalKey is the code of a code discount, or the title of an automatic one.
func naturalKey(m *commonModel) (bool, string) {
	if !m.Code.IsNull() {
		return true, m.Code.ValueString()
	}
	return false, m.Title.ValueString()
}

// write creates the discount, or updates live; cfg is the config, so unset attributes are never sent. It returns the GID, or "" on error.
func (r *BasicResource) write(ctx context.Context, cfg, plan *basicModel, live *node, diags *diag.Diagnostics) string {
	cfg.takeDefaults(&plan.commonModel)
	cfg.CustomerGets = plan.CustomerGets
	isCode := !cfg.Code.IsNull()
	summary := "Failed to create " + whatBasic
	if live != nil {
		isCode, summary = live.IsCode, "Failed to update "+whatBasic
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
			var res *client.CreateDiscountCodeBasicResponse
			if res, err = client.CreateDiscountCodeBasic(ctx, r.client, in); err == nil && res.DiscountCodeBasicCreate != nil {
				errs = res.DiscountCodeBasicCreate.UserErrors
				if n := res.DiscountCodeBasicCreate.CodeDiscountNode; n != nil {
					id = &n.Id
				}
			}
		} else {
			var res *client.UpdateDiscountCodeBasicResponse
			if res, err = client.UpdateDiscountCodeBasic(ctx, r.client, live.ID, in); err == nil && res.DiscountCodeBasicUpdate != nil {
				errs = res.DiscountCodeBasicUpdate.UserErrors
				if n := res.DiscountCodeBasicUpdate.CodeDiscountNode; n != nil {
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
			var res *client.CreateDiscountAutomaticBasicResponse
			if res, err = client.CreateDiscountAutomaticBasic(ctx, r.client, in); err == nil && res.DiscountAutomaticBasicCreate != nil {
				errs = res.DiscountAutomaticBasicCreate.UserErrors
				if n := res.DiscountAutomaticBasicCreate.AutomaticDiscountNode; n != nil {
					id = &n.Id
				}
			}
		} else {
			var res *client.UpdateDiscountAutomaticBasicResponse
			if res, err = client.UpdateDiscountAutomaticBasic(ctx, r.client, live.ID, in); err == nil && res.DiscountAutomaticBasicUpdate != nil {
				errs = res.DiscountAutomaticBasicUpdate.UserErrors
				if n := res.DiscountAutomaticBasicUpdate.AutomaticDiscountNode; n != nil {
					id = &n.Id
				}
			}
		}
	}
	if err != nil {
		diags.AddError(summary, err.Error())
		return ""
	}
	if diags.Append(userErrorDiags(summary, errs)...); diags.HasError() {
		return ""
	}
	if id == nil {
		diags.AddError(summary, "Shopify returned no discount.")
		return ""
	}
	return *id
}

// finish reads the written discount back into plan; it reports whether that worked.
func (r *BasicResource) finish(ctx context.Context, id string, cfg, plan *basicModel, diags *diag.Diagnostics, private patterns.PrivateSetter) bool {
	n, err := getNode(ctx, r.client, id)
	if err == nil && n != nil {
		err = resolveCode(ctx, r.client, n, patterns.ConfiguredString(cfg.Code))
	}
	switch {
	case err != nil:
		diags.AddError("Failed to read the written "+whatBasic, err.Error())
		return false
	case n == nil:
		diags.AddError("Failed to read the written "+whatBasic, fmt.Sprintf("Discount %s disappeared after the write.", id))
		return false
	}
	unsupported, d := plan.setFromAPI(ctx, n)
	diags.Append(d...)
	diags.Append(rememberContext(ctx, private, unsupported)...)
	return !diags.HasError()
}

// inputs maps the configured attributes against live (nil on create).
func (m *basicModel) inputs(ctx context.Context, live *node) (commonInput, *client.DiscountCustomerGetsInput, *client.DiscountMinimumRequirementInput, diag.Diagnostics) {
	common, diags := m.input(ctx, live)
	var cg customerGetsSel
	diags.Append(m.CustomerGets.As(ctx, &cg, basetypes.ObjectAsOptions{})...)
	var liveGets *client.DiscountCustomerGetsFields
	if live != nil {
		liveGets = live.CustomerGets
	}
	gets, err := customerGetsInput(cg, liveGets)
	if err != nil {
		diags.AddAttributeError(path.Root("customer_gets").AtName("items"), "Items can't be managed", fmt.Sprintf("The discount %s.", err))
	}
	var minimum *client.DiscountMinimumRequirementInput
	if !m.MinimumRequirement.IsNull() && !m.MinimumRequirement.IsUnknown() {
		var ms minimumSel
		diags.Append(m.MinimumRequirement.As(ctx, &ms, basetypes.ObjectAsOptions{})...)
		minimum = minimumInput(ms)
	}
	return common, gets, minimum, diags
}

func (m *basicModel) codeInput(ctx context.Context, live *node) (client.DiscountCodeBasicInput, diag.Diagnostics) {
	c, gets, minimum, diags := m.inputs(ctx, live)
	return client.DiscountCodeBasicInput{
		Title: c.Title, StartsAt: c.StartsAt, EndsAt: c.EndsAt, Code: c.Code, UsageLimit: c.UsageLimit, AppliesOncePerCustomer: c.AppliesOncePerCustomer,
		CombinesWith: c.CombinesWith, Context: c.Context, CustomerGets: gets, MinimumRequirement: minimum,
	}, diags
}

func (m *basicModel) automaticInput(ctx context.Context, live *node) (client.DiscountAutomaticBasicInput, diag.Diagnostics) {
	c, gets, minimum, diags := m.inputs(ctx, live)
	return client.DiscountAutomaticBasicInput{
		Title: c.Title, StartsAt: c.StartsAt, EndsAt: c.EndsAt,
		CombinesWith: c.CombinesWith, Context: c.Context, CustomerGets: gets, MinimumRequirement: minimum,
	}, diags
}

// setFromAPI copies n into m; shapes the schema can't hold are errors, so import fails and list skips them.
func (m *basicModel) setFromAPI(ctx context.Context, n *node) (string, diag.Diagnostics) {
	unsupported, diags := m.commonModel.setFromAPI(ctx, n)
	m.CustomerGets, m.MinimumRequirement = types.ObjectNull(customerGetsAttrTypes()), types.ObjectNull(minimumAttrTypes())
	if n.CustomerGets == nil {
		diags.AddError("Unsupported discount", fmt.Sprintf("Discount %q has no customer gets.", n.Title))
		return unsupported, diags
	}
	value, err1 := valueFromAPI(n.CustomerGets.Value)
	items, err2 := itemsFromAPI(n.CustomerGets.Items)
	minimum, err3 := minimumFromAPI(n.Minimum)
	for _, err := range []error{err1, err2, err3} {
		if err != nil {
			diags.AddError("Unsupported discount", fmt.Sprintf("Discount %q %s.", n.Title, err))
		}
	}
	if diags.HasError() {
		return unsupported, diags
	}
	var d diag.Diagnostics
	m.CustomerGets, d = types.ObjectValueFrom(ctx, customerGetsAttrTypes(), customerGetsSel{Value: value, Items: items})
	diags.Append(d...)
	m.MinimumRequirement, d = types.ObjectValueFrom(ctx, minimumAttrTypes(), minimum)
	diags.Append(d...)
	return unsupported, diags
}
