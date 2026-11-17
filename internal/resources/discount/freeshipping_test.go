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

const fsAddr = "shopify_discount_free_shipping.test"

func fsValue(path string, check knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(fsAddr, tfjsonpath.New(path), check)
}

func fsNested(check knownvalue.Check, first string, rest ...string) statecheck.StateCheck {
	p := tfjsonpath.New(first)
	for _, s := range rest {
		p = p.AtMapKey(s)
	}
	return statecheck.ExpectKnownValue(fsAddr, p, check)
}

func freeShipping(body string) string {
	return fmt.Sprintf("\nresource \"shopify_discount_free_shipping\" \"test\" {\n%s\n}\n", body)
}

func codeFreeShipping(t *testing.T, n *client.DiscountNodeFields) *client.DiscountNodeFieldsDiscountDiscountCodeFreeShipping {
	t.Helper()
	d, ok := n.Discount.(*client.DiscountNodeFieldsDiscountDiscountCodeFreeShipping)
	if !ok {
		t.Fatalf("discount %s is %T, want a code free shipping discount", n.Id, n.Discount)
	}
	return d
}

func automaticFreeShipping(t *testing.T, n *client.DiscountNodeFields) *client.DiscountNodeFieldsDiscountDiscountAutomaticFreeShipping {
	t.Helper()
	d, ok := n.Discount.(*client.DiscountNodeFieldsDiscountDiscountAutomaticFreeShipping)
	if !ok {
		t.Fatalf("discount %s is %T, want an automatic free shipping discount", n.Id, n.Discount)
	}
	return d
}

