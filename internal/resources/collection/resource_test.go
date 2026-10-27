package collection_test

import (
	"context"
	"encoding/json"
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

const addr = "shopify_collection.test"

var (
	gidRe        = regexp.MustCompile(`^gid://shopify/Collection/\d+$`)
	sourceRe     = regexp.MustCompile(`^gid://shopify/CollectionConditionsSource/\d+$`)
	productGIDRe = regexp.MustCompile(`^gid://shopify/Product/\d+$`)
)

var expectIdentity = statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(gidRe)})

// testHandle keeps test collections apart from real ones.
func testHandle() string {
	return "tf-acc-collection-" + strings.ToLower(sdkacctest.RandStringFromCharSet(6, sdkacctest.CharSetAlphaNum))
}

// accSetup skips without TF_ACC, because these tests call the API before resource.Test would skip them.
func accSetup(t *testing.T) {
	t.Helper()
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skip("Acceptance tests skipped unless env 'TF_ACC' set")
	}
	acctest.PreCheck(t)
}

// lookup returns nil when no collection has the handle.
func lookup(t *testing.T, handle string) *client.CollectionFields {
	t.Helper()
	res, err := client.GetCollectionByHandle(context.Background(), acctest.Client(t), handle)
	if err != nil {
		t.Fatal(err)
	}
	if res.CollectionByIdentifier == nil {
		return nil
	}
	return &res.CollectionByIdentifier.CollectionFields
}

func mustLookup(t *testing.T, handle string) *client.CollectionFields {
	t.Helper()
	c := lookup(t, handle)
	if c == nil {
		t.Fatalf("collection %q does not exist", handle)
	}
	return c
}

// liveSource is the one source of a test collection, as the API returns it.
type liveSource struct {
	ID        string
	Inclusion struct {
		MatchType  *string
		Conditions []json.RawMessage
		Selections struct {
			Nodes []struct{ Product struct{ ID string } }
		}
	}
}

func mustSource(t *testing.T, c *client.CollectionFields) liveSource {
	t.Helper()
	var sources []liveSource
	if err := json.Unmarshal(c.Sources, &sources); err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("collection %s has %d sources, want 1", c.Handle, len(sources))
	}
	return sources[0]
}

// mustCreate makes a collection through the client, outside Terraform.
func mustCreate(t *testing.T, in client.CollectionCreateInput) {
	t.Helper()
	res, err := client.CreateCollection(context.Background(), acctest.Client(t), in)
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.CollectionCreate.UserErrors; len(errs) > 0 {
		t.Fatalf("collectionCreate: %v", errs)
	}
	if res.CollectionCreate.Collection == nil {
		t.Fatal("collectionCreate returned no collection")
	}
}

// mustUpdate changes a collection through the client, outside Terraform.
func mustUpdate(t *testing.T, in client.CollectionUpdateInput) {
	t.Helper()
	res, err := client.UpdateCollection(context.Background(), acctest.Client(t), in)
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.CollectionUpdate.UserErrors; len(errs) > 0 {
		t.Fatalf("collectionUpdate: %v", errs)
	}
	if res.CollectionUpdate.Collection == nil {
		t.Fatal("collectionUpdate returned no collection")
	}
}

func deleteByID(t *testing.T, id string) error {
	t.Helper()
	res, err := client.DeleteCollection(context.Background(), acctest.Client(t), id)
	if err != nil {
		return err
	}
	if errs := res.CollectionDelete.UserErrors; len(errs) > 0 {
		return fmt.Errorf("collectionDelete %s: %v", id, errs)
	}
	return nil
}

func productID(t *testing.T, handle string) string {
	t.Helper()
	res, err := client.GetProductByHandle(context.Background(), acctest.Client(t), handle)
	if err != nil {
		t.Fatal(err)
	}
	if res.ProductByIdentifier == nil {
		return ""
	}
	return res.ProductByIdentifier.Id
}

func deleteProduct(t *testing.T, id string) error {
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

// cleanup deletes the test collections and products with these handles when the test ends, whatever state they are in.
func cleanup(t *testing.T, handles ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, h := range handles {
			if c := lookup(t, h); c != nil {
				if err := deleteByID(t, c.Id); err != nil {
					t.Error(err)
				}
			}
			if id := productID(t, h); id != "" {
				if err := deleteProduct(t, id); err != nil {
					t.Error(err)
				}
			}
		}
	})
}

