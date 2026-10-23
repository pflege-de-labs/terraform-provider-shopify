// Package metafielddefinition implements the shopify_metafield_definition resource.
package metafielddefinition

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const (
	what      = "metafield definition"
	importKey = "OWNER_TYPE/namespace/key"
)

// replaceAttrs can't change in place: the update input has no type, and owner type, namespace and key identify the definition.
var replaceAttrs = []string{"owner_type", "namespace", "key", "type"}

var (
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
	_ resource.ResourceWithModifyPlan  = &Resource{}
	_ resource.ResourceWithIdentity    = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one metafield definition.
type Resource struct {
	client *client.Client
}

type model struct {
	ID                       types.String `tfsdk:"id"`
	OwnerType                types.String `tfsdk:"owner_type"`
	Namespace                types.String `tfsdk:"namespace"`
	Key                      types.String `tfsdk:"key"`
	Type                     types.String `tfsdk:"type"`
	Name                     types.String `tfsdk:"name"`
	Description              types.String `tfsdk:"description"`
	Validations              types.Map    `tfsdk:"validations"`
	Access                   types.Object `tfsdk:"access"`
	Pin                      types.Bool   `tfsdk:"pin"`
	UseAsCollectionCondition types.Bool   `tfsdk:"use_as_collection_condition"`
	AllowDataDeletion        types.Bool   `tfsdk:"allow_data_deletion"`
}

type accessModel struct {
	Admin           types.String `tfsdk:"admin"`
	Storefront      types.String `tfsdk:"storefront"`
	CustomerAccount types.String `tfsdk:"customer_account"`
}

var accessAttrTypes = map[string]attr.Type{
	"admin":            types.StringType,
	"storefront":       types.StringType,
	"customer_account": types.StringType,
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metafield_definition"
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keepString := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	keepBool := []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A metafield definition. Optional attributes left unset are not managed, so admin edits to them are kept. " +
			"Import with `" + importKey + "`, e.g. `PRODUCT/custom/material`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Metafield definition GID.",
				Computed:            true,
				PlanModifiers:       keepString,
			},
			"owner_type": schema.StringAttribute{
				MarkdownDescription: "Owner resource type, e.g. `PRODUCT`. Changing it forces replacement.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"namespace": schema.StringAttribute{
				MarkdownDescription: "Namespace, e.g. `custom`. Changing it forces replacement.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "Key within the namespace. Changing it forces replacement.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Metafield type, e.g. `number_integer` or `list.single_line_text_field`. Changing it forces replacement.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name.",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       keepString,
			},
			"validations": schema.MapAttribute{
				MarkdownDescription: "Validation name to value, e.g. `{ min = \"1\" }`. JSON-shaped values are passed as JSON strings.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
			"access": schema.SingleNestedAttribute{
				MarkdownDescription: "API access settings.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"admin": schema.StringAttribute{
						MarkdownDescription: "`MERCHANT_READ` or `MERCHANT_READ_WRITE`. Null when Shopify reports a value that can't be set, e.g. the default `PUBLIC_READ_WRITE`.",
						Optional:            true,
						Computed:            true,
						PlanModifiers:       keepString,
						Validators:          []validator.String{stringvalidator.OneOf("MERCHANT_READ", "MERCHANT_READ_WRITE")},
					},
					"storefront": schema.StringAttribute{
						MarkdownDescription: "`PUBLIC_READ` or `NONE`.",
						Optional:            true,
						Computed:            true,
						PlanModifiers:       keepString,
						Validators:          []validator.String{stringvalidator.OneOf("PUBLIC_READ", "NONE")},
					},
					"customer_account": schema.StringAttribute{
						MarkdownDescription: "`READ_WRITE`, `READ` or `NONE`.",
						Optional:            true,
						Computed:            true,
						PlanModifiers:       keepString,
						Validators:          []validator.String{stringvalidator.OneOf("READ_WRITE", "READ", "NONE")},
					},
				},
			},
			"pin": schema.BoolAttribute{
				MarkdownDescription: "Pin the definition in the admin.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       keepBool,
			},
			"use_as_collection_condition": schema.BoolAttribute{
				MarkdownDescription: "Allow use in smart collection conditions. Shopify blocks deletion while a collection uses it.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       keepBool,
			},
			"allow_data_deletion": patterns.AllowDataDeletionAttribute(what),
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
	patterns.WarnOnDataDeletingReplace(ctx, req, resp, what, replaceAttrs...)
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, cfg model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.MetafieldDefinitionInput{
		OwnerType:   client.MetafieldOwnerType(plan.OwnerType.ValueString()),
		Namespace:   plan.Namespace.ValueStringPointer(),
		Key:         plan.Key.ValueString(),
		Type:        plan.Type.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: patterns.ConfiguredString(cfg.Description),
		Pin:         patterns.ConfiguredBool(cfg.Pin),
	}
	var diags diag.Diagnostics
	in.Validations, diags = validationsInput(ctx, cfg.Validations)
	resp.Diagnostics.Append(diags...)
	access, diags := accessFromConfig(ctx, cfg.Access)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if access != nil {
		in.Access = &client.MetafieldAccessInput{Admin: access.admin, Storefront: access.storefront, CustomerAccount: access.customerAccount}
	}
	if b := patterns.ConfiguredBool(cfg.UseAsCollectionCondition); b != nil {
		in.Capabilities = &client.MetafieldCapabilityCreateInput{SmartCollectionCondition: &client.MetafieldCapabilitySmartCollectionConditionInput{Enabled: *b}}
	}

	res, err := client.CreateMetafieldDefinition(ctx, r.client, in)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to create "+what, res.MetafieldDefinitionCreate.UserErrors)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(setFromAPI(ctx, &plan, &res.MetafieldDefinitionCreate.CreatedDefinition.MetafieldDefinitionFields)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Lookup is by natural key: it is immutable, so a definition recreated in the admin is still found. See resource model §4.
	res, err := client.GetMetafieldDefinition(ctx, r.client, client.MetafieldDefinitionIdentifierInput{
		OwnerType: client.MetafieldOwnerType(state.OwnerType.ValueString()),
		Namespace: state.Namespace.ValueStringPointer(),
		Key:       state.Key.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to read "+what, err.Error())
		return
	}
	if res.MetafieldDefinition == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	if state.AllowDataDeletion.IsNull() {
		state.AllowDataDeletion = types.BoolValue(false)
	}
	resp.Diagnostics.Append(setFromAPI(ctx, &state, &res.MetafieldDefinition.MetafieldDefinitionFields)...)
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

	in := client.MetafieldDefinitionUpdateInput{
		OwnerType:   client.MetafieldOwnerType(plan.OwnerType.ValueString()),
		Namespace:   plan.Namespace.ValueStringPointer(),
		Key:         plan.Key.ValueString(),
		Name:        plan.Name.ValueStringPointer(),
		Description: patterns.ConfiguredString(cfg.Description),
		Pin:         patterns.ConfiguredBool(cfg.Pin),
	}
	var diags diag.Diagnostics
	// Sent in full from the plan, which holds the state value when unmanaged, so an empty map can clear it.
	in.Validations, diags = validationsInput(ctx, plan.Validations)
	resp.Diagnostics.Append(diags...)
	access, diags := accessFromConfig(ctx, cfg.Access)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if access != nil {
		in.Access = &client.MetafieldAccessUpdateInput{Admin: access.admin, Storefront: access.storefront, CustomerAccount: access.customerAccount}
	}
	if b := patterns.ConfiguredBool(cfg.UseAsCollectionCondition); b != nil {
		in.Capabilities = &client.MetafieldCapabilityUpdateInput{SmartCollectionCondition: &client.MetafieldCapabilitySmartCollectionConditionInput{Enabled: *b}}
	}

	res, err := client.UpdateMetafieldDefinition(ctx, r.client, in)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to update "+what, res.MetafieldDefinitionUpdate.UserErrors)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(setFromAPI(ctx, &plan, &res.MetafieldDefinitionUpdate.UpdatedDefinition.MetafieldDefinitionFields)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(patterns.GuardDataDeletion(state.AllowDataDeletion, what)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := client.DeleteMetafieldDefinition(ctx, r.client, state.ID.ValueString(), true)
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to delete "+what, res.MetafieldDefinitionDelete.UserErrors)...)
}

