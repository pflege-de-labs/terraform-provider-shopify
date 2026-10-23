package deliveryprofile

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

var _ list.ListResourceWithConfigure = &ListResource{}

func NewListResource() list.ListResource {
	return &ListResource{}
}

// ListResource lists the general profile; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists the store's general delivery profile. Custom profiles are not supported yet.",
	}
}

func (r *ListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		err := r.eachProfile(ctx, 0, func(p profileNode) bool {
			if !p.Default {
				return true
			}
			// Exactly one general profile exists, so the limit never cuts it off.
			push(r.listResult(ctx, req, p.Id))
			return false
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list "+what+"s", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

func (r *ListResource) listResult(ctx context.Context, req list.ListRequest, id string) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = "default"
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(id)})...)
	if req.IncludeResource {
		m := model{ID: types.StringValue(id)}
		r.read(ctx, &m, &res.Diagnostics)
		if !res.Diagnostics.HasError() {
			res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
		}
	}
	return res
}
