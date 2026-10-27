package collection

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

// ListResource lists collections; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists all collections of the store, `frontpage` included. " +
			"Generated config carries `product_ids` as product GIDs, which tie it to the store: replace them with `shopify_product` references. " +
			"A collection whose membership can't be managed is listed without `match_type`, `conditions` and `product_ids`, with a warning.",
	}
}

func (r *ListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		err := patterns.Paginate(ctx, req.Limit, listPage(ctx, r.client), func(c *liveCollection) bool {
			return push(listResult(ctx, req, c))
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list collections", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

func listPage(ctx context.Context, c *client.Client) func(after *string) ([]*liveCollection, *string, error) {
	return func(after *string) ([]*liveCollection, *string, error) {
		res, err := client.ListCollections(ctx, c, after)
		if err != nil {
			return nil, nil, err
		}
		conn := res.Collections
		nodes := make([]*liveCollection, len(conn.Nodes))
		for i := range conn.Nodes {
			if nodes[i], err = completeCollection(ctx, c, &conn.Nodes[i].CollectionFields); err != nil {
				return nil, nil, err
			}
		}
		return nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}

// listResult fills the resource as an import would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, c *liveCollection) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = fmt.Sprintf("%s (%s)", c.Title, c.Handle)
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(c.Id)})...)
	if req.IncludeResource {
		m := model{Conditions: conditionsNull(), AdoptExisting: types.BoolValue(false)}
		d, unsupported := setFromAPI(ctx, &m, c, nil, true)
		m.PublicationIDs = publishing.NullIfEmpty(m.PublicationIDs)
		res.Diagnostics.Append(d...)
		if unsupported != nil {
			res.Diagnostics.AddWarning(fmt.Sprintf("Membership of %s is not managed", c.Handle), unsupportedDetail(unsupported))
		}
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
