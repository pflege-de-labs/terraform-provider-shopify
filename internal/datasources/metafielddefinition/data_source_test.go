package metafielddefinition_test

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	sdkacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const (
	dsAddr  = "data.shopify_metafield_definition.test"
	resAddr = "shopify_metafield_definition.test"
)

// Every attribute the data source mirrors from the resource.
var mirrored = []string{
	"id", "owner_type", "namespace", "key", "type", "name", "description",
	"validations.%", "validations.min", "validations.max",
	"access.admin", "access.storefront", "access.customer_account",
	"pin", "use_as_collection_condition",
}

func TestAccMetafieldDefinitionDataSource_mirrorsResource(t *testing.T) {
	// A test-only namespace: only definitions created here are ever deleted.
	ns := "tfacc_" + sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlpha)
	checks := []resource.TestCheckFunc{
		resource.TestCheckResourceAttr(dsAddr, "name", "Care level"),
		resource.TestCheckResourceAttr(dsAddr, "validations.max", "5"),
		resource.TestCheckResourceAttr(dsAddr, "access.storefront", "PUBLIC_READ"),
		resource.TestCheckResourceAttr(dsAddr, "pin", "true"),
	}
	for _, a := range mirrored {
		checks = append(checks, resource.TestCheckResourceAttrPair(dsAddr, a, resAddr, a))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkDestroyed(t, ns),
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
resource "shopify_metafield_definition" "test" {
  owner_type          = "PRODUCT"
  namespace           = %q
  key                 = "care_level"
  type                = "number_integer"
  name                = "Care level"
  description         = "How much care"
  validations         = { min = "1", max = "5" }
  access              = { storefront = "PUBLIC_READ" }
  pin                 = true
  allow_data_deletion = true
}

data "shopify_metafield_definition" "test" {
  owner_type = shopify_metafield_definition.test.owner_type
  namespace  = shopify_metafield_definition.test.namespace
  key        = shopify_metafield_definition.test.key
}
`, ns),
			Check: resource.ComposeAggregateTestCheckFunc(checks...),
		}},
	})
}

// The dev store has no Shopify-standard metafield definitions unless one was enabled in the admin.
func TestAccMetafieldDefinitionDataSource_shopifyStandard(t *testing.T) {
	acctest.Setup(t)
	ns := "shopify"
	res, err := client.GetMetafieldDefinition(context.Background(), acctest.Client(t), client.MetafieldDefinitionIdentifierInput{
		OwnerType: client.MetafieldOwnerTypeProduct, Namespace: &ns, Key: "material",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.MetafieldDefinition == nil {
		t.Skip("dev store has no standard metafield definition PRODUCT/shopify/material")
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
data "shopify_metafield_definition" "test" {
  owner_type = "PRODUCT"
  namespace  = "shopify"
  key        = "material"
}
`,
			Check: resource.TestMatchResourceAttr(dsAddr, "id", regexp.MustCompile(`^gid://shopify/MetafieldDefinition/\d+$`)),
		}},
	})
}

func TestAccMetafieldDefinitionDataSource_notFound(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
data "shopify_metafield_definition" "test" {
  owner_type = "PRODUCT"
  namespace  = "tfacc_missing"
  key        = "nope"
}
`,
			// Terraform wraps long error text, so match on whitespace.
			ExpectError: regexp.MustCompile(`no\s+metafield\s+definition\s+PRODUCT/tfacc_missing/nope`),
		}},
	})
}

func checkDestroyed(t *testing.T, ns string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		res, err := client.GetMetafieldDefinition(context.Background(), acctest.Client(t), client.MetafieldDefinitionIdentifierInput{
			OwnerType: client.MetafieldOwnerTypeProduct, Namespace: &ns, Key: "care_level",
		})
		if err != nil {
			return err
		}
		if res.MetafieldDefinition != nil {
			return fmt.Errorf("metafield definition %s/care_level still exists", ns)
		}
		return nil
	}
}
