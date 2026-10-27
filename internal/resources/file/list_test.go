package file_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/querycheck/queryfilter"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

func listConfig(query string) string {
	return fmt.Sprintf(`
provider "shopify" {}

list "shopify_file" "all" {
  provider         = shopify
  include_resource = true

  config {
    query = %q
  }
}
`, query)
}

func TestAccListFile(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	nameA, nameB := testFilename(), testFilename()
	tr := newTracker(t, nameA, nameB)
	files := fmt.Sprintf(`
resource "shopify_file" "a" {
  content_type = "IMAGE"
  filename     = %q
  source       = %q
  alt          = "tf-acc list a"

  allow_data_deletion = true
}

resource "shopify_file" "b" {
  content_type = "IMAGE"
  filename     = %q
  source       = %q

  allow_data_deletion = true
}
`, nameA, imageURL, nameB, imageURL)
	gid := knownvalue.StringRegexp(gidRe)
	byName := func(name string) queryfilter.QueryFilter {
		return queryfilter.ByDisplayName(knownvalue.StringExact(name + " (IMAGE)"))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             tr.gone(t, nameA, nameB),
		Steps: []resource.TestStep{
			{Config: files, Check: resource.ComposeTestCheckFunc(tr.check("shopify_file.a"), tr.check("shopify_file.b"))},
			{
				// New files reach the search index a few seconds late.
				PreConfig: func() { waitSearchable(t, nameA); waitSearchable(t, nameB) },
				Query:     true,
				Config:    listConfig("media_type:IMAGE"),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_file.all", 2),
					querycheck.ExpectIdentity("shopify_file.all", map[string]knownvalue.Check{"id": gid}),
					querycheck.ExpectResourceKnownValues("shopify_file.all", byName(nameA), []querycheck.KnownValueCheck{
						{Path: tfjsonpath.New("id"), KnownValue: gid},
						{Path: tfjsonpath.New("content_type"), KnownValue: knownvalue.StringExact("IMAGE")},
						{Path: tfjsonpath.New("filename"), KnownValue: knownvalue.StringExact(nameA)},
						{Path: tfjsonpath.New("alt"), KnownValue: knownvalue.StringExact("tf-acc list a")},
						{Path: tfjsonpath.New("source"), KnownValue: knownvalue.Null()},
						{Path: tfjsonpath.New("adopt_existing"), KnownValue: knownvalue.Bool(false)},
						{Path: tfjsonpath.New("allow_data_deletion"), KnownValue: knownvalue.Bool(false)},
					}),
				},
			},
			{
				// The query narrows the list.
				PreConfig: func() { time.Sleep(time.Second) },
				Query:     true,
				Config:    listConfig(fmt.Sprintf("filename:%q", nameB)),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLength("shopify_file.all", 1),
					querycheck.ExpectResourceDisplayName("shopify_file.all", byName(nameB), knownvalue.StringExact(nameB+" (IMAGE)")),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _basic.
		},
	})
}
