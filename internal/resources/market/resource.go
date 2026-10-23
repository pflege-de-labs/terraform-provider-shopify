// Package market implements the shopify_market resource.
package market

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const what = "market"

var (
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
	_ resource.ResourceWithIdentity    = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one region market (conditions-based Markets model).
type Resource struct {
	client *client.Client
}

type model struct {
	ID               types.String `tfsdk:"id"`
	Handle           types.String `tfsdk:"handle"`
	Name             types.String `tfsdk:"name"`
	Enabled          types.Bool   `tfsdk:"enabled"`
	Conditions       types.Object `tfsdk:"conditions"`
	CurrencySettings types.Object `tfsdk:"currency_settings"`
	WebPresenceIDs   types.Set    `tfsdk:"web_presence_ids"`
	CatalogIDs       types.Set    `tfsdk:"catalog_ids"`
}

type conditionsModel struct {
	Regions types.Set `tfsdk:"regions"`
}

type currencyModel struct {
	BaseCurrency    types.String `tfsdk:"base_currency"`
	LocalCurrencies types.Bool   `tfsdk:"local_currencies"`
}

var (
	conditionsAttrTypes = map[string]attr.Type{"regions": types.SetType{ElemType: types.StringType}}
	currencyAttrTypes   = map[string]attr.Type{"base_currency": types.StringType, "local_currencies": types.BoolType}
)

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_market"
	// Read re-adopts an object recreated outside Terraform by natural key, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	keepSet := []planmodifier.Set{setplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A region market. Optional attributes left unset are not managed. Import with the market `handle`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Market GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"handle": schema.StringAttribute{
				MarkdownDescription: "Market handle, unique per shop.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Market name.",
				Required:            true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the market is active (`status = ACTIVE`) or a draft.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"conditions": schema.SingleNestedAttribute{
				MarkdownDescription: "Which buyers the market applies to.",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"regions": schema.SetAttribute{
						MarkdownDescription: "Two-letter country codes. A country may be in several markets, but two active markets can't have the same set.",
						ElementType:         types.StringType,
						Required:            true,
						Validators:          []validator.Set{setvalidator.SizeAtLeast(1)},
					},
				},
			},
			"currency_settings": schema.SingleNestedAttribute{
				MarkdownDescription: "Currency settings.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"base_currency": schema.StringAttribute{
						MarkdownDescription: "Base currency code, e.g. `EUR`.",
						Optional:            true,
						Computed:            true,
						PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
					},
					"local_currencies": schema.BoolAttribute{
						MarkdownDescription: "Whether buyers see prices in their local currency.",
						Optional:            true,
						Computed:            true,
						PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
					},
				},
			},
			"web_presence_ids": schema.SetAttribute{
				MarkdownDescription: "GIDs of linked web presences.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       keepSet,
			},
			"catalog_ids": schema.SetAttribute{
				MarkdownDescription: "GIDs of linked catalogs.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       keepSet,
			},
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

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, cfg model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	regions := stringsOf(ctx, regionsOf(ctx, plan.Conditions, &resp.Diagnostics), &resp.Diagnostics)
	in := client.MarketCreateInput{
		Name:             plan.Name.ValueString(),
		Handle:           plan.Handle.ValueStringPointer(),
		Status:           status(cfg.Enabled),
		Conditions:       &client.MarketConditionsInput{RegionsCondition: regionsInput(regions)},
		CurrencySettings: currencyInput(ctx, cfg.CurrencySettings, &resp.Diagnostics),
		WebPresences:     stringsOf(ctx, cfg.WebPresenceIDs, &resp.Diagnostics),
		Catalogs:         stringsOf(ctx, cfg.CatalogIDs, &resp.Diagnostics),
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := client.CreateMarket(ctx, r.client, in)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to create "+what, res.MarketCreate.UserErrors)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(setFromAPI(ctx, &plan, &res.MarketCreate.Market.MarketFields)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GID first; fall back to the handle so a market recreated in the admin is found again.
	var market *client.MarketFields
	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		res, err := client.GetMarket(ctx, r.client, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
		if res.Market != nil {
			market = &res.Market.MarketFields
		}
	}
	if market == nil {
		var err error
		if market, err = r.findByHandle(ctx, state.Handle.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if market == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(setFromAPI(ctx, &state, market)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, cfg, state model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.MarketUpdateInput{
		Name:             plan.Name.ValueStringPointer(),
		Status:           status(cfg.Enabled),
		CurrencySettings: currencyInput(ctx, cfg.CurrencySettings, &resp.Diagnostics),
	}
	if !plan.Handle.Equal(state.Handle) {
		in.Handle = plan.Handle.ValueStringPointer()
	}

	// Sets are diffed against state; unset ones plan to the state value and so produce no change.
	add, del := setDiff(stringsOf(ctx, regionsOf(ctx, state.Conditions, &resp.Diagnostics), &resp.Diagnostics),
		stringsOf(ctx, regionsOf(ctx, plan.Conditions, &resp.Diagnostics), &resp.Diagnostics))
	if len(add) > 0 || len(del) > 0 {
		in.Conditions = &client.MarketConditionsUpdateInput{}
		if len(add) > 0 {
			in.Conditions.ConditionsToAdd = &client.MarketConditionsInput{RegionsCondition: regionsInput(add)}
		}
		if len(del) > 0 {
			in.Conditions.ConditionsToDelete = &client.MarketConditionsInput{RegionsCondition: regionsInput(del)}
		}
	}
	in.WebPresencesToAdd, in.WebPresencesToDelete = setDiff(stringsOf(ctx, state.WebPresenceIDs, &resp.Diagnostics), stringsOf(ctx, plan.WebPresenceIDs, &resp.Diagnostics))
	in.CatalogsToAdd, in.CatalogsToDelete = setDiff(stringsOf(ctx, state.CatalogIDs, &resp.Diagnostics), stringsOf(ctx, plan.CatalogIDs, &resp.Diagnostics))
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := client.UpdateMarket(ctx, r.client, state.ID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to update "+what, res.MarketUpdate.UserErrors)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(setFromAPI(ctx, &plan, &res.MarketUpdate.Market.MarketFields)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := client.DeleteMarket(ctx, r.client, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to delete "+what, res.MarketDelete.UserErrors)...)
}

// ImportState takes a handle from `terraform import`, or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		var id patterns.IDIdentity
		resp.Diagnostics.Append(req.Identity.Get(ctx, &id)...)
		if resp.Diagnostics.HasError() {
			return
		}
		// Read looks up by GID first, so the remaining attributes are filled in there.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id.ID)...)
		resp.Diagnostics.Append(resp.Identity.Set(ctx, id)...)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("handle"), req.ID)...)
}

