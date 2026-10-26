package product_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

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

const addr = "shopify_product.test"

var gidRe = regexp.MustCompile(`^gid://shopify/Product/\d+$`)

var expectIdentity = statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(gidRe)})

// testHandle keeps test products apart from real ones.
func testHandle() string {
	return "tf-acc-product-" + strings.ToLower(sdkacctest.RandStringFromCharSet(6, sdkacctest.CharSetAlphaNum))
}

func config(body string) string {
	return fmt.Sprintf("resource \"shopify_product\" \"test\" {\n%s\n}\n", body)
}

// accSetup skips without TF_ACC, because these tests call the API before resource.Test would skip them.
func accSetup(t *testing.T) {
	t.Helper()
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skip("Acceptance tests skipped unless env 'TF_ACC' set")
	}
	acctest.PreCheck(t)
}

// lookup returns nil when no product has the handle.
func lookup(t *testing.T, handle string) *client.ProductFields {
	t.Helper()
	res, err := client.GetProductByHandle(context.Background(), acctest.Client(t), handle)
	if err != nil {
		t.Fatal(err)
	}
	if res.ProductByIdentifier == nil {
		return nil
	}
	return &res.ProductByIdentifier.ProductFields
}

func mustLookup(t *testing.T, handle string) *client.ProductFields {
	t.Helper()
	p := lookup(t, handle)
	if p == nil {
		t.Fatalf("product %q does not exist", handle)
	}
	return p
}

// mustSet writes a product through the client, outside Terraform.
func mustSet(t *testing.T, id *string, in client.ProductSetInput) *client.ProductFields {
	t.Helper()
	var ident *client.ProductSetIdentifiers
	if id != nil {
		ident = &client.ProductSetIdentifiers{Id: id}
	}
	res, err := client.SetProduct(context.Background(), acctest.Client(t), ident, in)
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.ProductSet.UserErrors; len(errs) > 0 {
		t.Fatalf("productSet: %v", errs)
	}
	if res.ProductSet.Product == nil {
		t.Fatal("productSet returned no product")
	}
	return &res.ProductSet.Product.ProductFields
}

func deleteByID(t *testing.T, id string) error {
	t.Helper()
	res, err := client.DeleteProduct(context.Background(), acctest.Client(t), id)
	if err != nil {
		return err
	}
	if errs := res.ProductDelete.UserErrors; len(errs) > 0 {
		return fmt.Errorf("productDelete %s: %v", id, errs)
	}
	return nil
}

// cleanup deletes the products with these handles when the test ends, whatever state they are in.
func cleanup(t *testing.T, handles ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, h := range handles {
			if p := lookup(t, h); p != nil {
				if err := deleteByID(t, p.Id); err != nil {
					t.Error(err)
				}
			}
		}
	})
}

// checkGone fails, and removes the leftovers, when a test product still exists.
func checkGone(t *testing.T, handles ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for _, h := range handles {
			if p := lookup(t, h); p != nil {
				_ = deleteByID(t, p.Id)
				return fmt.Errorf("product %s still exists", h)
			}
		}
		return nil
	}
}

func str(s string) knownvalue.Check { return knownvalue.StringExact(s) }

func stateValue(path string, check knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(addr, tfjsonpath.New(path), check)
}

