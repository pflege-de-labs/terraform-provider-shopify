// Package blog implements the shopify_blog resource.
package blog

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

const what = "blog"

var (
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
	_ resource.ResourceWithIdentity    = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one online store blog.
type Resource struct {
	client *client.Client
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_blog"
	// Read re-adopts a blog recreated outside Terraform by its handle, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	unmanaged := " Unset means unmanaged: the value set in the admin is kept."
	resp.Schema = schema.Schema{
		MarkdownDescription: "An online store blog: the container for articles, with handle, title, comment policy and template. " +
			"**A good fit for Terraform:** these settings are stable configuration. The articles are content and are not managed by this resource. " +
			"Changing the handle changes the URL of every article in the blog, and no URL redirect is created. " +
			"Destroying a blog deletes its articles too, so a blog with articles needs `allow_data_deletion = true` in state first; an empty blog is deleted without it. " +
			"A blog that already has the handle, such as `news` which every store has, needs `adopt_existing = true`; destroy then leaves it in place. " +
			"Import with the handle.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Blog GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"handle": schema.StringAttribute{
				MarkdownDescription: "The blog's URL handle, unique per shop. Changing it renames the blog in place and creates no URL redirect. " +
					"Shopify gives a taken handle a suffix (`news-1`) instead of failing; the provider checks for that and fails instead. Lower case, no whitespace: Shopify lower-cases anything else.",
				Required:   true,
				Validators: []validator.String{patterns.HandleValidator()},
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "The blog title.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"comment_policy": schema.StringAttribute{
				MarkdownDescription: "Whether readers can comment, and whether comments are moderated: `AUTO_PUBLISHED`, `CLOSED` or `MODERATED`. A new blog is `CLOSED` when this is unset." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{stringvalidator.OneOf(patterns.EnumNames(client.AllCommentPolicy)...)},
			},
			"template_suffix": schema.StringAttribute{
				MarkdownDescription: "Theme template suffix, such as `ratgeber` for `blog.ratgeber.json`. Null and an empty string both mean the default template, and each reads back as given." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{patterns.UseStateForUnknownIncludingNull()},
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
	var plan, cfg model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Shopify suffixes a taken handle instead of failing, so existence is checked here and again after create.
	existing, err := r.getByHandle(ctx, plan.Handle.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
		return
	}
	if existing != nil && !plan.AdoptExisting.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("handle"), "Blog already exists",
			"A blog with this handle exists. Set adopt_existing = true to take it over, or import it.")
		return
	}
	var b *client.BlogFields
	if existing != nil {
		// Only what differs is sent, so a blog that already matches stays untouched.
		var live model
		setFromAPI(&live, existing)
		in, has := cfg.updateInput(&live)
		if b = existing; has {
			b = r.update(ctx, existing.Id, in, &resp.Diagnostics)
		}
	} else if b = r.create(ctx, cfg.createInput(), &resp.Diagnostics); b != nil {
		if err := patterns.KeyDrift(what, "handle", plan.Handle.ValueString(), b.Handle); err != nil {
			r.removeDuplicate(ctx, b, plan.Handle.ValueString(), &resp.Diagnostics)
			return
		}
	}
	if b == nil {
		return
	}
	setFromAPI(&plan, b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GID first; fall back to the handle so a blog recreated in the admin is found again.
	var b *client.BlogFields
	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		var err error
		if b, err = r.getByID(ctx, state.ID.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if b == nil && !state.Handle.IsNull() {
		var err error
		if b, err = r.getByHandle(ctx, state.Handle.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if b == nil {
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
	setFromAPI(&state, b)
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
		// Only adopt_existing or allow_data_deletion changed.
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	// blogUpdate suffixes a taken handle instead of failing, so a taken one is refused up front.
	if in.Handle != nil {
		other, err := r.getByHandle(ctx, *in.Handle)
		if err != nil {
			resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
			return
		}
		if other != nil && other.Id != plan.ID.ValueString() {
			resp.Diagnostics.AddAttributeError(path.Root("handle"), "Handle already taken",
				fmt.Sprintf("Another blog has the handle %q.", *in.Handle))
			return
		}
	}
	b := r.update(ctx, plan.ID.ValueString(), in, &resp.Diagnostics)
	if b == nil {
		return
	}
	// State takes the handle Shopify returned, so the next plan shows the drift.
	setFromAPI(&plan, b)
	if in.Handle != nil {
		if err := patterns.KeyDrift(what, "handle", *in.Handle, b.Handle); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("handle"), "Blog handle was changed by Shopify", err.Error())
		}
	}
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

	// Deleting a blog deletes its articles; an empty one holds only configuration.
	count, err := client.GetBlogArticlesCount(ctx, r.client, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read "+what, err.Error())
		return
	}
	if count.Blog == nil {
		return
	}
	if risk, detail := articlesAtRisk(count.Blog.ArticlesCount); risk {
		if guard := patterns.GuardDataDeletion(state.AllowDataDeletion, what); guard.HasError() {
			resp.Diagnostics.AddError("The blog may have articles", detail)
			resp.Diagnostics.Append(guard...)
			return
		}
	}

	del, err := client.DeleteBlog(ctx, r.client, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	if del.BlogDelete == nil {
		resp.Diagnostics.AddError("Failed to delete "+what, "Shopify returned no result.")
		return
	}
	// Already gone is what delete wants.
	if goneOnDelete(del.BlogDelete.UserErrors) {
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiagsWith("Failed to delete "+what, del.BlogDelete.UserErrors, errorPath)...)
}

// ImportState takes the handle from `terraform import`, or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		b, err := r.getByHandle(ctx, req.ID)
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+what, err.Error())
			return
		}
		if b == nil {
			resp.Diagnostics.AddError("Blog not found", fmt.Sprintf("No blog has the handle %q.", req.ID))
			return
		}
		// Read fills in the rest by GID.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), b.Id)...)
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

func (r *Resource) create(ctx context.Context, in client.BlogCreateInput, diags *diag.Diagnostics) *client.BlogFields {
	res, err := client.CreateBlog(ctx, r.client, in)
	if err != nil {
		diags.AddError("Failed to create "+what, err.Error())
		return nil
	}
	if res.BlogCreate == nil {
		diags.AddError("Failed to create "+what, "Shopify returned no result.")
		return nil
	}
	diags.Append(client.UserErrorDiagsWith("Failed to create "+what, res.BlogCreate.UserErrors, errorPath)...)
	if diags.HasError() {
		return nil
	}
	if res.BlogCreate.Blog == nil {
		diags.AddError("Failed to create "+what, "Shopify returned no blog.")
		return nil
	}
	return &res.BlogCreate.Blog.BlogFields
}

func (r *Resource) update(ctx context.Context, id string, in client.BlogUpdateInput, diags *diag.Diagnostics) *client.BlogFields {
	res, err := client.UpdateBlog(ctx, r.client, id, in)
	if err != nil {
		diags.AddError("Failed to update "+what, err.Error())
		return nil
	}
	if res.BlogUpdate == nil {
		diags.AddError("Failed to update "+what, "Shopify returned no result.")
		return nil
	}
	diags.Append(client.UserErrorDiagsWith("Failed to update "+what, res.BlogUpdate.UserErrors, errorPath)...)
	if diags.HasError() {
		return nil
	}
	if res.BlogUpdate.Blog == nil {
		diags.AddError("Failed to update "+what, "Shopify returned no blog.")
		return nil
	}
	return &res.BlogUpdate.Blog.BlogFields
}

// removeDuplicate deletes the suffixed blog a lost handle race created, and reports the clash.
func (r *Resource) removeDuplicate(ctx context.Context, created *client.BlogFields, want string, diags *diag.Diagnostics) {
	if res, err := client.DeleteBlog(ctx, r.client, created.Id); err != nil {
		diags.AddError("Failed to remove duplicate "+what, fmt.Sprintf("Remove blog %q in the admin: %s", created.Handle, err))
	} else if res.BlogDelete == nil || len(res.BlogDelete.UserErrors) > 0 {
		diags.AddError("Failed to remove duplicate "+what, fmt.Sprintf("Remove blog %q in the admin.", created.Handle))
	}
	diags.AddAttributeError(path.Root("handle"), "Blog already exists",
		fmt.Sprintf("A blog with handle %q exists. Set adopt_existing = true to take it over, or import it.", want))
}

// getByID returns the blog, or nil when it doesn't exist.
func (r *Resource) getByID(ctx context.Context, id string) (*client.BlogFields, error) {
	res, err := client.GetBlog(ctx, r.client, id)
	if err != nil || res.Blog == nil {
		return nil, err
	}
	return &res.Blog.BlogFields, nil
}

// getByHandle returns the blog, or nil when no blog has the handle. Blogs have no handle lookup, so this is a search.
func (r *Resource) getByHandle(ctx context.Context, handle string) (*client.BlogFields, error) {
	res, err := client.FindBlogs(ctx, r.client, handleQuery(handle))
	if err != nil {
		return nil, err
	}
	return matchHandle(res.Blogs.Nodes, handle), nil
}
