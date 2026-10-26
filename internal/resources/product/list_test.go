package product_test

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

list "shopify_product" "all" {
  provider         = shopify
  include_resource = true
}
`

func TestAccListProduct(t *testing.T) {
	accSetup(t)
	handleA, handleB := testHandle(), testHandle()
	cleanup(t, handleA, handleB)
	product := func(name, handle, extra string) string {
		return fmt.Sprintf(`
resource "shopify_product" %q {
  handle              = %q
  title               = "tf-acc list %s"
  status              = "DRAFT"
  allow_data_deletion = true
%s
}
`, name, handle, name, extra)
	}
	gid := knownvalue.StringRegexp(gidRe)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkGone(t, handleA, handleB),
		Steps: []resource.TestStep{
			{Config: product("a", handleA, `tags = ["tf-acc-list"]`) + product("b", handleB, "")},
			{
				Query:  true,
				Config: listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_product.all", 2),
					querycheck.ExpectIdentity("shopify_product.all", map[string]knownvalue.Check{"id": gid}),
					querycheck.ExpectResourceKnownValues("shopify_product.all",
						queryfilter.ByDisplayName(knownvalue.StringExact("tf-acc list a ("+handleA+")")),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("id"), KnownValue: gid},
							{Path: tfjsonpath.New("handle"), KnownValue: knownvalue.StringExact(handleA)},
							{Path: tfjsonpath.New("status"), KnownValue: knownvalue.StringExact("DRAFT")},
							{Path: tfjsonpath.New("tags"), KnownValue: knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("tf-acc-list")})},
							{Path: tfjsonpath.New("gift_card"), KnownValue: knownvalue.Bool(false)},
							{Path: tfjsonpath.New("adopt_existing"), KnownValue: knownvalue.Bool(false)},
						}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _basic.
		},
	})
}
