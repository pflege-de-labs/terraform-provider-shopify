// Package shopmetafields implements shopify_shop_metafields: the shop's own metafield values (resource model §9w).
package shopmetafields

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/metafields"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const what = "shop metafields"

var (
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
	_ resource.ResourceWithIdentity    = &Resource{}
)

func New() resource.Resource { return &Resource{} }

// Resource manages the metafield values of the shop.
type Resource struct {
	client *client.Client
}

type model struct {
	ID         types.String `tfsdk:"id"`
	Metafields types.Map    `tfsdk:"metafields"`
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_shop_metafields"
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	mf := metafields.Attribute("shop")
	// An empty resource would manage nothing; import and the list fill the map ({} for a shop without values).
	mf.Optional, mf.Required = false, true
	resp.Schema = schema.Schema{
		MarkdownDescription: "The metafield values owned by the shop. There is one per store: Create adopts the shop and writes the listed keys. " +
			"**Destroy only removes the resource from state**; the values stay in Shopify. " +
			"Import with `default`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Shop GID: the shop's metafields have no ID of their own.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"metafields": mf,
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
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	r.client = c
}

// readShop returns the shop GID and all its metafields.
func readShop(ctx context.Context, c *client.Client) (string, []metafields.Live, error) {
	var id string
	live, err := metafields.List(ctx, func(after *string) ([]metafields.Live, *string, error) {
		res, err := client.GetShopMetafields(ctx, c, after)
		if err != nil {
			return nil, nil, err
		}
		id = res.Shop.Id
		conn := res.Shop.Metafields
		out := make([]metafields.Live, len(conn.Nodes))
		for i, n := range conn.Nodes {
			out[i] = metafields.Live{Namespace: n.Namespace, Key: n.Key, Type: n.Type, Value: n.Value}
		}
		return out, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	})
	return id, live, err
}

// Create and Update write from the config, not the plan, and store what the shop really has.
func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.apply(ctx, req.Config.Get, &resp.State, resp.Identity, &resp.Diagnostics)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.apply(ctx, req.Config.Get, &resp.State, resp.Identity, &resp.Diagnostics)
}

type stateSetter interface {
	Set(ctx context.Context, val any) diag.Diagnostics
}

func (r *Resource) apply(ctx context.Context, getConfig func(context.Context, any) diag.Diagnostics, state, identity stateSetter, diags *diag.Diagnostics) {
	var cfg model
	diags.Append(getConfig(ctx, &cfg)...)
	if diags.HasError() {
		return
	}
	want, d := metafields.FromValue(ctx, cfg.Metafields)
	diags.Append(d...)
	id, live, err := readShop(ctx, r.client)
	if err != nil || diags.HasError() {
		if err != nil {
			diags.AddError("Failed to read "+what, err.Error())
		}
		return
	}
	d = metafields.Apply(ctx, r.client, id, want, metafields.Read(want, live))
	diags.Append(d...)
	// Even after a failed write the state records what the shop has now.
	if _, live, err = readShop(ctx, r.client); err != nil {
		diags.AddError("Failed to read "+what, err.Error())
		return
	}
	m := model{ID: types.StringValue(id), Metafields: cfg.Metafields}
	// After success state keeps the written values, as products do; a rewrite Shopify made then shows on the next plan, not as an apply error.
	if diags.HasError() {
		m.Metafields, d = metafields.ToValue(ctx, metafields.Read(want, live), false)
		diags.Append(d...)
	}
	diags.Append(state.Set(ctx, &m)...)
	diags.Append(identity.Set(ctx, patterns.IDIdentity{ID: m.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// A null map means import: take every key.
	prior, d := metafields.FromValue(ctx, m.Metafields)
	resp.Diagnostics.Append(d...)
	id, live, err := readShop(ctx, r.client)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read "+what, err.Error())
		return
	}
	m.ID = types.StringValue(id)
	m.Metafields, d = metafields.ToValue(ctx, metafields.Read(prior, live), false)
	resp.Diagnostics.Append(d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: m.ID})...)
}

// Delete keeps the values: they may be what apps and merchants rely on.
func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, what, "the metafield values stay in Shopify")
}

// ImportState takes "default" from `terraform import`, or the shop GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		if req.ID != "default" {
			resp.Diagnostics.AddError("Invalid import key", `The shop has one set of metafields; import it with "default".`)
			return
		}
		id, _, err := readShop(ctx, r.client)
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+what, err.Error())
			return
		}
		// Read fills in the metafields.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
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
