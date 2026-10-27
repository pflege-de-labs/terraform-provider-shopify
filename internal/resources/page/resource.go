// Package page implements the shopify_page resource.
package page

import (
	"context"
	"fmt"

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

const what = "page"

var (
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
	_ resource.ResourceWithIdentity    = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one online store page.
type Resource struct {
	client *client.Client
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_page"
	// Read re-adopts a page recreated outside Terraform by its handle, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	unmanaged := " Unset means unmanaged: the value set in the admin is kept."
	resp.Schema = schema.Schema{
		MarkdownDescription: "An online store page: handle, title, body and template. " +
			"**A poor fit for Terraform unless you bootstrap a store or keep a few pages in a repository:** page texts (imprint, about us, landing pages) are usually maintained in the admin by people outside the infrastructure team, " +
			"and every admin edit to a managed attribute is reverted on the next apply. Leave `body` unset and Terraform owns the handle, title and template while editors own the text. " +
			"Pages are visible on the online store only; `published` is the switch. Scheduled publishing (`publishDate`), metafield values and URL redirects are not managed: " +
			"a scheduled page reads as `published = false`, and a config with `published = true` publishes it at the next apply. " +
			"Destroying a page deletes it with its metafield values and translations, and menu items pointing at it may lose their target. " +
			"A page that already has the handle, such as `contact` which every store has, needs `adopt_existing = true`; destroy then leaves it in place. " +
			"Import with the handle.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Page GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"handle": schema.StringAttribute{
				MarkdownDescription: "The page's URL handle, unique per shop. Changing it renames the page in place and creates no URL redirect. Lower case, no whitespace: Shopify lower-cases anything else.",
				Required:            true,
				Validators:          []validator.String{patterns.HandleValidator()},
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "The page title.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"body": schema.StringAttribute{
				MarkdownDescription: "The page content as HTML. Shopify repairs malformed HTML; the configured text is kept while Shopify still holds what it returned at the last apply, so the repair is no diff, but a later admin edit is." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"published": schema.BoolAttribute{
				MarkdownDescription: "Whether the page is visible on the online store. A new page is published when this is unset." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"template_suffix": schema.StringAttribute{
				MarkdownDescription: "Theme template suffix, such as `legal` for `page.legal.json`. Null and an empty string both mean the default template, and each reads back as given." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{patterns.UseStateForUnknownIncludingNull()},
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
	var plan, cfg model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// pageCreate also fails on a taken handle, but this says how to take the page over.
	// TODO(verify): whether the search index lags behind a page created seconds ago; the lookup would then miss it and pageCreate reports TAKEN.
	existing, err := r.getByHandle(ctx, plan.Handle.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
		return
	}
	if existing != nil && !plan.AdoptExisting.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("handle"), "Page already exists",
			"A page with this handle exists. Set adopt_existing = true to take it over, or import it.")
		return
	}
	var p *client.PageFields
	if existing != nil {
		// Only what differs is sent, so a page that already matches stays untouched.
		var live model
		setFromAPI(&live, existing, nil)
		in, has := cfg.updateInput(&live)
		if p = existing; has {
			p = r.update(ctx, existing.Id, in, &resp.Diagnostics)
		}
	} else {
		p = r.create(ctx, cfg.createInput(), &resp.Diagnostics)
	}
	if p == nil {
		return
	}
	setFromAPI(&plan, p, &p.Body)
	resp.Diagnostics.Append(patterns.RememberWrittenHTML(ctx, resp.Private, p.Body)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GID first; fall back to the handle so a page recreated in the admin is found again.
	var p *client.PageFields
	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		var err error
		if p, err = r.getByID(ctx, state.ID.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if p == nil && !state.Handle.IsNull() {
		var err error
		if p, err = r.getByHandle(ctx, state.Handle.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if p == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	// Set on import, where only the ID is known.
	if state.AdoptExisting.IsNull() {
		state.AdoptExisting = types.BoolValue(false)
	}
	written, d := patterns.WrittenHTML(ctx, req.Private)
	resp.Diagnostics.Append(d...)
	setFromAPI(&state, p, written)
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
	in, has := cfg.updateInput(&state)
	if !has {
		// Only adopt_existing changed.
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}
	p := r.update(ctx, plan.ID.ValueString(), in, &resp.Diagnostics)
	if p == nil {
		return
	}
	setFromAPI(&plan, p, &p.Body)
	resp.Diagnostics.Append(patterns.RememberWrittenHTML(ctx, resp.Private, p.Body)...)
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
	del, err := client.DeletePage(ctx, r.client, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	// Already gone is what delete wants.
	if goneOnDelete(del.PageDelete.UserErrors) {
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiagsWith("Failed to delete "+what, del.PageDelete.UserErrors, errorPath)...)
}

// ImportState takes the handle from `terraform import`, or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		p, err := r.getByHandle(ctx, req.ID)
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+what, err.Error())
			return
		}
		if p == nil {
			resp.Diagnostics.AddError("Page not found", fmt.Sprintf("No page has the handle %q.", req.ID))
			return
		}
		// Read fills in the rest by GID.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), p.Id)...)
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

func (r *Resource) create(ctx context.Context, in client.PageCreateInput, diags *diag.Diagnostics) *client.PageFields {
	res, err := client.CreatePage(ctx, r.client, in)
	if err != nil {
		diags.AddError("Failed to create "+what, err.Error())
		return nil
	}
	diags.Append(client.UserErrorDiagsWith("Failed to create "+what, res.PageCreate.UserErrors, errorPath)...)
	if diags.HasError() {
		return nil
	}
	if res.PageCreate.Page == nil {
		diags.AddError("Failed to create "+what, "Shopify returned no page.")
		return nil
	}
	return &res.PageCreate.Page.PageFields
}

func (r *Resource) update(ctx context.Context, id string, in client.PageUpdateInput, diags *diag.Diagnostics) *client.PageFields {
	res, err := client.UpdatePage(ctx, r.client, id, in)
	if err != nil {
		diags.AddError("Failed to update "+what, err.Error())
		return nil
	}
	diags.Append(client.UserErrorDiagsWith("Failed to update "+what, res.PageUpdate.UserErrors, errorPath)...)
	if diags.HasError() {
		return nil
	}
	if res.PageUpdate.Page == nil {
		diags.AddError("Failed to update "+what, "Shopify returned no page.")
		return nil
	}
	return &res.PageUpdate.Page.PageFields
}

// getByID returns the page, or nil when it doesn't exist.
func (r *Resource) getByID(ctx context.Context, id string) (*client.PageFields, error) {
	res, err := client.GetPage(ctx, r.client, id)
	if err != nil || res.Page == nil {
		return nil, err
	}
	return &res.Page.PageFields, nil
}

// getByHandle returns the page, or nil when no page has the handle. Pages have no handle lookup, so this is a search.
func (r *Resource) getByHandle(ctx context.Context, handle string) (*client.PageFields, error) {
	res, err := client.FindPages(ctx, r.client, handleQuery(handle))
	if err != nil {
		return nil, err
	}
	return matchHandle(res.Pages.Nodes, handle), nil
}
