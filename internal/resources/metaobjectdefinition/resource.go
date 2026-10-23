// Package metaobjectdefinition implements the shopify_metaobject_definition resource.
package metaobjectdefinition

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const what = "metaobject definition"

var (
	_ resource.ResourceWithConfigure      = &Resource{}
	_ resource.ResourceWithImportState    = &Resource{}
	_ resource.ResourceWithModifyPlan     = &Resource{}
	_ resource.ResourceWithIdentity       = &Resource{}
	_ resource.ResourceWithValidateConfig = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one metaobject definition and its full field list.
type Resource struct {
	client *client.Client
}

type model struct {
	ID                types.String `tfsdk:"id"`
	Type              types.String `tfsdk:"type"`
	Name              types.String `tfsdk:"name"`
	Description       types.String `tfsdk:"description"`
	DisplayNameKey    types.String `tfsdk:"display_name_key"`
	Access            types.Object `tfsdk:"access"`
	Capabilities      types.Object `tfsdk:"capabilities"`
	Fields            []fieldModel `tfsdk:"field"`
	AllowDataDeletion types.Bool   `tfsdk:"allow_data_deletion"`
}

type fieldModel struct {
	Key         types.String `tfsdk:"key"`
	Type        types.String `tfsdk:"type"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Required    types.Bool   `tfsdk:"required"`
	Validations types.Map    `tfsdk:"validations"`
}

type accessModel struct {
	Admin           types.String `tfsdk:"admin"`
	Storefront      types.String `tfsdk:"storefront"`
	CustomerAccount types.String `tfsdk:"customer_account"`
}

type capabilitiesModel struct {
	Publishable  types.Bool `tfsdk:"publishable"`
	Translatable types.Bool `tfsdk:"translatable"`
	Renderable   types.Bool `tfsdk:"renderable"`
}

var (
	accessAttrTypes       = map[string]attr.Type{"admin": types.StringType, "storefront": types.StringType, "customer_account": types.StringType}
	capabilitiesAttrTypes = map[string]attr.Type{"publishable": types.BoolType, "translatable": types.BoolType, "renderable": types.BoolType}
)

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metaobject_definition"
	// Read re-adopts an object recreated outside Terraform by natural key, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	keepString := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	keepBool := []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}
	optionalString := func(desc string, values ...string) schema.StringAttribute {
		return schema.StringAttribute{
			MarkdownDescription: desc, Optional: true, Computed: true, PlanModifiers: keepString,
			Validators: []validator.String{stringvalidator.OneOf(values...)},
		}
	}
	admin := optionalString("`MERCHANT_READ` or `MERCHANT_READ_WRITE`. Null when Shopify reports a value that can't be set, e.g. the default `PUBLIC_READ_WRITE`.", "MERCHANT_READ", "MERCHANT_READ_WRITE")
	admin.PlanModifiers = []planmodifier.String{patterns.UseStateForUnknownIncludingNull()}
	optionalBool := func(desc string) schema.BoolAttribute {
		return schema.BoolAttribute{MarkdownDescription: desc, Optional: true, Computed: true, PlanModifiers: keepBool}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A metaobject definition and its fields. The `field` list is fully managed; other optional attributes left unset are not managed. " +
			"Import with the definition `type`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Metaobject definition GID.",
				Computed:            true,
				PlanModifiers:       keepString,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Definition type, e.g. `faq_entry`. Changing it forces replacement.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(3, 255)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
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
			"display_name_key": schema.StringAttribute{
				MarkdownDescription: "Key of the field used as display name for entries.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       keepString,
			},
			"access": schema.SingleNestedAttribute{
				MarkdownDescription: "API access settings.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"admin":            admin,
					"storefront":       optionalString("`PUBLIC_READ` or `NONE`.", "PUBLIC_READ", "NONE"),
					"customer_account": optionalString("`READ_WRITE`, `READ` or `NONE`.", "READ_WRITE", "READ", "NONE"),
				},
			},
			"capabilities": schema.SingleNestedAttribute{
				MarkdownDescription: "Capabilities of entries of this type.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"publishable":  optionalBool("Entries have a draft/active status."),
					"translatable": optionalBool("Entries can be translated."),
					"renderable":   optionalBool("Entries can be rendered as web pages (SEO data)."),
				},
			},
			"allow_data_deletion": patterns.AllowDataDeletionAttribute(what + " or one of its fields"),
		},
		Blocks: map[string]schema.Block{
			"field": schema.ListNestedBlock{
				MarkdownDescription: "Field definitions, in display order. Removing a field or changing its `type` deletes its data and requires `allow_data_deletion = true`.",
				Validators:          []validator.List{listvalidator.SizeAtLeast(1)},
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"key": schema.StringAttribute{
							MarkdownDescription: "Field key, unique within the definition.",
							Required:            true,
							Validators:          []validator.String{stringvalidator.LengthBetween(2, 64)},
						},
						"type": schema.StringAttribute{
							MarkdownDescription: "Field type, e.g. `single_line_text_field`. Changing it deletes and recreates the field.",
							Required:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Human-readable name.",
							Required:            true,
						},
						"description": schema.StringAttribute{
							MarkdownDescription: "Description.",
							Optional:            true,
						},
						"required": schema.BoolAttribute{
							MarkdownDescription: "Whether entries must set this field. Defaults to `false`.",
							Optional:            true,
							Computed:            true,
							Default:             booldefault.StaticBool(false),
						},
						"validations": schema.MapAttribute{
							MarkdownDescription: "Validation name to value, as for metafield definitions.",
							ElementType:         types.StringType,
							Optional:            true,
						},
					},
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
	fields, ok := toFields(ctx, cfg.Fields, &resp.Diagnostics)
	if !ok {
		return
	}
	if dups := duplicateKeys(fields); len(dups) > 0 {
		resp.Diagnostics.AddAttributeError(path.Root("field"), "Duplicate field keys", "Field keys must be unique: "+strings.Join(dups, ", "))
	}
}

func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	patterns.WarnOnDataDeletingReplace(ctx, req, resp, what, "type")
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var state, plan model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || !state.Type.Equal(plan.Type) {
		return
	}
	oldFields, ok1 := toFields(ctx, state.Fields, &resp.Diagnostics)
	newFields, ok2 := toFields(ctx, plan.Fields, &resp.Diagnostics)
	if !ok1 || !ok2 {
		return
	}
	if _, destructive := diffFields(oldFields, newFields); len(destructive) > 0 {
		detail := fmt.Sprintf("Removing fields or changing their type deletes the data stored in them: %s.", strings.Join(destructive, ", "))
		if !plan.AllowDataDeletion.ValueBool() {
			detail += " Apply will fail unless allow_data_deletion = true."
		}
		resp.Diagnostics.AddWarning("This change deletes metaobject field data", detail)
	}
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, cfg model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	fields, ok := toFields(ctx, plan.Fields, &resp.Diagnostics)
	if !ok {
		return
	}

	in := client.MetaobjectDefinitionCreateInput{
		Type:           plan.Type.ValueString(),
		Name:           plan.Name.ValueStringPointer(),
		Description:    patterns.ConfiguredString(cfg.Description),
		DisplayNameKey: patterns.ConfiguredString(cfg.DisplayNameKey),
		Access:         accessInput(ctx, cfg.Access, &resp.Diagnostics),
	}
	if c := capabilitiesInput(ctx, cfg.Capabilities, &resp.Diagnostics); c != nil {
		in.Capabilities = &client.MetaobjectCapabilityCreateInput{Publishable: c.Publishable, Translatable: c.Translatable, Renderable: c.Renderable}
	}
	for _, f := range fields {
		in.FieldDefinitions = append(in.FieldDefinitions, createFieldInput(f))
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := client.CreateMetaobjectDefinition(ctx, r.client, in)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to create "+what, res.MetaobjectDefinitionCreate.UserErrors)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(setFromAPI(ctx, &plan, &res.MetaobjectDefinitionCreate.MetaobjectDefinition.MetaobjectDefinitionFields)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GID first; fall back to the type so a definition recreated in the admin is found again.
	var def *client.MetaobjectDefinitionFields
	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		res, err := client.GetMetaobjectDefinition(ctx, r.client, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
		if res.MetaobjectDefinition != nil {
			def = &res.MetaobjectDefinition.MetaobjectDefinitionFields
		}
	}
	if def == nil {
		res, err := client.GetMetaobjectDefinitionByType(ctx, r.client, state.Type.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
		if res.MetaobjectDefinitionByType == nil {
			resp.State.RemoveResource(ctx)
			return
		}
		def = &res.MetaobjectDefinitionByType.MetaobjectDefinitionFields
	}

	if state.AllowDataDeletion.IsNull() {
		state.AllowDataDeletion = types.BoolValue(false)
	}
	resp.Diagnostics.Append(setFromAPI(ctx, &state, def)...)
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
	oldFields, ok1 := toFields(ctx, state.Fields, &resp.Diagnostics)
	newFields, ok2 := toFields(ctx, plan.Fields, &resp.Diagnostics)
	if !ok1 || !ok2 {
		return
	}

	ops, destructive := diffFields(oldFields, newFields)
	// The opt-in may be set in the same apply: field changes are an update, so the planned value counts.
	if len(destructive) > 0 && !plan.AllowDataDeletion.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("field"), "Field change would delete data",
			fmt.Sprintf("Removing fields or changing their type deletes their data (%s). Set allow_data_deletion = true to allow it.", strings.Join(destructive, ", ")))
		return
	}

	reset := true
	in := client.MetaobjectDefinitionUpdateInput{
		Name:            plan.Name.ValueStringPointer(),
		Description:     patterns.ConfiguredString(cfg.Description),
		DisplayNameKey:  patterns.ConfiguredString(cfg.DisplayNameKey),
		Access:          accessInput(ctx, cfg.Access, &resp.Diagnostics),
		ResetFieldOrder: &reset,
	}
	if c := capabilitiesInput(ctx, cfg.Capabilities, &resp.Diagnostics); c != nil {
		in.Capabilities = &client.MetaobjectCapabilityUpdateInput{Publishable: c.Publishable, Translatable: c.Translatable, Renderable: c.Renderable}
	}
	for _, op := range ops {
		switch {
		case op.Delete != "":
			in.FieldDefinitions = append(in.FieldDefinitions, client.MetaobjectFieldDefinitionOperationInput{Delete: &client.MetaobjectFieldDefinitionDeleteInput{Key: op.Delete}})
		case op.Create != nil:
			c := createFieldInput(*op.Create)
			in.FieldDefinitions = append(in.FieldDefinitions, client.MetaobjectFieldDefinitionOperationInput{Create: &c})
		case op.Update != nil:
			f := *op.Update
			in.FieldDefinitions = append(in.FieldDefinitions, client.MetaobjectFieldDefinitionOperationInput{Update: &client.MetaobjectFieldDefinitionUpdateInput{
				Key: f.Key, Name: &f.Name, Description: &f.Description, Required: &f.Required, Validations: validationsInput(f.Validations),
			}})
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// Shopify rejects a delete and a create of the same key in one call, so deletes go first on their own.
	var deletes, rest []client.MetaobjectFieldDefinitionOperationInput
	for _, op := range in.FieldDefinitions {
		if op.Delete != nil {
			deletes = append(deletes, op)
		} else {
			rest = append(rest, op)
		}
	}
	if len(deletes) > 0 {
		res, err := client.UpdateMetaobjectDefinition(ctx, r.client, plan.ID.ValueString(), client.MetaobjectDefinitionUpdateInput{FieldDefinitions: deletes})
		if err != nil {
			resp.Diagnostics.AddError("Failed to delete metaobject fields", err.Error())
			return
		}
		resp.Diagnostics.Append(client.UserErrorDiags("Failed to delete metaobject fields", res.MetaobjectDefinitionUpdate.UserErrors)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	in.FieldDefinitions = rest

	res, err := client.UpdateMetaobjectDefinition(ctx, r.client, plan.ID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to update "+what, res.MetaobjectDefinitionUpdate.UserErrors)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(setFromAPI(ctx, &plan, &res.MetaobjectDefinitionUpdate.MetaobjectDefinition.MetaobjectDefinitionFields)...)
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

	// Entries and their metafields are deleted asynchronously by Shopify; the definition itself is gone at once.
	res, err := client.DeleteMetaobjectDefinition(ctx, r.client, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to delete "+what, res.MetaobjectDefinitionDelete.UserErrors)...)
}

// ImportState takes the type from `terraform import`, or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	typ := req.ID
	if typ == "" {
		var id patterns.IDIdentity
		resp.Diagnostics.Append(req.Identity.Get(ctx, &id)...)
		if resp.Diagnostics.HasError() {
			return
		}
		// Read falls back to the type, so resolve it from the GID once here.
		res, err := client.GetMetaobjectDefinition(ctx, r.client, id.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+what, err.Error())
			return
		}
		if res.MetaobjectDefinition == nil {
			resp.Diagnostics.AddError("Failed to import "+what, fmt.Sprintf("no %s with ID %q", what, id.ID.ValueString()))
			return
		}
		typ = res.MetaobjectDefinition.Type
		resp.Diagnostics.Append(resp.Identity.Set(ctx, id)...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("type"), typ)...)
}

func setFromAPI(ctx context.Context, m *model, d *client.MetaobjectDefinitionFields) diag.Diagnostics {
	var diags, dd diag.Diagnostics
	m.ID = types.StringValue(d.Id)
	m.Type = types.StringValue(d.Type)
	m.Name = types.StringValue(d.Name)
	m.Description = types.StringValue(deref(d.Description))
	m.DisplayNameKey = types.StringValue(deref(d.DisplayNameKey))

	m.Access, dd = types.ObjectValueFrom(ctx, accessAttrTypes, accessModel{
		Admin:           patterns.AdminAccess(string(d.Access.Admin)),
		Storefront:      types.StringValue(string(d.Access.Storefront)),
		CustomerAccount: types.StringValue(string(d.Access.CustomerAccount)),
	})
	diags.Append(dd...)
	caps := capabilitiesModel{
		Publishable:  types.BoolValue(d.Capabilities.Publishable.Enabled),
		Translatable: types.BoolValue(d.Capabilities.Translatable.Enabled),
		Renderable:   types.BoolValue(d.Capabilities.Renderable != nil && d.Capabilities.Renderable.Enabled),
	}
	m.Capabilities, dd = types.ObjectValueFrom(ctx, capabilitiesAttrTypes, caps)
	diags.Append(dd...)

	m.Fields = make([]fieldModel, len(d.FieldDefinitions))
	for i, f := range d.FieldDefinitions {
		fm := fieldModel{
			Key:         types.StringValue(f.Key),
			Type:        types.StringValue(f.Type.Name),
			Name:        types.StringValue(f.Name),
			Description: types.StringNull(),
			Required:    types.BoolValue(f.Required),
			Validations: types.MapNull(types.StringType),
		}
		// Empty values map to null so unset config doesn't show a diff.
		if s := deref(f.Description); s != "" {
			fm.Description = types.StringValue(s)
		}
		if len(f.Validations) > 0 {
			v := map[string]string{}
			for _, val := range f.Validations {
				v[val.Name] = deref(val.Value)
			}
			fm.Validations, dd = types.MapValueFrom(ctx, types.StringType, v)
			diags.Append(dd...)
		}
		m.Fields[i] = fm
	}
	return diags
}

// toFields converts field blocks; ok is false while any value is still unknown.
func toFields(ctx context.Context, in []fieldModel, diags *diag.Diagnostics) ([]field, bool) {
	out := make([]field, len(in))
	for i, f := range in {
		if f.Key.IsUnknown() || f.Type.IsUnknown() || f.Name.IsUnknown() || f.Description.IsUnknown() || f.Required.IsUnknown() || f.Validations.IsUnknown() {
			return nil, false
		}
		out[i] = field{Key: f.Key.ValueString(), Type: f.Type.ValueString(), Name: f.Name.ValueString(), Description: f.Description.ValueString(), Required: f.Required.ValueBool()}
		if !f.Validations.IsNull() {
			diags.Append(f.Validations.ElementsAs(ctx, &out[i].Validations, false)...)
		}
	}
	return out, !diags.HasError()
}

func createFieldInput(f field) client.MetaobjectFieldDefinitionCreateInput {
	return client.MetaobjectFieldDefinitionCreateInput{
		Key: f.Key, Type: f.Type, Name: &f.Name, Description: &f.Description, Required: &f.Required, Validations: validationsInput(f.Validations),
	}
}

// validationsInput returns a non-nil slice so an update clears removed validations.
func validationsInput(v map[string]string) []client.MetafieldDefinitionValidationInput {
	out := []client.MetafieldDefinitionValidationInput{}
	for name, value := range v {
		out = append(out, client.MetafieldDefinitionValidationInput{Name: name, Value: value})
	}
	return out
}

func accessInput(ctx context.Context, o types.Object, diags *diag.Diagnostics) *client.MetaobjectAccessInput {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	var a accessModel
	diags.Append(o.As(ctx, &a, basetypes.ObjectAsOptions{})...)
	in := &client.MetaobjectAccessInput{}
	if s := patterns.ConfiguredString(a.Admin); s != nil {
		v := client.MetaobjectAdminAccessInput(*s)
		in.Admin = &v
	}
	if s := patterns.ConfiguredString(a.Storefront); s != nil {
		v := client.MetaobjectStorefrontAccess(*s)
		in.Storefront = &v
	}
	if s := patterns.ConfiguredString(a.CustomerAccount); s != nil {
		v := client.MetaobjectCustomerAccountAccess(*s)
		in.CustomerAccount = &v
	}
	return in
}

type capabilityInputs struct {
	Publishable  *client.MetaobjectCapabilityPublishableInput
	Translatable *client.MetaobjectCapabilityTranslatableInput
	Renderable   *client.MetaobjectCapabilityRenderableInput
}

func capabilitiesInput(ctx context.Context, o types.Object, diags *diag.Diagnostics) *capabilityInputs {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	var c capabilitiesModel
	diags.Append(o.As(ctx, &c, basetypes.ObjectAsOptions{})...)
	in := &capabilityInputs{}
	if b := patterns.ConfiguredBool(c.Publishable); b != nil {
		in.Publishable = &client.MetaobjectCapabilityPublishableInput{Enabled: *b}
	}
	if b := patterns.ConfiguredBool(c.Translatable); b != nil {
		in.Translatable = &client.MetaobjectCapabilityTranslatableInput{Enabled: *b}
	}
	if b := patterns.ConfiguredBool(c.Renderable); b != nil {
		in.Renderable = &client.MetaobjectCapabilityRenderableInput{Enabled: *b}
	}
	return in
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
