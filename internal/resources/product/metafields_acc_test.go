package product_test

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

// mfNamespace is random, so a metafield left by a failed cleanup can't block the next run.
func mfNamespace() string {
	return "tfacc_" + strings.ToLower(sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlpha))
}

func liveMetafields(t *testing.T, handle string) map[string]string {
	t.Helper()
	p := mustLookup(t, handle)
	out := map[string]string{}
	var after *string
	for {
		res, err := client.GetProductMetafields(context.Background(), acctest.Client(t), p.Id, after)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range res.Product.Metafields.Nodes {
			out[n.Namespace+"."+n.Key] = n.Value
		}
		if !res.Product.Metafields.PageInfo.HasNextPage {
			return out
		}
		after = res.Product.Metafields.PageInfo.EndCursor
	}
}

// setLive writes a metafield through the client, outside Terraform.
func setLive(t *testing.T, handle, ns, key, typ, value string) {
	t.Helper()
	res, err := client.SetMetafields(context.Background(), acctest.Client(t),
		[]client.MetafieldsSetInput{{OwnerId: mustLookup(t, handle).Id, Namespace: &ns, Key: key, Type: &typ, Value: value}})
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.MetafieldsSet.UserErrors; len(errs) > 0 {
		t.Fatalf("metafieldsSet: %v", errs)
	}
}

// checkLive compares live metafields with want; "" means the key must be absent.
func checkLive(t *testing.T, handle string, want map[string]string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got := liveMetafields(t, handle)
		for k, v := range want {
			if got[k] != v {
				return fmt.Errorf("metafield %s = %q, want %q", k, got[k], v)
			}
		}
		return nil
	}
}

func mfConfig(handle, metafields string) string {
	return config(fmt.Sprintf(`
  handle              = %q
  title               = "tf-acc metafields"
  status              = "DRAFT"
  allow_data_deletion = true
  metafields = {
%s
  }
`, handle, metafields))
}

func mfEntry(key, typ, value string) string {
	return fmt.Sprintf("    %q = { type = %q, value = %s }\n", key, typ, value)
}

func TestAccProductMetafields_lifecycle(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	handle, ns := testHandle(), mfNamespace()
	cleanup(t, handle)
	k := func(name string) string { return ns + "." + name }
	both := mfEntry(k("count"), "number_integer", `"007"`) + mfEntry(k("txt"), "single_line_text_field", `"a"`) +
		mfEntry(k("tags"), "list.single_line_text_field", `jsonencode(["x", "y"])`)
	updated := mfEntry(k("count"), "number_integer", `"007"`) + mfEntry(k("txt"), "single_line_text_field", `"b"`) +
		mfEntry(k("tags"), "list.single_line_text_field", `jsonencode(["x", "y"])`)
	dropped := mfEntry(k("count"), "number_integer", `"007"`) + mfEntry(k("txt"), "single_line_text_field", `"b"`)
	cleared := mfEntry(k("count"), "number_integer", `"007"`) + mfEntry(k("txt"), "single_line_text_field", `""`)
	value := func(key string, check knownvalue.Check) statecheck.StateCheck {
		return stateValue("metafields", knownvalue.MapPartial(map[string]knownvalue.Check{key: knownvalue.ObjectPartial(map[string]knownvalue.Check{"value": check})}))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{
				Config: mfConfig(handle, both),
				// State keeps the configured "007" although Shopify stores 7.
				ConfigStateChecks: []statecheck.StateCheck{value(k("count"), str("007")), value(k("tags"), str(`["x","y"]`))},
				Check:             checkLive(t, handle, map[string]string{k("count"): "7", k("txt"): "a", k("tags"): `["x","y"]`}),
			},
			{Config: mfConfig(handle, both), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{
				Config:            mfConfig(handle, updated),
				ConfigStateChecks: []statecheck.StateCheck{value(k("txt"), str("b"))},
				Check:             checkLive(t, handle, map[string]string{k("txt"): "b"}),
			},
			{
				// A dropped key stops being managed and keeps its value.
				Config: mfConfig(handle, dropped),
				Check:  checkLive(t, handle, map[string]string{k("tags"): `["x","y"]`}),
			},
			{
				// An unmanaged key edited elsewhere is no drift.
				PreConfig:        func() { setLive(t, handle, ns, "other", "single_line_text_field", "admin") },
				Config:           mfConfig(handle, dropped),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				PreConfig:          func() { setLive(t, handle, ns, "txt", "single_line_text_field", "admin edit") },
				Config:             mfConfig(handle, dropped),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				// Apply restores the managed value.
				Config: mfConfig(handle, dropped),
				Check:  checkLive(t, handle, map[string]string{k("txt"): "b"}),
			},
			{
				Config:            mfConfig(handle, cleared),
				ConfigStateChecks: []statecheck.StateCheck{value(k("txt"), str(""))},
				Check: func(*terraform.State) error {
					if v, ok := liveMetafields(t, handle)[k("txt")]; ok {
						return fmt.Errorf("metafield txt still has %q", v)
					}
					return nil
				},
			},
			{Config: mfConfig(handle, cleared), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{
				// Import takes every key outside app namespaces, the unmanaged ones included.
				Config:          mfConfig(handle, cleared),
				ResourceName:    addr,
				ImportState:     true,
				ImportStateId:   handle,
				ImportStateKind: resource.ImportCommandWithID,
				ImportStateCheck: func(s []*terraform.InstanceState) error {
					a := s[0].Attributes
					for _, key := range []string{k("count"), k("tags"), ns + ".other"} {
						if a["metafields."+key+".type"] == "" {
							return fmt.Errorf("import lacks %s: %v", key, a)
						}
					}
					if _, ok := a["metafields."+k("txt")+".value"]; ok {
						return fmt.Errorf("import has the cleared %s", k("txt"))
					}
					return nil
				},
			},
			{
				// Generated config must pass validation and plan as import-only.
				Config:          mfConfig(handle, cleared),
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
		},
	})
}

func TestAccProductMetafields_invalid(t *testing.T) {
	acctest.Setup(t)
	handle, ns := testHandle(), mfNamespace()
	cleanup(t, handle)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{Config: mfConfig(handle, mfEntry("app--123--x.key", "single_line_text_field", `"v"`)), PlanOnly: true, ExpectError: regexp.MustCompile(`app-owned namespace`)},
			{Config: mfConfig(handle, mfEntry("nodot", "single_line_text_field", `"v"`)), PlanOnly: true, ExpectError: regexp.MustCompile(`must be "namespace.key"`)},
			{Config: mfConfig(handle, mfEntry(ns+".count", "number_integer", `"abc"`)), ExpectError: regexp.MustCompile(`Value must be an integer`)},
		},
	})
}

