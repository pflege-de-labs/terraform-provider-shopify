// Package urlredirect implements the shopify_url_redirect resource.
package urlredirect

import (
	"context"
	"fmt"

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

const what = "URL redirect"

var (
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
	_ resource.ResourceWithIdentity    = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one online store URL redirect.
type Resource struct {
	client *client.Client
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_url_redirect"
	// Read re-adopts a redirect recreated outside Terraform by its path, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An online store URL redirect from a path to a target. " +
			"**A poor fit for Terraform:** redirects are also created by the admin (for example when a handle changes) and by apps, so what Terraform manages can drift often. " +
			"Path and target must be given in the canonical form Shopify stores, so nothing is rewritten after apply. " +
			"A path that already has a redirect needs `adopt_existing = true`; destroy then leaves it in place. " +
			"Import with the path, for example `/old-page`. A redirect holds no data that config can't restore, so there is no `allow_data_deletion`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "URL redirect GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"path": schema.StringAttribute{
				MarkdownDescription: "The path to redirect from, unique per shop. Changing it renames the redirect in place. " +
					"Must start with `/`, be lower case, have no trailing `/` and no `#fragment`, and have whitespace, non-ASCII characters and a literal `%` percent-encoded in lower case (`%20`, `%c3%a9`, `%25`). A query string is allowed, in lower case.",
				Required:   true,
				Validators: []validator.String{pathValidator()},
			},
			"target": schema.StringAttribute{
				MarkdownDescription: "Where to redirect to: a path starting with `/`, or an absolute URL such as `https://example.com/x` with a lower-case scheme and host and a `/` after the host. " +
					"Whitespace and non-ASCII characters must be percent-encoded. Shopify refuses a target equal to the path.",
				Required:   true,
				Validators: []validator.String{targetValidator()},
			},
			"adopt_existing": patterns.AdoptExistingAttribute(what),
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
	if resp.Diagnostics.HasError() {
		return
	}

	existing, err := r.getByPath(ctx, plan.Path.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
		return
	}
	if existing != nil && !plan.AdoptExisting.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("path"), "Redirect already exists",
			"A redirect from this path exists. Set adopt_existing = true to take it over, or import it.")
		return
	}
	var rd *client.UrlRedirectFields
	if existing != nil {
		// Only a differing target is written, so a matching redirect stays untouched.
		if rd = existing; existing.Target != plan.Target.ValueString() {
			rd = r.update(ctx, existing.Id, plan.input(), &resp.Diagnostics)
		}
	} else {
		rd = r.create(ctx, plan.input(), &resp.Diagnostics)
	}
	if rd == nil {
		return
	}
	setFromAPI(&plan, rd)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GID first; fall back to the path so a redirect recreated in the admin is found again.
	var rd *client.UrlRedirectFields
	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		var err error
		if rd, err = r.getByID(ctx, state.ID.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if rd == nil && !state.Path.IsNull() {
		var err error
		if rd, err = r.getByPath(ctx, state.Path.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if rd == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	if state.AdoptExisting.IsNull() {
		state.AdoptExisting = types.BoolValue(false)
	}
	setFromAPI(&state, rd)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Path.Equal(state.Path) && plan.Target.Equal(state.Target) {
		// Only adopt_existing changed.
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}
	// A taken path is an error from Shopify (UPDATE_FAILED on path), so no lookup is needed.
	rd := r.update(ctx, plan.ID.ValueString(), plan.input(), &resp.Diagnostics)
	if rd == nil {
		return
	}
	setFromAPI(&plan, rd)
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

	del, err := client.DeleteUrlRedirect(ctx, r.client, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	if del.UrlRedirectDelete == nil {
		resp.Diagnostics.AddError("Failed to delete "+what, "Shopify returned no result.")
		return
	}
	// Already gone is what delete wants.
	if goneOnDelete(del.UrlRedirectDelete.UserErrors) {
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiagsWith("Failed to delete "+what, del.UrlRedirectDelete.UserErrors, errorPath)...)
}

// ImportState takes the path from `terraform import`, or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		rd, err := r.getByPath(ctx, req.ID)
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+what, err.Error())
			return
		}
		if rd == nil {
			resp.Diagnostics.AddError("Redirect not found", fmt.Sprintf("No redirect has the path %q (Shopify stores paths lower case, without a trailing slash).", req.ID))
			return
		}
		// Read fills in the rest by GID.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), rd.Id)...)
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

func (r *Resource) create(ctx context.Context, in client.UrlRedirectInput, diags *diag.Diagnostics) *client.UrlRedirectFields {
	res, err := client.CreateUrlRedirect(ctx, r.client, in)
	if err != nil {
		diags.AddError("Failed to create "+what, err.Error())
		return nil
	}
	if res.UrlRedirectCreate == nil {
		diags.AddError("Failed to create "+what, "Shopify returned no result.")
		return nil
	}
	diags.Append(client.UserErrorDiagsWith("Failed to create "+what, res.UrlRedirectCreate.UserErrors, errorPath)...)
	if diags.HasError() {
		return nil
	}
	if res.UrlRedirectCreate.UrlRedirect == nil {
		diags.AddError("Failed to create "+what, "Shopify returned no redirect.")
		return nil
	}
	return &res.UrlRedirectCreate.UrlRedirect.UrlRedirectFields
}

func (r *Resource) update(ctx context.Context, id string, in client.UrlRedirectInput, diags *diag.Diagnostics) *client.UrlRedirectFields {
	res, err := client.UpdateUrlRedirect(ctx, r.client, id, in)
	if err != nil {
		diags.AddError("Failed to update "+what, err.Error())
		return nil
	}
	if res.UrlRedirectUpdate == nil {
		diags.AddError("Failed to update "+what, "Shopify returned no result.")
		return nil
	}
	diags.Append(client.UserErrorDiagsWith("Failed to update "+what, res.UrlRedirectUpdate.UserErrors, errorPath)...)
	if diags.HasError() {
		return nil
	}
	if res.UrlRedirectUpdate.UrlRedirect == nil {
		diags.AddError("Failed to update "+what, "Shopify returned no redirect.")
		return nil
	}
	return &res.UrlRedirectUpdate.UrlRedirect.UrlRedirectFields
}

// getByID returns the redirect, or nil when it doesn't exist.
func (r *Resource) getByID(ctx context.Context, id string) (*client.UrlRedirectFields, error) {
	res, err := client.GetUrlRedirect(ctx, r.client, id)
	if err != nil || res.UrlRedirect == nil {
		return nil, err
	}
	return &res.UrlRedirect.UrlRedirectFields, nil
}

// getByPath returns the redirect, or nil when none has the path. There is no lookup by path, so this is a search.
func (r *Resource) getByPath(ctx context.Context, p string) (*client.UrlRedirectFields, error) {
	var found *client.UrlRedirectFields
	q := searchQuery(p)
	page := func(after *string) ([]client.UrlRedirectFields, *string, error) {
		res, err := client.ListUrlRedirects(ctx, r.client, after, &q)
		if err != nil {
			return nil, nil, err
		}
		if found = matchPath(res.UrlRedirects.Nodes, p); found != nil {
			return []client.UrlRedirectFields{*found}, nil, nil
		}
		return nil, patterns.NextCursor(res.UrlRedirects.PageInfo.HasNextPage, res.UrlRedirects.PageInfo.EndCursor), nil
	}
	err := patterns.Paginate(ctx, 0, page, func(client.UrlRedirectFields) bool { return false })
	return found, err
}
