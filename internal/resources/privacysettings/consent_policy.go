package privacysettings

import (
	"context"
	"fmt"

	"github.com/Khan/genqlient/graphql"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const consentWhat = "consent policy"

var (
	_ resource.ResourceWithConfigure   = &ConsentPolicy{}
	_ resource.ResourceWithImportState = &ConsentPolicy{}
	_ resource.ResourceWithIdentity    = &ConsentPolicy{}
)

func NewConsentPolicy() resource.Resource {
	return &ConsentPolicy{}
}

// ConsentPolicy manages the consent requirements of one country or region.
type ConsentPolicy struct {
	withClient
}

type consentModel struct {
	ID                     types.String `tfsdk:"id"`
	CountryCode            types.String `tfsdk:"country_code"`
	RegionCode             types.String `tfsdk:"region_code"`
	ConsentRequired        types.Bool   `tfsdk:"consent_required"`
	DataSaleOptOutRequired types.Bool   `tfsdk:"data_sale_opt_out_required"`
}

func (m *consentModel) key() consentKey {
	return consentKey{Country: m.CountryCode.ValueString(), Region: m.RegionCode.ValueString()}
}

func (m *consentModel) set(r *client.ConsentPolicyFields, k consentKey) {
	m.ID = types.StringValue(r.Id)
	m.CountryCode = types.StringValue(k.Country)
	m.RegionCode = types.StringNull()
	if k.Region != "" {
		m.RegionCode = types.StringValue(k.Region)
	}
	m.ConsentRequired = types.BoolPointerValue(r.ConsentRequired)
	m.DataSaleOptOutRequired = types.BoolPointerValue(r.DataSaleOptOutRequired)
}

func (r *ConsentPolicy) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_consent_policy"
	// Writing a system-default row gives it a numeric ID, and Read re-adopts a row by its country and region.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *ConsentPolicy) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

// keepIDUnlessDefault is UseStateForUnknown, except that a system-default row gets a new numeric ID on its first write.
type keepIDUnlessDefault struct{}

func (keepIDUnlessDefault) Description(context.Context) string {
	return "Keeps the ID unless it is a system default, which gets a numeric ID when first written."
}

func (m keepIDUnlessDefault) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (keepIDUnlessDefault) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || !req.PlanValue.IsUnknown() || isSystemDefault(req.StateValue.ValueString()) {
		return
	}
	resp.PlanValue = req.StateValue
}

func (r *ConsentPolicy) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "What the shop requires from visitors in one country or region before collecting data. " +
			"Use it when the shop runs its own consent banner: **Shopify refuses every write while its own cookie banner is on**, so turn it off first with `shopify_privacy_settings` (`cookie_banner_auto_managed = false`) and add `depends_on`. " +
			"Every country and region already has a policy, Shopify's default until one is written, so create always takes the existing row over and writes only the configured values. " +
			"Destroy only removes the resource from state: the row keeps its values, and the API can't return it to Shopify's default. " +
			"**Writing a region (`region_code`) makes every regional row of that country a written row for good** (51 for the US), with their current values, and the API can't undo it. " +
			"Import with `<COUNTRY>` or `<COUNTRY>/<REGION>`, such as `DE` or `US/USCA`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Consent policy GID. Shopify's defaults have IDs like `SD-DE` until a policy is written, then the ID becomes numeric.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{keepIDUnlessDefault{}},
			},
			"country_code": schema.StringAttribute{
				MarkdownDescription: "Two-letter country code, such as `DE`.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(countryRe, "must be two uppercase letters, such as DE")},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"region_code": schema.StringAttribute{
				MarkdownDescription: "Country code plus subdivision, such as `USCA` or `CAQC`; only US and Canadian regions exist. Unset manages the country-level policy. The first write to any region of a country turns all of that country's regional policies into written ones permanently, and `terraform query` then lists all of them.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(regionRe, "must be the country code plus a subdivision, such as USCA")},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"consent_required": schema.BoolAttribute{
				MarkdownDescription: "Whether consent is required. Unset means not managed.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"data_sale_opt_out_required": schema.BoolAttribute{
				MarkdownDescription: "Whether data sale opt-out is required. Unset means not managed.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// Create adopts the row that always exists and writes only what differs.
func (r *ConsentPolicy) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.write(ctx, req.Config.Get, &resp.State, resp.Identity, &resp.Diagnostics)
}

func (r *ConsentPolicy) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.write(ctx, req.Config.Get, &resp.State, resp.Identity, &resp.Diagnostics)
}

// write works from the config, not the plan: the plan carries state values for unset attributes.
func (r *ConsentPolicy) write(ctx context.Context, getConfig func(context.Context, any) diag.Diagnostics, state, identity stateSetter, diags *diag.Diagnostics) {
	var cfg consentModel
	diags.Append(getConfig(ctx, &cfg)...)
	if diags.HasError() {
		return
	}
	row, d := upsertConsent(ctx, r.client, cfg.key(), patterns.ConfiguredBool(cfg.ConsentRequired), patterns.ConfiguredBool(cfg.DataSaleOptOutRequired))
	diags.Append(d...)
	if diags.HasError() {
		return
	}
	var m consentModel
	m.set(row, cfg.key())
	diags.Append(state.Set(ctx, &m)...)
	diags.Append(identity.Set(ctx, patterns.IDIdentity{ID: m.ID})...)
}

