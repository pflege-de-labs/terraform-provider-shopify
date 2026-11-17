package metaobject_test

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

const addr = "shopify_metaobject.test"

var gidRe = regexp.MustCompile(`^gid://shopify/Metaobject/\d+$`)

// testType is random, so a definition left by a failed cleanup can't block the next run.
func testType() string {
	return "tfacc_" + strings.ToLower(sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlpha))
}

// definition makes a publishable test type; allow_data_deletion lets the test destroy it with its entries.
func definition(typ string) string {
	return fmt.Sprintf(`
resource "shopify_metaobject_definition" "test" {
  type                = %q
  name                = "Acceptance test"
  display_name_key    = "title"
  allow_data_deletion = true

  capabilities = {
    publishable = true
  }

  field {
    key  = "title"
    name = "Title"
    type = "single_line_text_field"
  }
  field {
    key  = "num"
    name = "Num"
    type = "number_integer"
  }
  field {
    key  = "tags"
    name = "Tags"
    type = "list.single_line_text_field"
  }
  field {
    key  = "note"
    name = "Note"
    type = "multi_line_text_field"
  }
}
`, typ)
}

// entry is the resource under test; body goes inside the block after type.
func entry(typ, body string) string {
	return definition(typ) + fmt.Sprintf(`
resource "shopify_metaobject" "test" {
  type = shopify_metaobject_definition.test.type
%s
}
`, body)
}

func lookup(t *testing.T, typ, handle string) *client.MetaobjectFields {
	t.Helper()
	res, err := client.GetMetaobjectByHandle(context.Background(), acctest.Client(t), typ, handle)
	if err != nil {
		t.Fatal(err)
	}
	if res.MetaobjectByHandle == nil {
		return nil
	}
	return &res.MetaobjectByHandle.MetaobjectFields
}

// setField writes one value through the client, as an admin edit would.
func setField(t *testing.T, typ, handle, key, value string) {
	t.Helper()
	o := lookup(t, typ, handle)
	if o == nil {
		t.Fatalf("no entry %s/%s", typ, handle)
	}
	res, err := client.UpdateMetaobject(context.Background(), acctest.Client(t), o.Id,
		client.MetaobjectUpdateInput{Fields: []client.MetaobjectFieldInput{{Key: key, Value: value}}})
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.MetaobjectUpdate.UserErrors; len(errs) > 0 {
		t.Fatalf("updating %s/%s: %+v", typ, handle, errs)
	}
}

// createEntry makes an entry through the client, as the admin would; status is sent unless empty.
func createEntry(t *testing.T, typ, handle string, fields map[string]string, status client.MetaobjectStatus) string {
	t.Helper()
	in := client.MetaobjectCreateInput{Type: typ, Handle: &handle}
	for k, v := range fields {
		in.Fields = append(in.Fields, client.MetaobjectFieldInput{Key: k, Value: v})
	}
	if status != "" {
		in.Capabilities = &client.MetaobjectCapabilityDataInput{Publishable: &client.MetaobjectCapabilityDataPublishableInput{Status: status}}
	}
	res, err := client.CreateMetaobject(context.Background(), acctest.Client(t), in)
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.MetaobjectCreate.UserErrors; len(errs) > 0 {
		t.Fatalf("creating %s/%s: %+v", typ, handle, errs)
	}
	return res.MetaobjectCreate.Metaobject.Id
}

func deleteEntry(t *testing.T, id string) {
	t.Helper()
	res, err := client.DeleteMetaobject(context.Background(), acctest.Client(t), id)
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.MetaobjectDelete.UserErrors; len(errs) > 0 {
		t.Fatalf("deleting %s: %+v", id, errs)
	}
}

func checkGone(t *testing.T, typ string, handles ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for _, h := range handles {
			if o := lookup(t, typ, h); o != nil {
				return fmt.Errorf("entry %s/%s still exists", typ, h)
			}
		}
		return nil
	}
}

func field(key string) tfjsonpath.Path { return tfjsonpath.New("fields").AtMapKey(key) }

