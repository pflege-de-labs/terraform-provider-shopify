package metaobjectdefinition_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/querycheck/queryfilter"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

const listConfig = `
provider "shopify" {}

list "shopify_metaobject_definition" "all" {
  provider         = shopify
  include_resource = true
}
`

func TestAccListMetaobjectDefinition(t *testing.T) {
	typA, typB := testType(), testType()
	gid := knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/MetaobjectDefinition/\d+$`))
	def := func(name, typ string) string {
		return fmt.Sprintf(`
resource "shopify_metaobject_definition" %[1]q {
  type                = %[2]q
  name                = %[3]q
  allow_data_deletion = true

  field {
    key  = "title"
    name = "Title"
    type = "single_line_text_field"
  }
}
`, name, typ, "List "+name)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy: func(s *terraform.State) error {
			return firstErr(checkDestroyed(t, typA)(s), checkDestroyed(t, typB)(s))
		},
		Steps: []resource.TestStep{
			{Config: def("a", typA) + def("b", typB)},
			{
				Query:  true,
				Config: listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_metaobject_definition.all", 2),
					querycheck.ExpectIdentity("shopify_metaobject_definition.all", map[string]knownvalue.Check{"id": gid}),
					querycheck.ExpectResourceKnownValues("shopify_metaobject_definition.all",
						queryfilter.ByDisplayName(knownvalue.StringExact(typA)),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("id"), KnownValue: gid},
							{Path: tfjsonpath.New("type"), KnownValue: knownvalue.StringExact(typA)},
							{Path: tfjsonpath.New("name"), KnownValue: knownvalue.StringExact("List a")},
							{Path: tfjsonpath.New("allow_data_deletion"), KnownValue: knownvalue.Bool(false)},
						}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _deletionGuard.
		},
	})
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