// ImportState takes the natural key from `terraform import`, or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var parts []string
	if req.ID != "" {
		var err error
		if parts, err = patterns.SplitImportKey(req.ID, importKey); err != nil {
			resp.Diagnostics.AddError("Invalid import key", err.Error())
			return
		}
	} else {
		var id patterns.IDIdentity
		resp.Diagnostics.Append(req.Identity.Get(ctx, &id)...)
		if resp.Diagnostics.HasError() {
			return
		}
		// Read looks up by natural key, so resolve it from the GID once here.
		d, err := getByID(ctx, r.client, id.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+what, err.Error())
			return
		}
		parts = []string{string(d.OwnerType), d.Namespace, d.Key}
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("owner_type"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("namespace"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key"), parts[2])...)
}

func getByID(ctx context.Context, c *client.Client, id string) (*client.MetafieldDefinitionFields, error) {
	res, err := client.GetMetafieldDefinitionByID(ctx, c, id)
	if err != nil {
		return nil, err
	}
	var d client.MetafieldDefinitionFields
	if err := json.Unmarshal(res.Node, &d); err != nil {
		return nil, err
	}
	// A null node or a GID of another type decodes to an empty struct.
	if d.Id == "" {
		return nil, fmt.Errorf("no %s with ID %q", what, id)
	}
	return &d, nil
}

// setFromAPI copies server values into m; allow_data_deletion is provider-only and left as is.
func setFromAPI(ctx context.Context, m *model, d *client.MetafieldDefinitionFields) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(d.Id)
	m.OwnerType = types.StringValue(string(d.OwnerType))
	m.Namespace = types.StringValue(d.Namespace)
	m.Key = types.StringValue(d.Key)
	m.Type = types.StringValue(d.Type.Name)
	m.Name = types.StringValue(d.Name)
	m.Description = types.StringValue("")
	if d.Description != nil {
		m.Description = types.StringValue(*d.Description)
	}

	validations := make(map[string]string, len(d.Validations))
	for _, v := range d.Validations {
		if v.Value != nil {
			validations[v.Name] = *v.Value
		}
	}
	var dd diag.Diagnostics
	m.Validations, dd = types.MapValueFrom(ctx, types.StringType, validations)
	diags.Append(dd...)

	access := accessModel{
		Admin:           adminAccess(d.Access.Admin),
		Storefront:      types.StringPointerValue((*string)(d.Access.Storefront)),
		CustomerAccount: types.StringValue(string(d.Access.CustomerAccount)),
	}
	m.Access, dd = types.ObjectValueFrom(ctx, accessAttrTypes, access)
	diags.Append(dd...)

	m.Pin = types.BoolValue(d.PinnedPosition != nil)
	m.UseAsCollectionCondition = types.BoolValue(d.Capabilities.SmartCollectionCondition.Enabled)
	return diags
}

