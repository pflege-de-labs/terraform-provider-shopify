// Package deliveryprofile implements the shopify_delivery_profile resource (general profile only).
package deliveryprofile

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const what = "delivery profile"

var (
	_ resource.ResourceWithConfigure      = &Resource{}
	_ resource.ResourceWithImportState    = &Resource{}
	_ resource.ResourceWithValidateConfig = &Resource{}
	_ resource.ResourceWithIdentity       = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages the store's general delivery profile, which always exists (Pattern A, implicit).
type Resource struct {
	client *client.Client
}

type model struct {
	ID     types.String `tfsdk:"id"`
	Name   types.String `tfsdk:"name"`
	Groups []groupModel `tfsdk:"location_group"`
}

type groupModel struct {
	LocationIDs types.Set   `tfsdk:"location_ids"`
	Zones       []zoneModel `tfsdk:"zone"`
}

type zoneModel struct {
	Name      types.String  `tfsdk:"name"`
	Countries types.Set     `tfsdk:"countries"`
	Methods   []methodModel `tfsdk:"method"`
}

type methodModel struct {
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Active      types.Bool   `tfsdk:"active"`
	Price       types.String `tfsdk:"price"`
	Currency    types.String `tfsdk:"currency"`
	Condition   types.Object `tfsdk:"condition"`
}

type condModel struct {
	Field    types.String `tfsdk:"field"`
	Operator types.String `tfsdk:"operator"`
	Value    types.String `tfsdk:"value"`
	Unit     types.String `tfsdk:"unit"`
}

var condAttrTypes = map[string]attr.Type{"field": types.StringType, "operator": types.StringType, "value": types.StringType, "unit": types.StringType}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_delivery_profile"
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	method := schema.ListNestedBlock{
		MarkdownDescription: "Shipping rates. Matched by `name` plus `condition`, so a flat and a free-over-X rate may share a name. Carrier-calculated rates are not managed.",
		NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
			"name":        schema.StringAttribute{MarkdownDescription: "Rate name shown at checkout.", Required: true},
			"description": schema.StringAttribute{MarkdownDescription: "Rate description.", Optional: true},
			"active": schema.BoolAttribute{
				MarkdownDescription: "Whether the rate is offered. Defaults to `true`.",
				Optional:            true, Computed: true, Default: booldefault.StaticBool(true),
			},
			"price":    schema.StringAttribute{MarkdownDescription: "Flat price as a decimal string, e.g. `\"4.95\"`.", Required: true},
			"currency": schema.StringAttribute{MarkdownDescription: "Currency code of the price and of price conditions, e.g. `EUR`.", Required: true},
			"condition": schema.SingleNestedAttribute{
				MarkdownDescription: "Only offer the rate when the order total or weight meets this condition.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"field": schema.StringAttribute{
						MarkdownDescription: "`TOTAL_PRICE` or `TOTAL_WEIGHT`.", Required: true,
						Validators: []validator.String{stringvalidator.OneOf("TOTAL_PRICE", "TOTAL_WEIGHT")},
					},
					"operator": schema.StringAttribute{
						MarkdownDescription: "`GREATER_THAN_OR_EQUAL_TO` or `LESS_THAN_OR_EQUAL_TO`.", Required: true,
						Validators: []validator.String{stringvalidator.OneOf("GREATER_THAN_OR_EQUAL_TO", "LESS_THAN_OR_EQUAL_TO")},
					},
					"value": schema.StringAttribute{MarkdownDescription: "Threshold as a decimal string.", Required: true},
					"unit": schema.StringAttribute{
						MarkdownDescription: "Weight unit for `TOTAL_WEIGHT`: `KILOGRAMS`, `GRAMS`, `POUNDS` or `OUNCES`.", Optional: true,
						Validators: []validator.String{stringvalidator.OneOf("KILOGRAMS", "GRAMS", "POUNDS", "OUNCES")},
					},
				},
			},
		}},
	}
	zone := schema.ListNestedBlock{
		MarkdownDescription: "Shipping zones, matched by name.",
		NestedObject: schema.NestedBlockObject{
			Attributes: map[string]schema.Attribute{
				"name": schema.StringAttribute{MarkdownDescription: "Zone name, unique within the location group.", Required: true},
				"countries": schema.SetAttribute{
					MarkdownDescription: "Country codes; `*` is the rest of the world. All provinces are included when the set changes.",
					ElementType:         types.StringType, Required: true,
				},
			},
			Blocks: map[string]schema.Block{"method": method},
		},
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "The store's general delivery profile. It always exists: create takes it over, destroy only removes it from state. " +
			"Terraform owns the zones and rates in it; ones missing from config are deleted. Import with `default`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Delivery profile GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Profile name.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
		Blocks: map[string]schema.Block{
			"location_group": schema.ListNestedBlock{
				MarkdownDescription: "Groups of locations that share zones and rates, matched by position.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"location_ids": schema.SetAttribute{MarkdownDescription: "Location GIDs that ship with these rates.", ElementType: types.StringType, Required: true},
					},
					Blocks: map[string]schema.Block{"zone": zone},
				},
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

func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg model
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	t, ok := toTree(ctx, cfg, &resp.Diagnostics)
	if !ok {
		return
	}
	if dups := duplicates(t); len(dups) > 0 {
		resp.Diagnostics.AddAttributeError(path.Root("location_group"), "Duplicate zones or rates",
			"Zone names must be unique per location group, and rate name plus condition unique per zone: "+strings.Join(dups, "; "))
	}
	for _, g := range t.Groups {
		for _, z := range g.Zones {
			for _, m := range z.Methods {
				if c := m.Cond; c != nil && (c.Field == "TOTAL_WEIGHT") != (c.Unit != "") {
					resp.Diagnostics.AddAttributeError(path.Root("location_group"), "Invalid rate condition",
						fmt.Sprintf("Rate %q: unit is required for TOTAL_WEIGHT and not allowed for TOTAL_PRICE.", m.Name))
				}
			}
		}
	}
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := r.generalProfileID(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to find the general "+what, err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	r.apply(ctx, &plan, &resp.Diagnostics)
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
	if state.ID.IsNull() || state.ID.IsUnknown() || state.ID.ValueString() == "" {
		id, err := r.generalProfileID(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Failed to find the general "+what, err.Error())
			return
		}
		state.ID = types.StringValue(id)
	}
	r.read(ctx, &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, "general "+what, "every store has exactly one and it can't be deleted")
}

// ImportState takes "default" from `terraform import`, or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		var id patterns.IDIdentity
		resp.Diagnostics.Append(req.Identity.Get(ctx, &id)...)
		if resp.Diagnostics.HasError() {
			return
		}
		general, err := r.generalProfileID(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Failed to find the general "+what, err.Error())
			return
		}
		if id.ID.ValueString() != general {
			resp.Diagnostics.AddError("Unsupported delivery profile", "Only the general profile is supported; "+id.ID.ValueString()+" is not it.")
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id.ID)...)
		resp.Diagnostics.Append(resp.Identity.Set(ctx, id)...)
		return
	}
	if req.ID != "default" {
		resp.Diagnostics.AddError("Invalid import key", `Only the general profile is supported; import it with "default".`)
		return
	}
	id, err := r.generalProfileID(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to find the general "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(id)})...)
}

