package discount_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const bxgyAddr = "shopify_discount_bxgy.test"

func bxgyValue(path string, check knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(bxgyAddr, tfjsonpath.New(path), check)
}

func bxgyNested(check knownvalue.Check, first string, rest ...string) statecheck.StateCheck {
	p := tfjsonpath.New(first)
	for _, s := range rest {
		p = p.AtMapKey(s)
	}
	return statecheck.ExpectKnownValue(bxgyAddr, p, check)
}

func bxgy(body string) string {
	return fmt.Sprintf("\nresource \"shopify_discount_bxgy\" \"test\" {\n%s\n}\n", body)
}

func codeBxgy(t *testing.T, n *client.DiscountNodeFields) *client.DiscountNodeFieldsDiscountDiscountCodeBxgy {
	t.Helper()
	d, ok := n.Discount.(*client.DiscountNodeFieldsDiscountDiscountCodeBxgy)
	if !ok {
		t.Fatalf("discount %s is %T, want a code buy X get Y discount", n.Id, n.Discount)
	}
	return d
}

func automaticBxgy(t *testing.T, n *client.DiscountNodeFields) *client.DiscountNodeFieldsDiscountDiscountAutomaticBxgy {
	t.Helper()
	d, ok := n.Discount.(*client.DiscountNodeFieldsDiscountDiscountAutomaticBxgy)
	if !ok {
		t.Fatalf("discount %s is %T, want an automatic buy X get Y discount", n.Id, n.Discount)
	}
	return d
}