// adminAccess keeps only values the input accepts, so generated config passes validation.
func adminAccess(a *client.MetafieldAdminAccess) types.String {
	if a == nil || (*a != client.MetafieldAdminAccessMerchantRead && *a != client.MetafieldAdminAccessMerchantReadWrite) {
		return types.StringNull()
	}
	return types.StringValue(string(*a))
}

// validationsInput always returns a non-nil slice so an empty map clears validations instead of sending null.
func validationsInput(ctx context.Context, m types.Map) ([]client.MetafieldDefinitionValidationInput, diag.Diagnostics) {
	out := []client.MetafieldDefinitionValidationInput{}
	if m.IsNull() || m.IsUnknown() {
		return out, nil
	}
	var values map[string]string
	diags := m.ElementsAs(ctx, &values, false)
	for name, value := range values {
		out = append(out, client.MetafieldDefinitionValidationInput{Name: name, Value: value})
	}
	return out, diags
}

type accessInput struct {
	admin           *client.MetafieldAdminAccessInput
	storefront      *client.MetafieldStorefrontAccessInput
	customerAccount *client.MetafieldCustomerAccountAccessInput
}

func accessFromConfig(ctx context.Context, o types.Object) (*accessInput, diag.Diagnostics) {
	if o.IsNull() || o.IsUnknown() {
		return nil, nil
	}
	var a accessModel
	diags := o.As(ctx, &a, basetypes.ObjectAsOptions{})
	in := &accessInput{}
	if s := patterns.ConfiguredString(a.Admin); s != nil {
		v := client.MetafieldAdminAccessInput(*s)
		in.admin = &v
	}
	if s := patterns.ConfiguredString(a.Storefront); s != nil {
		v := client.MetafieldStorefrontAccessInput(*s)
		in.storefront = &v
	}
	if s := patterns.ConfiguredString(a.CustomerAccount); s != nil {
		v := client.MetafieldCustomerAccountAccessInput(*s)
		in.customerAccount = &v
	}
	return in, diags
}
