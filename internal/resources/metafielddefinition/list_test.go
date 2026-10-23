package metafielddefinition_test

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

func listConfig(ownerType string) string {
	return fmt.Sprintf(`
provider "shopify" {}

list "shopify_metafield_definition" "all" {
  provider         = shopify
  include_resource = true
  config {
    owner_type = %q
  }
}
`, ownerType)
}

func TestAccListMetafieldDefinition(t *testing.T) {
	ns := testNamespace()
	gid := knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/MetafieldDefinition/\d+$`))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkDestroyed(t, ns, "care_level", "care_note"),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "shopify_metafield_definition" "a" {
  owner_type          = "PRODUCT"
  namespace           = %[1]q
  key                 = "care_level"
  type                = "number_integer"
  name                = "Care level"
  allow_data_deletion = true
}

resource "shopify_metafield_definition" "b" {
  owner_type          = "PRODUCT"
  namespace           = %[1]q
  key                 = "care_note"
  type                = "single_line_text_field"
  name                = "Care note"
  allow_data_deletion = true
}
`, ns),
			},
			{
				Query:  true,
				Config: listConfig("PRODUCT"),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_metafield_definition.all", 2),
					querycheck.ExpectIdentity("shopify_metafield_definition.all", map[string]knownvalue.Check{"id": gid}),
					querycheck.ExpectResourceKnownValues("shopify_metafield_definition.all",
						queryfilter.ByDisplayName(knownvalue.StringExact("PRODUCT/"+ns+"/care_level")),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("id"), KnownValue: gid},
							{Path: tfjsonpath.New("type"), KnownValue: knownvalue.StringExact("number_integer")},
							{Path: tfjsonpath.New("name"), KnownValue: knownvalue.StringExact("Care level")},
							{Path: tfjsonpath.New("allow_data_deletion"), KnownValue: knownvalue.Bool(false)},
						}),
				},
			},
			// TODO(verify): GenerateConfig round-trip; terraform-plugin-testing v1.16.0 ignores GenerateConfig on Query steps (testStepNewQuery never reads it), so it is covered in Task 12.
		},
	})
}
