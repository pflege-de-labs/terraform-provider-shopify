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

// seo_title and seo_description share one productSet object: leaving one out must keep its admin value.
func TestAccProduct_unmanagedSEODescription(t *testing.T) {
	accSetup(t)
	handle := testHandle()
	cleanup(t, handle)
	cfg := func(seoTitle string) string {
		return config(fmt.Sprintf(`
  handle              = %q
  title               = "tf-acc seo"
  status              = "DRAFT"
  seo_title           = %q
  allow_data_deletion = true
`, handle, seoTitle))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{Config: cfg("tf-acc seo 1"), ConfigStateChecks: []statecheck.StateCheck{stateValue("seo_description", knownvalue.Null())}},
			{
				PreConfig: func() {
					d := "tf-acc admin seo description"
					mustSet(t, &mustLookup(t, handle).Id, client.ProductSetInput{Seo: &client.SEOInput{Description: &d}})
				},
				Config:            cfg("tf-acc seo 1"),
				ConfigPlanChecks:  resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				ConfigStateChecks: []statecheck.StateCheck{stateValue("seo_description", str("tf-acc admin seo description"))},
			},
			{
				Config: cfg("tf-acc seo 2"),
				Check: func(*terraform.State) error {
					p := mustLookup(t, handle)
					if p.Seo.Title == nil || *p.Seo.Title != "tf-acc seo 2" {
						return fmt.Errorf("seo title = %v, want tf-acc seo 2", p.Seo.Title)
					}
					if p.Seo.Description == nil || *p.Seo.Description != "tf-acc admin seo description" {
						return fmt.Errorf("seo description = %v after an update that left it out, want the admin value kept", p.Seo.Description)
					}
					return nil
				},
			},
		},
	})
}

var variantGIDRe = regexp.MustCompile(`^gid://shopify/ProductVariant/\d+$`)

// variantIDs maps each live variant's option values ("S/Red", "" for the default variant) to its variant and inventory item GIDs.
func variantIDs(t *testing.T, handle string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, v := range mustLookup(t, handle).Variants.Nodes {
		var vals []string
		for _, o := range v.SelectedOptions {
			vals = append(vals, o.Value)
		}
		key := strings.Join(vals, "/")
		if v.SelectedOptions[0].Value == "Default Title" {
			key = ""
		}
		out[key] = v.Id + " " + v.InventoryItem.Id
	}
	return out
}

// sameIDs checks that the variants in rename (old key to new key) kept their GIDs from *before, read at check time, and only added are new.
func sameIDs(t *testing.T, handle string, before *map[string]string, rename map[string]string, added ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		now := variantIDs(t, handle)
		for old, cur := range rename {
			if now[cur] == "" || now[cur] != (*before)[old] {
				return fmt.Errorf("variant %q is now %q with GIDs %q, want the GIDs %q it had", old, cur, now[cur], (*before)[old])
			}
		}
		if len(now) != len(rename)+len(added) {
			return fmt.Errorf("live variants %v, want %d", now, len(rename)+len(added))
		}
		for _, a := range added {
			if now[a] == "" {
				return fmt.Errorf("variant %q missing in %v", a, now)
			}
		}
		return nil
	}
}

// setVariantSKU changes one variant's SKU outside Terraform; productSet needs every option and variant, by ID, to keep them.
func setVariantSKU(t *testing.T, handle, value, sku string) {
	t.Helper()
	p := mustLookup(t, handle)
	in := client.ProductSetInput{}
	for _, o := range p.Options {
		oi := client.OptionSetInput{Id: &o.Id}
		for _, v := range o.OptionValues {
			oi.Values = append(oi.Values, client.OptionValueSetInput{Id: &v.Id})
		}
		in.ProductOptions = append(in.ProductOptions, oi)
	}
	found := false
	for _, v := range p.Variants.Nodes {
		vi := client.ProductVariantSetInput{Id: &v.Id}
		for _, so := range v.SelectedOptions {
			vi.OptionValues = append(vi.OptionValues, client.VariantOptionValueInput{OptionName: &so.Name, Name: &so.Value})
			if so.Value == value {
				vi.Sku, found = &sku, true
			}
		}
		in.Variants = append(in.Variants, vi)
	}
	if !found {
		t.Fatalf("product %s has no variant with value %q", handle, value)
	}
	mustSet(t, &p.Id, in)
}

func variantValue(i int, attr string, check knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(addr, tfjsonpath.New("variants").AtSliceIndex(i).AtMapKey(attr), check)
}

