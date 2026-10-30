package customeraccountpage_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

const dsAddr = "data.shopify_customer_account_page.test"

// Read-only: Shopify creates the `orders` page on every store, so the tests create nothing.
func TestAccCustomerAccountPageDataSource_orders(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
data "shopify_customer_account_page" "test" {
  handle = "orders"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(dsAddr, "id", regexp.MustCompile(`^gid://shopify/CustomerAccountPage/\d+$`)),
				resource.TestCheckResourceAttr(dsAddr, "handle", "orders"),
				resource.TestCheckResourceAttrSet(dsAddr, "title"),
			),
		}},
	})
}

func TestAccCustomerAccountPageDataSource_notFound(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
data "shopify_customer_account_page" "test" {
  handle = "tfacc-missing"
}
`,
			ExpectError: regexp.MustCompile(`tfacc-missing[\s\S]*available:`),
		}},
	})
}
