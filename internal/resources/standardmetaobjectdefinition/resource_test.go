package standardmetaobjectdefinition_test

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
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/querycheck/queryfilter"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const (
	addr = "shopify_standard_metaobject_definition.test"
	// Enabled and deleted again by _basic; the dev store has no entries of it.
	material = "shopify--material"
	// Enabled by Shopify on the dev store, with entries; tests only adopt it and never delete it.
	knowledgeBase = "shopify--knowledge-base-fact"
)

var gidRe = regexp.MustCompile(`^gid://shopify/MetaobjectDefinition/\d+$`)

func config(typ string) string {
	return fmt.Sprintf(`
resource "shopify_standard_metaobject_definition" "test" {
  type = %q
}
`, typ)
}

func lookup(t *testing.T, typ string) *client.MetaobjectDefinitionFields {
	t.Helper()
	res, err := client.GetMetaobjectDefinitionByType(context.Background(), acctest.Client(t), typ)
	if err != nil {
		t.Fatal(err)
	}
	if res.MetaobjectDefinitionByType == nil {
		return nil
	}
	return &res.MetaobjectDefinitionByType.MetaobjectDefinitionFields
}

func deleteDef(t *testing.T, id string) {
	t.Helper()
	res, err := client.DeleteMetaobjectDefinition(context.Background(), acctest.Client(t), id)
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.MetaobjectDefinitionDelete.UserErrors; len(errs) > 0 {
		t.Fatalf("deleting %s: %+v", id, errs)
	}
}

func enable(t *testing.T, typ string) string {
	t.Helper()
	res, err := client.EnableStandardMetaobjectDefinition(context.Background(), acctest.Client(t), typ)
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.StandardMetaobjectDefinitionEnable.UserErrors; len(errs) > 0 {
		t.Fatalf("enabling %s: %+v", typ, errs)
	}
	return res.StandardMetaobjectDefinitionEnable.MetaobjectDefinition.Id
}

// checkKept fails unless destroy left the definition enabled, with this GID when one is given.
func checkKept(t *testing.T, typ, id string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		d := lookup(t, typ)
		if d == nil || (id != "" && d.Id != id) {
			return fmt.Errorf("%s: want the definition kept after destroy (id %q), got %+v", typ, id, d)
		}
		return nil
	}
}

func TestAccStandardMetaobjectDefinition_basic(t *testing.T) {
	acctest.Setup(t)
	if lookup(t, material) != nil {
		t.Fatalf("%s is already enabled on the dev store; delete it in the admin if it is a leftover of an aborted run", material)
	}
	t.Cleanup(func() {
		if d := lookup(t, material); d != nil {
			deleteDef(t, d.Id)
		}
	})
	expectIdentity := statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(gidRe)})
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkKept(t, material, ""),
		Steps: []resource.TestStep{
			{Config: config("shopify--tf-acc-no-such-template"), ExpectError: regexp.MustCompile(`Record not found`)},
			{
				Config: config(material),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringRegexp(gidRe)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("type"), knownvalue.StringExact(material)),
				},
			},
			{RefreshState: true},
			{
				// Refresh steps can't carry state checks; this no-op step checks identity survives the refresh.
				Config:            config(material),
				ConfigStateChecks: []statecheck.StateCheck{expectIdentity},
				ConfigPlanChecks:  resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				// Generated config must pass validation and plan as import-only.
				Config:          config(material),
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{ResourceName: addr, ImportState: true, ImportStateId: material, ImportStateVerify: true},
		},
	})
}