func TestAccProduct_basic(t *testing.T) {
	accSetup(t)
	handle := testHandle()
	renamed := handle + "-b"
	cleanup(t, handle, renamed)
	create := config(fmt.Sprintf(`
  handle              = %q
  title               = "tf-acc product"
  status              = "DRAFT"
  vendor              = "tf-acc vendor"
  product_type        = "tf-acc type"
  tags                = ["tf-acc-b", "tf-acc-a"]
  description_html    = "<p>first</p>"
  seo_title           = "tf-acc seo"
  allow_data_deletion = true
`, handle))
	updated := config(fmt.Sprintf(`
  handle              = %q
  title               = "tf-acc product (updated)"
  status              = "ARCHIVED"
  vendor              = "tf-acc vendor"
  product_type        = "tf-acc type"
  tags                = ["tf-acc-c"]
  description_html    = "<p>second</p>"
  seo_title           = "tf-acc seo"
  seo_description     = "tf-acc seo description"
  allow_data_deletion = true
`, renamed))
	// Without allow_data_deletion = true, which an import can't know, so the import plans as a no-op.
	imported := strings.Replace(updated, "  allow_data_deletion = true\n", "", 1)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle, renamed),
		Steps: []resource.TestStep{
			{
				Config:      config(`handle = "Upper Case"` + "\n" + `title = "x"`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`upper-case`),
			},
			{
				Config: create,
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("id", knownvalue.StringRegexp(gidRe)),
					stateValue("handle", str(handle)),
					stateValue("status", str("DRAFT")),
					stateValue("vendor", str("tf-acc vendor")),
					stateValue("tags", knownvalue.SetExact([]knownvalue.Check{str("tf-acc-a"), str("tf-acc-b")})),
					stateValue("description_html", str("<p>first</p>")),
					stateValue("seo_title", str("tf-acc seo")),
					stateValue("seo_description", knownvalue.Null()),
					stateValue("category_id", knownvalue.Null()),
					stateValue("gift_card", knownvalue.Bool(false)),
					stateValue("adopt_existing", knownvalue.Bool(false)),
				},
			},
			{
				// The handle changes in place.
				Config: updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("handle", str(renamed)),
					stateValue("title", str("tf-acc product (updated)")),
					stateValue("status", str("ARCHIVED")),
					stateValue("tags", knownvalue.SetExact([]knownvalue.Check{str("tf-acc-c")})),
					stateValue("seo_description", str("tf-acc seo description")),
				},
			},
			{
				Config:           updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{RefreshState: true},
			{
				// Refresh steps can't carry state checks; this no-op step checks identity survives the refresh.
				Config:            updated,
				ConfigStateChecks: []statecheck.StateCheck{expectIdentity},
			},
			{Config: imported, ResourceName: addr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				// Generated config must pass validation and plan as import-only, without adopt_existing.
				Config:          imported,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        renamed,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "handle",
				ImportStateVerifyIgnore:              []string{"adopt_existing", "allow_data_deletion"},
			},
		},
	})
}

// Shopify repairs HTML; the repaired text must not show as a diff, but an admin edit must.
func TestAccProduct_htmlNormalisation(t *testing.T) {
	accSetup(t)
	handle := testHandle()
	cleanup(t, handle)
	const broken = "<p>Hi<br>there<b>bold</p>"
	const repaired = "<p>Hi<br>there<b>bold</b></p>"
	cfg := config(fmt.Sprintf(`
  handle              = %q
  title               = "tf-acc html"
  status              = "DRAFT"
  description_html    = %q
  allow_data_deletion = true
`, handle, broken))
	emptyPlan := resource.ConfigPlanChecks{
		PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
		PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
		PostApplyPreRefresh:  []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: func(*terraform.State) error {
					if got := mustLookup(t, handle).DescriptionHtml; got != repaired {
						return fmt.Errorf("live description = %q, want Shopify's repaired %q", got, repaired)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{stateValue("description_html", str(broken))},
			},
			{Config: cfg, ConfigPlanChecks: emptyPlan},
			{
				PreConfig: func() {
					edited := "<p>edited in the admin</p>"
					mustSet(t, &mustLookup(t, handle).Id, client.ProductSetInput{DescriptionHtml: &edited})
				},
				Config:             cfg,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{Config: cfg, ConfigStateChecks: []statecheck.StateCheck{stateValue("description_html", str(broken))}},
			{Config: cfg, ConfigPlanChecks: emptyPlan},
		},
	})
}

