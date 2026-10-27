package customersegment

import (
	"context"

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

// ListResource lists customer segments; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists all customer segments of the store, the five every store starts with included.",
	}
}

func (r *ListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		err := patterns.Paginate(ctx, req.Limit, listSegments(ctx, r.client), func(s *client.SegmentFields) bool {
			return push(listResult(ctx, req, s))
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list customer segments", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

func listSegments(ctx context.Context, c *client.Client) func(after *string) ([]*client.SegmentFields, *string, error) {
	return func(after *string) ([]*client.SegmentFields, *string, error) {
		res, err := client.ListSegments(ctx, c, after)
		if err != nil {
			return nil, nil, err
		}
		conn := res.Segments
		nodes := make([]*client.SegmentFields, len(conn.Nodes))
		for i := range conn.Nodes {
			nodes[i] = &conn.Nodes[i].SegmentFields
		}
		return nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}

// listResult fills the resource as an import would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, s *client.SegmentFields) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = s.Name
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(s.Id)})...)
	if req.IncludeResource {
		m := model{AdoptExisting: types.BoolValue(false)}
		setFromAPI(&m, s)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
