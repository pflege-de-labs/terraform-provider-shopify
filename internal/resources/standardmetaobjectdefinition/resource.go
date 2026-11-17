// Package standardmetaobjectdefinition implements the shopify_standard_metaobject_definition resource.
package standardmetaobjectdefinition

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const what = "standard metaobject definition"

var (
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
	_ resource.ResourceWithIdentity    = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource enables one of Shopify's standard metaobject definitions.
type Resource struct {
	client *client.Client
}

type model struct {
	ID   types.String `tfsdk:"id"`
	Type types.String `tfsdk:"type"`
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_standard_metaobject_definition"
	// Read re-adopts a definition disabled and re-enabled outside Terraform by its type, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Enables one of Shopify's standard metaobject definitions (a `shopify--…` type such as `shopify--material`) from its template. " +
			"Shopify sets the name, fields, access and capabilities; read them with the `shopify_metaobject_definition` data source. " +
			"A type that is already enabled is adopted as it is. " +
			"Destroy only removes the resource from state: deleting the definition would delete every entry in it, so turn it off in the admin if needed. " +
			"Import with the type, such as `shopify--material`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Metaobject definition GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Type of the standard template, such as `shopify--material`. Changing it enables the new type and leaves the old one enabled.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
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

// Create needs no adopt_existing: enabling is idempotent and returns the existing definition unchanged (checked on the dev store).
func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := client.EnableStandardMetaobjectDefinition(ctx, r.client, plan.Type.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to enable "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiagsWith("Failed to enable "+what, res.StandardMetaobjectDefinitionEnable.UserErrors, typePath)...)
	if resp.Diagnostics.HasError() {
		return
	}
	def := res.StandardMetaobjectDefinitionEnable.MetaobjectDefinition
	if def == nil {
		resp.Diagnostics.AddError("Failed to enable "+what, "Shopify returned no definition and no error.")
		return
	}
	setFromAPI(&plan, &def.MetaobjectDefinitionFields)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GID first; fall back to the type so a definition re-enabled outside Terraform is found again.
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
	if def == nil && !state.Type.IsNull() {
		res, err := client.GetMetaobjectDefinitionByType(ctx, r.client, state.Type.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
		if res.MetaobjectDefinitionByType != nil {
			def = &res.MetaobjectDefinitionByType.MetaobjectDefinitionFields
		}
	}
	if def == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	if def.StandardTemplate == nil {
		resp.Diagnostics.AddError("Not a standard metaobject definition",
			fmt.Sprintf("The metaobject definition %s (%s) has no standard template. Manage it with shopify_metaobject_definition instead.", def.Type, def.Id))
		return
	}
	setFromAPI(&state, def)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

// Update is never called with a change: type forces replacement and id is computed.
func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete leaves the definition in place: deleting it would delete every entry of the type.
func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, what, "deleting it would delete every entry of its type")
}

// ImportState takes the type from `terraform import` or the GID from an import block with identity; Read resolves the rest.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("type"), req.ID)...)
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

// typePath maps the enable mutation's ["type"] field, which FieldPath drops as the argument name.
func typePath(field []string) (path.Path, bool) {
	if len(field) == 1 && field[0] == "type" {
		return path.Root("type"), true
	}
	return path.Empty(), false
}

func setFromAPI(m *model, d *client.MetaobjectDefinitionFields) {
	m.ID = types.StringValue(d.Id)
	m.Type = types.StringValue(d.Type)
}
