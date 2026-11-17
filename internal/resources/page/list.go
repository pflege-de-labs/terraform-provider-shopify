package page

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
)

var _ list.ListResourceWithConfigure = &ListResource{}

func NewListResource() list.ListResource {
	return &ListResource{}
}

// ListResource lists pages; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists all pages of the store, `contact` included. Generated config carries the body, so page texts end up in the configuration.",
	}
}

func (r *ListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		err := patterns.Paginate(ctx, req.Limit, listPage(ctx, r.client), func(p listed) bool {
			return push(listResult(ctx, req, &p.PageFields, p.metafields))
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list pages", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

// listed is a page with its metafields, which come from their own query.
type listed struct {
	client.PageFields
	metafields []metafields.Live
}

func listPage(ctx context.Context, c *client.Client) func(after *string) ([]listed, *string, error) {
	return func(after *string) ([]listed, *string, error) {
		res, err := client.ListPages(ctx, c, after)
		if err != nil {
			return nil, nil, err
		}
		conn := res.Pages
		nodes := make([]listed, len(conn.Nodes))
		for i, n := range conn.Nodes {
			nodes[i].PageFields = n.PageFields
			if nodes[i].metafields, err = metafields.List(ctx, metafieldPage(ctx, c, n.Id)); err != nil {
				return nil, nil, err
			}
		}
		return nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}

// listResult fills the resource as an import would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, p *client.PageFields, mf []metafields.Live) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = fmt.Sprintf("%s (%s)", p.Title, p.Handle)
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(p.Id)})...)
	if req.IncludeResource {
		m := model{AdoptExisting: types.BoolValue(false)}
		setFromAPI(&m, p, nil)
		var d diag.Diagnostics
		m.Metafields, d = metafields.ToValue(ctx, metafields.Read(nil, mf), true)
		res.Diagnostics.Append(d...)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
