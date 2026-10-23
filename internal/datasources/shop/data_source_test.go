package shop_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

func TestAccShopDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "shopify_shop" "test" {}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.shopify_shop.test", tfjsonpath.New("id"),
						knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/Shop/\d+$`))),
					statecheck.ExpectKnownValue("data.shopify_shop.test", tfjsonpath.New("myshopify_domain"),
						knownvalue.StringExact(os.Getenv("SHOPIFY_STORE_DOMAIN"))),
					statecheck.ExpectKnownValue("data.shopify_shop.test", tfjsonpath.New("currency_code"),
						knownvalue.StringRegexp(regexp.MustCompile(`^[A-Z]{3}$`))),
				},
			},
		},
	})
}
