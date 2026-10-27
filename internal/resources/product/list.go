package product

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
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
		err := patterns.Paginate(ctx, req.Limit, listPage(ctx, r.client), func(p client.ProductFields) bool {
			return push(listResult(ctx, req, &p))
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list products", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

func listPage(ctx context.Context, c *client.Client) func(after *string) ([]client.ProductFields, *string, error) {
	return func(after *string) ([]client.ProductFields, *string, error) {
		res, err := client.ListProducts(ctx, c, after)
		if err != nil {
			return nil, nil, err
		}
		conn := res.Products
		nodes := make([]client.ProductFields, len(conn.Nodes))
		for i, n := range conn.Nodes {
			nodes[i] = n.ProductFields
			if err := completeProduct(ctx, c, &nodes[i]); err != nil {
				return nil, nil, err
			}
		}
		return nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}

// listResult fills the resource as Read would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, p *client.ProductFields) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = displayName(p)
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(p.Id)})...)
	if req.IncludeResource {
		m, d := listModel(ctx, p)
		res.Diagnostics.Append(d...)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}

// listModel is what Read would set, minus options and variants of a product with a linked option, which can't be managed.
func listModel(ctx context.Context, p *client.ProductFields) (model, diag.Diagnostics) {
	m := model{AdoptExisting: types.BoolValue(false), AllowDataDeletion: types.BoolValue(false)}
	diags := setFromAPI(ctx, &m, p, nil)
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
