package webpresence

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

// ListResource lists web presences; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists all web presences of the store, including the primary-domain one.",
	}
}

func (r *ListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		err := patterns.Paginate(ctx, req.Limit, listPage(ctx, r.client), func(p client.WebPresenceFields) bool {
			return push(listResult(ctx, req, &p))
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list web presences", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

func listPage(ctx context.Context, c *client.Client) func(after *string) ([]client.WebPresenceFields, *string, error) {
	return func(after *string) ([]client.WebPresenceFields, *string, error) {
		res, err := client.ListWebPresences(ctx, c, after)
		if err != nil {
			return nil, nil, err
		}
		conn := res.WebPresences
		nodes := make([]client.WebPresenceFields, len(conn.Nodes))
		for i, n := range conn.Nodes {
			nodes[i] = n.WebPresenceFields
		}
		return nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}

// listResult fills the resource as Read would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, p *client.WebPresenceFields) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = displayName(p)
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(p.Id)})...)
	if req.IncludeResource {
		m := model{AdoptExisting: types.BoolValue(false)}
		res.Diagnostics.Append(setFromAPI(ctx, &m, p)...)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}

func displayName(p *client.WebPresenceFields) string {
	if p.SubfolderSuffix != nil {
		return *p.SubfolderSuffix
	}
	if p.Domain != nil {
		return p.Domain.Host
	}
	return p.Id
}
