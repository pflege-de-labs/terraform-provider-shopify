package discount

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/list"
)

var _ list.ListResourceWithConfigure = &BasicListResource{}

func NewBasicListResource() list.ListResource {
	return &BasicListResource{BasicResource{discountResource[basicModel, *basicModel]{method: methodBasic, what: whatBasic}}}
}

// BasicListResource lists basic discounts; Metadata and Configure come from the embedded BasicResource.
type BasicListResource struct {
	BasicResource
}

func (r *BasicListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listSchema("basic (amount-off)")
}

func (r *BasicListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	listDiscounts[basicModel](ctx, r.client, methodBasic, req, stream)
}
