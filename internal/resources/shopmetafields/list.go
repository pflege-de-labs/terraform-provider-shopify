package shopmetafields

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/metafields"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

var _ list.ListResourceWithConfigure = &List{}

func NewList() list.ListResource { return &List{} }

// List lists the shop's one set of metafields; Metadata and Configure come from the embedded resource.
type List struct {
	Resource
}

func (r *List) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists the shop's metafields: always one result, holding every metafield outside app namespaces and the SEO ones.",
	}
}

func (r *List) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		id, live, err := readShop(ctx, r.client)
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list "+what, err.Error())
			push(list.ListResult{Diagnostics: diags})
			return
		}
		res := req.NewListResult(ctx)
		res.DisplayName = "Shop metafields"
		res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(id)})...)
		if req.IncludeResource {
			m := model{ID: types.StringValue(id)}
			var d diag.Diagnostics
			m.Metafields, d = metafields.ToValue(ctx, metafields.Read(nil, live), false)
			res.Diagnostics.Append(d...)
			res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
		}
		push(res)
	}
}
