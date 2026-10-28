package discount_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/compare"
	sdkacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const addr = "shopify_discount_basic.test"

var (
	codeGIDRe       = regexp.MustCompile(`^gid://shopify/DiscountCodeNode/\d+$`)
	automaticGIDRe  = regexp.MustCompile(`^gid://shopify/DiscountAutomaticNode/\d+$`)
	anyGIDRe        = regexp.MustCompile(`^gid://shopify/Discount(Code|Automatic)Node/\d+$`)
	productGIDRe    = regexp.MustCompile(`^gid://shopify/Product/\d+$`)
	collectionGIDRe = regexp.MustCompile(`^gid://shopify/Collection/\d+$`)
)

func random() string { return sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlphaNum) }

// testCode and testTitle keep test discounts apart from real ones.
func testCode() string  { return "TFACC" + strings.ToUpper(random()) }
func testTitle() string { return "tf-acc-discount-" + random() }

func getByID(t *testing.T, id string) *client.DiscountNodeFields {
	t.Helper()
	res, err := client.GetDiscount(context.Background(), acctest.Client(t), id)
	if err != nil {
		t.Fatal(err)
	}
	if res.DiscountNode == nil {
		return nil
	}
	return &res.DiscountNode.DiscountNodeFields
}

// byCode returns nil when no discount has the code.
func byCode(t *testing.T, code string) *client.DiscountNodeFields {
	t.Helper()
	res, err := client.GetDiscountByCode(context.Background(), acctest.Client(t), code)
	if err != nil {
		t.Fatal(err)
	}
	if res.CodeDiscountNodeByCode == nil {
		return nil
	}
	return getByID(t, res.CodeDiscountNodeByCode.Id)
}

// byTitle returns every discount with exactly this title.
func byTitle(t *testing.T, title string) []*client.DiscountNodeFields {
	t.Helper()
	q := fmt.Sprintf("title:%q", title)
	var out []*client.DiscountNodeFields
	var after *string
	for {
		res, err := client.ListDiscounts(context.Background(), acctest.Client(t), after, &q)
		if err != nil {
			t.Fatal(err)
		}
		for i := range res.DiscountNodes.Nodes {
			n := &res.DiscountNodes.Nodes[i].DiscountNodeFields
			if titled, ok := n.Discount.(interface{ GetTitle() string }); ok && titled.GetTitle() == title {
				out = append(out, n)
			}
		}
		if !res.DiscountNodes.PageInfo.HasNextPage {
			return out
		}
		after = res.DiscountNodes.PageInfo.EndCursor
	}
}

func codeBasic(t *testing.T, n *client.DiscountNodeFields) *client.DiscountNodeFieldsDiscountDiscountCodeBasic {
	t.Helper()
	d, ok := n.Discount.(*client.DiscountNodeFieldsDiscountDiscountCodeBasic)
	if !ok {
		t.Fatalf("discount %s is %T, want a code basic discount", n.Id, n.Discount)
	}
	return d
}

func automaticBasic(t *testing.T, n *client.DiscountNodeFields) *client.DiscountNodeFieldsDiscountDiscountAutomaticBasic {
	t.Helper()
	d, ok := n.Discount.(*client.DiscountNodeFieldsDiscountDiscountAutomaticBasic)
	if !ok {
		t.Fatalf("discount %s is %T, want an automatic basic discount", n.Id, n.Discount)
	}
	return d
}

func deleteNode(t *testing.T, n *client.DiscountNodeFields) error {
	t.Helper()
	ctx, c := context.Background(), acctest.Client(t)
	var errs []client.DiscountUserErrorFields
	if strings.Contains(n.Id, "DiscountCodeNode") {
		res, err := client.DeleteDiscountCode(ctx, c, n.Id)
		if err != nil {
			return err
		}
		if res.DiscountCodeDelete == nil {
			return fmt.Errorf("discountCodeDelete %s returned no payload", n.Id)
		}
		errs = res.DiscountCodeDelete.UserErrors
	} else {
		res, err := client.DeleteDiscountAutomatic(ctx, c, n.Id)
		if err != nil {
			return err
		}
		if res.DiscountAutomaticDelete == nil {
			return fmt.Errorf("discountAutomaticDelete %s returned no payload", n.Id)
		}
		errs = res.DiscountAutomaticDelete.UserErrors
	}
	if len(errs) > 0 {
		return fmt.Errorf("deleting %s: %+v", n.Id, errs)
	}
	return nil
}

