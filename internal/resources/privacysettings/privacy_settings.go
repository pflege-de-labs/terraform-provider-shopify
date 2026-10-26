package privacysettings

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Khan/genqlient/graphql"
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

const privacyWhat = "privacy settings"

var (
	_ resource.ResourceWithConfigure   = &PrivacySettings{}
	_ resource.ResourceWithImportState = &PrivacySettings{}
	_ resource.ResourceWithIdentity    = &PrivacySettings{}
	_ resource.ResourceWithModifyPlan  = &PrivacySettings{}
)

func NewPrivacySettings() resource.Resource {
	return &PrivacySettings{}
}

// PrivacySettings manages which privacy features Shopify still runs for the shop.
type PrivacySettings struct {
	withClient
}

type privacyModel struct {
	ID                 types.String `tfsdk:"id"`
	CookieBanner       types.Bool   `tfsdk:"cookie_banner_auto_managed"`
	DataSaleOptOutPage types.Bool   `tfsdk:"data_sale_opt_out_page_auto_managed"`
	PrivacyPolicy      types.Bool   `tfsdk:"privacy_policy_auto_managed"`
	CookieBannerOn     types.Bool   `tfsdk:"cookie_banner_enabled"`
}

func (m *privacyModel) want() privacyWant {
	return privacyWant{
		CookieBanner:       patterns.ConfiguredBool(m.CookieBanner),
		DataSaleOptOutPage: patterns.ConfiguredBool(m.DataSaleOptOutPage),
		PrivacyPolicy:      patterns.ConfiguredBool(m.PrivacyPolicy),
	}
}

func (m *privacyModel) set(r privacyRead) {
	m.ID = types.StringValue(r.ShopID)
	m.CookieBanner = types.BoolValue(r.Flags.CookieBanner)
	m.DataSaleOptOutPage = types.BoolValue(r.Flags.DataSaleOptOutPage)
	m.PrivacyPolicy = types.BoolValue(r.Flags.PrivacyPolicy)
	m.CookieBannerOn = types.BoolValue(r.BannerEnabled)
}

func (r *PrivacySettings) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_privacy_settings"
}

func (r *PrivacySettings) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *PrivacySettings) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	autoManaged := func(feature string) schema.BoolAttribute {
		return schema.BoolAttribute{
			MarkdownDescription: fmt.Sprintf("Whether Shopify still runs the %s. Set `false` to turn it off. "+
				"**It can only be turned off:** the Admin API has no way to turn it back on, so `true` fails when the feature is off, and turning it back on happens in the Shopify admin. "+
				"Unset means not managed.", feature),
			Optional:      true,
			Computed:      true,
			PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "The shop's privacy features, which Shopify runs for you until you turn them off. " +
			"Use it when the shop has its own consent banner: Shopify's banner has to be off before `shopify_consent_policy` can be written, so add `depends_on` there. " +
			"**Turning a feature off is one-way through the API.** Destroy only removes the resource from state and never touches a feature. " +
			"If someone turns a feature back on in the admin, the next apply turns it off again. " +
			"Import with `default`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Shop GID: the privacy settings have no ID of their own.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"cookie_banner_auto_managed":          autoManaged("cookie banner"),
			"data_sale_opt_out_page_auto_managed": autoManaged("data sale opt-out page"),
			"privacy_policy_auto_managed":         autoManaged("privacy policy"),
			"cookie_banner_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the cookie banner is shown. Informational.",
				Computed:            true,
			},
		},
	}
}

// ModifyPlan reports `true` against a feature that is off while the plan can still say so, before anything is applied.
func (r *PrivacySettings) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var cfg, state privacyModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	live := privacyFlags{
		CookieBanner:       state.CookieBanner.ValueBool(),
		DataSaleOptOutPage: state.DataSaleOptOutPage.ValueBool(),
		PrivacyPolicy:      state.PrivacyPolicy.ValueBool(),
	}
	if c := enableConflicts(cfg.want(), live); len(c) > 0 {
		resp.Diagnostics.Append(enableConflictDiag(c))
	}
}

func enableConflictDiag(attrs []string) diag.Diagnostic {
	return diag.NewErrorDiagnostic("Privacy features can't be turned back on",
		"The Admin API can only turn privacy features off, so they can only be turned back on in the Shopify admin. "+
			"These are true in config but the feature is off: "+strings.Join(attrs, ", ")+".")
}

