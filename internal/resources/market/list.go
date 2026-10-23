package market

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

// ListResource lists region markets; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists the region markets of the store. Markets without a region condition are skipped with a warning.",
	}
}

func (r *ListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		page := func(after *string) ([]client.MarketFields, *string, error) {
			res, err := client.ListMarkets(ctx, r.client, after)
			if err != nil {
				return nil, nil, err
			}
			conn := res.Markets
			nodes := make([]client.MarketFields, len(conn.Nodes))
			for i, n := range conn.Nodes {
				nodes[i] = n.MarketFields
			}
			return nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
		}
		err := patterns.Paginate(ctx, req.Limit, page, func(m client.MarketFields) bool {
			if !isRegionMarket(&m) {
				var diags diag.Diagnostics
				diags.AddWarning("Skipping market "+m.Handle, "The market has no region condition and can't be managed by shopify_market.")
				return push(list.ListResult{Diagnostics: diags})
			}
			return push(listResult(ctx, req, &m))
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list "+what+"s", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

// isRegionMarket is false for location, company-location and channel markets, which would fail the regions validator.
func isRegionMarket(m *client.MarketFields) bool {
	return m.Conditions != nil && m.Conditions.RegionsCondition != nil && len(m.Conditions.RegionsCondition.Regions.Nodes) > 0
}

func listResult(ctx context.Context, req list.ListRequest, mk *client.MarketFields) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = mk.Handle
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(mk.Id)})...)
	if req.IncludeResource {
		var m model
		res.Diagnostics.Append(setFromAPI(ctx, &m, mk)...)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