// leftovers are the test discounts, products and collections that still exist.
func leftovers(t *testing.T, codes, titles, handles []string) (discounts []*client.DiscountNodeFields, products, collections []string) {
	t.Helper()
	for _, c := range codes {
		if n := byCode(t, c); n != nil {
			discounts = append(discounts, n)
		}
	}
	for _, title := range titles {
		discounts = append(discounts, byTitle(t, title)...)
	}
	ctx, c := context.Background(), acctest.Client(t)
	for _, h := range handles {
		p, err := client.GetProductByHandle(ctx, c, h)
		if err != nil {
			t.Fatal(err)
		}
		if p.ProductByIdentifier != nil {
			products = append(products, p.ProductByIdentifier.Id)
		}
		col, err := client.GetCollectionByHandle(ctx, c, h)
		if err != nil {
			t.Fatal(err)
		}
		if col.CollectionByIdentifier != nil {
			collections = append(collections, col.CollectionByIdentifier.Id)
		}
	}
	return discounts, products, collections
}

func removeAll(t *testing.T, discounts []*client.DiscountNodeFields, products, collections []string) {
	t.Helper()
	ctx, c := context.Background(), acctest.Client(t)
	for _, n := range discounts {
		if err := deleteNode(t, n); err != nil {
			t.Error(err)
		}
	}
	for _, id := range collections {
		if res, err := client.DeleteCollection(ctx, c, id); err != nil || len(res.CollectionDelete.UserErrors) > 0 {
			t.Errorf("deleting collection %s: %v %+v", id, err, res)
		}
	}
	for _, id := range products {
		if res, err := client.DeleteProduct(ctx, c, id); err != nil || len(res.ProductDelete.UserErrors) > 0 {
			t.Errorf("deleting product %s: %v %+v", id, err, res)
		}
	}
}

// cleanup removes whatever the test left behind when it ends, also on failure.
func cleanup(t *testing.T, codes, titles, handles []string) {
	t.Helper()
	t.Cleanup(func() {
		d, p, c := leftovers(t, codes, titles, handles)
		removeAll(t, d, p, c)
	})
}

// checkGone fails, and removes the leftovers, when a test object still exists after destroy.
func checkGone(t *testing.T, codes, titles, handles []string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		d, p, c := leftovers(t, codes, titles, handles)
		if len(d)+len(p)+len(c) == 0 {
			return nil
		}
		removeAll(t, d, p, c)
		return fmt.Errorf("left behind: %d discounts, %d products, %d collections", len(d), len(p), len(c))
	}
}