// A metaobject_reference value comes from a resource reference, since GIDs differ per store.
func TestAccProductMetafields_metaobjectReference(t *testing.T) {
	acctest.Setup(t)
	handle, ns := testHandle(), mfNamespace()
	typ := "tfacc_" + strings.ToLower(sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlpha))
	cleanup(t, handle)
	cfg := fmt.Sprintf(`
resource "shopify_metaobject_definition" "ref" {
  type                = %q
  name                = "Acceptance test"
  display_name_key    = "title"
  allow_data_deletion = true
  field {
    key  = "title"
    name = "Title"
    type = "single_line_text_field"
  }
}

resource "shopify_metaobject" "ref" {
  type   = shopify_metaobject_definition.ref.type
  handle = "tfacc-ref"
  fields = { title = "Ref" }
  allow_data_deletion = true
}

resource "shopify_metafield_definition" "ref" {
  owner_type          = "PRODUCT"
  namespace           = %q
  key                 = "ref"
  name                = "Ref"
  type                = "metaobject_reference"
  validations         = { metaobject_definition_id = shopify_metaobject_definition.ref.id }
  allow_data_deletion = true
}

resource "shopify_product" "test" {
  handle              = %q
  title               = "tf-acc metafields"
  status              = "DRAFT"
  allow_data_deletion = true
  metafields = {
    "%s.ref" = { type = "metaobject_reference", value = shopify_metaobject.ref.id }
  }
  depends_on = [shopify_metafield_definition.ref]
}
`, typ, ns, handle, ns)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: func(*terraform.State) error {
					if v := liveMetafields(t, handle)[ns+".ref"]; !strings.HasPrefix(v, "gid://shopify/Metaobject/") {
						return fmt.Errorf("reference = %q", v)
					}
					return nil
				},
			},
			{Config: cfg, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		},
	})
}

func TestAccListProduct_metafields(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	handle, ns := testHandle(), mfNamespace()
	cleanup(t, handle)
	cfg := mfConfig(handle, mfEntry(ns+".txt", "single_line_text_field", `"listed"`))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				Query:  true,
				Config: listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectResourceKnownValues("shopify_product.all",
						queryfilter.ByDisplayName(knownvalue.StringExact("tf-acc metafields ("+handle+")")),
						[]querycheck.KnownValueCheck{{
							Path: tfjsonpath.New("metafields"),
							KnownValue: knownvalue.MapExact(map[string]knownvalue.Check{
								ns + ".txt": knownvalue.ObjectExact(map[string]knownvalue.Check{"type": str("single_line_text_field"), "value": str("listed")}),
							}),
						}}),
				},
			},
		},
	})
}
