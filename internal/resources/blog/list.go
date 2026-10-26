package blog

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

var _ list.ListResourceWithConfigure = &ListResource{}

func NewListResource() list.ListResource {
	return &ListResource{}
}

// ListResource lists blogs; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists all blogs of the store, `news` included.",
	}
}

func (r *ListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		err := patterns.Paginate(ctx, req.Limit, listBlog(ctx, r.client), func(p *client.BlogFields) bool {
			return push(listResult(ctx, req, p))
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list blogs", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

func listBlog(ctx context.Context, c *client.Client) func(after *string) ([]*client.BlogFields, *string, error) {
	return func(after *string) ([]*client.BlogFields, *string, error) {
		res, err := client.ListBlogs(ctx, c, after)
		if err != nil {
			return nil, nil, err
		}
		conn := res.Blogs
		nodes := make([]*client.BlogFields, len(conn.Nodes))
		for i := range conn.Nodes {
			nodes[i] = &conn.Nodes[i].BlogFields
		}
		return nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}

// listResult fills the resource as an import would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, p *client.BlogFields) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = fmt.Sprintf("%s (%s)", p.Title, p.Handle)
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(p.Id)})...)
	if req.IncludeResource {
		m := model{AdoptExisting: types.BoolValue(false), AllowDataDeletion: types.BoolValue(false)}
		setFromAPI(&m, p)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
