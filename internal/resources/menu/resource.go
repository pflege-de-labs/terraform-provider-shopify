// Package menu implements the shopify_menu resource.
package menu

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

const what = "menu"

var itemTypes = []string{"FRONTPAGE", "COLLECTION", "COLLECTIONS", "PRODUCT", "CATALOG", "PAGE", "BLOG", "ARTICLE", "SEARCH", "SHOP_POLICY", "HTTP", "METAOBJECT", "CUSTOMER_ACCOUNT_PAGE"}

var (
	_ resource.ResourceWithConfigure      = &Resource{}
	_ resource.ResourceWithImportState    = &Resource{}
	_ resource.ResourceWithIdentity       = &Resource{}
	_ resource.ResourceWithValidateConfig = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one navigation menu and its whole item tree.
type Resource struct {
	client *client.Client
}

type model struct {
	ID            types.String `tfsdk:"id"`
	Handle        types.String `tfsdk:"handle"`
	Title         types.String `tfsdk:"title"`
	IsDefault     types.Bool   `tfsdk:"is_default"`
	AdoptExisting types.Bool   `tfsdk:"adopt_existing"`
	Items         []level1     `tfsdk:"item"`
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_menu"
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	itemAttrs := func() map[string]schema.Attribute {
		return map[string]schema.Attribute{
			"title": schema.StringAttribute{MarkdownDescription: "Link text.", Required: true},
			"type": schema.StringAttribute{
				MarkdownDescription: "Item type, e.g. `COLLECTION`, `PAGE` or `HTTP`.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.OneOf(itemTypes...)},
			},
			"resource_id": schema.StringAttribute{MarkdownDescription: "GID of the linked resource, for resource types such as `COLLECTION` or `PAGE`.", Optional: true},
			"url":         schema.StringAttribute{MarkdownDescription: "Target URL, for `HTTP` items only.", Optional: true},
			"tags":        schema.ListAttribute{MarkdownDescription: "Tags to filter a linked collection by.", ElementType: types.StringType, Optional: true},
		}
	}
	level3 := schema.ListNestedBlock{
		MarkdownDescription: "Third-level items.",
		NestedObject:        schema.NestedBlockObject{Attributes: itemAttrs()},
	}
	level2 := schema.ListNestedBlock{
		MarkdownDescription: "Second-level items.",
		NestedObject:        schema.NestedBlockObject{Attributes: itemAttrs(), Blocks: map[string]schema.Block{"item": level3}},
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A navigation menu. Terraform owns the whole item tree: items added in the admin are removed on the next apply. " +
			"Import with the menu `handle`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Menu GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"handle": schema.StringAttribute{
				MarkdownDescription: "Menu handle, e.g. `main-menu`. Can't be changed for default menus.",
				Required:            true,
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "Menu title.",
				Required:            true,
			},
			"is_default": schema.BoolAttribute{
				MarkdownDescription: "Whether this is a default menu, which Shopify won't delete.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"adopt_existing": patterns.AdoptExistingAttribute(what),
		},
		Blocks: map[string]schema.Block{
			"item": schema.ListNestedBlock{
				MarkdownDescription: "Top-level items, in order.",
				NestedObject:        schema.NestedBlockObject{Attributes: itemAttrs(), Blocks: map[string]schema.Block{"item": level2}},
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
	check := func(p path.Path, f itemFields) {
		if f.Type.IsUnknown() || f.ResourceID.IsUnknown() || f.URL.IsUnknown() {
			return
		}
		if err := checkItem(f.Type.ValueString(), !f.ResourceID.IsNull(), !f.URL.IsNull()); err != nil {
			resp.Diagnostics.AddAttributeError(p, "Invalid menu item", err.Error())
		}
	}
	for i, a := range cfg.Items {
		pa := path.Root("item").AtListIndex(i)
		check(pa, a.itemFields)
		for j, b := range a.Items {
			pb := pa.AtName("item").AtListIndex(j)
			check(pb, b.itemFields)
			for k, c := range b.Items {
				check(pb.AtName("item").AtListIndex(k), c.itemFields)
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

	// Shopify suffixes a taken handle instead of failing, so existence is checked here and again after create
	// (the menus list lags a few seconds behind).
	existing, err := r.findByHandle(ctx, plan.Handle.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
		return
	}
	if existing != nil {
		if !plan.AdoptExisting.ValueBool() {
			resp.Diagnostics.AddAttributeError(path.Root("handle"), "Menu already exists",
				fmt.Sprintf("A menu with handle %q exists. Set adopt_existing = true to take it over, or import it.", plan.Handle.ValueString()))
			return
		}
		plan.ID = types.StringValue(existing.Id)
		r.update(ctx, &plan, &resp.Diagnostics)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
		return
	}

	res, err := client.CreateMenu(ctx, r.client, plan.Title.ValueString(), plan.Handle.ValueString(), createInputs(plan.Items))
	if err != nil {
		resp.Diagnostics.AddError("Failed to create "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to create "+what, res.MenuCreate.UserErrors)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if created := res.MenuCreate.Menu; created.Handle != plan.Handle.ValueString() {
		if _, err := client.DeleteMenu(ctx, r.client, created.Id); err != nil {
			resp.Diagnostics.AddError("Failed to remove duplicate "+what, fmt.Sprintf("Remove menu %q in the admin: %s", created.Handle, err))
		}
		resp.Diagnostics.AddAttributeError(path.Root("handle"), "Menu already exists",
			fmt.Sprintf("A menu with handle %q exists. Set adopt_existing = true to take it over, or import it.", plan.Handle.ValueString()))
		return
	}
	setFromAPI(&plan, &res.MenuCreate.Menu.MenuFields)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GID first; fall back to the handle so a menu recreated in the admin is found again.
	var menu *client.MenuFields
	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		res, err := client.GetMenu(ctx, r.client, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
		if res.Menu != nil {
			menu = &res.Menu.MenuFields
		}
	}
	if menu == nil {
		var err error
		if menu, err = r.findByHandle(ctx, state.Handle.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if menu == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	if state.AdoptExisting.IsNull() {
		state.AdoptExisting = types.BoolValue(false)
	}
	setFromAPI(&state, menu)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.update(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
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
	switch {
	case state.IsDefault.ValueBool():
		patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, what, "Shopify doesn't delete default menus")
		return
	case state.AdoptExisting.ValueBool():
		patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, what, "adopt_existing is true")
		return
	}

	res, err := client.DeleteMenu(ctx, r.client, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to delete "+what, res.MenuDelete.UserErrors)...)
}

// ImportState takes the handle from `terraform import`, or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("handle"), req.ID)...)
		return
	}
	var id patterns.IDIdentity
	resp.Diagnostics.Append(req.Identity.Get(ctx, &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Read looks up by GID first, so the handle is filled in there.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id.ID)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, id)...)
}

func (r *Resource) update(ctx context.Context, plan *model, diags *diag.Diagnostics) {
	// Default menu handles can't change, so the handle is only sent when it differs.
	var handle *string
	if existing, err := client.GetMenu(ctx, r.client, plan.ID.ValueString()); err != nil {
		diags.AddError("Failed to read "+what, err.Error())
		return
	} else if existing.Menu != nil && existing.Menu.Handle != plan.Handle.ValueString() {
		handle = plan.Handle.ValueStringPointer()
	}

	res, err := client.UpdateMenu(ctx, r.client, plan.ID.ValueString(), plan.Title.ValueString(), handle, updateInputs(plan.Items))
	if err != nil {
		diags.AddError("Failed to update "+what, err.Error())
		return
	}
	diags.Append(client.UserErrorDiags("Failed to update "+what, res.MenuUpdate.UserErrors)...)
	if diags.HasError() {
		return
	}
	setFromAPI(plan, &res.MenuUpdate.Menu.MenuFields)
}

func (r *Resource) findByHandle(ctx context.Context, handle string) (*client.MenuFields, error) {
	var after *string
	for {
		res, err := client.ListMenus(ctx, r.client, after)
		if err != nil {
			return nil, err
		}
		for _, m := range res.Menus.Nodes {
			if m.Handle == handle {
				return &m.MenuFields, nil
			}
		}
		if !res.Menus.PageInfo.HasNextPage {
			return nil, nil
		}
		after = res.Menus.PageInfo.EndCursor
	}
}

func setFromAPI(m *model, menu *client.MenuFields) {
	m.ID = types.StringValue(menu.Id)
	m.Handle = types.StringValue(menu.Handle)
	m.Title = types.StringValue(menu.Title)
	m.IsDefault = types.BoolValue(menu.IsDefault)
	m.Items = level1FromAPI(menu.Items)
}
