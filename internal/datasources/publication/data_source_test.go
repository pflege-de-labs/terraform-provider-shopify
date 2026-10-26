package publication_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

const dsAddr = "data.shopify_publication.test"

// Read-only: `Online Store` exists on every store, so the tests create nothing.
func TestAccPublicationDataSource_onlineStore(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
data "shopify_publication" "test" {
  name = "Online Store"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(dsAddr, "id", regexp.MustCompile(`^gid://shopify/Publication/\d+$`)),
				resource.TestCheckResourceAttr(dsAddr, "name", "Online Store"),
				resource.TestMatchResourceAttr(dsAddr, "catalog_id", regexp.MustCompile(`^gid://shopify/AppCatalog/\d+$`)),
				// Observed on the dev store; a merchant can flip it in the admin.
				resource.TestCheckResourceAttr(dsAddr, "auto_publish", "false"),
			),
		}},
	})
}

func TestAccPublicationDataSource_notFound(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
data "shopify_publication" "test" {
  name = "tfacc-missing"
}
`,
			// Terraform wraps long error text, so match on whitespace; the list must include `Online Store`.
			ExpectError: regexp.MustCompile(`tfacc-missing[\s\S]*available:[\s\S]*Online\s+Store`),
		}},
	})
}