// Unset attributes are unmanaged: an admin edit to them must not plan, and an update must not send them.
func TestAccProduct_unmanagedVendor(t *testing.T) {
	accSetup(t)
	handle := testHandle()
	cleanup(t, handle)
	withVendor := config(fmt.Sprintf(`
  handle              = %q
  title               = "tf-acc vendor"
  status              = "DRAFT"
  vendor              = "tf-acc v1"
  allow_data_deletion = true
`, handle))
	withoutVendor := func(title string) string {
		return config(fmt.Sprintf(`
  handle              = %q
  title               = %q
  status              = "DRAFT"
  allow_data_deletion = true
`, handle, title))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{Config: withVendor, ConfigStateChecks: []statecheck.StateCheck{stateValue("vendor", str("tf-acc v1"))}},
			{
				PreConfig: func() {
					v := "tf-acc v2"
					mustSet(t, &mustLookup(t, handle).Id, client.ProductSetInput{Vendor: &v})
				},
				Config:            withoutVendor("tf-acc vendor"),
				ConfigPlanChecks:  resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				ConfigStateChecks: []statecheck.StateCheck{stateValue("vendor", str("tf-acc v2"))},
			},
			{
				Config: withoutVendor("tf-acc vendor (renamed)"),
				Check: func(*terraform.State) error {
					if got := mustLookup(t, handle).Vendor; got != "tf-acc v2" {
						return fmt.Errorf("vendor = %q after an update, want the admin value tf-acc v2 left alone", got)
					}
					return nil
				},
			},
		},
	})
}

// Pattern A: an adopted product is updated to the config, and destroy leaves it in Shopify.
func TestAccProduct_adoptExisting(t *testing.T) {
	accSetup(t)
	handle := testHandle()
	title, vendor, productType := "tf-acc original", "tf-acc original vendor", "tf-acc original type"
	existing := mustSet(t, nil, client.ProductSetInput{
		Handle: &handle, Title: &title, Vendor: &vendor, ProductType: &productType,
	})
	cleanup(t, handle)
	cfg := func(adopt bool) string {
		return config(fmt.Sprintf(`
  handle         = %q
  title          = "tf-acc adopted"
  status         = "DRAFT"
  adopt_existing = %t
`, handle, adopt))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			// Review focus 1: destroy of an adopted product must leave it exactly as the last apply wrote it.
			p := lookup(t, handle)
			switch {
			case p == nil:
				return fmt.Errorf("adopted product %s was deleted by destroy, want it kept", handle)
			case p.Id != existing.Id || p.Title != "tf-acc adopted" || p.Vendor != vendor || p.ProductType != productType:
				return fmt.Errorf("adopted product changed on destroy: %+v", p)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: cfg(false), ExpectError: regexp.MustCompile(`(?i)already`)},
			{
				Config: cfg(true),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("id", str(existing.Id)),
					stateValue("title", str("tf-acc adopted")),
					stateValue("vendor", str(vendor)),
					stateValue("product_type", str(productType)),
					stateValue("adopt_existing", knownvalue.Bool(true)),
				},
			},
		},
	})
}

// Pattern B: destroy fails unless allow_data_deletion is true in state; gift_card forces a replace.
func TestAccProduct_deletionGuard(t *testing.T) {
	accSetup(t)
	handle := testHandle()
	cleanup(t, handle)
	cfg := func(allow bool, extra string) string {
		return config(fmt.Sprintf(`
  handle              = %q
  title               = "tf-acc guard"
  status              = "DRAFT"
  allow_data_deletion = %t
%s
`, handle, allow, extra))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{Config: cfg(false, ""), ConfigStateChecks: []statecheck.StateCheck{stateValue("allow_data_deletion", knownvalue.Bool(false))}},
			{Config: cfg(false, ""), Destroy: true, ExpectError: regexp.MustCompile(`allow_data_deletion = true`)},
			{
				// A replace would delete the product, so it is planned but would fail the same guard.
				Config:             cfg(false, "gift_card = true"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: cfg(false, "gift_card = true"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				ExpectError: regexp.MustCompile(`allow_data_deletion = true`),
			},
			{
				// The flag must be in state before the delete runs, so it is set in a separate apply.
				Config:            cfg(true, ""),
				ConfigStateChecks: []statecheck.StateCheck{stateValue("allow_data_deletion", knownvalue.Bool(true))},
			},
		},
	})
}