func (r *ConsentPolicy) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state consentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key := state.key()
	row, err := lookupConsent(ctx, r.client, state.ID.ValueString(), &key)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read "+consentWhat, err.Error())
		return
	}
	if row == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.set(row, key)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

// Delete leaves the row alone: there is no delete, and no way back to Shopify's default.
func (r *ConsentPolicy) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, consentWhat, "the Admin API can't delete consent policies or reset them to Shopify's default")
}

// ImportState takes `<COUNTRY>` or `<COUNTRY>/<REGION>` from `terraform import`, or the GID from an import block with identity.
func (r *ConsentPolicy) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		key, err := parseConsentKey(req.ID)
		if err != nil {
			resp.Diagnostics.AddError("Invalid import key", err.Error())
			return
		}
		row, err := lookupConsent(ctx, r.client, "", &key)
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+consentWhat, err.Error())
			return
		}
		if row == nil {
			resp.Diagnostics.AddError("Consent policy not found", fmt.Sprintf("Shopify has no consent policy for %s.", key))
			return
		}
		var m consentModel
		m.set(row, key)
		// Read fills in the rest; the key tells it where to look if the ID doesn't resolve.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), m.ID)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("country_code"), m.CountryCode)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("region_code"), m.RegionCode)...)
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

// consentUserError is the generated userErrors type of consentPolicyUpdate.
type consentUserError = client.UpdateConsentPolicyConsentPolicyUpdateConsentPolicyUpdatePayloadUserErrorsConsentPolicyError

// consentErrorDiags maps by code because the API returns an empty `field`.
func consentErrorDiags(summary string, errs []consentUserError) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, e := range errs {
		detail := e.Message
		if e.Code != nil && *e.Code == client.ConsentPolicyErrorCodeShopifyCookieBannerNotDisabled {
			detail += " Set cookie_banner_auto_managed = false on shopify_privacy_settings and add depends_on for it to this resource."
		}
		if e.Code != nil {
			if p, ok := consentErrorPath(*e.Code); ok {
				diags.AddAttributeError(p, summary, detail)
				continue
			}
		}
		diags.AddError(summary, detail)
	}
	return diags
}

// lookupConsent finds the row by numeric ID first, then by country and region. A system-default ID doesn't
// resolve by ID, so it supplies the key when the caller has none. key is filled in when it was empty.
func lookupConsent(ctx context.Context, c graphql.Client, id string, key *consentKey) (*client.ConsentPolicyFields, error) {
	if id != "" && !isSystemDefault(id) {
		res, err := client.GetConsentPolicyByID(ctx, c, id)
		if err != nil {
			return nil, err
		}
		if len(res.ConsentPolicy) > 0 {
			row := res.ConsentPolicy[0].ConsentPolicyFields
			if key.Country == "" && row.CountryCode != nil {
				*key = consentKey{Country: string(*row.CountryCode)}
				if row.RegionCode != nil {
					key.Region = *row.RegionCode
				}
			}
			return &row, nil
		}
	}
	if key.Country == "" {
		k, ok := keyFromSystemDefaultID(id)
		if !ok {
			return nil, nil
		}
		*key = k
	}
	var region *string
	if key.Region != "" {
		region = &key.Region
	}
	res, err := client.GetConsentPolicyByRegion(ctx, c, client.PrivacyCountryCode(key.Country), region)
	if err != nil {
		return nil, err
	}
	rows := make([]client.ConsentPolicyFields, len(res.ConsentPolicy))
	for i, p := range res.ConsentPolicy {
		rows[i] = p.ConsentPolicyFields
	}
	return findConsent(rows, *key), nil
}

// upsertConsent writes only when a configured value differs, then returns the row as the API now has it.
// The mutation returns only rows it changed, so the result always comes from a read.
func upsertConsent(ctx context.Context, c graphql.Client, key consentKey, consentRequired, optOutRequired *bool) (*client.ConsentPolicyFields, diag.Diagnostics) {
	var diags diag.Diagnostics
	existing, err := lookupConsent(ctx, c, "", &key)
	if err != nil {
		diags.AddError("Failed to look up "+consentWhat, err.Error())
		return nil, diags
	}
	if needsWrite(existing, consentRequired, optOutRequired) {
		res, err := client.UpdateConsentPolicy(ctx, c, consentInput(key, consentRequired, optOutRequired))
		if err != nil {
			diags.AddError("Failed to write "+consentWhat, err.Error())
			return nil, diags
		}
		if res.ConsentPolicyUpdate == nil {
			diags.AddError("Failed to write "+consentWhat, "The API returned no result.")
			return nil, diags
		}
		diags.Append(consentErrorDiags("Failed to write "+consentWhat, res.ConsentPolicyUpdate.UserErrors)...)
		if diags.HasError() {
			return nil, diags
		}
		existing, err = lookupConsent(ctx, c, "", &key)
		if err != nil {
			diags.AddError("Failed to read "+consentWhat, err.Error())
			return nil, diags
		}
	}
	if existing == nil {
		diags.AddError("Consent policy not found", "Shopify has no consent policy for "+key.String()+".")
	}
	return existing, diags
}