// segmentID is a segment every store has, so the segment context needs no shopify_customer_segment yet.
func segmentID(t *testing.T) string {
	t.Helper()
	res, err := client.ListSegments(context.Background(), acctest.Client(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Segments.Nodes) == 0 {
		t.Fatal("the dev store has no customer segment")
	}
	return res.Segments.Nodes[0].Id
}

// marketID is a market of the dev store (it has its primary market, `de`), for the markets context.
func marketID(t *testing.T) string {
	t.Helper()
	res, err := client.ListMarkets(context.Background(), acctest.Client(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Markets.Nodes) == 0 {
		t.Fatal("the dev store has no market")
	}
	return res.Markets.Nodes[0].Id
}

func value(path string, check knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(addr, tfjsonpath.New(path), check)
}

func nested(check knownvalue.Check, first string, rest ...string) statecheck.StateCheck {
	p := tfjsonpath.New(first)
	for _, s := range rest {
		p = p.AtMapKey(s)
	}
	return statecheck.ExpectKnownValue(addr, p, check)
}

func str(s string) knownvalue.Check { return knownvalue.StringExact(s) }

var emptyPlan = resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}

// items are the product and collection a discount can select, managed in the same configuration.
func items(handle string) string {
	return fmt.Sprintf(`
resource "shopify_product" "p" {
  handle              = %q
  title               = "tf-acc discount product"
  status              = "DRAFT"
  allow_data_deletion = true
}

resource "shopify_collection" "c" {
  handle = %q
  title  = "tf-acc discount collection"
}
`, handle, handle)
}

func basic(body string) string {
	return fmt.Sprintf("\nresource \"shopify_discount_basic\" \"test\" {\n%s\n}\n", body)
}

func TestAccDiscountBasic_code(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	code, renamed := testCode(), testCode()
	title := testTitle()
	handle := strings.ToLower(title)
	cleanup(t, []string{code, renamed}, []string{title, title + " b"}, []string{handle})
	segment, market := segmentID(t), marketID(t)
	sameID := statecheck.CompareValue(compare.ValuesSame())

	created := items(handle) + basic(fmt.Sprintf(`
  title         = %q
  code          = %q
  starts_at     = "2026-10-01T12:00:00+02:00"
  customer_gets = { value = { percentage = 0.1 }, items = { all = true } }
`, title, code))
	updated := func(minimum, value, itemsSel, contextSel string) string {
		return items(handle) + basic(fmt.Sprintf(`
  title                     = %q
  code                      = %q
  starts_at                 = "2026-10-01T12:00:00+02:00"
  ends_at                   = "2027-01-31T23:59:59+01:00"
  context                   = %s
  combines_with             = { shipping_discounts = true }
  usage_limit               = 100
  applies_once_per_customer = true
  minimum_requirement       = %s
  customer_gets             = { value = %s, items = %s }
`, title+" b", renamed, contextSel, minimum, value, itemsSel))
	}
	toSegment := updated(`{ subtotal = "30" }`, `{ amount = { amount = "5" } }`, `{ collection_ids = [shopify_collection.c.id] }`, fmt.Sprintf(`{ customer_segment_ids = [%q] }`, segment))
	toMarket := updated(`{ subtotal = "30" }`, `{ amount = { amount = "5" } }`, `{ collection_ids = [shopify_collection.c.id] }`, fmt.Sprintf(`{ market_ids = [%q] }`, market))
	toProducts := updated(`{ quantity = 2 }`, `{ percentage = 0.25 }`, `{ product_ids = [shopify_product.p.id] }`, `{ all = true }`)
	cleared := updated(`{}`, `{ percentage = 0.25 }`, `{ product_ids = [shopify_product.p.id] }`, `{ all = true }`)
	// ends_at and combines_with left out: unmanaged, so the client's changes below must not plan.
	unmanaged := items(handle) + basic(fmt.Sprintf(`
  title                     = %q
  code                      = %q
  starts_at                 = "2026-10-01T12:00:00+02:00"
  context                   = { all = true }
  usage_limit               = 100
  applies_once_per_customer = true
  minimum_requirement       = {}
  customer_gets             = { value = { percentage = 0.25 }, items = { product_ids = [shopify_product.p.id] } }
`, title+" b", renamed))
	// An import can only return UTC, and a plan must take the configured spelling, so import configs spell it in UTC.
	imported := strings.Replace(unmanaged, "2026-10-01T12:00:00+02:00", "2026-10-01T10:00:00Z", 1)
	automatic := items(handle) + basic(fmt.Sprintf(`
  title         = %q
  customer_gets = { value = { percentage = 0.25 }, items = { product_ids = [shopify_product.p.id] } }
`, title+" b"))
	clientEnd := "2027-06-30T22:00:00Z"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, []string{code, renamed}, []string{title, title + " b"}, []string{handle}),
		Steps: []resource.TestStep{
			{
				Config:      basic(`title = "x"` + "\n" + `usage_limit = 5` + "\n" + `customer_gets = { value = { percentage = 0.1 }, items = { all = true } }`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`"code" must be specified`),
			},
			{
				Config:      basic(`title = "x"` + "\n" + `customer_gets = { value = { percentage = 0.1, amount = { amount = "1" } }, items = { all = true } }`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
			{
				Config: created,
				Check: func(*terraform.State) error {
					d := codeBasic(t, byCode(t, code))
					if d.StartsAt != "2026-10-01T10:00:00Z" {
						return fmt.Errorf("live startsAt = %q, want the instant in UTC", d.StartsAt)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					sameID.AddStateValue(addr, tfjsonpath.New("id")),
					statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(codeGIDRe)}),
					value("id", knownvalue.StringRegexp(codeGIDRe)),
					value("code", str(code)),
					// The configured spelling stays although Shopify returns UTC.
					value("starts_at", str("2026-10-01T12:00:00+02:00")),
					value("ends_at", knownvalue.Null()),
					value("status", str("ACTIVE")),
					value("context", knownvalue.ObjectExact(map[string]knownvalue.Check{
						"all": knownvalue.Bool(true), "customer_segment_ids": knownvalue.Null(), "market_ids": knownvalue.Null(),
					})),
					value("combines_with", knownvalue.ObjectExact(map[string]knownvalue.Check{
						"product_discounts": knownvalue.Bool(false), "order_discounts": knownvalue.Bool(false), "shipping_discounts": knownvalue.Bool(false),
					})),
					value("minimum_requirement", knownvalue.ObjectExact(map[string]knownvalue.Check{"subtotal": knownvalue.Null(), "quantity": knownvalue.Null()})),
					value("usage_limit", knownvalue.Null()),
					value("applies_once_per_customer", knownvalue.Bool(false)),
					value("adopt_existing", knownvalue.Bool(false)),
				},
			},
			{Config: created, ConfigPlanChecks: emptyPlan},
			{
				// Code rename, value, items, context, minimum, dates and limits in one in-place update.
				Config: toSegment,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
					// A real update leaves status to the apply, since the dates may move it.
					plancheck.ExpectUnknownValue(addr, tfjsonpath.New("status")),
				}},
				Check: func(*terraform.State) error {
					d := codeBasic(t, byCode(t, renamed))
					ctxSegments, ok := d.Context.(*client.DiscountContextFieldsDiscountCustomerSegments)
					if !ok || len(ctxSegments.Segments) != 1 || ctxSegments.Segments[0].Id != segment {
						return fmt.Errorf("live context = %#v, want the segment", d.Context)
					}
					if byCode(t, code) != nil {
						return fmt.Errorf("old code %s still finds a discount", code)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					sameID.AddStateValue(addr, tfjsonpath.New("id")),
					value("code", str(renamed)),
					value("ends_at", str("2027-01-31T23:59:59+01:00")),
					value("usage_limit", knownvalue.Int64Exact(100)),
					value("applies_once_per_customer", knownvalue.Bool(true)),
					nested(knownvalue.SetExact([]knownvalue.Check{str(segment)}), "context", "customer_segment_ids"),
					nested(knownvalue.Bool(true), "combines_with", "shipping_discounts"),
					nested(knownvalue.Bool(false), "combines_with", "order_discounts"),
					// "30" and "5" stay although Shopify returns "30.0" and "5.0".
					nested(str("30"), "minimum_requirement", "subtotal"),
					nested(str("5"), "customer_gets", "value", "amount", "amount"),
					nested(knownvalue.Bool(false), "customer_gets", "value", "amount", "applies_on_each_item"),
					nested(knownvalue.SetExact([]knownvalue.Check{knownvalue.StringRegexp(collectionGIDRe)}), "customer_gets", "items", "collection_ids"),
				},
			},
			{Config: toSegment, ConfigPlanChecks: emptyPlan},
			{
				// Segments to markets and back: the new kind alone replaces the old one (checked, 2026-10-27).
				Config: toMarket,
				Check: func(*terraform.State) error {
					d := codeBasic(t, byCode(t, renamed))
					m, ok := d.Context.(*client.DiscountContextFieldsDiscountMarkets)
					if !ok || len(m.Markets.Nodes) != 1 || m.Markets.Nodes[0].Id != market {
						return fmt.Errorf("live context = %#v, want the market", d.Context)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					nested(knownvalue.SetExact([]knownvalue.Check{str(market)}), "context", "market_ids"),
					nested(knownvalue.Null(), "context", "customer_segment_ids"),
				},
			},
			{Config: toMarket, ConfigPlanChecks: emptyPlan},
			{
				Config: toSegment,
				ConfigStateChecks: []statecheck.StateCheck{
					nested(knownvalue.SetExact([]knownvalue.Check{str(segment)}), "context", "customer_segment_ids"),
					nested(knownvalue.Null(), "context", "market_ids"),
				},
			},
			{
				// Items switch kind, the context goes back to all buyers, the minimum becomes a quantity.
				Config: toProducts,
				ConfigStateChecks: []statecheck.StateCheck{
					sameID.AddStateValue(addr, tfjsonpath.New("id")),
					value("context", knownvalue.ObjectExact(map[string]knownvalue.Check{
						"all": knownvalue.Bool(true), "customer_segment_ids": knownvalue.Null(), "market_ids": knownvalue.Null(),
					})),
					nested(knownvalue.Int64Exact(2), "minimum_requirement", "quantity"),
					nested(knownvalue.Null(), "minimum_requirement", "subtotal"),
					nested(knownvalue.Float64Exact(0.25), "customer_gets", "value", "percentage"),
					nested(knownvalue.SetExact([]knownvalue.Check{knownvalue.StringRegexp(productGIDRe)}), "customer_gets", "items", "product_ids"),
					// Selecting a product doesn't list its variants.
					nested(knownvalue.Null(), "customer_gets", "items", "variant_ids"),
					nested(knownvalue.Null(), "customer_gets", "items", "collection_ids"),
				},
			},
			{Config: toProducts, ConfigPlanChecks: emptyPlan},
			{
				// {} removes a quantity minimum too.
				Config: cleared,
				Check: func(*terraform.State) error {
					if d := codeBasic(t, byCode(t, renamed)); d.MinimumRequirement != nil && *d.MinimumRequirement != nil {
						return fmt.Errorf("live minimum = %#v, want none", *d.MinimumRequirement)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					value("minimum_requirement", knownvalue.ObjectExact(map[string]knownvalue.Check{"subtotal": knownvalue.Null(), "quantity": knownvalue.Null()})),
				},
			},
			{Config: cleared, ConfigPlanChecks: emptyPlan},
			{
				// Review Focus 2: ends_at and combines_with changed through the client while unset in config.
				PreConfig: func() {
					n := byCode(t, renamed)
					yes, no := true, false
					res, err := client.UpdateDiscountCodeBasic(context.Background(), acctest.Client(t), n.Id, client.DiscountCodeBasicInput{
						EndsAt:       &clientEnd,
						CombinesWith: &client.DiscountCombinesWithInput{ProductDiscounts: &no, OrderDiscounts: &yes, ShippingDiscounts: &no},
					})
					if err != nil || res.DiscountCodeBasicUpdate == nil || len(res.DiscountCodeBasicUpdate.UserErrors) > 0 {
						t.Fatalf("discountCodeBasicUpdate: %v %+v", err, res)
					}
				},
				Config:           unmanaged,
				ConfigPlanChecks: emptyPlan,
				ConfigStateChecks: []statecheck.StateCheck{
					value("ends_at", str(clientEnd)),
					nested(knownvalue.Bool(true), "combines_with", "order_discounts"),
				},
			},
			{RefreshState: true},
			{Config: unmanaged, ConfigStateChecks: []statecheck.StateCheck{statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(codeGIDRe)})}},
			{Config: imported, ResourceName: addr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				// Generated config must pass validation and plan as import-only.
				Config:          items(handle),
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				Config:            imported,
				ResourceName:      addr,
				ImportState:       true,
				ImportStateId:     "code/" + renamed,
				ImportStateVerify: true,
				// Shopify returns UTC, so the configured spelling of starts_at can't come back from an import.
				ImportStateVerifyIgnore: []string{"starts_at"},
			},
			{
				// Removing the code replaces the discount, with a warning about the usage count.
				Config:           automatic,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate)}},
				Check: func(*terraform.State) error {
					if byCode(t, renamed) != nil {
						return fmt.Errorf("code discount %s survived the replace", renamed)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					value("id", knownvalue.StringRegexp(automaticGIDRe)),
					value("code", knownvalue.Null()),
					value("usage_limit", knownvalue.Null()),
					value("applies_once_per_customer", knownvalue.Null()),
				},
			},
		},
	})
}

