package location

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

// ListResource lists locations; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

type listConfig struct {
	IncludeInactive types.Bool `tfsdk:"include_inactive"`
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists the locations of the store. An inactive location can share its name with an active one; the ID tells them apart.",
		Attributes: map[string]listschema.Attribute{
			"include_inactive": listschema.BoolAttribute{
				MarkdownDescription: "Also list deactivated locations. Defaults to `false`.",
				Optional:            true,
			},
		},
	}
}

func (r *ListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	var cfg listConfig
	if diags := req.Config.Get(ctx, &cfg); diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		page := func(after *string) ([]client.LocationFields, *string, error) {
			res, err := client.ListLocations(ctx, r.client, cfg.IncludeInactive.ValueBool(), after)
			if err != nil {
				return nil, nil, err
			}
			conn := res.Locations
			nodes := make([]client.LocationFields, len(conn.Nodes))
			for i, n := range conn.Nodes {
				nodes[i] = n.LocationFields
			}
			return nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
		}
		err := patterns.Paginate(ctx, req.Limit, page, func(l client.LocationFields) bool {
			return push(listResult(ctx, req, &l))
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list "+what+"s", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

// listResult fills the resource as Read would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, l *client.LocationFields) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = l.Name
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(l.Id)})...)
	if req.IncludeResource {
		m := model{DeletionPolicy: types.StringValue("deactivate"), AllowDataDeletion: types.BoolValue(false)}
		res.Diagnostics.Append(setFromAPI(ctx, &m, l)...)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