func TestAccMetaobject_basic(t *testing.T) {
	acctest.Setup(t)
	typ := testType()
	first := entry(typ, `
  handle              = "eins"
  allow_data_deletion = true
  fields = {
    title = "Eins"
    num   = "007"
    tags  = "[\"x\",  \"y\"]"
  }
`)
	second := entry(typ, `
  handle              = "zwei"
  status              = "ACTIVE"
  allow_data_deletion = true
  fields = {
    title = "Zwei"
    num   = "8"
    tags  = jsonencode(["y"])
  }
`)
	expectIdentity := statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(gidRe)})
	var recreated string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, typ, "eins", "zwei"),
		Steps: []resource.TestStep{
			{
				Config: first,
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringRegexp(gidRe)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("type"), knownvalue.StringExact(typ)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("handle"), knownvalue.StringExact("eins")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("status"), knownvalue.StringExact("DRAFT")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("display_name"), knownvalue.StringExact("Eins")),
					// Written values stay as configured although Shopify stores 7 and ["x","y"].
					statecheck.ExpectKnownValue(addr, field("num"), knownvalue.StringExact("007")),
					statecheck.ExpectKnownValue(addr, field("tags"), knownvalue.StringExact(`["x",  "y"]`)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("fields"), knownvalue.MapSizeExact(3)),
				},
			},
			{
				// Reformatted values don't show as drift after a refresh.
				Config:           first,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: second,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("handle"), knownvalue.StringExact("zwei")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("status"), knownvalue.StringExact("ACTIVE")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("display_name"), knownvalue.StringExact("Zwei")),
					statecheck.ExpectKnownValue(addr, field("num"), knownvalue.StringExact("8")),
				},
			},
			{
				// An admin edit to a managed field shows in the plan and is reverted.
				PreConfig: func() { setField(t, typ, "zwei", "title", "Admin") },
				Config:    second,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{statecheck.ExpectKnownValue(addr, field("title"), knownvalue.StringExact("Zwei"))},
			},
			{
				// An admin edit to an unmanaged field doesn't.
				PreConfig:        func() { setField(t, typ, "zwei", "note", "admin note") },
				Config:           second,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				// An entry recreated outside Terraform is found again by type and handle, under its new GID.
				PreConfig: func() {
					old := lookup(t, typ, "zwei").Id
					deleteEntry(t, old)
					recreated = createEntry(t, typ, "zwei", map[string]string{"title": "Zwei", "num": "8", "tags": `["y"]`}, client.MetaobjectStatusActive)
					if recreated == old {
						t.Fatal("recreating kept the GID; this step no longer tests the fallback")
					}
				},
				Config:           second,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            resource.TestCheckResourceAttrPtr(addr, "id", &recreated),
			},
			{RefreshState: true},
			{
				// Generated config (every field with a value) must pass validation and plan as import-only.
				Config:          second,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateId:     typ + "/zwei",
				ImportStateVerify: true,
				// Import takes every field with a value, note included; the provider-only flag defaults to false.
				ImportStateVerifyIgnore: []string{"fields.%", "fields.note", "allow_data_deletion"},
			},
			{
				// Clearing a value with "" is stable.
				Config: entry(typ, `
  handle              = "zwei"
  allow_data_deletion = true
  fields = {
    title = "Zwei"
    note  = ""
  }
`),
				ConfigStateChecks: []statecheck.StateCheck{statecheck.ExpectKnownValue(addr, field("note"), knownvalue.StringExact(""))},
			},
			{
				Config: entry(typ, `
  handle              = "zwei"
  allow_data_deletion = true
  fields = {
    title = "Zwei"
    note  = ""
  }
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
		},
	})
}

// Invalid values and unknown keys fail with Shopify's message; the map-key path is unit-tested (TestErrorPath).
func TestAccMetaobject_invalidField(t *testing.T) {
	acctest.Setup(t)
	typ := testType()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: entry(typ, `
  handle = "kaputt"
  fields = { num = "zwei" }
`), ExpectError: regexp.MustCompile(`Value must be an integer`)},
			{Config: entry(typ, `
  handle = "kaputt"
  fields = { nope = "x" }
`), ExpectError: regexp.MustCompile(`"nope" does not exist`)},
		},
	})
}

// Destroying an entry needs allow_data_deletion in state first.
func TestAccMetaobject_deletionGuard(t *testing.T) {
	acctest.Setup(t)
	typ := testType()
	body := func(allow bool) string {
		return fmt.Sprintf(`
  handle              = "geschuetzt"
  allow_data_deletion = %t
  fields = { title = "Geschützt" }
`, allow)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, typ, "geschuetzt"),
		Steps: []resource.TestStep{
			{Config: entry(typ, body(false))},
			{Config: definition(typ), ExpectError: regexp.MustCompile(`would delete its data`)},
			{Config: entry(typ, body(true))},
			{Config: definition(typ), Check: checkGone(t, typ, "geschuetzt")},
		},
	})
}

// An existing handle needs adopt_existing; destroy then leaves the entry in place.
func TestAccMetaobject_adopt(t *testing.T) {
	acctest.Setup(t)
	typ := testType()
	var id string
	body := func(adopt bool) string {
		return fmt.Sprintf(`
  handle         = "da"
  adopt_existing = %t
  fields         = { title = "Übernommen" }
`, adopt)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: definition(typ)},
			{
				PreConfig:   func() { id = createEntry(t, typ, "da", map[string]string{"title": "Schon da"}, "") },
				Config:      entry(typ, body(false)),
				ExpectError: regexp.MustCompile(`adopt_existing = true`),
			},
			{
				Config: entry(typ, body(true)),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPtr(addr, "id", &id),
					resource.TestCheckResourceAttr(addr, "fields.title", "Übernommen"),
				),
			},
			{
				Config: definition(typ),
				Check: func(*terraform.State) error {
					if o := lookup(t, typ, "da"); o == nil || o.Id != id {
						return fmt.Errorf("adopted entry should stay after destroy, got %+v", o)
					}
					return nil
				},
			},
		},
	})
}

func TestAccListMetaobject(t *testing.T) {
	acctest.Setup(t)
	typ := testType()
	cfg := entry(typ, `
  handle              = "gelistet"
  allow_data_deletion = true
  fields = {
    title = "Gelistet"
    num   = "007"
  }
`)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkGone(t, typ, "gelistet"),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				Query: true,
				Config: fmt.Sprintf(`
provider "shopify" {}

list "shopify_metaobject" "typed" {
  provider         = shopify
  include_resource = true
  config {
    type = %q
  }
}

list "shopify_metaobject" "all" {
  provider = shopify
}
`, typ),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLength("shopify_metaobject.typed", 1),
					querycheck.ExpectResourceKnownValues("shopify_metaobject.typed",
						queryfilter.ByDisplayName(knownvalue.StringExact(typ+"/gelistet")),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("id"), KnownValue: knownvalue.StringRegexp(gidRe)},
							{Path: tfjsonpath.New("type"), KnownValue: knownvalue.StringExact(typ)},
							{Path: tfjsonpath.New("handle"), KnownValue: knownvalue.StringExact("gelistet")},
							{Path: field("title"), KnownValue: knownvalue.StringExact("Gelistet")},
							// The list shows Shopify's stored form.
							{Path: field("num"), KnownValue: knownvalue.StringExact("7")},
							{Path: tfjsonpath.New("fields"), KnownValue: knownvalue.MapSizeExact(2)},
							{Path: tfjsonpath.New("status"), KnownValue: knownvalue.StringExact("DRAFT")},
							{Path: tfjsonpath.New("allow_data_deletion"), KnownValue: knownvalue.Bool(false)},
						}),
					// The unfiltered list walks every type, this one included.
					querycheck.ExpectResourceDisplayName("shopify_metaobject.all",
						queryfilter.ByDisplayName(knownvalue.StringExact(typ+"/gelistet")), knownvalue.StringExact(typ+"/gelistet")),
				},
			},
		},
	})
}

// The use case of #48: an entry in a Shopify standard type, enabled by shopify_standard_metaobject_definition.
// shopify--knowledge-base-fact is enabled on the dev store already, so it is only adopted and stays after destroy.
func TestAccMetaobject_standardType(t *testing.T) {
	acctest.Setup(t)
	const typ = "shopify--knowledge-base-fact"
	res, err := client.GetMetaobjectDefinitionByType(context.Background(), acctest.Client(t), typ)
	if err != nil {
		t.Fatal(err)
	}
	if res.MetaobjectDefinitionByType == nil {
		t.Skipf("%s is not enabled on the dev store", typ)
	}
	handle := "tfacc-" + strings.ToLower(sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlpha))
	// Values in Shopify's stored form, so import verifies them exactly.
	cfg := fmt.Sprintf(`
resource "shopify_standard_metaobject_definition" "kbf" {
  type = %q
}

resource "shopify_metaobject" "test" {
  type                = shopify_standard_metaobject_definition.kbf.type
  handle              = %q
  allow_data_deletion = true
  fields = {
    title             = "TF acceptance test"
    published         = "false"
    value_number      = "1.5"
    value_string_list = jsonencode(["a", "b"])
  }
}
`, typ, handle)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkGone(t, typ, handle),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("type"), knownvalue.StringExact(typ)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("display_name"), knownvalue.StringExact("TF acceptance test")),
					// The type has no publishable capability.
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("status"), knownvalue.Null()),
				},
			},
			{Config: cfg, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateId:           typ + "/" + handle,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"allow_data_deletion"},
			},
			{
				Query: true,
				Config: fmt.Sprintf(`
provider "shopify" {}

list "shopify_metaobject" "kbf" {
  provider         = shopify
  include_resource = true
  config {
    type = %q
  }
}
`, typ),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectResourceKnownValues("shopify_metaobject.kbf",
						queryfilter.ByDisplayName(knownvalue.StringExact(typ+"/"+handle)),
						[]querycheck.KnownValueCheck{
							{Path: field("value_string_list"), KnownValue: knownvalue.StringExact(`["a","b"]`)},
							{Path: tfjsonpath.New("fields"), KnownValue: knownvalue.MapSizeExact(4)},
						}),
				},
			},
		},
	})
}