func (r *Resource) findByHandle(ctx context.Context, handle string) (*client.MarketFields, error) {
	var after *string
	for {
		res, err := client.ListMarkets(ctx, r.client, after)
		if err != nil {
			return nil, err
		}
		for _, m := range res.Markets.Nodes {
			if m.Handle == handle {
				return &m.MarketFields, nil
			}
		}
		if !res.Markets.PageInfo.HasNextPage {
			return nil, nil
		}
		after = res.Markets.PageInfo.EndCursor
	}
}

func setFromAPI(ctx context.Context, m *model, mk *client.MarketFields) diag.Diagnostics {
	var diags, dd diag.Diagnostics
	m.ID = types.StringValue(mk.Id)
	m.Handle = types.StringValue(mk.Handle)
	m.Name = types.StringValue(mk.Name)
	m.Enabled = types.BoolValue(mk.Status == client.MarketStatusActive)

	regions := []string{}
	if mk.Conditions != nil && mk.Conditions.RegionsCondition != nil {
		for _, n := range mk.Conditions.RegionsCondition.Regions.Nodes {
			if c, ok := n.(interface{ GetCode() client.CountryCode }); ok {
				regions = append(regions, string(c.GetCode()))
			}
		}
	}
	regionSet, dd := types.SetValueFrom(ctx, types.StringType, regions)
	diags.Append(dd...)
	m.Conditions, dd = types.ObjectValue(conditionsAttrTypes, map[string]attr.Value{"regions": regionSet})
	diags.Append(dd...)

	m.CurrencySettings = types.ObjectNull(currencyAttrTypes)
	if cs := mk.CurrencySettings; cs != nil {
		m.CurrencySettings, dd = types.ObjectValueFrom(ctx, currencyAttrTypes, currencyModel{
			BaseCurrency:    types.StringValue(string(cs.BaseCurrency.CurrencyCode)),
			LocalCurrencies: types.BoolValue(cs.LocalCurrencies),
		})
		diags.Append(dd...)
	}

	ids := func(n int, id func(int) string) types.Set {
		out := make([]string, n)
		for i := range out {
			out[i] = id(i)
		}
		s, d := types.SetValueFrom(ctx, types.StringType, out)
		diags.Append(d...)
		return s
	}
	m.WebPresenceIDs = ids(len(mk.WebPresences.Nodes), func(i int) string { return mk.WebPresences.Nodes[i].Id })
	m.CatalogIDs = ids(len(mk.Catalogs.Nodes), func(i int) string { return mk.Catalogs.Nodes[i].Id })
	return diags
}

func regionsOf(ctx context.Context, conditions types.Object, diags *diag.Diagnostics) types.Set {
	if conditions.IsNull() || conditions.IsUnknown() {
		return types.SetNull(types.StringType)
	}
	var c conditionsModel
	diags.Append(conditions.As(ctx, &c, basetypes.ObjectAsOptions{})...)
	return c.Regions
}

// stringsOf returns nil for null or unknown sets, so unset links are omitted.
func stringsOf(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	var out []string
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	return out
}

func regionsInput(codes []string) *client.MarketConditionsRegionsInput {
	in := &client.MarketConditionsRegionsInput{}
	for _, c := range codes {
		in.Regions = append(in.Regions, client.MarketConditionsRegionInput{CountryCode: client.CountryCode(c)})
	}
	return in
}

func status(enabled types.Bool) *client.MarketStatus {
	b := patterns.ConfiguredBool(enabled)
	if b == nil {
		return nil
	}
	s := client.MarketStatusDraft
	if *b {
		s = client.MarketStatusActive
	}
	return &s
}

func currencyInput(ctx context.Context, o types.Object, diags *diag.Diagnostics) *client.MarketCurrencySettingsUpdateInput {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	var c currencyModel
	diags.Append(o.As(ctx, &c, basetypes.ObjectAsOptions{})...)
	in := &client.MarketCurrencySettingsUpdateInput{LocalCurrencies: patterns.ConfiguredBool(c.LocalCurrencies)}
	if s := patterns.ConfiguredString(c.BaseCurrency); s != nil {
		cc := client.CurrencyCode(*s)
		in.BaseCurrency = &cc
	}
	return in
}