func TestAccDiscountBasic_automatic(t *testing.T) {
	acctest.Setup(t)
	// A slash in the title checks that the import key splits at the first one.
	title := testTitle() + "/50"
	cleanup(t, nil, []string{title}, nil)
	cfg := basic(fmt.Sprintf(`
  title               = %q
  minimum_requirement = { subtotal = "50" }
  customer_gets       = { value = { percentage = 0.15 }, items = { all = true } }
`, title))
	duplicate := cfg + fmt.Sprintf(`
resource "shopify_discount_basic" "duplicate" {
  title         = %q
  customer_gets = { value = { percentage = 0.2 }, items = { all = true } }
  depends_on    = [shopify_discount_basic.test]
}
`, title)
	// combines_with and ends_at unset: unmanaged.
	clientEnd := "2027-03-01T00:00:00Z"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, nil, []string{title}, nil),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: func(*terraform.State) error {
					n := byTitle(t, title)
					if len(n) != 1 {
						return fmt.Errorf("%d discounts titled %q, want 1", len(n), title)
					}
					d := automaticBasic(t, n[0])
					if d.MinimumRequirement == nil {
						return fmt.Errorf("live discount has no minimum")
					}
					if m, ok := (*d.MinimumRequirement).(*client.DiscountMinimumFieldsDiscountMinimumSubtotal); !ok || m.GreaterThanOrEqualToSubtotal.Amount != "50.0" {
						return fmt.Errorf("live minimum = %#v, want a subtotal of 50.0", *d.MinimumRequirement)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					value("id", knownvalue.StringRegexp(automaticGIDRe)),
					value("code", knownvalue.Null()),
					value("usage_limit", knownvalue.Null()),
					value("applies_once_per_customer", knownvalue.Null()),
					value("status", str("ACTIVE")),
					nested(str("50"), "minimum_requirement", "subtotal"),
					nested(knownvalue.Bool(true), "context", "all"),
				},
			},
			{Config: cfg, ConfigPlanChecks: emptyPlan},
			{Config: duplicate, ExpectError: regexp.MustCompile(`(?i)title must be unique`)},
			{
				PreConfig: func() {
					n := byTitle(t, title)
					if len(n) != 1 {
						t.Fatalf("%d discounts titled %q, want 1", len(n), title)
					}
					yes, no := true, false
					res, err := client.UpdateDiscountAutomaticBasic(context.Background(), acctest.Client(t), n[0].Id, client.DiscountAutomaticBasicInput{
						EndsAt:       &clientEnd,
						CombinesWith: &client.DiscountCombinesWithInput{ProductDiscounts: &yes, OrderDiscounts: &no, ShippingDiscounts: &no},
					})
					if err != nil || res.DiscountAutomaticBasicUpdate == nil || len(res.DiscountAutomaticBasicUpdate.UserErrors) > 0 {
						t.Fatalf("discountAutomaticBasicUpdate: %v %+v", err, res)
					}
				},
				Config:           cfg,
				ConfigPlanChecks: emptyPlan,
				ConfigStateChecks: []statecheck.StateCheck{
					value("ends_at", str(clientEnd)),
					nested(knownvalue.Bool(true), "combines_with", "product_discounts"),
				},
			},
			{
				Config:                  cfg,
				ResourceName:            addr,
				ImportState:             true,
				ImportStateId:           "automatic/" + title,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"starts_at", "minimum_requirement.subtotal"},
			},
		},
	})
}

