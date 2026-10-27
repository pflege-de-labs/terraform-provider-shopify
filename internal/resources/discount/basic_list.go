package discount

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

var _ list.ListResourceWithConfigure = &BasicListResource{}

func NewBasicListResource() list.ListResource {
	return &BasicListResource{}
}

// BasicListResource lists basic discounts; Metadata and Configure come from the embedded BasicResource.
type BasicListResource struct {
	BasicResource
}

func (r *BasicListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists the basic (amount-off) discounts of the store, code and automatic. Other methods and app discounts are left out. " +
			"Discounts this resource can't hold (more than 250 selected products, variants, collections or markets, or a code discount without a code) are skipped with a warning and don't count toward the limit. " +
			"Generated config holds product, collection, segment and market GIDs, which tie it to the store: replace them with references.",
	}
}

func (r *BasicListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		err := patterns.PaginateCounting(ctx, req.Limit, listPage(ctx, r.client, nil), func(raw client.DiscountNodeFields) (bool, bool) {
			n, err := nodeOf(&raw)
			if err != nil {
				return false, push(skipped(raw.Id, err.Error()))
			}
			if n.Method != methodBasic {
				return false, true
			}
			res, ok := basicListResult(ctx, req, n)
			if !ok {
				return false, push(res)
			}
			return true, push(res)
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list discounts", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

func skipped(name, detail string) list.ListResult {
	var diags diag.Diagnostics
	diags.AddWarning("Skipping discount "+name, detail)
	return list.ListResult{Diagnostics: diags}
}

// basicListResult fills the resource as an import would; it returns a skip warning instead when the discount can't be held.
func basicListResult(ctx context.Context, req list.ListRequest, n *node) (list.ListResult, bool) {
	m := basicModel{commonModel: commonModel{AdoptExisting: types.BoolValue(false)}}
	unsupported, d := m.setFromAPI(ctx, n)
	if d.HasError() {
		var details []string
		for _, e := range d.Errors() {
			details = append(details, e.Detail())
		}
		return skipped(displayName(n), strings.Join(details, " ")), false
	}
	res := req.NewListResult(ctx)
	res.DisplayName = displayName(n)
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(n.ID)})...)
	if req.IncludeResource {
		if unsupported != "" {
			res.Diagnostics.AddWarning("Context of "+displayName(n)+" is not managed", unsupportedContextDetail(unsupported))
		}
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res, true
}
