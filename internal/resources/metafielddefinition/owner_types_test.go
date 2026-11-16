package metafielddefinition_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/querycheck/queryfilter"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// TestAccMetafieldDefinition_ownerTypes covers the owner types beyond PRODUCT; COMPANY* need a B2B store and SHOPIFY_ACC_B2B=1.
func TestAccMetafieldDefinition_ownerTypes(t *testing.T) {
	for _, tc := range []struct {
		ownerType client.MetafieldOwnerType
		b2b       bool
	}{
		{client.MetafieldOwnerTypeCustomer, false},
		{client.MetafieldOwnerTypeOrder, false},
		{client.MetafieldOwnerTypeDraftorder, false},
		{client.MetafieldOwnerTypeDiscount, false},
		{client.MetafieldOwnerTypeCompany, true},
		{client.MetafieldOwnerTypeCompanyLocation, true},
	} {
		t.Run(string(tc.ownerType), func(t *testing.T) {
			if tc.b2b && os.Getenv("SHOPIFY_ACC_B2B") == "" {
				t.Skip("set SHOPIFY_ACC_B2B=1 on a dev store with B2B and the companies scopes")
			}
			testOwnerType(t, string(tc.ownerType))
		})
	}
}

func testOwnerType(t *testing.T, ownerType string) {
	ns := testNamespace()
	gid := knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/MetafieldDefinition/\d+$`))
	cfg := func(body string) string {
		return fmt.Sprintf(`
resource "shopify_metafield_definition" "test" {
  owner_type          = %q
  namespace           = %q
  key                 = "care_level"
  type                = "single_line_text_field"
  allow_data_deletion = true
%s
}
`, ownerType, ns, body)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkDestroyedOwner(t, client.MetafieldOwnerType(ownerType), ns),
		Steps: []resource.TestStep{
			{Config: cfg(`  name = "Care level"`)},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateId:           ownerType + "/" + ns + "/care_level",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"allow_data_deletion"},
			},
			{
				Query:  true,
				Config: listConfig(ownerType),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectResourceKnownValues("shopify_metafield_definition.all",
						queryfilter.ByDisplayName(knownvalue.StringExact(ownerType+"/"+ns+"/care_level")),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("id"), KnownValue: gid},
							{Path: tfjsonpath.New("owner_type"), KnownValue: knownvalue.StringExact(ownerType)},
						}),
				},
			},
			{
				// Generated config sets these for every owner type; an update must not be rejected for owners that can't use them.
				Config: cfg(`
  name                        = "Care level (renamed)"
  pin                         = false
  use_as_collection_condition = false
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
			},
		},
	})
}
