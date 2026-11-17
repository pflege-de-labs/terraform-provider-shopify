package collection_test

import (
	"context"
	"fmt"
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
	id := mustLookup(t, handle).Id
	out := map[string]string{}
	var after *string
	for {
		res, err := client.GetCollectionMetafields(context.Background(), acctest.Client(t), id, after)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range res.Collection.Metafields.Nodes {
			out[n.Namespace+"."+n.Key] = n.Value
		}
		if !res.Collection.Metafields.PageInfo.HasNextPage {
			return out
		}
		after = res.Collection.Metafields.PageInfo.EndCursor
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

func mfCollection(handle, entries string) string {
	return fmt.Sprintf(`
resource "shopify_collection" "test" {
  handle = %q
  title  = "tf-acc metafields"
  metafields = {
%s
  }
}
`, handle, entries)
}

func mfEntry(key, typ, value string) string {
	return fmt.Sprintf("    %q = { type = %q, value = %s }\n", key, typ, value)
}

// The shared metafields code is covered in depth by the product tests; this checks the collection wiring.
func TestAccCollectionMetafields(t *testing.T) {
	acctest.Setup(t)
	handle, ns := testHandle(), mfNamespace()
	cleanup(t, handle)
	k := func(name string) string { return ns + "." + name }
	first := mfEntry(k("count"), "number_integer", `"007"`) + mfEntry(k("txt"), "single_line_text_field", `"a"`)
	second := mfEntry(k("count"), "number_integer", `"007"`) + mfEntry(k("txt"), "single_line_text_field", `""`)
	value := func(key string, check knownvalue.Check) statecheck.StateCheck {
		return statecheck.ExpectKnownValue(addr, tfjsonpath.New("metafields"),
			knownvalue.MapPartial(map[string]knownvalue.Check{key: knownvalue.ObjectPartial(map[string]knownvalue.Check{"value": check})}))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{
				Config:            mfCollection(handle, first),
				ConfigStateChecks: []statecheck.StateCheck{value(k("count"), str("007"))},
				Check:             checkLive(t, handle, map[string]string{k("count"): "7", k("txt"): "a"}),
			},
			{
				// An unmanaged key edited elsewhere is no drift; reformatted values aren't either.
				PreConfig:        func() { setLive(t, handle, ns, "other", "single_line_text_field", "admin") },
				Config:           mfCollection(handle, first),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				// A managed key edited elsewhere is drift, and apply restores it.
				PreConfig: func() { setLive(t, handle, ns, "txt", "single_line_text_field", "admin edit") },
				Config:    mfCollection(handle, first),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				Check: checkLive(t, handle, map[string]string{k("txt"): "a"}),
			},
			{
				Config: mfCollection(handle, second),
				Check: func(*terraform.State) error {
					if v, ok := liveMetafields(t, handle)[k("txt")]; ok {
						return fmt.Errorf("metafield txt still has %q", v)
					}
					return nil
				},
			},
			{Config: mfCollection(handle, second), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{
				// Generated config holds every key outside app namespaces and plans import-only.
				Config:          mfCollection(handle, second),
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				Query: true,
				Config: `
provider "shopify" {}

list "shopify_collection" "all" {
  provider         = shopify
  include_resource = true
}
`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectResourceKnownValues("shopify_collection.all",
						queryfilter.ByDisplayName(knownvalue.StringExact("tf-acc metafields ("+handle+")")),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("metafields"), KnownValue: knownvalue.MapExact(map[string]knownvalue.Check{
								k("count"):    knownvalue.ObjectExact(map[string]knownvalue.Check{"type": str("number_integer"), "value": str("7")}),
								ns + ".other": knownvalue.ObjectExact(map[string]knownvalue.Check{"type": str("single_line_text_field"), "value": str("admin")}),
							})},
						}),
				},
			},
		},
	})
}