// Pattern A: an existing discount is only taken over with adopt_existing, and destroy leaves it as Terraform left it.
func TestAccDiscountBasic_adopt(t *testing.T) {
	acctest.Setup(t)
	code := testCode()
	cleanup(t, []string{code}, nil, nil)
	start := time.Now().UTC().Format(time.RFC3339)
	all, yes, pct := client.DiscountBuyerSelectionAll, true, 0.05
	res, err := client.CreateDiscountCodeBasic(context.Background(), acctest.Client(t), client.DiscountCodeBasicInput{
		Title: ptr("tf-acc adopt (client)"), Code: &code, StartsAt: &start,
		Context:      &client.DiscountContextInput{All: &all},
		CombinesWith: &client.DiscountCombinesWithInput{OrderDiscounts: &yes},
		CustomerGets: &client.DiscountCustomerGetsInput{Value: &client.DiscountCustomerGetsValueInput{Percentage: &pct}, Items: &client.DiscountItemsInput{All: &yes}},
	})
	if err != nil || res.DiscountCodeBasicCreate == nil || len(res.DiscountCodeBasicCreate.UserErrors) > 0 {
		t.Fatalf("discountCodeBasicCreate: %v %+v", err, res)
	}
	body := func(adopt bool) string {
		return basic(fmt.Sprintf(`
  title          = "tf-acc adopt (terraform)"
  code           = %q
  customer_gets  = { value = { percentage = 0.2 }, items = { all = true } }
  adopt_existing = %t
`, code, adopt))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Review Focus 1: destroy only drops state, so the discount is still there, unchanged.
		CheckDestroy: func(*terraform.State) error {
			n := byCode(t, code)
			if n == nil {
				return fmt.Errorf("adopted discount %s was deleted", code)
			}
			d := codeBasic(t, n)
			p, ok := d.CustomerGets.Value.(*client.DiscountCustomerGetsFieldsValueDiscountPercentage)
			if d.Title != "tf-acc adopt (terraform)" || !ok || p.Percentage != 0.2 || !d.CombinesWith.OrderDiscounts {
				return fmt.Errorf("adopted discount changed on destroy: %+v", d)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: body(false), ExpectError: regexp.MustCompile(`Code must be unique`)},
			{
				Config: body(true),
				ConfigStateChecks: []statecheck.StateCheck{
					value("title", str("tf-acc adopt (terraform)")),
					// Unset, so the client's value stays.
					nested(knownvalue.Bool(true), "combines_with", "order_discounts"),
					value("adopt_existing", knownvalue.Bool(true)),
				},
			},
		},
	})
}

func ptr[T any](v T) *T { return &v }

// A selection of single variants must read back as variant_ids only, so it plans empty after apply.
func TestAccDiscountBasic_variants(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	title := testTitle()
	handle := strings.ToLower(title)
	cleanup(t, nil, []string{title}, []string{handle})
	cfg := fmt.Sprintf(`
resource "shopify_product" "v" {
  handle              = %q
  title               = "tf-acc discount variants"
  status              = "DRAFT"
  allow_data_deletion = true
  options             = [{ name = "Size", values = ["S", "M"] }]
  variants            = [{ option_values = { Size = "S" }, price = "5" }, { option_values = { Size = "M" }, price = "6" }]
}
`, handle) + basic(fmt.Sprintf(`
  title         = %q
  customer_gets = { value = { percentage = 0.1 }, items = { variant_ids = [shopify_product.v.variants[0].id] } }
`, title))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, nil, []string{title}, []string{handle}),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				ConfigStateChecks: []statecheck.StateCheck{
					nested(knownvalue.SetSizeExact(1), "customer_gets", "items", "variant_ids"),
					nested(knownvalue.Null(), "customer_gets", "items", "product_ids"),
				},
			},
			{Config: cfg, ConfigPlanChecks: emptyPlan},
		},
	})
}
