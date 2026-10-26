package checkoutandaccountsconfiguration

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

// ListResource lists checkout and accounts configurations; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists every checkout and accounts configuration, published or not, with its live branding (write-only paths left out). " +
			"The published one's display name ends in ` (published)`.",
	}
}

func (r *ListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		err := patterns.Paginate(ctx, req.Limit, listPage(ctx, r.client, nil), func(c client.CheckoutAndAccountsConfigurationFields) bool {
			return push(listResult(ctx, req, &c))
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list checkout and accounts configurations", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

// listResult fills the resource as import would.
func listResult(ctx context.Context, req list.ListRequest, c *client.CheckoutAndAccountsConfigurationFields) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = c.Name
	if c.IsPublished {
		res.DisplayName += " (published)"
	}
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(c.Id)})...)
	if req.IncludeResource {
		m, err := importModel(c)
		if err != nil {
			res.Diagnostics.AddError("Failed to read branding", err.Error())
			return res
		}
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
