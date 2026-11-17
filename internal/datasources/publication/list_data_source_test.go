package publication_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

// Read-only: the dev store's three channel publications must match the singular lookup by name.
func TestAccPublicationsDataSource_matchesSingular(t *testing.T) {
	cfg := `data "shopify_publications" "all" {}` + "\n"
	checks := []resource.TestCheckFunc{}
	// Sorted by name: Online Store, Point of Sale, Shop.
	for i, name := range []string{"Online Store", "Point of Sale", "Shop"} {
		key := fmt.Sprintf("s%d", i)
		cfg += fmt.Sprintf("data \"shopify_publication\" %q {\n  name = %q\n}\n", key, name)
		checks = append(checks,
			resource.TestCheckResourceAttr("data.shopify_publications.all", fmt.Sprintf("publications.%d.name", i), name),
			resource.TestCheckResourceAttrPair("data.shopify_publications.all", fmt.Sprintf("publications.%d.id", i), "data.shopify_publication."+key, "id"),
			resource.TestCheckResourceAttrPair("data.shopify_publications.all", fmt.Sprintf("publications.%d.catalog_id", i), "data.shopify_publication."+key, "catalog_id"),
			resource.TestCheckResourceAttrPair("data.shopify_publications.all", fmt.Sprintf("publications.%d.auto_publish", i), "data.shopify_publication."+key, "auto_publish"),
		)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps:                    []resource.TestStep{{Config: cfg, Check: resource.ComposeAggregateTestCheckFunc(checks...)}},
	})
}