func (r *PrivacySettings) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.apply(ctx, req.Config.Get, &resp.State, resp.Identity, &resp.Diagnostics)
}

func (r *PrivacySettings) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.apply(ctx, req.Config.Get, &resp.State, resp.Identity, &resp.Diagnostics)
}

type stateSetter interface {
	Set(ctx context.Context, val any) diag.Diagnostics
}

// apply works from the config, not the plan: the plan carries state values for unset attributes.
func (r *PrivacySettings) apply(ctx context.Context, getConfig func(context.Context, any) diag.Diagnostics, state, identity stateSetter, diags *diag.Diagnostics) {
	var cfg privacyModel
	diags.Append(getConfig(ctx, &cfg)...)
	if diags.HasError() {
		return
	}
	read, d := applyPrivacy(ctx, r.client, cfg.want())
	diags.Append(d...)
	if diags.HasError() {
		return
	}
	var m privacyModel
	m.set(read)
	diags.Append(state.Set(ctx, &m)...)
	diags.Append(identity.Set(ctx, patterns.IDIdentity{ID: m.ID})...)
}

func (r *PrivacySettings) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	read, err := readPrivacy(ctx, r.client)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read "+privacyWhat, err.Error())
		return
	}
	var m privacyModel
	m.set(read)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: m.ID})...)
}

// Delete never touches a feature: the API can't turn one back on, so there is nothing to undo.
func (r *PrivacySettings) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, privacyWhat, "the privacy features can't be turned back on through the Admin API")
}

// ImportState takes "default" from `terraform import`, or the shop GID from an import block with identity.
func (r *PrivacySettings) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		if req.ID != "default" {
			resp.Diagnostics.AddError("Invalid import key", `The shop has one set of privacy settings; import it with "default".`)
			return
		}
		read, err := readPrivacy(ctx, r.client)
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+privacyWhat, err.Error())
			return
		}
		// Read fills in the rest.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), read.ShopID)...)
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

type privacyRead struct {
	ShopID        string
	Flags         privacyFlags
	BannerEnabled bool
}

func readPrivacy(ctx context.Context, c graphql.Client) (privacyRead, error) {
	res, err := client.GetPrivacySettings(ctx, c)
	if err != nil {
		return privacyRead{}, err
	}
	ps := res.PrivacySettings
	if ps.Banner == nil || ps.DataSaleOptOutPage == nil || ps.PrivacyPolicy == nil {
		return privacyRead{}, errors.New("the API returned incomplete privacy settings")
	}
	return privacyRead{
		ShopID: res.Shop.Id,
		Flags: privacyFlags{
			CookieBanner:       ps.Banner.AutoManaged,
			DataSaleOptOutPage: ps.DataSaleOptOutPage.AutoManaged,
			PrivacyPolicy:      ps.PrivacyPolicy.AutoManaged,
		},
		BannerEnabled: ps.Banner.Enabled,
	}, nil
}

// applyPrivacy turns off what config turns off and returns the settings as the API now reports them.
func applyPrivacy(ctx context.Context, c graphql.Client, want privacyWant) (privacyRead, diag.Diagnostics) {
	var diags diag.Diagnostics
	live, err := readPrivacy(ctx, c)
	if err != nil {
		diags.AddError("Failed to read "+privacyWhat, err.Error())
		return live, diags
	}
	if conflicts := enableConflicts(want, live.Flags); len(conflicts) > 0 {
		diags.Append(enableConflictDiag(conflicts))
		return live, diags
	}
	features := featuresToDisable(want, live.Flags)
	if len(features) == 0 {
		return live, diags
	}
	res, err := client.DisablePrivacyFeatures(ctx, c, features)
	if err != nil {
		diags.AddError("Failed to disable privacy features", err.Error())
		return live, diags
	}
	if res.PrivacyFeaturesDisable == nil {
		diags.AddError("Failed to disable privacy features", "The API returned no result.")
		return live, diags
	}
	diags.Append(client.UserErrorDiags("Failed to disable privacy features", res.PrivacyFeaturesDisable.UserErrors)...)
	if diags.HasError() {
		return live, diags
	}
	live, err = readPrivacy(ctx, c)
	if err != nil {
		diags.AddError("Failed to read "+privacyWhat, err.Error())
	}
	return live, diags
}
