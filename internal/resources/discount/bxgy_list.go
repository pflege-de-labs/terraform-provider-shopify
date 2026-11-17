package discount

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/list"
)

var _ list.ListResourceWithConfigure = &BxgyListResource{}

func NewBxgyListResource() list.ListResource {
	return &BxgyListResource{BxgyResource{discountResource[bxgyModel, *bxgyModel]{method: methodBxgy, what: whatBxgy}}}
}

// BxgyListResource lists buy X get Y discounts; Metadata and Configure come from the embedded BxgyResource.
type BxgyListResource struct {
	BxgyResource
}

func (r *BxgyListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listSchema("buy X get Y")
}

func (r *BxgyListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	listDiscounts[bxgyModel](ctx, r.client, methodBxgy, req, stream)
}
