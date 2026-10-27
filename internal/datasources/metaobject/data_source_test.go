package metaobject_test

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	sdkacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const dsAddr = "data.shopify_metaobject.test"

func TestAccMetaobjectDataSource_entryOfOwnDefinition(t *testing.T) {
	acctest.Setup(t)
	// A test-only type: only definitions created here are ever deleted.
	typ := "tfacc_" + strings.ToLower(sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlpha))
	const handle = "entry-one"
	definition := fmt.Sprintf(`
resource "shopify_metaobject_definition" "test" {
  type                = %q
  name                = "Data source test"
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
    key  = "summary"
    type = "single_line_text_field"
    name = "Summary"
  }
  field {
    key  = "tags"
    type = "list.single_line_text_field"
    name = "Tags"
  }
}
`, typ)
	var entryID string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: definition},
			{
				PreConfig: func() { entryID = createEntry(t, typ, handle) },
				Config: definition + fmt.Sprintf(`
data "shopify_metaobject" "test" {
  type   = shopify_metaobject_definition.test.type
  handle = %q
}

output "summary_is_null" { value = data.shopify_metaobject.test.fields["summary"] == null }
`, handle),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(dsAddr, "id", regexp.MustCompile(`^gid://shopify/Metaobject/\d+$`)),
					resource.TestCheckResourceAttr(dsAddr, "type", typ),
					resource.TestCheckResourceAttr(dsAddr, "handle", handle),
					resource.TestCheckResourceAttr(dsAddr, "display_name", "First entry"),
					resource.TestCheckResourceAttr(dsAddr, "status", "ACTIVE"),
					resource.TestCheckResourceAttrSet(dsAddr, "updated_at"),
					resource.TestCheckResourceAttr(dsAddr, "fields.title", "First entry"),
					resource.TestCheckResourceAttr(dsAddr, "fields.tags", `["a","b"]`),
					resource.TestCheckOutput("summary_is_null", "true"),
				),
			},
			// The entry goes before the definition does, so the destroy has nothing async left to wait for.
			{
				PreConfig: func() { deleteEntry(t, entryID) },
				Config:    definition,
			},
		},
	})
}

// Shopify enables some standard definitions itself; the dev store had this entry (2026-10-26).
func TestAccMetaobjectDataSource_shopifyStandard(t *testing.T) {
	acctest.Setup(t)
	const typ, handle = "shopify--knowledge-base-fact", "delivery_countries"
	res, err := client.GetMetaobjectByHandle(context.Background(), acctest.Client(t), typ, handle)
	if err != nil {
		t.Fatal(err)
	}
	if res.MetaobjectByHandle == nil {
		t.Skipf("dev store has no standard metaobject %s/%s", typ, handle)
	}
	live := res.MetaobjectByHandle
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`data "shopify_metaobject" "test" {
  type   = %q
  handle = %q
}`, typ, handle),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(dsAddr, "id", live.Id),
				resource.TestCheckResourceAttr(dsAddr, "display_name", live.DisplayName),
				resource.TestCheckResourceAttr(dsAddr, "fields.%", strconv.Itoa(len(live.Fields))),
				resource.TestCheckResourceAttrSet(dsAddr, "updated_at"),
				// The standard definition has no publishable capability.
				resource.TestCheckNoResourceAttr(dsAddr, "status"),
			),
		}},
	})
}

func TestAccMetaobjectDataSource_notFound(t *testing.T) {
	acctest.Setup(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `data "shopify_metaobject" "test" {
  type   = "tfacc_missing"
  handle = "nope"
}`,
			ExpectError: regexp.MustCompile(`no\s+metaobject\s+tfacc_missing/nope`),
		}},
	})
}

func createEntry(t *testing.T, typ, handle string) string {
	t.Helper()
	title, tags := "First entry", `["a","b"]`
	res, err := client.CreateMetaobject(context.Background(), acctest.Client(t), client.MetaobjectCreateInput{
		Type:   typ,
		Handle: &handle,
		Fields: []client.MetaobjectFieldInput{{Key: "title", Value: title}, {Key: "tags", Value: tags}},
		Capabilities: &client.MetaobjectCapabilityDataInput{
			Publishable: &client.MetaobjectCapabilityDataPublishableInput{Status: client.MetaobjectStatusActive},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	p := res.MetaobjectCreate
	if p == nil {
		t.Fatal("metaobjectCreate returned no payload")
	}
	if len(p.UserErrors) > 0 || p.Metaobject == nil {
		t.Fatalf("metaobjectCreate: %v", p.UserErrors)
	}
	return p.Metaobject.Id
}

func deleteEntry(t *testing.T, id string) {
	t.Helper()
	res, err := client.DeleteMetaobject(context.Background(), acctest.Client(t), id)
	if err != nil {
		t.Fatal(err)
	}
	p := res.MetaobjectDelete
	if p == nil {
		t.Fatal("metaobjectDelete returned no payload")
	}
	if len(p.UserErrors) > 0 {
		t.Fatalf("metaobjectDelete: %v", p.UserErrors)
	}
}
