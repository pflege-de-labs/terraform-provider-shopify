package metaobjectdefinition_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
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

const addr = "shopify_metaobject_definition.test"

// testType keeps test definitions apart from real ones; only these are ever deleted by tests.
func testType() string {
	return "tfacc_" + strings.ToLower(sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlpha))
}

func config(typ, body string) string {
	return fmt.Sprintf(`
resource "shopify_metaobject_definition" "test" {
  type = %q
%s
}
`, typ, body)
}

var expectIdentity = statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{
	"id": knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/MetaobjectDefinition/\d+$`)),
})

func fieldKeys(keys ...string) statecheck.StateCheck {
	checks := make([]knownvalue.Check, len(keys))
	for i, k := range keys {
		checks[i] = knownvalue.ObjectPartial(map[string]knownvalue.Check{"key": knownvalue.StringExact(k)})
	}
	return statecheck.ExpectKnownValue(addr, tfjsonpath.New("field"), knownvalue.ListExact(checks))
}

func TestAccMetaobjectDefinition_basic(t *testing.T) {
	typ := testType()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(t, typ),
		Steps: []resource.TestStep{
			{
				Config: config(typ, `
  name                = "FAQ entry"
  display_name_key    = "question"
  access              = { storefront = "PUBLIC_READ" }
  capabilities        = { publishable = true }
  allow_data_deletion = true

  field {
    key      = "question"
    name     = "Question"
    type     = "single_line_text_field"
    required = true
  }
  field {
    key  = "answer"
    name = "Answer"
    type = "single_line_text_field"
  }
`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/MetaobjectDefinition/\d+$`))),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("capabilities").AtMapKey("publishable"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("access").AtMapKey("storefront"), knownvalue.StringExact("PUBLIC_READ")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("field").AtSliceIndex(0).AtMapKey("required"), knownvalue.Bool(true)),
					fieldKeys("question", "answer"),
				},
			},
			{
				// Add a field in the middle and a validation; access and capabilities are dropped and stay unmanaged.
				Config: config(typ, `
  name                = "FAQ entry (updated)"
  display_name_key    = "question"
  allow_data_deletion = true

  field {
    key         = "question"
    name        = "Question"
    type        = "single_line_text_field"
    required    = true
    validations = { max = "200" }
  }
  field {
    key  = "category"
    name = "Category"
    type = "single_line_text_field"
  }
  field {
    key  = "answer"
    name = "Answer"
    type = "single_line_text_field"
  }
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					fieldKeys("question", "category", "answer"),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("field").AtSliceIndex(0).AtMapKey("validations"),
						knownvalue.MapExact(map[string]knownvalue.Check{"max": knownvalue.StringExact("200")})),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("capabilities").AtMapKey("publishable"), knownvalue.Bool(true)),
				},
			},
			{
				// Reorder, drop category, and change answer's type (delete + create of that field).
				Config: config(typ, `
  name                = "FAQ entry (updated)"
  display_name_key    = "question"
  allow_data_deletion = true

  field {
    key  = "answer"
    name = "Answer"
    type = "multi_line_text_field"
  }
  field {
    key      = "question"
    name     = "Question"
    type     = "single_line_text_field"
    required = true
  }
`),
				ConfigStateChecks: []statecheck.StateCheck{
					fieldKeys("answer", "question"),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("field").AtSliceIndex(0).AtMapKey("type"), knownvalue.StringExact("multi_line_text_field")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("field").AtSliceIndex(1).AtMapKey("validations"), knownvalue.Null()),
				},
			},
			{
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        typ,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "type",
				ImportStateVerifyIgnore:              []string{"allow_data_deletion"},
			},
		},
	})
}

func TestAccMetaobjectDefinition_deletionGuard(t *testing.T) {
	typ := testType()
	fields := `
  field {
    key  = "aa"
    name = "A"
    type = "single_line_text_field"
  }
`
	fieldB := `
  field {
    key  = "bb"
    name = "B"
    type = "single_line_text_field"
  }
`
	guardedConfig := config(typ, `name = "Guarded"`+fields+fieldB)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(t, typ),
		Steps: []resource.TestStep{
			{
				Config:            guardedConfig,
				ConfigStateChecks: []statecheck.StateCheck{expectIdentity},
			},
			{RefreshState: true},
			{
				// Refresh steps can't carry state checks; this no-op step checks identity survives the refresh.
				Config:            guardedConfig,
				ConfigStateChecks: []statecheck.StateCheck{expectIdentity},
			},
			{
				// Here, not in _basic: its allow_data_deletion = true would turn the import into an update.
				Config:          guardedConfig,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
			},
			{
				// Generated config must pass validation; admin access can report values the input rejects.
				Config:          guardedConfig,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				Config:      config(typ, `name = "Guarded"`+fields),
				ExpectError: regexp.MustCompile(`allow_data_deletion = true`),
			},
			{
				Config:      guardedConfig,
				Destroy:     true,
				ExpectError: regexp.MustCompile(`allow_data_deletion = true`),
			},
			{
				// Opt in so the test can clean up after itself.
				Config: config(typ, `
  name                = "Guarded"
  allow_data_deletion = true
`+fields+fieldB),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
			},
		},
	})
}

func TestAccMetaobjectDefinition_duplicateKeys(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "shopify" {
  store_domain = "example.myshopify.com"
  access_token = "unused"
}
` + config("tfacc_dup", `
  name = "Dup"
  field {
    key  = "aa"
    name = "A"
    type = "single_line_text_field"
  }
  field {
    key  = "aa"
    name = "A again"
    type = "single_line_text_field"
  }
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Field keys must be unique: aa`),
			},
		},
	})
}