// apply diffs the plan against the profile as it is now, sends the changes, and reads the result back into plan.
func (r *Resource) apply(ctx context.Context, plan *model, diags *diag.Diagnostics) {
	current, _, ok := r.fetch(ctx, plan.ID.ValueString(), diags)
	if !ok {
		return
	}
	desired, ok := toTree(ctx, *plan, diags)
	if !ok {
		diags.AddError("Unknown values in "+what, "All zone and rate values must be known at apply time.")
		return
	}
	if in, changed := diff(current, desired); changed {
		res, err := client.UpdateDeliveryProfile(ctx, r.client, plan.ID.ValueString(), in)
		if err != nil {
			diags.AddError("Failed to update "+what, err.Error())
			return
		}
		diags.Append(client.UserErrorDiags("Failed to update "+what, res.DeliveryProfileUpdate.UserErrors)...)
		if diags.HasError() {
			return
		}
	}
	r.read(ctx, plan, diags)
}

// read replaces m's tree with the API's, keeping m's amount strings where they are numerically equal.
func (r *Resource) read(ctx context.Context, m *model, diags *diag.Diagnostics) {
	t, name, ok := r.fetch(ctx, m.ID.ValueString(), diags)
	if !ok {
		return
	}
	prior, _ := toTree(ctx, *m, &diag.Diagnostics{})
	m.Name = types.StringValue(name)
	m.Groups = fromTree(ctx, t, prior, diags)
}

func (r *Resource) generalProfileID(ctx context.Context) (string, error) {
	var id string
	err := r.eachProfile(ctx, 0, func(p profileNode) bool {
		if p.Default {
			id = p.Id
		}
		return !p.Default
	})
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("no default delivery profile found")
	}
	return id, nil
}

