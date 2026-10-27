package collection_test

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

list "shopify_collection" "all" {
  provider         = shopify
  include_resource = true
}
`

func TestAccListCollection(t *testing.T) {
	accSetup(t)
	acctest.KeepFrontpage(t)
	handleA, handleB := testHandle(), testHandle()
	productHandle := handleA + "-p"
	cleanup(t, handleA, handleB, productHandle)
	online := acctest.PublicationID(t, "Online Store")
	collections := publicationData + product(productHandle) + fmt.Sprintf(`
resource "shopify_collection" "a" {
  handle          = %q
  title           = "tf-acc list a"
  product_ids     = [shopify_product.p.id]
  publication_ids = [data.shopify_publication.online.id]
}

resource "shopify_collection" "b" {
  handle     = %q
  title      = "tf-acc list b"
  match_type = "ALL"
  conditions = jsonencode([%s])
}
`, handleA, handleB, tagCondition)
	gid := knownvalue.StringRegexp(gidRe)
	byName := func(name, handle string) queryfilter.QueryFilter {
		return queryfilter.ByDisplayName(knownvalue.StringExact(name + " (" + handle + ")"))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkGone(t, handleA, handleB, productHandle),
		Steps: []resource.TestStep{
			{Config: collections},
			{
				Query:  true,
				Config: listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_collection.all", 2),
					querycheck.ExpectIdentity("shopify_collection.all", map[string]knownvalue.Check{"id": gid}),
					querycheck.ExpectResourceKnownValues("shopify_collection.all", byName("tf-acc list a", handleA), []querycheck.KnownValueCheck{
						{Path: tfjsonpath.New("id"), KnownValue: gid},
						{Path: tfjsonpath.New("handle"), KnownValue: knownvalue.StringExact(handleA)},
						{Path: tfjsonpath.New("sort_order"), KnownValue: knownvalue.StringExact("MOST_RELEVANT")},
						{Path: tfjsonpath.New("product_ids"), KnownValue: knownvalue.SetExact([]knownvalue.Check{knownvalue.StringRegexp(productGIDRe)})},
						{Path: tfjsonpath.New("conditions"), KnownValue: knownvalue.Null()},
						{Path: tfjsonpath.New("match_type"), KnownValue: knownvalue.Null()},
						{Path: tfjsonpath.New("source_id"), KnownValue: knownvalue.StringRegexp(sourceRe)},
						{Path: tfjsonpath.New("publication_ids"), KnownValue: knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact(online)})},
						{Path: tfjsonpath.New("adopt_existing"), KnownValue: knownvalue.Bool(false)},
					}),
					querycheck.ExpectResourceKnownValues("shopify_collection.all", byName("tf-acc list b", handleB), []querycheck.KnownValueCheck{
						{Path: tfjsonpath.New("handle"), KnownValue: knownvalue.StringExact(handleB)},
						{Path: tfjsonpath.New("match_type"), KnownValue: knownvalue.StringExact("ALL")},
						{Path: tfjsonpath.New("conditions"), KnownValue: knownvalue.StringExact(`[{"productTag":{"matchType":"ANY","relation":"TAGGED_WITH","values":["tf-acc-collection"]}}]`)},
						{Path: tfjsonpath.New("product_ids"), KnownValue: knownvalue.SetExact([]knownvalue.Check{})},
						// An unpublished collection lists no publication_ids, so generated config doesn't claim to manage them.
						{Path: tfjsonpath.New("publication_ids"), KnownValue: knownvalue.Null()},
					}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _basic.
		},
	})
}
