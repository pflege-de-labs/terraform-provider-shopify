package fulfillmentservice_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

const dsAddr = "data.shopify_fulfillment_service.test"

// Read-only: `manual` exists on every store, so the tests create nothing.
func TestAccFulfillmentServiceDataSource_manual(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
data "shopify_fulfillment_service" "test" {
  handle = "manual"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(dsAddr, "id", regexp.MustCompile(`^gid://shopify/FulfillmentService/\w+$`)),
				resource.TestCheckResourceAttr(dsAddr, "handle", "manual"),
				resource.TestCheckResourceAttr(dsAddr, "type", "MANUAL"),
				resource.TestCheckResourceAttrSet(dsAddr, "service_name"),
				resource.TestMatchResourceAttr(dsAddr, "location_id", regexp.MustCompile(`^gid://shopify/Location/\d+$`)),
				resource.TestCheckResourceAttrSet(dsAddr, "inventory_management"),
				resource.TestCheckResourceAttrSet(dsAddr, "tracking_support"),
				resource.TestCheckResourceAttrSet(dsAddr, "requires_shipping_method"),
			),
		}},
	})
}

func TestAccFulfillmentServiceDataSource_notFound(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
data "shopify_fulfillment_service" "test" {
  handle = "tfacc-missing"
}
`,
			// Terraform wraps long error text, so match on whitespace; the list must include `manual`.
			ExpectError: regexp.MustCompile(`tfacc-missing[\s\S]*available\s+handles:[\s\S]*manual`),
		}},
	})
}
