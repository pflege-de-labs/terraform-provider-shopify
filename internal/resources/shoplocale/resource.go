// Package shoplocale implements the shopify_shop_locale resource.
package shoplocale

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const what = "shop locale"

var (
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
	_ resource.ResourceWithIdentity    = &Resource{}
	_ resource.ResourceWithModifyPlan  = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one enabled locale of the shop.
type Resource struct {
	client *client.Client
}

type model struct {
	ID                types.String `tfsdk:"id"`
	Locale            types.String `tfsdk:"locale"`
	Published         types.Bool   `tfsdk:"published"`
	Name              types.String `tfsdk:"name"`
	Primary           types.Bool   `tfsdk:"primary"`
	AdoptExisting     types.Bool   `tfsdk:"adopt_existing"`
	AllowDataDeletion types.Bool   `tfsdk:"allow_data_deletion"`
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_shop_locale"
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A locale enabled on the shop. Publish it to make it visible to buyers; " +
			"link it to markets with `default_locale` and `alternate_locales` on `shopify_web_presence`, which owns that relation. " +
			"Disabling a locale deletes all translations of it, so destroying one needs `allow_data_deletion = true`. " +
			"The primary locale can't be changed or disabled through the API: set `adopt_existing = true` to manage its entry, destroy only removes it from state. " +
			"Import with the locale code, such as `fr` or `pt-BR`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The locale code. Shop locales have no GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"locale": schema.StringAttribute{
				MarkdownDescription: "Locale code, case-sensitive: `fr`, `pt-BR`. Changing it replaces the locale and deletes the translations of the old one.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"published": schema.BoolAttribute{
				MarkdownDescription: "Whether buyers can see the locale. A new locale starts unpublished. Unset means unmanaged.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "English display name, such as `French`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"primary": schema.BoolAttribute{
				MarkdownDescription: "Whether this is the shop's primary locale.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"adopt_existing":      patterns.AdoptExistingAttribute(what),
			"allow_data_deletion": patterns.AllowDataDeletionAttribute(what + " (its translations)"),
		},
	}
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

func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	// Adopted and primary locales are only dropped from state on replace, so nothing is deleted.
	if resp.Diagnostics.HasError() || keepReason(state.Primary.ValueBool(), state.AdoptExisting.ValueBool()) != "" {
		return
	}
	patterns.WarnOnDataDeletingReplace(ctx, req, resp, what, "locale")
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, cfg model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The API fails on a taken locale, but adopting needs the lookup anyway.
	l, err := r.find(ctx, plan.Locale.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
		return
	}
	switch {
	case l != nil && !plan.AdoptExisting.ValueBool():
		resp.Diagnostics.AddAttributeError(path.Root("locale"), "Shop locale already exists",
			"This locale is already enabled. Set adopt_existing = true to take it over, or import it.")
		return
	case l == nil:
		res, err := client.EnableShopLocale(ctx, r.client, plan.Locale.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to enable "+what, err.Error())
			return
		}
		resp.Diagnostics.Append(client.UserErrorDiags("Failed to enable "+what, res.ShopLocaleEnable.UserErrors)...)
		if resp.Diagnostics.HasError() {
			return
		}
		l = &res.ShopLocaleEnable.ShopLocale.ShopLocaleFields
	}
	if want := patterns.ConfiguredBool(cfg.Published); needsPublish(want, l.Published) {
		if l = r.publish(ctx, l.Locale, *want, &resp.Diagnostics); l == nil {
			return
		}
	}
	setFromAPI(&plan, l)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The locale code is both the ID and the natural key, so there is no separate fallback lookup.
	l, err := r.find(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read "+what, err.Error())
		return
	}
	if l == nil {
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
	setFromAPI(&state, l)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state, cfg model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Only published can change in place; the other differences are provider-side flags.
	if want := patterns.ConfiguredBool(cfg.Published); needsPublish(want, state.Published.ValueBool()) {
		l := r.publish(ctx, plan.ID.ValueString(), *want, &resp.Diagnostics)
		if l == nil {
			return
		}
		setFromAPI(&plan, l)
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
	if reason := keepReason(state.Primary.ValueBool(), state.AdoptExisting.ValueBool()); reason != "" {
		patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, what, reason)
		return
	}
	resp.Diagnostics.Append(patterns.GuardDataDeletion(state.AllowDataDeletion, what)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// TODO(verify): disabling a locale that a web presence still uses.
	res, err := client.DisableShopLocale(ctx, r.client, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to disable "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to disable "+what, res.ShopLocaleDisable.UserErrors)...)
}

// ImportState takes the locale code from `terraform import` or from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		// Read fills in the rest and removes the resource if the locale isn't enabled.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
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

// publish returns nil after adding diagnostics.
func (r *Resource) publish(ctx context.Context, code string, published bool, diags *diag.Diagnostics) *client.ShopLocaleFields {
	res, err := client.UpdateShopLocale(ctx, r.client, code, client.ShopLocaleInput{Published: &published})
	if err != nil {
		diags.AddError("Failed to update "+what, err.Error())
		return nil
	}
	diags.Append(client.UserErrorDiags("Failed to update "+what, res.ShopLocaleUpdate.UserErrors)...)
	if diags.HasError() {
		return nil
	}
	return &res.ShopLocaleUpdate.ShopLocale.ShopLocaleFields
}

func (r *Resource) find(ctx context.Context, code string) (*client.ShopLocaleFields, error) {
	ls, err := listAll(ctx, r.client)
	if err != nil {
		return nil, err
	}
	return findLocale(ls, code), nil
}

func listAll(ctx context.Context, c *client.Client) ([]client.ShopLocaleFields, error) {
	res, err := client.ListShopLocales(ctx, c)
	if err != nil {
		return nil, err
	}
	out := make([]client.ShopLocaleFields, len(res.ShopLocales))
	for i, l := range res.ShopLocales {
		out[i] = l.ShopLocaleFields
	}
	return out, nil
}

func setFromAPI(m *model, l *client.ShopLocaleFields) {
	m.ID = types.StringValue(l.Locale)
	m.Locale = types.StringValue(l.Locale)
	m.Published = types.BoolValue(l.Published)
	m.Name = types.StringValue(l.Name)
	m.Primary = types.BoolValue(l.Primary)
}
