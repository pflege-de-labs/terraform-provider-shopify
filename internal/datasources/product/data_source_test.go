package product_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	sdkacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const dsAddr = "data.shopify_product.test"

func product(handle, variants string) string {
	return fmt.Sprintf(`
resource "shopify_product" "test" {
  handle              = %q
  title               = "tf-acc sku lookup"
  status              = "DRAFT"
  allow_data_deletion = true
  options             = [{ name = "Size", values = ["S", "M"] }]
  variants            = %s
}
`, handle, variants)
}

// waitIndexed polls until the search finds sku, since Shopify indexes new variants a few seconds late.
func waitIndexed(t *testing.T, sku string, want int) {
	t.Helper()
	c := acctest.Client(t)
	for range 30 {
		res, err := client.FindProductVariantsBySku(context.Background(), c, `sku:"`+sku+`"`, nil)
		if err == nil && len(res.ProductVariants.Nodes) >= want {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("SKU %s not indexed after 60s", sku)
}

func TestAccProductDataSource_sku(t *testing.T) {
	suffix := strings.ToUpper(sdkacctest.RandStringFromCharSet(6, sdkacctest.CharSetAlphaNum))
	handle, sku, dup := "tf-acc-product-ds-"+strings.ToLower(suffix), "TF-DS-"+suffix, "TF-DS-DUP-"+suffix
	created := product(handle, fmt.Sprintf(`[{ option_values = { Size = "S" }, sku = %q }, { option_values = { Size = "M" }, sku = %q }]`, sku, sku+"-M"))
	lookup := func(s string) string { return fmt.Sprintf("data \"shopify_product\" \"test\" {\n  sku = %q\n}\n", s) }

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: created},
			{
				PreConfig: func() { waitIndexed(t, sku+"-M", 1) },
				Config:    created + lookup(sku),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(dsAddr, "id", "shopify_product.test", "id"),
					resource.TestCheckResourceAttrPair(dsAddr, "variant_id", "shopify_product.test", "variants.0.id"),
					resource.TestCheckResourceAttr(dsAddr, "handle", handle),
					resource.TestCheckResourceAttr(dsAddr, "title", "tf-acc sku lookup"),
				),
			},
			// The search for sku also returns sku-M; only the exact SKU may match.
			{Config: created + lookup("TF-DS-MISSING-"+suffix), ExpectError: regexp.MustCompile(`no product variant has SKU`)},
			{Config: product(handle, fmt.Sprintf(`[{ option_values = { Size = "S" }, sku = %q }, { option_values = { Size = "M" }, sku = %q }]`, dup, dup))},
			{
				PreConfig:   func() { waitIndexed(t, dup, 2) },
				Config:      product(handle, fmt.Sprintf(`[{ option_values = { Size = "S" }, sku = %q }, { option_values = { Size = "M" }, sku = %q }]`, dup, dup)) + lookup(dup),
				ExpectError: regexp.MustCompile(`2 product variants have SKU`),
			},
		},
	})
}