// checkGone fails, and removes the leftovers, when a test collection or product still exists.
func checkGone(t *testing.T, handles ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for _, h := range handles {
			if c := lookup(t, h); c != nil {
				_ = deleteByID(t, c.Id)
				return fmt.Errorf("collection %s still exists", h)
			}
			if id := productID(t, h); id != "" {
				_ = deleteProduct(t, id)
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

// product is the test product the collections select, managed in the same configuration.
func product(handle string) string {
	return fmt.Sprintf(`
resource "shopify_product" "p" {
  handle              = %q
  title               = "tf-acc collection product"
  status              = "DRAFT"
  tags                = ["tf-acc-collection"]
  allow_data_deletion = true
}
`, handle)
}

func config(productHandle, body string) string {
	return product(productHandle) + fmt.Sprintf("\nresource \"shopify_collection\" \"test\" {\n%s\n}\n", body)
}

const (
	tagCondition   = `{ productTag = { relation = "TAGGED_WITH", values = ["tf-acc-collection"], matchType = "ANY" } }`
	priceCondition = `{ variantPrice = { relation = "GREATER_THAN", value = { amount = "5", currencyCode = "EUR" } } }`
)

func TestAccCollection_basic(t *testing.T) {
	accSetup(t)
	acctest.KeepFrontpage(t)
	handle := testHandle()
	renamed := handle + "-b"
	productHandle := handle + "-p"
	cleanup(t, handle, renamed, productHandle)
	const broken = "<p>Hi<br>there<b>bold</p>"
	manual := config(productHandle, fmt.Sprintf(`
  handle           = %q
  title            = "tf-acc collection"
  description_html = %q
  sort_order       = "ALPHA_ASC"
  seo_title        = "tf-acc seo"
  product_ids      = [shopify_product.p.id]
`, handle, broken))
	emptied := config(productHandle, fmt.Sprintf(`
  handle      = %q
  title       = "tf-acc collection"
  sort_order  = "ALPHA_ASC"
  product_ids = []
`, handle))
	smart := func(matchType, conditions, products string) string {
		return config(productHandle, fmt.Sprintf(`
  handle           = %q
  title            = "tf-acc collection (smart)"
  description_html = "<p>smart</p>"
  sort_order       = "ALPHA_ASC"
  match_type       = %q
  conditions       = jsonencode([%s])
  product_ids      = [%s]
`, renamed, matchType, conditions, products))
	}
	smartAll := smart("ALL", tagCondition+", "+priceCondition, "shopify_product.p.id")
	smartAny := smart("ANY", strings.Replace(tagCondition, `"ANY"`, `"ALL"`, 1), "")
	// sort_order and product_ids unset: unmanaged, so out-of-band changes must not plan.
	unmanaged := config(productHandle, fmt.Sprintf(`
  handle     = %q
  title      = "tf-acc collection (smart)"
  match_type = "ANY"
  conditions = jsonencode([%s])
`, renamed, strings.Replace(tagCondition, `"ANY"`, `"ALL"`, 1)))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle, renamed, productHandle),
		Steps: []resource.TestStep{
			{
				Config:      config(productHandle, `handle = "x"`+"\n"+`title = "x"`+"\n"+`conditions = jsonencode([{ productTag = {}, productType = {} }])`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`exactly one key`),
			},
			{
				Config:      config(productHandle, `handle = "x"`+"\n"+`title = "x"`+"\n"+`conditions = jsonencode([`+tagCondition+`])`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Missing match_type`),
			},
			{
				// Manual: one product, and Shopify repairs the HTML without a diff.
				Config: manual,
				Check: func(*terraform.State) error {
					c := mustLookup(t, handle)
					if c.DescriptionHtml != "<p>Hi<br>there<b>bold</b></p>" {
						return fmt.Errorf("live description = %q, want Shopify's repaired HTML", c.DescriptionHtml)
					}
					if s := mustSource(t, c); len(s.Inclusion.Selections.Nodes) != 1 || s.Inclusion.Selections.Nodes[0].Product.ID != productID(t, productHandle) {
						return fmt.Errorf("live selections = %+v, want the test product", s.Inclusion.Selections.Nodes)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("id", knownvalue.StringRegexp(gidRe)),
					stateValue("handle", str(handle)),
					stateValue("description_html", str(broken)),
					stateValue("sort_order", str("ALPHA_ASC")),
					stateValue("seo_title", str("tf-acc seo")),
					stateValue("seo_description", knownvalue.Null()),
					stateValue("template_suffix", knownvalue.Null()),
					stateValue("product_ids", knownvalue.SetExact([]knownvalue.Check{knownvalue.StringRegexp(productGIDRe)})),
					stateValue("source_id", knownvalue.StringRegexp(sourceRe)),
					stateValue("match_type", knownvalue.Null()),
					stateValue("conditions", knownvalue.Null()),
					stateValue("adopt_existing", knownvalue.Bool(false)),
				},
			},
			{Config: manual, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{
				// Removing the last product deletes the source, which Shopify can't keep empty.
				Config: emptied,
				Check: func(*terraform.State) error {
					if c := mustLookup(t, handle); string(c.Sources) != "[]" {
						return fmt.Errorf("live sources = %s, want none", c.Sources)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("product_ids", knownvalue.SetExact([]knownvalue.Check{})),
					stateValue("source_id", knownvalue.Null()),
					// Unset now, and Shopify still holds what it returned at the last write, so the configured text stays.
					stateValue("description_html", str(broken)),
				},
			},
			{
				// Smart, renamed in place, with a hand-picked product too: the first condition creates a source again.
				Config: smartAll,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				Check: func(*terraform.State) error {
					s := mustSource(t, mustLookup(t, renamed))
					if s.Inclusion.MatchType == nil || *s.Inclusion.MatchType != "ALL" || len(s.Inclusion.Conditions) != 2 || len(s.Inclusion.Selections.Nodes) != 1 {
						return fmt.Errorf("live source = %+v, want ALL with 2 conditions and 1 product", s.Inclusion)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("handle", str(renamed)),
					stateValue("match_type", str("ALL")),
					stateValue("source_id", knownvalue.StringRegexp(sourceRe)),
					stateValue("product_ids", knownvalue.SetSizeExact(1)),
					// The configured "5" is kept although Shopify returns "5.0".
					stateValue("conditions", knownvalue.StringRegexp(regexp.MustCompile(`"amount":"5"`))),
				},
			},
			{Config: smartAll, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{
				// One call replaces the conditions, changes the match type and removes the product.
				Config: smartAny,
				Check: func(*terraform.State) error {
					s := mustSource(t, mustLookup(t, renamed))
					if s.Inclusion.MatchType == nil || *s.Inclusion.MatchType != "ANY" || len(s.Inclusion.Conditions) != 1 || len(s.Inclusion.Selections.Nodes) != 0 {
						return fmt.Errorf("live source = %+v, want ANY with 1 condition and no products", s.Inclusion)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("match_type", str("ANY")),
					stateValue("product_ids", knownvalue.SetExact([]knownvalue.Check{})),
					stateValue("conditions", knownvalue.StringExact(`[{"productTag":{"matchType":"ALL","relation":"TAGGED_WITH","values":["tf-acc-collection"]}}]`)),
				},
			},
			{
				// Review Focus 2: sort order and products changed through the client while unset in config.
				PreConfig: func() {
					c := mustLookup(t, renamed)
					order := client.CollectionSortOrderPriceDesc
					mustUpdate(t, client.CollectionUpdateInput{Id: &c.Id, SortOrder: &order, SourcesToUpdate: []client.CollectionUpdateSourceTargetInput{{
						Condition: &client.CollectionUpdateConditionsSourceInput{Id: mustSource(t, c).ID, Inclusion: &client.CollectionUpdateSourceInclusionInput{
							SelectionsToAdd: []client.CollectionInclusionProductSelectionInput{{ProductId: productID(t, productHandle)}},
						}},
					}}})
				},
				Config:           unmanaged,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("sort_order", str("PRICE_DESC")),
					stateValue("product_ids", knownvalue.SetExact([]knownvalue.Check{knownvalue.StringRegexp(productGIDRe)})),
				},
			},
			{RefreshState: true},
			{
				// Refresh steps can't carry state checks; this no-op step checks identity survives the refresh.
				Config:            unmanaged,
				ConfigStateChecks: []statecheck.StateCheck{expectIdentity},
			},
			{Config: unmanaged, ResourceName: addr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				// Generated config must pass validation and plan as import-only, without adopt_existing.
				Config:          product(productHandle),
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				Config:                               unmanaged,
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        renamed,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "handle",
				ImportStateVerifyIgnore:              []string{"adopt_existing"},
			},
		},
	})
}

// Pattern A: an existing collection is only taken over with adopt_existing, and destroy leaves it as Terraform left it.
func TestAccCollection_adopt(t *testing.T) {
	accSetup(t)
	acctest.KeepFrontpage(t)
	handle := testHandle()
	cleanup(t, handle)
	order := client.CollectionSortOrderBestSelling
	mustCreate(t, client.CollectionCreateInput{Title: "tf-acc adopt (client)", Handle: &handle, SortOrder: &order})
	body := func(adopt bool) string {
		return fmt.Sprintf(`
resource "shopify_collection" "test" {
  handle         = %q
  title          = "tf-acc adopt (terraform)"
  adopt_existing = %t
}
`, handle, adopt)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Review Focus 1: destroy only drops state, so the collection is still there, unchanged.
		CheckDestroy: func(*terraform.State) error {
			c := lookup(t, handle)
			switch {
			case c == nil:
				return fmt.Errorf("adopted collection %s was deleted", handle)
			case c.Title != "tf-acc adopt (terraform)" || c.SortOrder != order || string(c.Sources) != "[]":
				return fmt.Errorf("adopted collection changed on destroy: title %q, sort order %s, sources %s", c.Title, c.SortOrder, c.Sources)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: body(false), ExpectError: regexp.MustCompile(`Collection already exists`)},
			{
				Config: body(true),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("title", str("tf-acc adopt (terraform)")),
					// Unset, so the client's value stays.
					stateValue("sort_order", str("BEST_SELLING")),
					stateValue("source_id", knownvalue.Null()),
					stateValue("adopt_existing", knownvalue.Bool(true)),
				},
			},
		},
	})
}

// twoSources are two condition sources, which v1 can't manage.
func twoSources(tag string) []client.CollectionCreateSourceTargetInput {
	all := client.CollectionConditionMatchTypeAll
	out := make([]client.CollectionCreateSourceTargetInput, 2)
	for i, title := range []string{"tf-acc source a", "tf-acc source b"} {
		out[i].Source = &client.CollectionCreateConditionsSourceInput{Title: title, Inclusion: &client.CollectionCreateSourceInclusionInput{
			MatchType:  &all,
			Conditions: json.RawMessage(`[{"productTag":{"relation":"TAGGED_WITH","values":["` + tag + `"],"matchType":"ANY"}}]`),
		}}
	}
	return out
}

func sourceCount(t *testing.T, c *client.CollectionFields) int {
	t.Helper()
	var sources []json.RawMessage
	if err := json.Unmarshal(c.Sources, &sources); err != nil {
		t.Fatal(err)
	}
	return len(sources)
}

// Adopting and importing a collection whose membership can't be managed works while config leaves membership unset.
func TestAccCollection_unsupportedAdopt(t *testing.T) {
	accSetup(t)
	acctest.KeepFrontpage(t)
	handle := testHandle()
	cleanup(t, handle)
	mustCreate(t, client.CollectionCreateInput{Title: "tf-acc unsupported (client)", Handle: &handle, Sources: twoSources("tf-acc-unsupported")})
	body := func(extra string) string {
		return fmt.Sprintf(`
resource "shopify_collection" "test" {
  handle         = %q
  title          = "tf-acc unsupported (terraform)"
  adopt_existing = true
%s
}
`, handle, extra)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			c := lookup(t, handle)
			if c == nil || c.Title != "tf-acc unsupported (terraform)" || sourceCount(t, c) != 2 {
				return fmt.Errorf("adopted collection changed on destroy: %+v", c)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: body(""),
				Check: func(*terraform.State) error {
					if n := sourceCount(t, mustLookup(t, handle)); n != 2 {
						return fmt.Errorf("live collection has %d sources after adopt, want both kept", n)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("title", str("tf-acc unsupported (terraform)")),
					stateValue("product_ids", knownvalue.Null()),
					stateValue("conditions", knownvalue.Null()),
					stateValue("match_type", knownvalue.Null()),
					stateValue("source_id", knownvalue.Null()),
				},
			},
			{Config: body("  product_ids = []"), PlanOnly: true, ExpectError: regexp.MustCompile(`Membership can't be managed`)},
			{
				Config:                               body(""),
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        handle,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "handle",
				ImportStateVerifyIgnore:              []string{"adopt_existing"},
			},
		},
	})
}

// A managed collection that gains a second source outside Terraform still refreshes; only config that sets membership fails.
func TestAccCollection_becomesUnsupported(t *testing.T) {
	accSetup(t)
	acctest.KeepFrontpage(t)
	handle := testHandle()
	cleanup(t, handle)
	managed := fmt.Sprintf(`
resource "shopify_collection" "test" {
  handle     = %q
  title      = "tf-acc becomes unsupported"
  match_type = "ALL"
  conditions = jsonencode([%s])
}
`, handle, tagCondition)
	unmanaged := fmt.Sprintf(`
resource "shopify_collection" "test" {
  handle = %q
  title  = "tf-acc becomes unsupported (renamed)"
}
`, handle)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{Config: managed, ConfigStateChecks: []statecheck.StateCheck{stateValue("source_id", knownvalue.StringRegexp(sourceRe))}},
			{
				PreConfig: func() {
					c := mustLookup(t, handle)
					mustUpdate(t, client.CollectionUpdateInput{Id: &c.Id, SourcesToCreate: twoSources("tf-acc-second")[:1]})
				},
				Config:      managed,
				ExpectError: regexp.MustCompile(`sources other than\s+one non-shareable conditions source`),
			},
			{
				Config: unmanaged,
				ConfigStateChecks: []statecheck.StateCheck{
					stateValue("title", str("tf-acc becomes unsupported (renamed)")),
					stateValue("conditions", knownvalue.Null()),
					stateValue("product_ids", knownvalue.Null()),
					stateValue("source_id", knownvalue.Null()),
				},
				Check: func(*terraform.State) error {
					if n := sourceCount(t, mustLookup(t, handle)); n != 2 {
						return fmt.Errorf("live collection has %d sources, want both kept", n)
					}
					return nil
				},
			},
		},
	})
}
