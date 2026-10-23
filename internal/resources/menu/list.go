package menu

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

// ListResource lists menus; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists all menus of the store, including the default ones.",
	}
}

func (r *ListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		page := func(after *string) ([]client.MenuFields, *string, error) {
			res, err := client.ListMenus(ctx, r.client, after)
			if err != nil {
				return nil, nil, err
			}
			conn := res.Menus
			nodes := make([]client.MenuFields, len(conn.Nodes))
			for i, n := range conn.Nodes {
				nodes[i] = n.MenuFields
			}
			return nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
		}
		err := patterns.Paginate(ctx, req.Limit, page, func(m client.MenuFields) bool {
			return push(listResult(ctx, req, &m))
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list "+what+"s", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

// listResult fills the resource as Read would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, menu *client.MenuFields) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = menu.Handle
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(menu.Id)})...)
	if req.IncludeResource {
		m := model{AdoptExisting: types.BoolValue(false)}
		setFromAPI(&m, menu)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
