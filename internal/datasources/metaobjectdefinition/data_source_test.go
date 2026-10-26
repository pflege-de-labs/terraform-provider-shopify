package metaobjectdefinition_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	sdkacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const (
	dsAddr  = "data.shopify_metaobject_definition.test"
	resAddr = "shopify_metaobject_definition.test"
)

// Every attribute the data source mirrors from the resource, including both fields.
var mirrored = []string{
	"id", "type", "name", "description", "display_name_key",
	"access.admin", "access.storefront", "access.customer_account",
	"capabilities.publishable", "capabilities.translatable", "capabilities.renderable",
	"field.#",
	"field.0.key", "field.0.type", "field.0.name", "field.0.description", "field.0.required", "field.0.validations.%",
	"field.1.key", "field.1.type", "field.1.name", "field.1.description", "field.1.required", "field.1.validations.%", "field.1.validations.max",
}

func TestAccMetaobjectDefinitionDataSource_mirrorsResource(t *testing.T) {
	// A test-only type: only definitions created here are ever deleted.
	typ := "tfacc_" + strings.ToLower(sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlpha))
	checks := []resource.TestCheckFunc{
		resource.TestCheckResourceAttr(dsAddr, "field.1.key", "summary"),
		resource.TestCheckResourceAttr(dsAddr, "field.1.validations.max", "100"),
		resource.TestCheckResourceAttr(dsAddr, "capabilities.publishable", "true"),
	}
	for _, a := range mirrored {
		checks = append(checks, resource.TestCheckResourceAttrPair(dsAddr, a, resAddr, a))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(t, typ),
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
resource "shopify_metaobject_definition" "test" {
  type                = %q
  name                = "Data source test"
  description         = "Created by an acceptance test"
  display_name_key    = "title"
  capabilities        = { publishable = true }
  allow_data_deletion = true

  field {
    key      = "title"
    type     = "single_line_text_field"
    name     = "Title"
    required = true
  }
  field {
    key         = "summary"
    type        = "single_line_text_field"
    name        = "Summary"
    description = "Short text"
    validations = { max = "100" }
  }
}

data "shopify_metaobject_definition" "test" {
  type = shopify_metaobject_definition.test.type
}
`, typ),
			Check: resource.ComposeAggregateTestCheckFunc(checks...),
		}},
	})
}

// Shopify enables some standard definitions itself; the dev store had shopify--knowledge-base-fact (2026-10-26).
func TestAccMetaobjectDefinitionDataSource_shopifyStandard(t *testing.T) {
	acctest.PreCheck(t)
	const typ = "shopify--knowledge-base-fact"
	res, err := client.GetMetaobjectDefinitionByType(context.Background(), acctest.Client(t), typ)
	if err != nil {
		t.Fatal(err)
	}
	if res.MetaobjectDefinitionByType == nil {
		t.Skipf("dev store has no standard metaobject definition %s", typ)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`data "shopify_metaobject_definition" "test" { type = %q }`, typ),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(dsAddr, "id", regexp.MustCompile(`^gid://shopify/MetaobjectDefinition/\d+$`)),
				resource.TestCheckResourceAttr(dsAddr, "type", typ),
				resource.TestCheckResourceAttrSet(dsAddr, "field.0.key"),
				// Shopify's default PUBLIC_READ_WRITE can't be set, so it reads back as null.
				resource.TestCheckNoResourceAttr(dsAddr, "access.admin"),
			),
		}},
	})
}

func TestAccMetaobjectDefinitionDataSource_notFound(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      `data "shopify_metaobject_definition" "test" { type = "tfacc_missing" }`,
			ExpectError: regexp.MustCompile(`no\s+metaobject\s+definition\s+of\s+type\s+tfacc_missing`),
		}},
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
