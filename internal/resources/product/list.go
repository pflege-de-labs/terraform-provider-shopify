package product

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/metafields"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/publishing"
)

var _ list.ListResourceWithConfigure = &ListResource{}

func NewListResource() list.ListResource {
	return &ListResource{}
}

// ListResource lists products; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists all products of the store, gift card products included. " +
			"Generated config carries every core attribute and one block per variant, so a large catalogue produces a long file. " +
			"A product with a linked option is listed with `options` and `variants` left out, so they stay unmanaged.",
	}
}

func (r *ListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		err := patterns.Paginate(ctx, req.Limit, listPage(ctx, r.client), func(p listed) bool {
			return push(listResult(ctx, req, &p.ProductFields, p.metafields, p.variantMetafields))
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list products", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

// listed is a product with its metafields, which come from their own query to keep the page query under the cost limit.
type listed struct {
	client.ProductFields
	metafields []metafields.Live
	// variantMetafields are by variant GID, read per variant for the same reason.
	variantMetafields map[string][]metafields.Live
}

func listPage(ctx context.Context, c *client.Client) func(after *string) ([]listed, *string, error) {
	return func(after *string) ([]listed, *string, error) {
		res, err := client.ListProducts(ctx, c, after)
		if err != nil {
			return nil, nil, err
		}
		conn := res.Products
		nodes := make([]listed, len(conn.Nodes))
		for i, n := range conn.Nodes {
			nodes[i].ProductFields = n.ProductFields
			if err := completeProduct(ctx, c, &nodes[i].ProductFields); err != nil {
				return nil, nil, err
			}
			var err error
			if nodes[i].metafields, err = metafields.List(ctx, metafieldPage(ctx, c, n.Id)); err != nil {
				return nil, nil, err
			}
			if linkedOptionError(&nodes[i].ProductFields) != nil {
				continue
			}
			nodes[i].variantMetafields = map[string][]metafields.Live{}
			for _, v := range nodes[i].Variants.Nodes {
				if nodes[i].variantMetafields[v.Id], err = metafields.List(ctx, variantMetafieldPage(ctx, c, v.Id)); err != nil {
					return nil, nil, err
				}
			}
		}
		return nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}

// listResult fills the resource as Read would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, p *client.ProductFields, mf []metafields.Live, vmf map[string][]metafields.Live) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = displayName(p)
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(p.Id)})...)
	if req.IncludeResource {
		m, d := listModel(ctx, p, mf, vmf)
		res.Diagnostics.Append(d...)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}

// listModel is what Read would set, minus options and variants of a product with a linked option, which can't be managed.
func listModel(ctx context.Context, p *client.ProductFields, mf []metafields.Live, vmf map[string][]metafields.Live) (model, diag.Diagnostics) {
	m := model{AdoptExisting: types.BoolValue(false), AllowDataDeletion: types.BoolValue(false)}
	diags := setFromAPI(ctx, &m, p, nil)
	var d diag.Diagnostics
	m.Metafields, d = metafields.ToValue(ctx, metafields.Read(nil, mf), true)
	diags.Append(d...)
	diags.Append(fillVariantMetafields(ctx, &m, p, true, func(id string) ([]metafields.Live, error) { return vmf[id], nil })...)
	m.PublicationIDs = publishing.NullIfEmpty(m.PublicationIDs)
	if len(m.MediaIDs.Elements()) == 0 {
		m.MediaIDs = types.ListNull(types.StringType)
	}
	if linkedOptionError(p) != nil {
		m.Options, m.Variants = types.ListNull(optionType), types.ListNull(variantType)
	}
	return m, diags
}

func displayName(p *client.ProductFields) string {
	return fmt.Sprintf("%s (%s)", p.Title, p.Handle)
}