func checkDestroyed(t *testing.T, typ string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		res, err := client.GetMetaobjectDefinitionByType(context.Background(), acctest.Client(t), typ)
		if err != nil {
			return err
		}
		if res.MetaobjectDefinitionByType != nil {
			return fmt.Errorf("metaobject definition %s still exists", typ)
		}
		return nil
	}
}

func TestAccMetaobjectDefinition_recreatedOutOfBand(t *testing.T) {
	typ := testType()
	cfg := config(typ, `
  name                = "Recreated"
  allow_data_deletion = true

  field {
    key  = "aa"
    name = "A"
    type = "single_line_text_field"
  }
`)
	var newID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(t, typ),
		Steps: []resource.TestStep{
			{Config: cfg, ConfigStateChecks: []statecheck.StateCheck{expectIdentity}},
			{
				// Read finds the recreated definition by type; its new GID must be accepted as the identity.
				PreConfig: func() { newID = recreate(t, typ) },
				Config:    cfg,
				Check: func(s *terraform.State) error {
					if got := s.RootModule().Resources[addr].Primary.ID; got != newID {
						return fmt.Errorf("state id %q, want recreated %q", got, newID)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{expectIdentity},
			},
		},
	})
}

// recreate deletes and re-creates the definition behind the Terraform provider's back and returns the new GID.
func recreate(t *testing.T, typ string) string {
	t.Helper()
	ctx, c := context.Background(), acctest.Client(t)
	old, err := client.GetMetaobjectDefinitionByType(ctx, c, typ)
	if err != nil || old.MetaobjectDefinitionByType == nil {
		t.Fatalf("lookup %s: %v", typ, err)
	}
	del, err := client.DeleteMetaobjectDefinition(ctx, c, old.MetaobjectDefinitionByType.Id)
	if err != nil || len(del.MetaobjectDefinitionDelete.UserErrors) > 0 {
		t.Fatalf("delete: %v %v", err, del)
	}
	name, fname := "Recreated", "A"
	res, err := client.CreateMetaobjectDefinition(ctx, c, client.MetaobjectDefinitionCreateInput{
		Name: &name, Type: typ,
		FieldDefinitions: []client.MetaobjectFieldDefinitionCreateInput{{Key: "aa", Name: &fname, Type: "single_line_text_field"}},
	})
	if err != nil || res.MetaobjectDefinitionCreate.MetaobjectDefinition == nil {
		t.Fatalf("create: %v %v", err, res)
	}
	return res.MetaobjectDefinitionCreate.MetaobjectDefinition.Id
}
