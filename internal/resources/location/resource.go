// Package location implements the shopify_location resource.
package location

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const what = "location"

var (
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one location.
type Resource struct {
	client *client.Client
}

type model struct {
	ID                   types.String `tfsdk:"id"`
	LegacyResourceID     types.String `tfsdk:"legacy_resource_id"`
	Name                 types.String `tfsdk:"name"`
	Address              types.Object `tfsdk:"address"`
	FulfillsOnlineOrders types.Bool   `tfsdk:"fulfills_online_orders"`
	Active               types.Bool   `tfsdk:"active"`
	DeletionPolicy       types.String `tfsdk:"deletion_policy"`
	AllowDataDeletion    types.Bool   `tfsdk:"allow_data_deletion"`
}

type addressModel struct {
	Address1     types.String `tfsdk:"address1"`
	Address2     types.String `tfsdk:"address2"`
	City         types.String `tfsdk:"city"`
	Zip          types.String `tfsdk:"zip"`
	ProvinceCode types.String `tfsdk:"province_code"`
	CountryCode  types.String `tfsdk:"country_code"`
	Phone        types.String `tfsdk:"phone"`
}

var addressAttrTypes = map[string]attr.Type{
	"address1": types.StringType, "address2": types.StringType, "city": types.StringType, "zip": types.StringType,
	"province_code": types.StringType, "country_code": types.StringType, "phone": types.StringType,
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_location"
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	keepString := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	optional := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc, Optional: true, Computed: true, PlanModifiers: keepString}
	}
	keepBool := []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A location. Optional attributes left unset are not managed. Import with the location `name`, or its numeric ID if names are ambiguous.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Location GID.",
				Computed:            true,
				PlanModifiers:       keepString,
			},
			"legacy_resource_id": schema.StringAttribute{
				MarkdownDescription: "Numeric location ID.",
				Computed:            true,
				PlanModifiers:       keepString,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Location name, unique per shop.",
				Required:            true,
			},
			"address": schema.SingleNestedAttribute{
				MarkdownDescription: "Address.",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"address1":      optional("Street address."),
					"address2":      optional("Additional address line."),
					"city":          optional("City."),
					"zip":           optional("Postal code."),
					"province_code": optional("Province or state code, e.g. `CA`."),
					"country_code":  schema.StringAttribute{MarkdownDescription: "Two-letter country code, e.g. `DE`.", Required: true},
					"phone":         optional("Phone number."),
				},
			},
			"fulfills_online_orders": schema.BoolAttribute{
				MarkdownDescription: "Whether online orders can be fulfilled from here.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       keepBool,
			},
			"active": schema.BoolAttribute{
				MarkdownDescription: "Whether the location is active. Deactivating fails while it holds inventory.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       keepBool,
			},
			"deletion_policy": schema.StringAttribute{
				MarkdownDescription: "What destroy does: `deactivate` (default) keeps the location inactive in Shopify, `delete` removes it and needs `allow_data_deletion = true`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("deactivate"),
				Validators:          []validator.String{stringvalidator.OneOf("deactivate", "delete")},
			},
			"allow_data_deletion": patterns.AllowDataDeletionAttribute(what + " (only checked when deletion_policy = \"delete\")"),
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
	var addr addressModel
	resp.Diagnostics.Append(cfg.Address.As(ctx, &addr, basetypes.ObjectAsOptions{})...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := client.AddLocation(ctx, r.client, client.LocationAddInput{
		Name:                 plan.Name.ValueString(),
		FulfillsOnlineOrders: patterns.ConfiguredBool(cfg.FulfillsOnlineOrders),
		Address: client.LocationAddAddressInput{
			Address1:     patterns.ConfiguredString(addr.Address1),
			Address2:     patterns.ConfiguredString(addr.Address2),
			City:         patterns.ConfiguredString(addr.City),
			Zip:          patterns.ConfiguredString(addr.Zip),
			ProvinceCode: patterns.ConfiguredString(addr.ProvinceCode),
			CountryCode:  client.CountryCode(addr.CountryCode.ValueString()),
			Phone:        patterns.ConfiguredString(addr.Phone),
		},
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to create "+what, res.LocationAdd.UserErrors)...)
	if resp.Diagnostics.HasError() {
		return
	}
	loc := &res.LocationAdd.Location.LocationFields
	// Save the location before activation changes, so a failure there doesn't orphan it.
	resp.Diagnostics.Append(setFromAPI(ctx, &plan, loc)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

	if want := patterns.ConfiguredBool(cfg.Active); want != nil && *want != loc.IsActive {
		if loc = r.setActive(ctx, loc.Id, *want, &resp.Diagnostics); loc == nil {
			return
		}
		resp.Diagnostics.Append(setFromAPI(ctx, &plan, loc)...)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GID first; fall back to the name so a location recreated in the admin is found again.
	var loc *client.LocationFields
	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		res, err := client.GetLocation(ctx, r.client, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
		if res.Location != nil {
			loc = &res.Location.LocationFields
		}
	}
	if loc == nil {
		var diags diag.Diagnostics
		loc, diags = r.findByKey(ctx, state.Name.ValueString())
		resp.Diagnostics.Append(diags...)
		if diags.HasError() {
			return
		}
	}
	if loc == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	if state.DeletionPolicy.IsNull() {
		state.DeletionPolicy = types.StringValue("deactivate")
	}
	if state.AllowDataDeletion.IsNull() {
		state.AllowDataDeletion = types.BoolValue(false)
	}
	resp.Diagnostics.Append(setFromAPI(ctx, &state, loc)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, cfg, state model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var addr addressModel
	resp.Diagnostics.Append(cfg.Address.As(ctx, &addr, basetypes.ObjectAsOptions{})...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	res, err := client.EditLocation(ctx, r.client, id, client.LocationEditInput{
		Name:                 plan.Name.ValueStringPointer(),
		FulfillsOnlineOrders: patterns.ConfiguredBool(cfg.FulfillsOnlineOrders),
		Address: &client.LocationEditAddressInput{
			Address1:     patterns.ConfiguredString(addr.Address1),
			Address2:     patterns.ConfiguredString(addr.Address2),
			City:         patterns.ConfiguredString(addr.City),
			Zip:          patterns.ConfiguredString(addr.Zip),
			ProvinceCode: patterns.ConfiguredString(addr.ProvinceCode),
			CountryCode:  (*client.CountryCode)(addr.CountryCode.ValueStringPointer()),
			Phone:        patterns.ConfiguredString(addr.Phone),
		},
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to update "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to update "+what, res.LocationEdit.UserErrors)...)
	if resp.Diagnostics.HasError() {
		return
	}
	loc := &res.LocationEdit.Location.LocationFields

	if want := patterns.ConfiguredBool(cfg.Active); want != nil && *want != loc.IsActive {
		if loc = r.setActive(ctx, id, *want, &resp.Diagnostics); loc == nil {
			return
		}
	}
	resp.Diagnostics.Append(setFromAPI(ctx, &plan, loc)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	deleting := state.DeletionPolicy.ValueString() == "delete"
	if deleting {
		resp.Diagnostics.Append(patterns.GuardDataDeletion(state.AllowDataDeletion, what)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Shopify only deletes inactive locations, so both policies deactivate first.
	if state.Active.ValueBool() {
		if r.setActive(ctx, state.ID.ValueString(), false, &resp.Diagnostics) == nil {
			return
		}
	}
	if !deleting {
		resp.Diagnostics.AddWarning("The location was deactivated, not deleted",
			"deletion_policy is \"deactivate\", so the location stays in Shopify as inactive. Creating a location with the same name will fail until it is deleted in the admin.")
		return
	}

	res, err := client.DeleteLocation(ctx, r.client, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to delete "+what, res.LocationDelete.LocationDeleteUserErrors)...)
}

func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	loc, diags := r.findByKey(ctx, req.ID)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}
	if loc == nil {
		resp.Diagnostics.AddError("Location not found", fmt.Sprintf("No location with name or ID %q. New locations can take a few seconds to become searchable.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), loc.Id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), loc.Name)...)
}

// setActive activates or deactivates; it returns nil after adding an error.
func (r *Resource) setActive(ctx context.Context, id string, active bool, diags *diag.Diagnostics) *client.LocationFields {
	key, err := idempotencyKey()
	if err != nil {
		diags.AddError("Failed to create idempotency key", err.Error())
		return nil
	}
	if active {
		res, err := client.ActivateLocation(ctx, r.client, id, key)
		if err != nil {
			diags.AddError("Failed to activate "+what, err.Error())
			return nil
		}
		diags.Append(client.UserErrorDiags("Failed to activate "+what, res.LocationActivate.LocationActivateUserErrors)...)
		if diags.HasError() {
			return nil
		}
		return &res.LocationActivate.Location.LocationFields
	}
	res, err := client.DeactivateLocation(ctx, r.client, id, key)
	if err != nil {
		diags.AddError("Failed to deactivate "+what, err.Error())
		return nil
	}
	diags.Append(client.UserErrorDiags("Failed to deactivate "+what, res.LocationDeactivate.LocationDeactivateUserErrors)...)
	if diags.HasError() {
		return nil
	}
	return &res.LocationDeactivate.Location.LocationFields
}

// findByKey looks up by numeric ID or exact name; names matching more than one location are an error.
func (r *Resource) findByKey(ctx context.Context, key string) (*client.LocationFields, diag.Diagnostics) {
	var diags diag.Diagnostics
	gid, query := lookupFor(key)
	if gid != "" {
		res, err := client.GetLocation(ctx, r.client, gid)
		if err != nil {
			diags.AddError("Failed to read "+what, err.Error())
			return nil, diags
		}
		if res.Location == nil {
			return nil, diags
		}
		return &res.Location.LocationFields, diags
	}

	res, err := client.FindLocations(ctx, r.client, query)
	if err != nil {
		diags.AddError("Failed to search "+what, err.Error())
		return nil, diags
	}
	var found *client.LocationFields
	for _, l := range res.Locations.Nodes {
		if l.Name != key {
			continue // search is fuzzy
		}
		if found != nil {
			diags.AddError("Ambiguous location name", fmt.Sprintf("More than one location is named %q; import it by its numeric ID.", key))
			return nil, diags
		}
		found = &l.LocationFields
	}
	return found, diags
}

func setFromAPI(ctx context.Context, m *model, l *client.LocationFields) diag.Diagnostics {
	m.ID = types.StringValue(l.Id)
	m.LegacyResourceID = types.StringValue(l.LegacyResourceId)
	m.Name = types.StringValue(l.Name)
	m.FulfillsOnlineOrders = types.BoolValue(l.FulfillsOnlineOrders)
	m.Active = types.BoolValue(l.IsActive)
	a := l.Address
	var diags diag.Diagnostics
	m.Address, diags = types.ObjectValueFrom(ctx, addressAttrTypes, addressModel{
		Address1:     types.StringPointerValue(a.Address1),
		Address2:     types.StringPointerValue(a.Address2),
		City:         types.StringPointerValue(a.City),
		Zip:          types.StringPointerValue(a.Zip),
		ProvinceCode: types.StringPointerValue(a.ProvinceCode),
		CountryCode:  types.StringPointerValue(a.CountryCode),
		Phone:        types.StringPointerValue(a.Phone),
	})
	return diags
}

func idempotencyKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