// The dev store ships to Germany only, and Shopify silently drops countries outside its zones' offers, so tests use DE.
func TestAccDiscountFreeShipping_code(t *testing.T) {
	acctest.Setup(t)
	code, renamed := testCode(), testCode()
	title := testTitle()
	cleanup(t, []string{code, renamed}, []string{title, title + " b"}, nil)
	sameID := statecheck.CompareValue(compare.ValuesSame())

	created := freeShipping(fmt.Sprintf(`
  title                  = %q
  code                   = %q
  starts_at              = "2026-10-01T12:00:00+02:00"
  destination            = { country_codes = ["DE"] }
  maximum_shipping_price = "20.00"
  minimum_requirement    = { subtotal = "50" }
  combines_with          = { product_discounts = true }
  usage_limit            = 5
`, title, code))
	updated := func(destination, maximum, minimum string) string {
		return freeShipping(fmt.Sprintf(`
  title                     = %q
  code                      = %q
  starts_at                 = "2026-10-01T12:00:00+02:00"
  ends_at                   = "2027-01-31T23:59:59+01:00"
  destination               = %s
  maximum_shipping_price    = %s
  minimum_requirement       = %s
  usage_limit               = 9
  applies_once_per_customer = true
`, title+" b", renamed, destination, maximum, minimum))
	}
	toAll := updated(`{ all = true }`, `"25.5"`, `{ quantity = 2 }`)
	back := updated(`{ country_codes = ["DE"] }`, `"20"`, `{}`)
	// destination, maximum_shipping_price and combines_with left out: unmanaged, so the client's changes below must not plan.
	unmanaged := freeShipping(fmt.Sprintf(`
  title                     = %q
  code                      = %q
  starts_at                 = "2026-10-01T12:00:00+02:00"
  context                   = { all = true }
  minimum_requirement       = {}
  usage_limit               = 9
  applies_once_per_customer = true
`, title+" b", renamed))
	// An import can only return UTC, and a plan must take the configured spelling, so import configs spell it in UTC.
	imported := strings.Replace(unmanaged, "2026-10-01T12:00:00+02:00", "2026-10-01T10:00:00Z", 1)
	automatic := freeShipping(fmt.Sprintf(`
  title       = %q
  destination = { all = true }
`, title+" b"))
	clientMax := "40"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, []string{code, renamed}, []string{title, title + " b"}, nil),
		Steps: []resource.TestStep{
			{
				Config:      freeShipping(`title = "x"` + "\n" + `usage_limit = 5`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`"code" must be specified`),
			},
			{
				Config:      freeShipping(`title = "x"` + "\n" + `destination = { all = true, country_codes = ["DE"] }`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
			{
				Config:      freeShipping(`title = "x"` + "\n" + `destination = { country_codes = ["de"] }`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`upper-case ISO\s+country code`),
			},
			{
				Config: created,
				Check: func(*terraform.State) error {
					d := codeFreeShipping(t, byCode(t, code))
					if d.StartsAt != "2026-10-01T10:00:00Z" {
						return fmt.Errorf("live startsAt = %q, want the instant in UTC", d.StartsAt)
					}
					if c, ok := d.DestinationSelection.(*client.DiscountDestinationFieldsDiscountCountries); !ok || len(c.Countries) != 1 || c.Countries[0] != "DE" {
						return fmt.Errorf("live destination = %#v, want DE", d.DestinationSelection)
					}
					if d.MaximumShippingPrice == nil || d.MaximumShippingPrice.Amount != "20.0" {
						return fmt.Errorf("live maximum shipping price = %+v, want 20.0", d.MaximumShippingPrice)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					sameID.AddStateValue(fsAddr, tfjsonpath.New("id")),
					fsValue("id", knownvalue.StringRegexp(codeGIDRe)),
					fsValue("code", str(code)),
					fsValue("status", str("ACTIVE")),
					fsValue("maximum_shipping_price", str("20.00")),
					fsValue("usage_limit", knownvalue.Int64Exact(5)),
					fsValue("applies_once_per_customer", knownvalue.Bool(false)),
					fsNested(knownvalue.SetExact([]knownvalue.Check{str("DE")}), "destination", "country_codes"),
					fsNested(knownvalue.Null(), "destination", "all"),
					fsNested(str("50"), "minimum_requirement", "subtotal"),
					fsNested(knownvalue.Bool(true), "combines_with", "product_discounts"),
					fsNested(knownvalue.Bool(true), "context", "all"),
					// Shopify's defaults for a shop without subscriptions, read back as computed.
					fsValue("applies_on_one_time_purchase", knownvalue.Bool(true)),
					fsValue("applies_on_subscription", knownvalue.Bool(false)),
				},
			},
			{Config: created, ConfigPlanChecks: emptyPlan},
			{
				// Update in place: rename, code rename, dates, destination all, maximum and minimum changed.
				Config: toAll,
				Check: func(*terraform.State) error {
					if byCode(t, code) != nil {
						return fmt.Errorf("old code %s still exists", code)
					}
					d := codeFreeShipping(t, byCode(t, renamed))
					if _, ok := d.DestinationSelection.(*client.DiscountDestinationFieldsDiscountCountryAll); !ok {
						return fmt.Errorf("live destination = %#v, want all countries", d.DestinationSelection)
					}
					if d.MaximumShippingPrice == nil || d.MaximumShippingPrice.Amount != "25.5" || d.UsageLimit == nil || *d.UsageLimit != 9 || !d.AppliesOncePerCustomer {
						return fmt.Errorf("live discount = %+v", d)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					sameID.AddStateValue(fsAddr, tfjsonpath.New("id")),
					fsValue("title", str(title+" b")),
					fsValue("code", str(renamed)),
					fsNested(knownvalue.Bool(true), "destination", "all"),
					fsNested(knownvalue.Null(), "destination", "country_codes"),
					fsNested(knownvalue.Int64Exact(2), "minimum_requirement", "quantity"),
					fsNested(knownvalue.Null(), "minimum_requirement", "subtotal"),
					fsValue("ends_at", str("2027-01-31T23:59:59+01:00")),
				},
			},
			{Config: toAll, ConfigPlanChecks: emptyPlan},
			{
				// {} removes the minimum; back to countries.
				Config: back,
				Check: func(*terraform.State) error {
					d := codeFreeShipping(t, byCode(t, renamed))
					if d.MinimumRequirement != nil && *d.MinimumRequirement != nil {
						return fmt.Errorf("live minimum = %#v, want none", *d.MinimumRequirement)
					}
					if c, ok := d.DestinationSelection.(*client.DiscountDestinationFieldsDiscountCountries); !ok || len(c.Countries) != 1 {
						return fmt.Errorf("live destination = %#v, want DE", d.DestinationSelection)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					fsNested(knownvalue.SetExact([]knownvalue.Check{str("DE")}), "destination", "country_codes"),
					fsNested(knownvalue.Null(), "minimum_requirement", "quantity"),
				},
			},
			{Config: back, ConfigPlanChecks: emptyPlan},
			{
				// Review Focus 2: maximum_shipping_price, destination and combines_with changed through the client while unset in config.
				PreConfig: func() {
					n := byCode(t, renamed)
					yes, no := true, false
					res, err := client.UpdateDiscountCodeFreeShipping(context.Background(), acctest.Client(t), n.Id, client.DiscountCodeFreeShippingInput{
						MaximumShippingPrice: &clientMax,
						Destination:          &client.DiscountShippingDestinationSelectionInput{All: &yes},
						CombinesWith:         &client.DiscountCombinesWithInput{ProductDiscounts: &no, OrderDiscounts: &yes, ShippingDiscounts: &no},
					})
					if err != nil || res.DiscountCodeFreeShippingUpdate == nil || len(res.DiscountCodeFreeShippingUpdate.UserErrors) > 0 {
						t.Fatalf("discountCodeFreeShippingUpdate: %v %+v", err, res)
					}
				},
				Config:           unmanaged,
				ConfigPlanChecks: emptyPlan,
				ConfigStateChecks: []statecheck.StateCheck{
					fsValue("maximum_shipping_price", str("40.0")),
					fsNested(knownvalue.Bool(true), "destination", "all"),
					fsNested(knownvalue.Bool(true), "combines_with", "order_discounts"),
				},
			},
			{RefreshState: true},
			{Config: unmanaged, ConfigStateChecks: []statecheck.StateCheck{statecheck.ExpectIdentity(fsAddr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(codeGIDRe)})}},
			{Config: imported, ResourceName: fsAddr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				// Generated config must pass validation and plan as import-only.
				Config:          "",
				ResourceName:    fsAddr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				Config:                  imported,
				ResourceName:            fsAddr,
				ImportState:             true,
				ImportStateId:           "code/" + renamed,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"starts_at", "ends_at"},
			},
			{
				// Removing the code replaces the discount, with a warning about the usage count.
				Config:           automatic,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(fsAddr, plancheck.ResourceActionDestroyBeforeCreate)}},
				Check: func(*terraform.State) error {
					if byCode(t, renamed) != nil {
						return fmt.Errorf("code discount %s survived the replace", renamed)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					fsValue("id", knownvalue.StringRegexp(automaticGIDRe)),
					fsValue("code", knownvalue.Null()),
					fsValue("usage_limit", knownvalue.Null()),
					fsNested(knownvalue.Bool(true), "destination", "all"),
				},
			},
		},
	})
}

