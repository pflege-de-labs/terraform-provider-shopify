package blog_test

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

list "shopify_blog" "all" {
  provider         = shopify
  include_resource = true
}
`

func TestAccListBlog(t *testing.T) {
	acctest.Setup(t)
	handleA, handleB := testHandle(), testHandle()
	cleanup(t, handleA, handleB)
	blogs := fmt.Sprintf(`
resource "shopify_blog" "a" {
  handle          = %q
  title           = "tf-acc list a"
  comment_policy  = "MODERATED"
  template_suffix = "tf-acc"
}

resource "shopify_blog" "b" {
  handle = %q
  title  = "tf-acc list b"
}
`, handleA, handleB)
	gid := knownvalue.StringRegexp(gidRe)
	byName := func(name, handle string) queryfilter.QueryFilter {
		return queryfilter.ByDisplayName(knownvalue.StringExact(name + " (" + handle + ")"))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkGone(t, handleA, handleB),
		Steps: []resource.TestStep{
			{Config: blogs},
			{
				Query:  true,
				Config: listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_blog.all", 2),
					querycheck.ExpectIdentity("shopify_blog.all", map[string]knownvalue.Check{"id": gid}),
					querycheck.ExpectResourceKnownValues("shopify_blog.all", byName("tf-acc list a", handleA), []querycheck.KnownValueCheck{
						{Path: tfjsonpath.New("id"), KnownValue: gid},
						{Path: tfjsonpath.New("handle"), KnownValue: knownvalue.StringExact(handleA)},
						{Path: tfjsonpath.New("comment_policy"), KnownValue: knownvalue.StringExact("MODERATED")},
						{Path: tfjsonpath.New("template_suffix"), KnownValue: knownvalue.StringExact("tf-acc")},
						{Path: tfjsonpath.New("adopt_existing"), KnownValue: knownvalue.Bool(false)},
						{Path: tfjsonpath.New("allow_data_deletion"), KnownValue: knownvalue.Bool(false)},
					}),
					querycheck.ExpectResourceKnownValues("shopify_blog.all", byName("tf-acc list b", handleB), []querycheck.KnownValueCheck{
						{Path: tfjsonpath.New("comment_policy"), KnownValue: knownvalue.StringExact("CLOSED")},
						{Path: tfjsonpath.New("template_suffix"), KnownValue: knownvalue.Null()},
					}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _basic.
		},
	})
}
