package patterns

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// IDIdentity is the resource identity model: the Shopify GID.
type IDIdentity struct {
	ID types.String `tfsdk:"id"`
}

// IDIdentitySchema is the shared identity schema for every resource.
func IDIdentitySchema() identityschema.Schema {
	return identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{RequiredForImport: true},
		},
	}
}

// Paginate pulls pages until limit (0 = none), push returns false, or there is no next cursor.
func Paginate[T any](ctx context.Context, limit int64, page func(after *string) ([]T, *string, error), push func(T) bool) error {
	var after *string
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, next, err := page(after)
		if err != nil {
			return err
		}
		for _, it := range items {
			if !push(it) {
				return nil
			}
			n++
			if limit > 0 && n >= limit {
				return nil
			}
		}
		if next == nil {
			return nil
		}
		after = next
	}
}
