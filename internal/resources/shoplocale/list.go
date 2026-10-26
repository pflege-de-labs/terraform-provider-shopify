package shoplocale

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

// ListResource lists shop locales; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists all locales enabled on the store, including the primary one.",
	}
}

func (r *ListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		ls, err := listAll(ctx, r.client)
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list shop locales", err.Error())
			push(list.ListResult{Diagnostics: diags})
			return
		}
		for i := range ls {
			if req.Limit > 0 && int64(i) >= req.Limit {
				return
			}
			if !push(listResult(ctx, req, &ls[i])) {
				return
			}
		}
	}
}

// listResult fills the resource as Read would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, l *client.ShopLocaleFields) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = displayName(l)
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(l.Locale)})...)
	if req.IncludeResource {
		m := model{AdoptExisting: types.BoolValue(false), AllowDataDeletion: types.BoolValue(false)}
		setFromAPI(&m, l)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
