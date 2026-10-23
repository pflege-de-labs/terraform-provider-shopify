package metaobjectdefinition

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

// ListResource lists metaobject definitions; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists all metaobject definitions of the store.",
	}
}

func (r *ListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		page := func(after *string) ([]client.MetaobjectDefinitionFields, *string, error) {
			res, err := client.ListMetaobjectDefinitions(ctx, r.client, after)
			if err != nil {
				return nil, nil, err
			}
			conn := res.MetaobjectDefinitions
			return conn.Nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
		}
		err := patterns.Paginate(ctx, req.Limit, page, func(d client.MetaobjectDefinitionFields) bool {
			return push(listResult(ctx, req, &d))
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list "+what+"s", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

// listResult fills the resource as Read would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, d *client.MetaobjectDefinitionFields) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = d.Type
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(d.Id)})...)
	if req.IncludeResource {
		m := model{AllowDataDeletion: types.BoolValue(false)}
		res.Diagnostics.Append(setFromAPI(ctx, &m, d)...)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
