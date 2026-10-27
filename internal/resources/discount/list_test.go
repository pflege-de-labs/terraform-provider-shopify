package discount_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/querycheck/queryfilter"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

const listConfig = `
provider "shopify" {}

list "shopify_discount_basic" "all" {
  provider         = shopify
  include_resource = true
}
`

func TestAccListDiscountBasic(t *testing.T) {
	acctest.Setup(t)
	code := testCode()
	codeTitle, autoTitle := testTitle(), testTitle()
	cleanup(t, []string{code}, []string{codeTitle, autoTitle}, nil)
	discounts := fmt.Sprintf(`
resource "shopify_discount_basic" "code" {
  title               = %q
  code                = %q
  usage_limit         = 10
  minimum_requirement = { quantity = 3 }
  customer_gets       = { value = { amount = { amount = "5" } }, items = { all = true } }
}

resource "shopify_discount_basic" "automatic" {
  title         = %q
  combines_with = { order_discounts = true }
  customer_gets = { value = { percentage = 0.1 }, items = { all = true } }
}
`, codeTitle, code, autoTitle)
	byName := func(name string) queryfilter.QueryFilter {
		return queryfilter.ByDisplayName(knownvalue.StringExact(name))
	}
	check := func(name string, checks map[string]knownvalue.Check) querycheck.QueryResultCheck {
		var kv []querycheck.KnownValueCheck
		for p, c := range checks {
			kv = append(kv, querycheck.KnownValueCheck{Path: tfjsonpath.New(p), KnownValue: c})
		}
		return querycheck.ExpectResourceKnownValues("shopify_discount_basic.all", byName(name), kv)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkGone(t, []string{code}, []string{codeTitle, autoTitle}, nil),
		Steps: []resource.TestStep{
			{Config: discounts},
			{
				Query:  true,
				Config: listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_discount_basic.all", 2),
					querycheck.ExpectIdentity("shopify_discount_basic.all", map[string]knownvalue.Check{"id": knownvalue.StringRegexp(anyGIDRe)}),
					check(fmt.Sprintf("%s (code %s)", codeTitle, code), map[string]knownvalue.Check{
						"id":                        knownvalue.StringRegexp(codeGIDRe),
						"code":                      knownvalue.StringExact(code),
						"usage_limit":               knownvalue.Int64Exact(10),
						"applies_once_per_customer": knownvalue.Bool(false),
						"minimum_requirement":       knownvalue.ObjectExact(map[string]knownvalue.Check{"subtotal": knownvalue.Null(), "quantity": knownvalue.Int64Exact(3)}),
						"customer_gets": knownvalue.ObjectExact(map[string]knownvalue.Check{
							"value": knownvalue.ObjectExact(map[string]knownvalue.Check{
								"percentage": knownvalue.Null(),
								"amount":     knownvalue.ObjectExact(map[string]knownvalue.Check{"amount": knownvalue.StringExact("5.0"), "applies_on_each_item": knownvalue.Bool(false)}),
							}),
							"items": knownvalue.ObjectExact(map[string]knownvalue.Check{
								"all": knownvalue.Bool(true), "product_ids": knownvalue.Null(), "variant_ids": knownvalue.Null(), "collection_ids": knownvalue.Null(),
							}),
						}),
						"adopt_existing": knownvalue.Bool(false),
					}),
					check(autoTitle+" (automatic)", map[string]knownvalue.Check{
						"id":                        knownvalue.StringRegexp(automaticGIDRe),
						"code":                      knownvalue.Null(),
						"usage_limit":               knownvalue.Null(),
						"applies_once_per_customer": knownvalue.Null(),
						"context":                   knownvalue.ObjectExact(map[string]knownvalue.Check{"all": knownvalue.Bool(true), "customer_segment_ids": knownvalue.Null(), "market_ids": knownvalue.Null()}),
						"combines_with": knownvalue.ObjectExact(map[string]knownvalue.Check{
							"product_discounts": knownvalue.Bool(false), "order_discounts": knownvalue.Bool(true), "shipping_discounts": knownvalue.Bool(false),
						}),
						"minimum_requirement": knownvalue.ObjectExact(map[string]knownvalue.Check{"subtotal": knownvalue.Null(), "quantity": knownvalue.Null()}),
					}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _code.
		},
	})
}