// Read re-adopts a type re-enabled outside Terraform under its new GID, and plans a create once it is gone.
func TestAccStandardMetaobjectDefinition_drift(t *testing.T) {
	acctest.Setup(t)
	if lookup(t, material) != nil {
		t.Fatalf("%s is already enabled on the dev store; delete it in the admin if it is a leftover of an aborted run", material)
	}
	t.Cleanup(func() {
		if d := lookup(t, material); d != nil {
			deleteDef(t, d.Id)
		}
	})
	var reenabled string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkKept(t, material, ""),
		Steps: []resource.TestStep{
			{Config: config(material)},
			{
				PreConfig: func() {
					old := lookup(t, material).Id
					deleteDef(t, old)
					if reenabled = enable(t, material); reenabled == old {
						t.Fatal("re-enabling kept the GID; this step no longer tests the fallback by type")
					}
				},
				Config:           config(material),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            resource.TestCheckResourceAttrPtr(addr, "id", &reenabled),
			},
			{
				PreConfig: func() { deleteDef(t, lookup(t, material).Id) },
				Config:    config(material),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)},
				},
			},
		},
	})
}

// A merchant definition is not taken over: import fails and points to shopify_metaobject_definition.
func TestAccStandardMetaobjectDefinition_notStandard(t *testing.T) {
	acctest.Setup(t)
	typ := "tfacc_" + strings.ToLower(sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlpha))
	res, err := client.CreateMetaobjectDefinition(context.Background(), acctest.Client(t), client.MetaobjectDefinitionCreateInput{
		Type:             typ,
		FieldDefinitions: []client.MetaobjectFieldDefinitionCreateInput{{Key: "title", Type: "single_line_text_field"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.MetaobjectDefinitionCreate.UserErrors; len(errs) > 0 {
		t.Fatalf("creating %s: %+v", typ, errs)
	}
	t.Cleanup(func() { deleteDef(t, res.MetaobjectDefinitionCreate.MetaobjectDefinition.Id) })
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:        config(typ),
			ResourceName:  addr,
			ImportState:   true,
			ImportStateId: typ,
			ExpectError:   regexp.MustCompile(`has no standard template`),
		}},
	})
}

// Enabling is idempotent, so an enabled type is adopted under its GID and destroy keeps it.
func TestAccStandardMetaobjectDefinition_adopt(t *testing.T) {
	acctest.Setup(t)
	pre := lookup(t, knowledgeBase)
	if pre == nil {
		t.Skipf("%s is not enabled on the dev store", knowledgeBase)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkKept(t, knowledgeBase, pre.Id),
		Steps: []resource.TestStep{{
			Config: config(knowledgeBase),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact(pre.Id)),
			},
		}},
	})
}

const listConfig = `
provider "shopify" {}

list "shopify_standard_metaobject_definition" "all" {
  provider         = shopify
  include_resource = true
}

list "shopify_metaobject_definition" "all" {
  provider = shopify
}
`

// The standard list holds the enabled standard definitions, and the ordinary list leaves them out (D7).
func TestAccListStandardMetaobjectDefinition(t *testing.T) {
	acctest.Setup(t)
	pre := lookup(t, knowledgeBase)
	if pre == nil {
		t.Skipf("%s is not enabled on the dev store", knowledgeBase)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		Steps: []resource.TestStep{{
			Query:  true,
			Config: listConfig,
			QueryResultChecks: []querycheck.QueryResultCheck{
				querycheck.ExpectIdentity("shopify_standard_metaobject_definition.all", map[string]knownvalue.Check{"id": knownvalue.StringExact(pre.Id)}),
				querycheck.ExpectResourceKnownValues("shopify_standard_metaobject_definition.all",
					queryfilter.ByDisplayName(knownvalue.StringExact(knowledgeBase)),
					[]querycheck.KnownValueCheck{
						{Path: tfjsonpath.New("id"), KnownValue: knownvalue.StringExact(pre.Id)},
						{Path: tfjsonpath.New("type"), KnownValue: knownvalue.StringExact(knowledgeBase)},
					}),
				querycheck.ExpectNoIdentity("shopify_metaobject_definition.all", map[string]knownvalue.Check{"id": knownvalue.StringExact(pre.Id)}),
			},
		}},
	})
}
