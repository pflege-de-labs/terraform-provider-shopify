package product_test

import (
	"fmt"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/publishing"
)

const publicationData = `
data "shopify_publication" "online" { name = "Online Store" }
data "shopify_publication" "shop" { name = "Shop" }
`

// livePublications are the sales channel publications Shopify lists for the product, sorted.
func livePublications(t *testing.T, handle string) []string {
	t.Helper()
	ids := publishing.IDs(mustLookup(t, handle).GetResourcePublicationsV2())
	slices.Sort(ids)
	return ids
}

// checkPublished fails unless the product is published to exactly these publications.
func checkPublished(t *testing.T, handle string, want ...string) resource.TestCheckFunc {
	slices.Sort(want)
	return func(*terraform.State) error {
		if got := livePublications(t, handle); !slices.Equal(got, want) {
			return fmt.Errorf("product %s is published to %v, want %v", handle, got, want)
		}
		return nil
	}
}

func publicationChecks(ids ...string) statecheck.StateCheck {
	checks := make([]knownvalue.Check, len(ids))
	for i, id := range ids {
		checks[i] = str(id)
	}
	return stateValue("publication_ids", knownvalue.SetExact(checks))
}

func TestAccProduct_publications(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	handle := testHandle()
	cleanup(t, handle)
	online, shop, pos := acctest.PublicationID(t, "Online Store"), acctest.PublicationID(t, "Shop"), acctest.PublicationID(t, "Point of Sale")
	cfg := func(title, publications string) string {
		return publicationData + config(fmt.Sprintf(`
  handle              = %q
  title               = %q
  status              = "DRAFT"
  allow_data_deletion = true
%s
`, handle, title, publications))
	}
	const title = "tf-acc publications"
	// Without allow_data_deletion = true, which an import can't know, so the import plans as a no-op.
	imported := publicationData + config(fmt.Sprintf("handle = %q\ntitle = %q\nstatus = \"DRAFT\"\npublication_ids = [%q]", handle, title, shop))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{
				Config:      cfg(title, `publication_ids = ["123"]`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Publication\s+GID`),
			},
			{
				// A new product is published nowhere; the attribute publishes it on create.
				Config:            cfg(title, `publication_ids = [data.shopify_publication.online.id]`),
				Check:             checkPublished(t, handle, online),
				ConfigStateChecks: []statecheck.StateCheck{expectIdentity, publicationChecks(online)},
			},
			{
				Config: cfg(title, `publication_ids = [data.shopify_publication.online.id, data.shopify_publication.shop.id]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				Check:             checkPublished(t, handle, online, shop),
				ConfigStateChecks: []statecheck.StateCheck{publicationChecks(online, shop)},
			},
			{
				// Removing an ID unpublishes it.
				Config:            cfg(title, `publication_ids = [data.shopify_publication.shop.id]`),
				Check:             checkPublished(t, handle, shop),
				ConfigStateChecks: []statecheck.StateCheck{publicationChecks(shop)},
			},
			{
				Config:           cfg(title, `publication_ids = [data.shopify_publication.shop.id]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{Config: imported, ResourceName: addr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				// Generated config must pass validation and plan as import-only.
				Config:          imported,
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
				ImportStateVerifyIgnore:              []string{"adopt_existing", "allow_data_deletion", "media_managed"},
			},
			{
				// Review focus 2: unset means unmanaged, so a publication made elsewhere is no diff and survives other updates.
				PreConfig:         func() { acctest.Publish(t, mustLookup(t, handle).Id, pos) },
				Config:            cfg(title, ""),
				ConfigPlanChecks:  resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				ConfigStateChecks: []statecheck.StateCheck{publicationChecks(shop, pos)},
			},
			{
				Config:            cfg(title+" (renamed)", ""),
				Check:             checkPublished(t, handle, shop, pos),
				ConfigStateChecks: []statecheck.StateCheck{publicationChecks(shop, pos)},
			},
			{
				Config:            cfg(title, `publication_ids = []`),
				Check:             checkPublished(t, handle),
				ConfigStateChecks: []statecheck.StateCheck{publicationChecks()},
			},
		},
	})
}

// A rejected publication ID fails the create after the product exists, which leaves it tainted.
func TestAccProduct_publicationRejected(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	handle := testHandle()
	cleanup(t, handle)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{{
			Config: config(fmt.Sprintf(`
  handle              = %q
  title               = "tf-acc rejected"
  status              = "DRAFT"
  allow_data_deletion = true
  publication_ids     = ["gid://shopify/Publication/1"]
`, handle)),
			ExpectError: regexp.MustCompile(`Failed\s+to\s+publish`),
		}},
	})
}