func TestAccDiscountFreeShipping_automatic(t *testing.T) {
	acctest.Setup(t)
	// A slash in the title checks that the import key splits at the first one.
	title := testTitle() + "/50"
	cleanup(t, nil, []string{title}, nil)
	cfg := freeShipping(fmt.Sprintf(`
  title               = %q
  minimum_requirement = { subtotal = "50" }
`, title))
	duplicate := cfg + fmt.Sprintf(`
resource "shopify_discount_free_shipping" "duplicate" {
  title      = %q
  depends_on = [shopify_discount_free_shipping.test]
}
`, title)
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
					d := automaticFreeShipping(t, n[0])
					if _, ok := d.DestinationSelection.(*client.DiscountDestinationFieldsDiscountCountryAll); !ok || d.MaximumShippingPrice != nil {
						return fmt.Errorf("live defaults = %#v, %+v; want all countries and no maximum", d.DestinationSelection, d.MaximumShippingPrice)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					fsValue("id", knownvalue.StringRegexp(automaticGIDRe)),
					fsValue("code", knownvalue.Null()),
					fsValue("usage_limit", knownvalue.Null()),
					fsValue("maximum_shipping_price", knownvalue.Null()),
					fsValue("status", str("ACTIVE")),
					fsNested(knownvalue.Bool(true), "destination", "all"),
					fsNested(str("50"), "minimum_requirement", "subtotal"),
				},
			},
			{Config: cfg, ConfigPlanChecks: emptyPlan},
			{Config: duplicate, ExpectError: regexp.MustCompile(`(?i)title must be unique`)},
			{
				Config:                  cfg,
				ResourceName:            fsAddr,
				ImportState:             true,
				ImportStateId:           "automatic/" + title,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"starts_at", "minimum_requirement.subtotal"},
			},
			{
				Config:          "",
				ResourceName:    fsAddr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
		},
	})
}

// A discount of another method is refused with the resource that manages it (issue #62).
func TestAccDiscountFreeShipping_wrongResource(t *testing.T) {
	acctest.Setup(t)
	title := testTitle()
	cleanup(t, nil, []string{title}, nil)
	cfg := freeShipping(fmt.Sprintf("title = %q", title))
	basicCfg := basic(`
  title         = "unused"
  customer_gets = { value = { percentage = 0.1 }, items = { all = true } }
`)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, nil, []string{title}, nil),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				Config:        cfg + strings.Replace(basicCfg, `"test"`, `"other"`, 1),
				ResourceName:  "shopify_discount_basic.other",
				ImportState:   true,
				ImportStateId: "automatic/" + title,
				ExpectError:   regexp.MustCompile(`use\s+shopify_discount_free_shipping`),
			},
		},
	})
}
