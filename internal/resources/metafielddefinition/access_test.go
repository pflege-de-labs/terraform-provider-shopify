package metafielddefinition

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func TestAdminAccess(t *testing.T) {
	for in, want := range map[client.MetafieldAdminAccess]types.String{
		client.MetafieldAdminAccessMerchantRead:      types.StringValue("MERCHANT_READ"),
		client.MetafieldAdminAccessMerchantReadWrite: types.StringValue("MERCHANT_READ_WRITE"),
		client.MetafieldAdminAccessPublicReadWrite:   types.StringNull(),
		client.MetafieldAdminAccessPublicRead:        types.StringNull(),
		client.MetafieldAdminAccessPrivate:           types.StringNull(),
	} {
		if got := adminAccess(&in); !got.Equal(want) {
			t.Errorf("%s: got %v, want %v", in, got, want)
		}
	}
	if got := adminAccess(nil); !got.IsNull() {
		t.Errorf("nil: got %v", got)
	}
}
