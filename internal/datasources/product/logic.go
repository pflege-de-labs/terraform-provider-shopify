// Package product implements the shopify_product data source.
package product

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type variant struct {
	ID, SKU, ProductID, Handle, Title string
}

type model struct {
	ID        types.String `tfsdk:"id"`
	SKU       types.String `tfsdk:"sku"`
	VariantID types.String `tfsdk:"variant_id"`
	Handle    types.String `tfsdk:"handle"`
	Title     types.String `tfsdk:"title"`
}

// skuQuery is the productVariants(query:) filter for a SKU; the caller still compares SKUs, since the search is fuzzy.
func skuQuery(sku string) string {
	return "sku:" + strconv.Quote(sku)
}

// matchSKU wants exactly one variant: a SKU shared by several variants can't name one product reliably.
func matchSKU(vs []variant, sku string) (*variant, error) {
	var hits []*variant
	for i := range vs {
		if vs[i].SKU == sku {
			hits = append(hits, &vs[i])
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return nil, fmt.Errorf("no product variant has SKU %q", sku)
	}
	list := make([]string, len(hits))
	for i, h := range hits {
		list[i] = fmt.Sprintf("%s (product %s)", h.ID, h.Handle)
	}
	return nil, fmt.Errorf("%d product variants have SKU %q: %s", len(hits), sku, strings.Join(list, ", "))
}

func toModel(v *variant) model {
	return model{
		ID:        types.StringValue(v.ProductID),
		SKU:       types.StringValue(v.SKU),
		VariantID: types.StringValue(v.ID),
		Handle:    types.StringValue(v.Handle),
		Title:     types.StringValue(v.Title),
	}
}
