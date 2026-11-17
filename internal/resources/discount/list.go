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

// listSchema describes a list resource; what names the method in the text shared by all of them.
func listSchema(what string) listschema.Schema {
	return listschema.Schema{
		MarkdownDescription: "Lists the " + what + " discounts of the store, code and automatic. Other methods and app discounts are skipped with a warning naming their kind. " +
			"Discounts this resource can't hold (more than 250 markets, or a code discount without a code) are skipped with a warning and don't count toward the limit. " +
			"Generated config holds product, collection, segment and market GIDs, which tie it to the store: replace them with references.",
	}
}

// listDiscounts streams the discounts of one method; the others are skipped with a warning.
func listDiscounts[M any, P kindModel[M]](ctx context.Context, c *client.Client, method string, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		err := patterns.PaginateCounting(ctx, req.Limit, listPage(ctx, c, nil), func(raw client.DiscountNodeFields) (bool, bool) {
			res, counted := listResult[M, P](ctx, c, method, req, &raw)
			return counted, push(res)
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list discounts", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

// listResult maps one listed discount; a skip is a warning that doesn't count toward the limit.
func listResult[M any, P kindModel[M]](ctx context.Context, c *client.Client, method string, req list.ListRequest, raw *client.DiscountNodeFields) (list.ListResult, bool) {
	n, err := nodeOf(raw)
	if err != nil {
		return skipped(raw.Id, err.Error()), false
	}
	if err := checkMethod(n.Typename, method); err != nil {
		return skipped(displayName(n), err.Error()), false
	}
	if err := completeItems(ctx, c, n); err != nil {
		return skipped(displayName(n), err.Error()), false
	}
	return modelListResult[M, P](ctx, req, n)
}

func skipped(name, detail string) list.ListResult {
	var diags diag.Diagnostics
	diags.AddWarning("Skipping discount "+name, detail)
	return list.ListResult{Diagnostics: diags}
}

// modelListResult fills the resource as an import would; it returns a skip warning instead when the discount can't be held.
func modelListResult[M any, P kindModel[M]](ctx context.Context, req list.ListRequest, n *node) (list.ListResult, bool) {
	var m P = new(M)
	m.common().AdoptExisting = types.BoolValue(false)
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
		res.Diagnostics.Append(res.Resource.Set(ctx, m)...)
	}
	return res, true
}
