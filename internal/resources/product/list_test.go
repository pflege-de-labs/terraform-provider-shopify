package product_test

import (
	"fmt"
	"regexp"
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
	f := newFiles(t)
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
	online := acctest.PublicationID(t, "Online Store")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkGone(t, handleA, handleB),
		Steps: []resource.TestStep{
			{Config: publicationData + fileConfig("a", testFilename()) + product("a", handleA, `
  tags     = ["tf-acc-list"]
  publication_ids = [data.shopify_publication.online.id]
  media_ids       = [shopify_file.a.id]
  options  = [{ name = "Size", values = ["S", "M"] }]
  variants = [{ option_values = { Size = "S" }, price = "3.00" }, { option_values = { Size = "M" }, sku = "TF-LIST-M" }]
`) + product("b", handleB, ""), Check: f.track("a")},
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
							{Path: tfjsonpath.New("publication_ids"), KnownValue: knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact(online)})},
							{Path: tfjsonpath.New("media_ids"), KnownValue: knownvalue.ListExact([]knownvalue.Check{knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/MediaImage/\d+$`))})},
							{Path: tfjsonpath.New("options").AtSliceIndex(0).AtMapKey("values"), KnownValue: knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact("S"), knownvalue.StringExact("M")})},
							{Path: tfjsonpath.New("variants").AtSliceIndex(0).AtMapKey("price"), KnownValue: knownvalue.StringExact("3.00")},
							{Path: tfjsonpath.New("variants").AtSliceIndex(1).AtMapKey("option_values"), KnownValue: knownvalue.MapExact(map[string]knownvalue.Check{"Size": knownvalue.StringExact("M")})},
							{Path: tfjsonpath.New("variants").AtSliceIndex(1).AtMapKey("sku"), KnownValue: knownvalue.StringExact("TF-LIST-M")},
							{Path: tfjsonpath.New("adopt_existing"), KnownValue: knownvalue.Bool(false)},
						}),
				},
			},
			// A product published nowhere and without media lists neither, so generated config doesn't claim to manage them.
			{
				Query:  true,
				Config: listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectResourceKnownValues("shopify_product.all",
						queryfilter.ByDisplayName(knownvalue.StringExact("tf-acc list b ("+handleB+")")),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("publication_ids"), KnownValue: knownvalue.Null()},
							{Path: tfjsonpath.New("media_ids"), KnownValue: knownvalue.Null()},
						}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _basic.
		},
	})
}
