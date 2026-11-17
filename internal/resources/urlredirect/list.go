package urlredirect

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

// ListResource lists URL redirects; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

type listConfig struct {
	Query types.String `tfsdk:"query"`
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists the URL redirects of the store, 250 per request. Redirects whose stored path or target the resource's validators would reject are skipped and don't count toward the limit.",
		Attributes: map[string]listschema.Attribute{
			"query": listschema.StringAttribute{
				MarkdownDescription: "A Shopify search filter, such as `path:\"/old-page\"` or `target:\"/new-page\"`, to import a large set in parts.",
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
		page := func(after *string) ([]client.UrlRedirectFields, *string, error) {
			res, err := client.ListUrlRedirects(ctx, r.client, after, patterns.ConfiguredString(cfg.Query))
			if err != nil {
				return nil, nil, err
			}
			conn := res.UrlRedirects
			out := make([]client.UrlRedirectFields, len(conn.Nodes))
			for i := range conn.Nodes {
				out[i] = conn.Nodes[i].UrlRedirectFields
			}
			return out, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
		}
		err := patterns.PaginateCounting(ctx, req.Limit, page, func(rd client.UrlRedirectFields) (bool, bool) {
			if validatePath(rd.Path) != nil || validateTarget(rd.Target) != nil {
				return false, true
			}
			return true, push(listResult(ctx, req, &rd))
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list URL redirects", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

// listResult fills the resource as an import would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, rd *client.UrlRedirectFields) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = fmt.Sprintf("%s -> %s", rd.Path, rd.Target)
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(rd.Id)})...)
	if req.IncludeResource {
		m := model{AdoptExisting: types.BoolValue(false)}
		setFromAPI(&m, rd)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