func TestAccDiscountBxgy_code(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	code, renamed := testCode(), testCode()
	title := testTitle()
	handle := strings.ToLower(title)
	cleanup(t, []string{code, renamed}, []string{title, title + " b"}, []string{handle})
	sameID := statecheck.CompareValue(compare.ValuesSame())

	created := items(handle) + bxgy(fmt.Sprintf(`
  title         = %q
  code          = %q
  starts_at     = "2026-10-01T12:00:00+02:00"
  customer_buys = { quantity = 2, items = { product_ids = [shopify_product.p.id] } }
  customer_gets = { quantity = 1, percentage = 1, items = { collection_ids = [shopify_collection.c.id] } }
`, title, code))
	updated := func(buys, gets string) string {
		return items(handle) + bxgy(fmt.Sprintf(`
  title                     = %q
  code                      = %q
  starts_at                 = "2026-10-01T12:00:00+02:00"
  ends_at                   = "2027-01-31T23:59:59+01:00"
  combines_with             = { order_discounts = true }
  usage_limit               = 9
  applies_once_per_customer = true
  uses_per_order_limit      = 2
  customer_buys             = %s
  customer_gets             = %s
`, title+" b", renamed, buys, gets))
	}
	// Buys quantity to amount, gets percentage to amount, and the item kinds swapped.
	switched := updated(
		`{ amount = "30", items = { collection_ids = [shopify_collection.c.id] } }`,
		`{ quantity = 2, amount = "5", items = { product_ids = [shopify_product.p.id] } }`)
	back := updated(
		`{ quantity = 3, items = { product_ids = [shopify_product.p.id] } }`,
		`{ quantity = 1, percentage = 0.1, items = { collection_ids = [shopify_collection.c.id] } }`)
	// ends_at, combines_with and uses_per_order_limit left out: unmanaged, so the client's changes below must not plan.
	unmanaged := items(handle) + bxgy(fmt.Sprintf(`
  title                     = %q
  code                      = %q
  starts_at                 = "2026-10-01T12:00:00+02:00"
  context                   = { all = true }
  usage_limit               = 9
  applies_once_per_customer = true
  customer_buys             = { quantity = 3, items = { product_ids = [shopify_product.p.id] } }
  # 0.1 has no exact float64 form, so the import steps catch config/state number mismatches.
  customer_gets             = { quantity = 1, percentage = 0.1, items = { collection_ids = [shopify_collection.c.id] } }
`, title+" b", renamed))
	imported := strings.Replace(unmanaged, "2026-10-01T12:00:00+02:00", "2026-10-01T10:00:00Z", 1)
	automatic := items(handle) + bxgy(fmt.Sprintf(`
  title         = %q
  customer_buys = { quantity = 1, items = { product_ids = [shopify_product.p.id] } }
  customer_gets = { quantity = 1, percentage = 1, items = { product_ids = [shopify_product.p.id] } }
`, title+" b"))
	clientPerOrder := 7

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, []string{code, renamed}, []string{title, title + " b"}, []string{handle}),
		Steps: []resource.TestStep{
			{
				Config:      bxgy(`title = "x"` + "\n" + `customer_buys = { quantity = 1, items = { all = true } }` + "\n" + `customer_gets = { quantity = 1, percentage = 1, items = { product_ids = ["p"] } }`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
			{
				Config:      bxgy(`title = "x"` + "\n" + `customer_buys = { quantity = 1, amount = "5", items = { product_ids = ["p"] } }` + "\n" + `customer_gets = { quantity = 1, percentage = 1, items = { product_ids = ["p"] } }`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
			{
				Config:      bxgy(`title = "x"` + "\n" + `customer_buys = { quantity = 1, items = { product_ids = ["p"] } }` + "\n" + `customer_gets = { quantity = 1, items = { product_ids = ["p"] } }`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
			{
				Config: created,
				Check: func(*terraform.State) error {
					d := codeBxgy(t, byCode(t, code))
					if d.StartsAt != "2026-10-01T10:00:00Z" {
						return fmt.Errorf("live startsAt = %q, want the instant in UTC", d.StartsAt)
					}
					if v, ok := d.CustomerBuys.Value.(*client.DiscountCustomerBuysFieldsValueDiscountQuantity); !ok || v.Quantity != "2" {
						return fmt.Errorf("live buys = %#v, want a quantity of 2", d.CustomerBuys.Value)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					sameID.AddStateValue(bxgyAddr, tfjsonpath.New("id")),
					bxgyValue("id", knownvalue.StringRegexp(codeGIDRe)),
					bxgyValue("code", str(code)),
					bxgyValue("status", str("ACTIVE")),
					bxgyValue("uses_per_order_limit", knownvalue.Null()),
					bxgyNested(knownvalue.Int64Exact(2), "customer_buys", "quantity"),
					bxgyNested(knownvalue.Null(), "customer_buys", "amount"),
					bxgyNested(knownvalue.SetExact([]knownvalue.Check{knownvalue.StringRegexp(productGIDRe)}), "customer_buys", "items", "product_ids"),
					bxgyNested(knownvalue.Int64Exact(1), "customer_gets", "quantity"),
					bxgyNested(knownvalue.Float64Exact(1), "customer_gets", "percentage"),
					bxgyNested(knownvalue.SetExact([]knownvalue.Check{knownvalue.StringRegexp(collectionGIDRe)}), "customer_gets", "items", "collection_ids"),
					bxgyNested(knownvalue.Bool(true), "context", "all"),
				},
			},
			{Config: created, ConfigPlanChecks: emptyPlan},
			{
				Config: switched,
				Check: func(*terraform.State) error {
					d := codeBxgy(t, byCode(t, renamed))
					if v, ok := d.CustomerBuys.Value.(*client.DiscountCustomerBuysFieldsValueDiscountPurchaseAmount); !ok || v.Amount != "30.0" {
						return fmt.Errorf("live buys = %#v, want an amount of 30.0", d.CustomerBuys.Value)
					}
					v, ok := d.CustomerGets.Value.(*client.DiscountCustomerGetsFieldsValueDiscountOnQuantity)
					if !ok || v.Quantity.Quantity != "2" {
						return fmt.Errorf("live gets = %#v, want a quantity of 2", d.CustomerGets.Value)
					}
					if e, ok := v.Effect.(*client.DiscountCustomerGetsFieldsValueDiscountOnQuantityEffectDiscountAmount); !ok || e.Amount.Amount != "5.0" {
						return fmt.Errorf("live effect = %#v, want an amount of 5.0", v.Effect)
					}
					if d.UsesPerOrderLimit == nil || *d.UsesPerOrderLimit != 2 || d.UsageLimit == nil || *d.UsageLimit != 9 {
						return fmt.Errorf("live limits = %v, %v", d.UsesPerOrderLimit, d.UsageLimit)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					sameID.AddStateValue(bxgyAddr, tfjsonpath.New("id")),
					bxgyValue("title", str(title+" b")),
					bxgyValue("code", str(renamed)),
					bxgyValue("uses_per_order_limit", knownvalue.Int64Exact(2)),
					bxgyNested(knownvalue.Null(), "customer_buys", "quantity"),
					bxgyNested(str("30"), "customer_buys", "amount"),
					bxgyNested(knownvalue.SetExact([]knownvalue.Check{knownvalue.StringRegexp(collectionGIDRe)}), "customer_buys", "items", "collection_ids"),
					bxgyNested(knownvalue.Null(), "customer_gets", "percentage"),
					bxgyNested(str("5"), "customer_gets", "amount"),
					bxgyNested(knownvalue.SetExact([]knownvalue.Check{knownvalue.StringRegexp(productGIDRe)}), "customer_gets", "items", "product_ids"),
				},
			},
			{Config: switched, ConfigPlanChecks: emptyPlan},
			{Config: back},
			{Config: back, ConfigPlanChecks: emptyPlan},
			{
				// Review Focus 2: ends_at, combines_with and uses_per_order_limit changed through the client while unset in config.
				PreConfig: func() {
					n := byCode(t, renamed)
					yes, no := true, false
					end := "2027-06-30T22:00:00Z"
					res, err := client.UpdateDiscountCodeBxgy(context.Background(), acctest.Client(t), n.Id, client.DiscountCodeBxgyInput{
						EndsAt:            &end,
						UsesPerOrderLimit: &clientPerOrder,
						CombinesWith:      &client.DiscountCombinesWithInput{ProductDiscounts: &yes, OrderDiscounts: &no, ShippingDiscounts: &no},
					})
					if err != nil || res.DiscountCodeBxgyUpdate == nil || len(res.DiscountCodeBxgyUpdate.UserErrors) > 0 {
						t.Fatalf("discountCodeBxgyUpdate: %v %+v", err, res)
					}
				},
				Config:           unmanaged,
				ConfigPlanChecks: emptyPlan,
				ConfigStateChecks: []statecheck.StateCheck{
					bxgyValue("ends_at", str("2027-06-30T22:00:00Z")),
					bxgyValue("uses_per_order_limit", knownvalue.Int64Exact(7)),
					bxgyNested(knownvalue.Bool(true), "combines_with", "product_discounts"),
				},
			},
			{RefreshState: true},
			{Config: unmanaged, ConfigStateChecks: []statecheck.StateCheck{statecheck.ExpectIdentity(bxgyAddr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(codeGIDRe)})}},
			{Config: imported, ResourceName: bxgyAddr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				// Generated config must pass validation and plan as import-only.
				Config:          items(handle),
				ResourceName:    bxgyAddr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				Config:                  imported,
				ResourceName:            bxgyAddr,
				ImportState:             true,
				ImportStateId:           "code/" + renamed,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"starts_at", "ends_at"},
			},
			{
				// Removing the code replaces the discount, with a warning about the usage count.
				Config:           automatic,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(bxgyAddr, plancheck.ResourceActionDestroyBeforeCreate)}},
				Check: func(*terraform.State) error {
					if byCode(t, renamed) != nil {
						return fmt.Errorf("code discount %s survived the replace", renamed)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					bxgyValue("id", knownvalue.StringRegexp(automaticGIDRe)),
					bxgyValue("code", knownvalue.Null()),
					bxgyValue("usage_limit", knownvalue.Null()),
				},
			},
		},
	})
}

func TestAccDiscountBxgy_automatic(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	// A slash in the title checks that the import key splits at the first one.
	title := testTitle() + "/2for1"
	handle := strings.ToLower(testTitle())
	cleanup(t, nil, []string{title}, []string{handle})
	cfg := items(handle) + bxgy(fmt.Sprintf(`
  title                = %q
  uses_per_order_limit = 3
  customer_buys        = { amount = "30", items = { product_ids = [shopify_product.p.id] } }
  customer_gets        = { quantity = 2, amount = "5", items = { product_ids = [shopify_product.p.id] } }
`, title))
	duplicate := cfg + fmt.Sprintf(`
resource "shopify_discount_bxgy" "duplicate" {
  title         = %q
  customer_buys = { quantity = 1, items = { product_ids = [shopify_product.p.id] } }
  customer_gets = { quantity = 1, percentage = 1, items = { product_ids = [shopify_product.p.id] } }
  depends_on    = [shopify_discount_bxgy.test]
}
`, title)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, nil, []string{title}, []string{handle}),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: func(*terraform.State) error {
					n := byTitle(t, title)
					if len(n) != 1 {
						return fmt.Errorf("%d discounts titled %q, want 1", len(n), title)
					}
					if d := automaticBxgy(t, n[0]); d.UsesPerOrderLimit == nil || *d.UsesPerOrderLimit != 3 {
						return fmt.Errorf("live uses per order limit = %v, want 3", d.UsesPerOrderLimit)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					bxgyValue("id", knownvalue.StringRegexp(automaticGIDRe)),
					bxgyValue("code", knownvalue.Null()),
					bxgyValue("usage_limit", knownvalue.Null()),
					bxgyValue("uses_per_order_limit", knownvalue.Int64Exact(3)),
					bxgyNested(str("30"), "customer_buys", "amount"),
					bxgyNested(str("5"), "customer_gets", "amount"),
					bxgyNested(knownvalue.Int64Exact(2), "customer_gets", "quantity"),
				},
			},
			{Config: cfg, ConfigPlanChecks: emptyPlan},
			{Config: duplicate, ExpectError: regexp.MustCompile(`(?i)title must be unique`)},
			{
				Config:                  cfg,
				ResourceName:            bxgyAddr,
				ImportState:             true,
				ImportStateId:           "automatic/" + title,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"starts_at", "customer_buys.amount", "customer_gets.amount"},
			},
			{
				Config:          items(handle),
				ResourceName:    bxgyAddr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
		},
	})
}
