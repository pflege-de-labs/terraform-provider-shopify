package metafielddefinition_test

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	sdkacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const addr = "shopify_metafield_definition.test"

// testNamespace keeps test definitions apart from real ones; only these are ever deleted by tests.
func testNamespace() string {
	return "tfacc_" + sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlpha)
}

var expectIdentity = statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{
	"id": knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/MetafieldDefinition/\d+$`)),
})

func config(ns, body string) string {
	return fmt.Sprintf(`
resource "shopify_metafield_definition" "test" {
  owner_type = "PRODUCT"
  namespace  = %q
  key        = "care_level"
%s
}
`, ns, body)
}

func TestAccMetafieldDefinition_basic(t *testing.T) {
	ns := testNamespace()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(t, ns),
		Steps: []resource.TestStep{
			{
				Config: config(ns, `
  type                = "number_integer"
  name                = "Care level"
  validations         = { min = "1", max = "5" }
  access              = { storefront = "PUBLIC_READ" }
  pin                 = true
  allow_data_deletion = true
`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/MetafieldDefinition/\d+$`))),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("pin"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("validations"), knownvalue.MapExact(map[string]knownvalue.Check{
						"min": knownvalue.StringExact("1"),
						"max": knownvalue.StringExact("5"),
					})),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("access").AtMapKey("storefront"), knownvalue.StringExact("PUBLIC_READ")),
				},
			},
			{
				// Shopify reports an admin access that maps to null; an update must plan it as null, not "known after apply".
				Config: config(ns, `
  type                = "number_integer"
  name                = "Care level (renamed)"
  validations         = { min = "1", max = "5" }
  access              = { storefront = "PUBLIC_READ" }
  pin                 = true
  allow_data_deletion = true
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
						plancheck.ExpectKnownValue(addr, tfjsonpath.New("access").AtMapKey("admin"), knownvalue.Null()),
					},
				},
			},
			{
				// Dropping max shows whether an update replaces the validation list or merges into it.
				Config: config(ns, `
  type                = "number_integer"
  name                = "Care level (updated)"
  description         = "Relevant care level"
  validations         = { min = "1" }
  pin                 = false
  allow_data_deletion = true
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Care level (updated)")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("pin"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("validations"), knownvalue.MapExact(map[string]knownvalue.Check{
						"min": knownvalue.StringExact("1"),
					})),
					// access was dropped from config, so the storefront setting stays unmanaged and unchanged.
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("access").AtMapKey("storefront"), knownvalue.StringExact("PUBLIC_READ")),
				},
			},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateId:           "PRODUCT/" + ns + "/care_level",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"allow_data_deletion"},
			},
			{
				// Changing type forces replace; allowed because allow_data_deletion is already true in state.
				Config: config(ns, `
  type                = "single_line_text_field"
  name                = "Care level (updated)"
  allow_data_deletion = true
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate)},
				},
			},
		},
	})
}

func TestAccMetafieldDefinition_deletionGuard(t *testing.T) {
	ns := testNamespace()
	guarded := config(ns, `
  type = "single_line_text_field"
  name = "Guarded"
`)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(t, ns),
		Steps: []resource.TestStep{
			{
				Config: guarded,
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("allow_data_deletion"), knownvalue.Bool(false)),
				},
			},
			{
				RefreshState: true,
			},
			{
				// Refresh steps can't carry state checks; this no-op step checks identity survives the refresh.
				Config:            guarded,
				ConfigStateChecks: []statecheck.StateCheck{expectIdentity},
			},
			{
				// Here, not in _basic: its allow_data_deletion = true would turn the import into an update.
				Config:          guarded,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
			},
			{
				// Generated config must pass validation; the default admin access PUBLIC_READ_WRITE can't be set.
				Config:          guarded,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				Config:      guarded,
				Destroy:     true,
				ExpectError: regexp.MustCompile(`allow_data_deletion = true`),
			},
			{
				Config: config(ns, `
  type = "number_integer"
  name = "Guarded"
`),
				ExpectError: regexp.MustCompile(`allow_data_deletion = true`),
			},
			{
				// Opt in so the test can clean up after itself.
				Config: config(ns, `
  type                = "single_line_text_field"
  name                = "Guarded"
  allow_data_deletion = true
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
			},
		},
	})
}

// checkDestroyed checks PRODUCT definitions in ns; keys defaults to care_level.
func checkDestroyed(t *testing.T, ns string, keys ...string) resource.TestCheckFunc {
	if len(keys) == 0 {
		keys = []string{"care_level"}
	}
	return func(*terraform.State) error {
		for _, key := range keys {
			res, err := client.GetMetafieldDefinition(context.Background(), acctest.Client(t), client.MetafieldDefinitionIdentifierInput{
				OwnerType: client.MetafieldOwnerTypeProduct,
				Namespace: &ns,
				Key:       key,
			})
			if err != nil {
				return err
			}
			if res.MetafieldDefinition != nil {
				return fmt.Errorf("metafield definition %s/%s still exists", ns, key)
			}
		}
		return nil
	}
}
