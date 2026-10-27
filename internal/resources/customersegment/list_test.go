package customersegment_test

import (
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

list "shopify_customer_segment" "all" {
  provider         = shopify
  include_resource = true
}
`

func TestAccListCustomerSegment(t *testing.T) {
	accSetup(t)
	nameA, nameB := testName(), testName()
	cleanup(t, nameA, nameB)
	segments := `
resource "shopify_customer_segment" "a" {
  name  = "` + nameA + `"
  query = "` + someQuery + `"
}

resource "shopify_customer_segment" "b" {
  name  = "` + nameB + `"
  query = "` + otherQuery + `"
}
`
	gid := knownvalue.StringRegexp(gidRe)
	byName := func(name string) queryfilter.QueryFilter {
		return queryfilter.ByDisplayName(knownvalue.StringExact(name))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkGone(t, nameA, nameB),
		Steps: []resource.TestStep{
			{Config: segments},
			{
				Query:  true,
				Config: listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_customer_segment.all", 2),
					querycheck.ExpectIdentity("shopify_customer_segment.all", map[string]knownvalue.Check{"id": gid}),
					querycheck.ExpectResourceKnownValues("shopify_customer_segment.all", byName(nameA), []querycheck.KnownValueCheck{
						{Path: tfjsonpath.New("id"), KnownValue: gid},
						{Path: tfjsonpath.New("name"), KnownValue: knownvalue.StringExact(nameA)},
						{Path: tfjsonpath.New("query"), KnownValue: knownvalue.StringExact(someQuery)},
						{Path: tfjsonpath.New("adopt_existing"), KnownValue: knownvalue.Bool(false)},
					}),
					querycheck.ExpectResourceKnownValues("shopify_customer_segment.all", byName(nameB), []querycheck.KnownValueCheck{
						{Path: tfjsonpath.New("query"), KnownValue: knownvalue.StringExact(otherQuery)},
					}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _basic.
		},
	})
}