func TestAccProduct_variants(t *testing.T) {
	accSetup(t)
	handle := testHandle()
	cleanup(t, handle)
	// Prices use Shopify's spelling so the import steps plan empty; TestAccProduct_defaultVariant covers other spellings.
	cfg := func(allow bool, options, variants string) string {
		return config(fmt.Sprintf(`
  handle              = %q
  title               = "tf-acc variants"
  status              = "DRAFT"
  allow_data_deletion = %t
  options             = %s
  variants            = %s
`, handle, allow, options, variants))
	}
	small := `{
    option_values     = { Size = "S" }
    price             = "10.00"
    sku               = "TF-S"
    barcodes          = [{ value = "4006381333931", type = "EAN" }]
    inventory_policy  = "CONTINUE"
    taxable           = false
    tracked           = true
    requires_shipping = false
    weight            = { value = 0.4, unit = "KILOGRAMS" }
  }`
	two := cfg(false, `[{ name = "Size", values = ["S", "M"] }]`,
		`[`+small+`, { option_values = { Size = "M" }, price = "12.50", compare_at_price = "15.00" }]`)
	three := cfg(false, `[{ name = "Size", values = ["S", "M", "L"] }]`,
		`[`+small+`, { option_values = { Size = "M" }, price = "12.50", compare_at_price = "15.00" }, { option_values = { Size = "L" }, price = "14.00" }]`)
	smallRenamed := strings.Replace(small, `Size = "S"`, `Größe = "S"`, 1)
	renamed := func(allow bool, withL bool) string {
		values, l := `["S", "Medium"]`, ""
		if withL {
			values, l = `["S", "Medium", "L"]`, `, { option_values = { Größe = "L" }, price = "14.00" }`
		}
		return cfg(allow, `[{ name = "Größe", values = `+values+` }]`,
			`[`+smallRenamed+`, { option_values = { Größe = "Medium" }, price = "12.50", compare_at_price = "15.00" }`+l+`]`)
	}
	// XS goes before S: S and Medium keep their names and variants, XS is added.
	withXS := cfg(true, `[{ name = "Größe", values = ["XS", "S", "Medium"] }]`,
		`[{ option_values = { Größe = "XS" }, price = "9.00" }, `+smallRenamed+`, { option_values = { Größe = "Medium" }, price = "12.50", compare_at_price = "15.00" }]`)
	var ids map[string]string
	gid := knownvalue.StringRegexp(variantGIDRe)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{
				Config: two,
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("options", knownvalue.ListExact([]knownvalue.Check{knownvalue.ObjectExact(map[string]knownvalue.Check{
						"name": str("Size"), "values": knownvalue.ListExact([]knownvalue.Check{str("S"), str("M")}),
					})})),
					variantValue(0, "id", gid),
					variantValue(0, "inventory_item_id", knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/InventoryItem/\d+$`))),
					variantValue(0, "option_values", knownvalue.MapExact(map[string]knownvalue.Check{"Size": str("S")})),
					variantValue(0, "price", str("10.00")),
					variantValue(0, "sku", str("TF-S")),
					variantValue(0, "barcodes", knownvalue.ListExact([]knownvalue.Check{knownvalue.ObjectExact(map[string]knownvalue.Check{
						"value": str("4006381333931"), "type": str("EAN"),
					})})),
					variantValue(0, "inventory_policy", str("CONTINUE")),
					variantValue(0, "taxable", knownvalue.Bool(false)),
					variantValue(0, "tracked", knownvalue.Bool(true)),
					variantValue(0, "requires_shipping", knownvalue.Bool(false)),
					variantValue(0, "weight", knownvalue.ObjectExact(map[string]knownvalue.Check{
						"value": knownvalue.Float64Exact(0.4), "unit": str("KILOGRAMS"),
					})),
					variantValue(0, "compare_at_price", knownvalue.Null()),
					variantValue(1, "compare_at_price", str("15.00")),
					variantValue(1, "sku", knownvalue.Null()),
					variantValue(1, "inventory_policy", str("DENY")),
				},
				Check: func(*terraform.State) error {
					ids = variantIDs(t, handle)
					return nil
				},
			},
			{
				// Review focus 2: sku is unset on M, so an admin edit to it plans nothing.
				PreConfig:         func() { setVariantSKU(t, handle, "M", "TF-ADMIN") },
				Config:            two,
				ConfigPlanChecks:  resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				ConfigStateChecks: []statecheck.StateCheck{variantValue(1, "sku", str("TF-ADMIN"))},
			},
			{
				Config: three,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
					plancheck.ExpectUnknownValue(addr, tfjsonpath.New("variants").AtSliceIndex(2).AtMapKey("id")),
				}},
				ConfigStateChecks: []statecheck.StateCheck{variantValue(2, "id", gid), variantValue(1, "sku", str("TF-ADMIN"))},
				Check:             sameIDs(t, handle, &ids, map[string]string{"S": "S", "M": "M"}, "L"),
			},
			{
				Config: renamed(false, true),
				Check: func(s *terraform.State) error {
					if err := sameIDs(t, handle, &ids, map[string]string{"S": "S", "M": "Medium"}, "L")(s); err != nil {
						return err
					}
					ids = variantIDs(t, handle)
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					variantValue(1, "option_values", knownvalue.MapExact(map[string]knownvalue.Check{"Größe": str("Medium")})),
					variantValue(1, "sku", str("TF-ADMIN")),
				},
			},
			{
				// Removing L deletes its inventory item, so it needs allow_data_deletion.
				Config:      renamed(false, false),
				ExpectError: regexp.MustCompile(`allow_data_deletion = true`),
			},
			{
				// The planned flag counts for variant removal, so one apply sets it and removes L.
				Config: renamed(true, false),
				Check:  sameIDs(t, handle, &ids, map[string]string{"S": "S", "Medium": "Medium"}),
			},
			{
				Config: withXS,
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("options", knownvalue.ListExact([]knownvalue.Check{knownvalue.ObjectExact(map[string]knownvalue.Check{
						"name": str("Größe"), "values": knownvalue.ListExact([]knownvalue.Check{str("XS"), str("S"), str("Medium")}),
					})})),
					variantValue(0, "option_values", knownvalue.MapExact(map[string]knownvalue.Check{"Größe": str("XS")})),
				},
				Check: sameIDs(t, handle, &ids, map[string]string{"S": "S", "Medium": "Medium"}, "XS"),
			},
			{
				Config:          strings.Replace(withXS, "  allow_data_deletion = true\n", "", 1),
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        handle,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "handle",
				ImportStateVerifyIgnore:              []string{"adopt_existing", "allow_data_deletion"},
			},
		},
	})
}

// A product without options has one default variant: options = [] manages it, and adding options keeps it.
func TestAccProduct_defaultVariant(t *testing.T) {
	accSetup(t)
	handle := testHandle()
	cleanup(t, handle)
	cfg := func(extra string) string {
		return config(fmt.Sprintf(`
  handle              = %q
  title               = "tf-acc default variant"
  status              = "DRAFT"
  allow_data_deletion = true
%s
`, handle, extra))
	}
	asDefault := cfg(`
  options  = []
  variants = [{ option_values = {}, price = "5", weight = { value = 250, unit = "GRAMS" } }]
`)
	withOptions := cfg(`
  options  = [{ name = "Size", values = ["S", "M"] }]
  variants = [{ option_values = { Size = "S" }, price = "5" }, { option_values = { Size = "M" }, price = "6.5" }]
`)
	var ids map[string]string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{
				Config:      cfg(`variants = [{ option_values = {} }]`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`options`),
			},
			{
				Config:      cfg(`options = []` + "\n" + `variants = [{ option_values = {} }, { option_values = {} }]`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`exactly one variant`),
			},
			{
				Config:      cfg(`options = [{ name = "Size", values = ["S"] }]` + "\n" + `variants = [{ option_values = { Size = "M" } }]`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`not one of the values`),
			},
			{
				Config:      cfg(`options = [{ name = "Size", values = ["S"] }]` + "\n" + `variants = [{ option_values = { Size = "S" }, price = "1,50" }]`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`price`),
			},
			{
				// Unset options and variants are unmanaged and read as Shopify's default variant.
				Config: cfg(""),
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("options", knownvalue.ListExact([]knownvalue.Check{})),
					stateValue("variants", knownvalue.ListSizeExact(1)),
					variantValue(0, "option_values", knownvalue.MapExact(map[string]knownvalue.Check{})),
					variantValue(0, "price", str("0.00")),
				},
				Check: func(*terraform.State) error {
					ids = variantIDs(t, handle)
					return nil
				},
			},
			{
				// The configured spelling stays in state, and the default variant is updated in place.
				Config: asDefault,
				ConfigStateChecks: []statecheck.StateCheck{
					variantValue(0, "price", str("5")),
					variantValue(0, "weight", knownvalue.ObjectExact(map[string]knownvalue.Check{"value": knownvalue.Float64Exact(250), "unit": str("GRAMS")})),
				},
				Check: func(*terraform.State) error {
					if got := mustLookup(t, handle).Variants.Nodes[0].Price; got != "5.00" {
						return fmt.Errorf("live price = %q, want 5.00", got)
					}
					return sameIDs(t, handle, &ids, map[string]string{"": ""})(nil)
				},
			},
			{
				Config:            withOptions,
				ConfigStateChecks: []statecheck.StateCheck{variantValue(1, "price", str("6.5"))},
				Check:             sameIDs(t, handle, &ids, map[string]string{"": "S"}, "M"),
			},
			{
				// Back to no options: the first variant becomes the default one, M is removed.
				Config: asDefault,
				Check:  sameIDs(t, handle, &ids, map[string]string{"": ""}),
			},
		},
	})
}