type profileNode = client.ListDeliveryProfilesDeliveryProfilesDeliveryProfileConnectionNodesDeliveryProfile

// eachProfile pages through the merchant-owned profiles until push returns false.
func (r *Resource) eachProfile(ctx context.Context, limit int64, push func(profileNode) bool) error {
	page := func(after *string) ([]profileNode, *string, error) {
		res, err := client.ListDeliveryProfiles(ctx, r.client, after)
		if err != nil {
			return nil, nil, err
		}
		conn := res.DeliveryProfiles
		return conn.Nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
	return patterns.Paginate(ctx, limit, page, push)
}

// fetch reads the profile tree; it errors on truncated lists rather than manage part of the profile.
func (r *Resource) fetch(ctx context.Context, id string, diags *diag.Diagnostics) (tree, string, bool) {
	res, err := client.GetDeliveryProfile(ctx, r.client, id)
	if err != nil {
		diags.AddError("Failed to read "+what, err.Error())
		return tree{}, "", false
	}
	p := res.DeliveryProfile
	if p == nil {
		diags.AddError("Failed to read "+what, "Profile "+id+" not found.")
		return tree{}, "", false
	}
	truncated := func(what string) {
		diags.AddError("Delivery profile too large", fmt.Sprintf("More %s than the provider reads in one request; this profile is not supported yet.", what))
	}
	var t tree
	for _, pg := range p.ProfileLocationGroups {
		g := group{ID: pg.LocationGroup.Id, Locations: []string{}}
		if pg.LocationGroup.Locations.PageInfo.HasNextPage {
			truncated("locations")
		}
		for _, l := range pg.LocationGroup.Locations.Nodes {
			g.Locations = append(g.Locations, l.Id)
		}
		if pg.LocationGroupZones.PageInfo.HasNextPage {
			truncated("zones")
		}
		for _, lz := range pg.LocationGroupZones.Nodes {
			z := zone{ID: lz.Zone.Id, Name: lz.Zone.Name}
			for _, c := range lz.Zone.Countries {
				if c.Code.RestOfWorld {
					z.Countries = append(z.Countries, restOfWorld)
				} else if c.Code.CountryCode != nil {
					z.Countries = append(z.Countries, string(*c.Code.CountryCode))
				}
			}
			if lz.MethodDefinitions.PageInfo.HasNextPage {
				truncated("rates in zone " + z.Name)
			}
			for _, md := range lz.MethodDefinitions.Nodes {
				m, ok, err := methodFromAPI(md.DeliveryMethodFields)
				if err != nil {
					diags.AddError("Unsupported rate in "+what, fmt.Sprintf("Zone %q: %s", z.Name, err))
					continue
				}
				if ok {
					z.Methods = append(z.Methods, m)
				}
			}
			g.Zones = append(g.Zones, z)
		}
		t.Groups = append(t.Groups, g)
	}
	return t, p.Name, !diags.HasError()
}

// methodFromAPI skips carrier-calculated rates, which this resource doesn't manage.
// A rate with a min and a max comes back as two conditions; those fail rather than lose a bound on the next change.
func methodFromAPI(md client.DeliveryMethodFields) (method, bool, error) {
	rate, ok := md.RateProvider.(*client.DeliveryMethodFieldsRateProviderDeliveryRateDefinition)
	if !ok {
		return method{}, false, nil
	}
	if len(md.MethodConditions) > 1 {
		return method{}, false, fmt.Errorf("rate %q has both a minimum and a maximum condition, which is not supported yet", md.Name)
	}
	m := method{
		ID: md.Id, RateID: rate.Id, Name: md.Name, Active: md.Active,
		Price: rate.Price.Amount, Currency: string(rate.Price.CurrencyCode),
	}
	if md.Description != nil {
		m.Description = *md.Description
	}
	for _, c := range md.MethodConditions {
		mc := &cond{ID: c.Id, Field: string(c.Field), Operator: string(c.Operator)}
		switch crit := c.ConditionCriteria.(type) {
		case *client.DeliveryMethodFieldsMethodConditionsDeliveryConditionConditionCriteriaMoneyV2:
			mc.Value = crit.Amount
		case *client.DeliveryMethodFieldsMethodConditionsDeliveryConditionConditionCriteriaWeight:
			mc.Value = strconv.FormatFloat(crit.Value, 'f', -1, 64)
			mc.Unit = string(crit.Unit)
		}
		m.Cond = mc
	}
	return m, true, nil
}

// toTree converts the model; ok is false while any value is unknown.
func toTree(ctx context.Context, m model, diags *diag.Diagnostics) (tree, bool) {
	var t tree
	for _, g := range m.Groups {
		locs, ok := setStrings(ctx, g.LocationIDs, diags)
		if !ok {
			return tree{}, false
		}
		tg := group{Locations: locs}
		for _, z := range g.Zones {
			countries, ok := setStrings(ctx, z.Countries, diags)
			if !ok || z.Name.IsUnknown() {
				return tree{}, false
			}
			tz := zone{Name: z.Name.ValueString(), Countries: countries}
			for _, mm := range z.Methods {
				if mm.Name.IsUnknown() || mm.Price.IsUnknown() || mm.Currency.IsUnknown() || mm.Active.IsUnknown() || mm.Description.IsUnknown() || mm.Condition.IsUnknown() {
					return tree{}, false
				}
				tm := method{
					Name: mm.Name.ValueString(), Description: mm.Description.ValueString(), Active: mm.Active.ValueBool(),
					Price: mm.Price.ValueString(), Currency: mm.Currency.ValueString(),
				}
				if !mm.Condition.IsNull() {
					var c condModel
					diags.Append(mm.Condition.As(ctx, &c, basetypes.ObjectAsOptions{})...)
					if c.Field.IsUnknown() || c.Operator.IsUnknown() || c.Value.IsUnknown() || c.Unit.IsUnknown() {
						return tree{}, false
					}
					tm.Cond = &cond{Field: c.Field.ValueString(), Operator: c.Operator.ValueString(), Value: c.Value.ValueString(), Unit: c.Unit.ValueString()}
				}
				tz.Methods = append(tz.Methods, tm)
			}
			tg.Zones = append(tg.Zones, tz)
		}
		t.Groups = append(t.Groups, tg)
	}
	return t, !diags.HasError()
}

// fromTree builds the model, reusing prior amount strings that are numerically equal so "55.00" doesn't drift to "55.0".
func fromTree(ctx context.Context, t tree, prior tree, diags *diag.Diagnostics) []groupModel {
	priorMethod := func(gi int, zoneName, key string) *method {
		if gi >= len(prior.Groups) {
			return nil
		}
		for _, z := range prior.Groups[gi].Zones {
			if z.Name != zoneName {
				continue
			}
			for _, m := range z.Methods {
				if m.key() == key {
					return &m
				}
			}
		}
		return nil
	}
	out := make([]groupModel, len(t.Groups))
	for gi, g := range t.Groups {
		locs, d := types.SetValueFrom(ctx, types.StringType, g.Locations)
		diags.Append(d...)
		gm := groupModel{LocationIDs: locs, Zones: make([]zoneModel, len(g.Zones))}
		for zi, z := range g.Zones {
			countries, d := types.SetValueFrom(ctx, types.StringType, z.Countries)
			diags.Append(d...)
			zm := zoneModel{Name: types.StringValue(z.Name), Countries: countries, Methods: make([]methodModel, len(z.Methods))}
			for mi, m := range z.Methods {
				if p := priorMethod(gi, z.Name, m.key()); p != nil {
					if sameAmount(p.Price, m.Price) {
						m.Price = p.Price
					}
					if m.Cond != nil && p.Cond != nil {
						m.Cond.Value = p.Cond.Value
					}
				}
				mm := methodModel{
					Name: types.StringValue(m.Name), Description: types.StringNull(), Active: types.BoolValue(m.Active),
					Price: types.StringValue(m.Price), Currency: types.StringValue(m.Currency), Condition: types.ObjectNull(condAttrTypes),
				}
				if m.Description != "" {
					mm.Description = types.StringValue(m.Description)
				}
				if c := m.Cond; c != nil {
					unit := types.StringNull()
					if c.Unit != "" {
						unit = types.StringValue(c.Unit)
					}
					mm.Condition, d = types.ObjectValue(condAttrTypes, map[string]attr.Value{
						"field": types.StringValue(c.Field), "operator": types.StringValue(c.Operator), "value": types.StringValue(c.Value), "unit": unit,
					})
					diags.Append(d...)
				}
				zm.Methods[mi] = mm
			}
			gm.Zones[zi] = zm
		}
		out[gi] = gm
	}
	return out
}

func setStrings(ctx context.Context, s types.Set, diags *diag.Diagnostics) ([]string, bool) {
	if s.IsUnknown() {
		return nil, false
	}
	out := []string{}
	if !s.IsNull() {
		diags.Append(s.ElementsAs(ctx, &out, false)...)
	}
	return out, true
}
