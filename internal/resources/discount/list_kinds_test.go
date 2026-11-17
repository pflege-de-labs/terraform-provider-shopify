package discount_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/querycheck/queryfilter"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

// lazyCheck builds its check when the query runs, for IDs that only exist after the apply.
type lazyCheck func() querycheck.QueryResultCheck

func (l lazyCheck) CheckQuery(ctx context.Context, req querycheck.CheckQueryRequest, resp *querycheck.CheckQueryResponse) {
	l().CheckQuery(ctx, req, resp)
}

func listFor(kind string) string {
	return fmt.Sprintf(`
provider "shopify" {}

list "shopify_discount_%s" "all" {
  provider         = shopify
  include_resource = true
}
`, kind)
}

// Each list returns its own method only; the other methods are skipped with a warning.
func TestAccListDiscountKinds(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	code := testCode()
	basicTitle, fsTitle, bxgyTitle := testTitle(), testTitle(), testTitle()
	handle := strings.ToLower(testTitle())
	cleanup(t, []string{code}, []string{basicTitle, fsTitle, bxgyTitle}, []string{handle})
	discounts := items(handle) + fmt.Sprintf(`
resource "shopify_discount_basic" "basic" {
  title         = %q
  customer_gets = { value = { percentage = 0.1 }, items = { all = true } }
}

resource "shopify_discount_free_shipping" "code" {
  title                  = %q
  code                   = %q
  usage_limit            = 10
  destination            = { country_codes = ["DE"] }
  maximum_shipping_price = "20"
  minimum_requirement    = { quantity = 3 }
}

resource "shopify_discount_bxgy" "automatic" {
  title                = %q
  uses_per_order_limit = 2
  customer_buys        = { quantity = 2, items = { product_ids = [shopify_product.p.id] } }
  customer_gets        = { quantity = 1, percentage = 1, items = { collection_ids = [shopify_collection.c.id] } }
}
`, basicTitle, fsTitle, code, bxgyTitle)

	idOf := func(title string) string {
		n := byTitle(t, title)
		if len(n) != 1 {
			t.Fatalf("%d discounts titled %q, want 1", len(n), title)
		}
		return n[0].Id
	}
	absent := func(kind, title string) querycheck.QueryResultCheck {
		return lazyCheck(func() querycheck.QueryResultCheck {
			return querycheck.ExpectNoIdentity("shopify_discount_"+kind+".all", map[string]knownvalue.Check{"id": knownvalue.StringExact(idOf(title))})
		})
	}
	known := func(kind, name string, checks map[string]knownvalue.Check) querycheck.QueryResultCheck {
		var kv []querycheck.KnownValueCheck
		for p, c := range checks {
			kv = append(kv, querycheck.KnownValueCheck{Path: tfjsonpath.New(p), KnownValue: c})
		}
		return querycheck.ExpectResourceKnownValues("shopify_discount_"+kind+".all", queryfilter.ByDisplayName(knownvalue.StringExact(name)), kv)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkGone(t, []string{code}, []string{basicTitle, fsTitle, bxgyTitle}, []string{handle}),
		Steps: []resource.TestStep{
			{Config: discounts},
			{
				Query:  true,
				Config: listFor("free_shipping"),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_discount_free_shipping.all", 1),
					querycheck.ExpectIdentity("shopify_discount_free_shipping.all", map[string]knownvalue.Check{"id": knownvalue.StringRegexp(anyGIDRe)}),
					known("free_shipping", fmt.Sprintf("%s (code %s)", fsTitle, code), map[string]knownvalue.Check{
						"id":                     knownvalue.StringRegexp(codeGIDRe),
						"code":                   knownvalue.StringExact(code),
						"usage_limit":            knownvalue.Int64Exact(10),
						"maximum_shipping_price": knownvalue.StringExact("20.0"),
						"destination":            knownvalue.ObjectExact(map[string]knownvalue.Check{"all": knownvalue.Null(), "country_codes": knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("DE")})}),
						"minimum_requirement":    knownvalue.ObjectExact(map[string]knownvalue.Check{"subtotal": knownvalue.Null(), "quantity": knownvalue.Int64Exact(3)}),
						"adopt_existing":         knownvalue.Bool(false),
					}),
					absent("free_shipping", basicTitle),
					absent("free_shipping", bxgyTitle),
				},
			},
			{
				Query:  true,
				Config: listFor("bxgy"),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_discount_bxgy.all", 1),
					known("bxgy", bxgyTitle+" (automatic)", map[string]knownvalue.Check{
						"id":                   knownvalue.StringRegexp(automaticGIDRe),
						"code":                 knownvalue.Null(),
						"uses_per_order_limit": knownvalue.Int64Exact(2),
						"customer_buys": knownvalue.ObjectExact(map[string]knownvalue.Check{
							"quantity": knownvalue.Int64Exact(2), "amount": knownvalue.Null(),
							"items": knownvalue.ObjectExact(map[string]knownvalue.Check{
								"product_ids": knownvalue.SetExact([]knownvalue.Check{knownvalue.StringRegexp(productGIDRe)}), "variant_ids": knownvalue.Null(), "collection_ids": knownvalue.Null(),
							}),
						}),
						"customer_gets": knownvalue.ObjectExact(map[string]knownvalue.Check{
							"quantity": knownvalue.Int64Exact(1), "percentage": knownvalue.Float64Exact(1), "amount": knownvalue.Null(),
							"items": knownvalue.ObjectExact(map[string]knownvalue.Check{
								"product_ids": knownvalue.Null(), "variant_ids": knownvalue.Null(), "collection_ids": knownvalue.SetExact([]knownvalue.Check{knownvalue.StringRegexp(collectionGIDRe)}),
							}),
						}),
					}),
					absent("bxgy", basicTitle),
					absent("bxgy", fsTitle),
				},
			},
			{
				Query:  true,
				Config: listFor("basic"),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_discount_basic.all", 1),
					absent("basic", fsTitle),
					absent("basic", bxgyTitle),
				},
			},
		},
	})
}
