package discount

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/list"
)

var _ list.ListResourceWithConfigure = &FreeShippingListResource{}

func NewFreeShippingListResource() list.ListResource {
	return &FreeShippingListResource{FreeShippingResource{discountResource[freeShippingModel, *freeShippingModel]{method: methodFreeShipping, what: whatFreeShipping}}}
}

// FreeShippingListResource lists free shipping discounts; Metadata and Configure come from the embedded FreeShippingResource.
type FreeShippingListResource struct {
	FreeShippingResource
}

func (r *FreeShippingListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listSchema("free shipping")
}

func (r *FreeShippingListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	listDiscounts[freeShippingModel](ctx, r.client, methodFreeShipping, req, stream)
}
