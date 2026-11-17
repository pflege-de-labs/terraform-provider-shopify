// Package metaobject implements the shopify_metaobject resource.
package metaobject

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const what = "metaobject"

var (
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
	_ resource.ResourceWithIdentity    = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one metaobject entry.
type Resource struct {
	client *client.Client
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metaobject"
	// Read re-adopts an entry recreated outside Terraform by type and handle, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One metaobject entry, in a merchant type, a Shopify standard type (`shopify--…`, enabled with `shopify_standard_metaobject_definition`) or the provider's own app type. " +
			"**A poor fit for Terraform unless the entries are maintained in the repository:** entries are content that people often edit in the admin. " +
			"Only the field keys in `fields` are managed; other fields keep their values. " +
			"Destroying an entry breaks every reference to it (metafields, other entries, collection conditions), so it needs `allow_data_deletion = true` in state first. " +
			"An entry that already has the handle needs `adopt_existing = true`; destroy then leaves it in place. " +
			"Import with `<type>/<handle>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Metaobject GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Definition type, such as `shopify--material` or a merchant type. A type of the provider's own app can be given as `$app:<name>`. An entry can't change type, so changing it replaces the entry.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"handle": schema.StringAttribute{
				MarkdownDescription: "The entry's handle, unique per type. Changing it renames the entry in place and creates no redirect. " +
					"Shopify gives a taken handle a suffix (`baumwolle-1`) on create instead of failing; the provider checks for that and fails instead.",
				Required:   true,
				Validators: []validator.String{patterns.HandleValidator()},
			},
			"fields": schema.MapAttribute{
				MarkdownDescription: "Field key to value, in Shopify's string form: text as is, references as GIDs, numbers and booleans as strings (`\"7\"`, `\"true\"`), list and JSON types as a JSON string (use `jsonencode()`). " +
					"Values Shopify only reformats (`\"007\"`, minified JSON) don't show as a change. `\"\"` clears a value. " +
					"Removing a key stops managing that field and keeps its value. " +
					"References are GIDs of this store: config generated from another store needs them replaced by resource or data source references.",
				ElementType: types.StringType,
				Required:    true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "`ACTIVE` or `DRAFT`; only for types with the publishable capability, null otherwise. A new entry is `DRAFT` when this is unset. Unset means unmanaged: the status set in the admin is kept.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{patterns.UseStateForUnknownIncludingNull()},
				Validators:          []validator.String{stringvalidator.OneOf(patterns.EnumNames(client.AllMetaobjectStatus)...)},
			},
			"display_name": schema.StringAttribute{
				MarkdownDescription: "The entry's label: the display-name field's value, or Shopify's fallback.",
				Computed:            true,
			},
			"adopt_existing":      patterns.AdoptExistingAttribute(what),
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

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	want, diags := plan.fields(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	typ, handle := plan.Type.ValueString(), plan.Handle.ValueString()

	// Shopify suffixes a taken handle instead of failing, so existence is checked here and again after create.
	existing, err := r.getByHandle(ctx, typ, handle)
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
		return
	}
	if existing != nil && !plan.AdoptExisting.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("handle"), "Metaobject already exists",
			fmt.Sprintf("An entry %s/%s exists. Set adopt_existing = true to take it over, or import it.", typ, handle))
		return
	}
	var o *client.MetaobjectFields
	if existing != nil {
		// Only what differs is sent, so an entry that already matches stays untouched.
		in := client.MetaobjectUpdateInput{Fields: changedFields(want, readFields(want, existing.Fields)), Capabilities: status(plan.Status, statusValue(existing))}
		if o = existing; in.Fields != nil || in.Capabilities != nil {
			o = r.update(ctx, existing.Id, in, &resp.Diagnostics)
		}
	} else if o = r.create(ctx, plan, want, &resp.Diagnostics); o != nil {
		if err := patterns.KeyDrift(what, "handle", handle, o.Handle); err != nil {
			r.removeDuplicate(ctx, o, typ, handle, &resp.Diagnostics)
			return
		}
	}
	if o == nil {
		return
	}
	// State keeps the planned values; Shopify's reformatted ones would be an inconsistent result.
	resp.Diagnostics.Append(plan.setFromAPI(o, want)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	prior, diags := state.fields(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GID first; fall back to type and handle so an entry recreated in the admin is found again.
	var o *client.MetaobjectFields
	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		res, err := client.GetMetaobject(ctx, r.client, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
		if res.Metaobject != nil {
			o = &res.Metaobject.MetaobjectFields
		}
	}
	if o == nil && !state.Type.IsNull() && !state.Handle.IsNull() {
		var err error
		if o, err = r.getByHandle(ctx, state.Type.ValueString(), state.Handle.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if o == nil {
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
	resp.Diagnostics.Append(state.setFromAPI(o, readFields(prior, o.Fields))...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	want, diags := plan.fields(ctx)
	resp.Diagnostics.Append(diags...)
	have, diags := state.fields(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.MetaobjectUpdateInput{Fields: changedFields(want, have), Capabilities: status(plan.Status, state.Status)}
	if !plan.Handle.Equal(state.Handle) {
		in.Handle = plan.Handle.ValueStringPointer()
	}
	if in.Handle == nil && in.Fields == nil && in.Capabilities == nil {
		// Only adopt_existing or allow_data_deletion changed; display_name is unknown in the plan.
		plan.DisplayName = state.DisplayName
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}
	o := r.update(ctx, plan.ID.ValueString(), in, &resp.Diagnostics)
	if o == nil {
		return
	}
	resp.Diagnostics.Append(plan.setFromAPI(o, want)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.AdoptExisting.ValueBool() {
		patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, what, "adopt_existing is true")
		return
	}
	// Deleting breaks every reference to the entry, used or not.
	if guard := patterns.GuardDataDeletion(state.AllowDataDeletion, what); guard.HasError() {
		resp.Diagnostics.Append(guard...)
		return
	}
	res, err := client.DeleteMetaobject(ctx, r.client, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	if res.MetaobjectDelete == nil {
		resp.Diagnostics.AddError("Failed to delete "+what, "Shopify returned no result.")
		return
	}
	// Already gone is what delete wants.
	if goneOnDelete(res.MetaobjectDelete.UserErrors) {
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to delete "+what, res.MetaobjectDelete.UserErrors)...)
}

// ImportState takes `<type>/<handle>` from `terraform import`, or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		parts, err := patterns.SplitImportKey(req.ID, "<type>/<handle>")
		if err != nil {
			resp.Diagnostics.AddError("Invalid import key", err.Error())
			return
		}
		// Read finds the entry by type and handle and fills in the rest.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("type"), parts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("handle"), parts[1])...)
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

func (r *Resource) create(ctx context.Context, plan model, fields map[string]string, diags *diag.Diagnostics) *client.MetaobjectFields {
	handle := plan.Handle.ValueString()
	res, err := client.CreateMetaobject(ctx, r.client, client.MetaobjectCreateInput{
		Type:         plan.Type.ValueString(),
		Handle:       &handle,
		Fields:       changedFields(fields, nil),
		Capabilities: status(plan.Status, types.StringNull()),
	})
	if err != nil {
		diags.AddError("Failed to create "+what, err.Error())
		return nil
	}
	if res.MetaobjectCreate == nil {
		diags.AddError("Failed to create "+what, "Shopify returned no result.")
		return nil
	}
	diags.Append(client.UserErrorDiagsBy("Failed to create "+what, res.MetaobjectCreate.UserErrors, errorPath)...)
	if diags.HasError() {
		return nil
	}
	if res.MetaobjectCreate.Metaobject == nil {
		diags.AddError("Failed to create "+what, "Shopify returned no metaobject.")
		return nil
	}
	return &res.MetaobjectCreate.Metaobject.MetaobjectFields
}

func (r *Resource) update(ctx context.Context, id string, in client.MetaobjectUpdateInput, diags *diag.Diagnostics) *client.MetaobjectFields {
	res, err := client.UpdateMetaobject(ctx, r.client, id, in)
	if err != nil {
		diags.AddError("Failed to update "+what, err.Error())
		return nil
	}
	if res.MetaobjectUpdate == nil {
		diags.AddError("Failed to update "+what, "Shopify returned no result.")
		return nil
	}
	diags.Append(client.UserErrorDiagsBy("Failed to update "+what, res.MetaobjectUpdate.UserErrors, errorPath)...)
	if diags.HasError() {
		return nil
	}
	if res.MetaobjectUpdate.Metaobject == nil {
		diags.AddError("Failed to update "+what, "Shopify returned no metaobject.")
		return nil
	}
	return &res.MetaobjectUpdate.Metaobject.MetaobjectFields
}

// removeDuplicate deletes the suffixed entry a lost handle race created, and reports the clash.
func (r *Resource) removeDuplicate(ctx context.Context, created *client.MetaobjectFields, typ, want string, diags *diag.Diagnostics) {
	if res, err := client.DeleteMetaobject(ctx, r.client, created.Id); err != nil {
		diags.AddError("Failed to remove duplicate "+what, fmt.Sprintf("Remove entry %s/%s in the admin: %s", typ, created.Handle, err))
	} else if res.MetaobjectDelete == nil || len(res.MetaobjectDelete.UserErrors) > 0 {
		diags.AddError("Failed to remove duplicate "+what, fmt.Sprintf("Remove entry %s/%s in the admin.", typ, created.Handle))
	}
	diags.AddAttributeError(path.Root("handle"), "Metaobject already exists",
		fmt.Sprintf("An entry %s/%s exists. Set adopt_existing = true to take it over, or import it.", typ, want))
}

// getByHandle returns the entry, or nil when the type has no entry with the handle.
func (r *Resource) getByHandle(ctx context.Context, typ, handle string) (*client.MetaobjectFields, error) {
	res, err := client.GetMetaobjectByHandle(ctx, r.client, typ, handle)
	if err != nil || res.MetaobjectByHandle == nil {
		return nil, err
	}
	return &res.MetaobjectByHandle.MetaobjectFields, nil
}

// goneOnDelete reports whether metaobjectDelete failed only because the entry no longer exists.
func goneOnDelete(errs []client.DeleteMetaobjectMetaobjectDeleteMetaobjectDeletePayloadUserErrorsMetaobjectUserError) bool {
	for _, e := range errs {
		if e.Code == nil || *e.Code != client.MetaobjectUserErrorCodeRecordNotFound {
			return false
		}
	}
	return len(errs) > 0
}
