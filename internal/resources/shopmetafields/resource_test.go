package shopmetafields_test

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

const addr = "shopify_shop_metafields.test"

// mfNamespace is random per test: all tests share the one shop, and destroy keeps the values.
func mfNamespace() string {
	return "tfacc_" + strings.ToLower(sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlpha))
}

func live(t *testing.T) (string, map[string]string) {
	t.Helper()
	out := map[string]string{}
	var id string
	var after *string
	for {
		res, err := client.GetShopMetafields(context.Background(), acctest.Client(t), after)
		if err != nil {
			t.Fatal(err)
		}
		id = res.Shop.Id
		for _, n := range res.Shop.Metafields.Nodes {
			out[n.Namespace+"."+n.Key] = n.Value
		}
		if !res.Shop.Metafields.PageInfo.HasNextPage {
			return id, out
		}
		after = res.Shop.Metafields.PageInfo.EndCursor
	}
}

// cleanup deletes every key of the namespace, also when the test failed halfway.
func cleanup(t *testing.T, ns string) {
	t.Cleanup(func() {
		id, all := live(t)
		var ids []client.MetafieldIdentifierInput
		for k := range all {
			if n, key, _ := strings.Cut(k, "."); n == ns {
				ids = append(ids, client.MetafieldIdentifierInput{OwnerId: id, Namespace: n, Key: key})
			}
		}
		if len(ids) == 0 {
			return
		}
		if _, err := client.DeleteMetafields(context.Background(), acctest.Client(t), ids); err != nil {
			t.Errorf("cleanup of %s: %v", ns, err)
		}
	})
}

// setLive writes a metafield through the client, outside Terraform.
func setLive(t *testing.T, ns, key, value string) {
	t.Helper()
	id, _ := live(t)
	typ := "single_line_text_field"
	res, err := client.SetMetafields(context.Background(), acctest.Client(t),
		[]client.MetafieldsSetInput{{OwnerId: id, Namespace: &ns, Key: key, Type: &typ, Value: value}})
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.MetafieldsSet.UserErrors; len(errs) > 0 {
		t.Fatalf("metafieldsSet: %v", errs)
	}
}

// checkLive compares live values with want; "" means the key must be absent.
func checkLive(t *testing.T, want map[string]string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		_, got := live(t)
		for k, v := range want {
			if g, ok := got[k]; g != v || (v == "" && ok) {
				return fmt.Errorf("metafield %s = %q, want %q", k, g, v)
			}
		}
		return nil
	}
}

func mfConfig(entries ...string) string {
	return fmt.Sprintf(`
resource "shopify_shop_metafields" "test" {
  metafields = {
%s
  }
}
`, strings.Join(entries, ""))
}

func entry(key, typ, value string) string {
	return fmt.Sprintf("    %q = { type = %q, value = %q }\n", key, typ, value)
}

var emptyPlan = resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}

func TestAccShopMetafields_lifecycle(t *testing.T) {
	acctest.Setup(t)
	ns := mfNamespace()
	cleanup(t, ns)
	k := func(name string) string { return ns + "." + name }
	both := mfConfig(entry(k("count"), "number_integer", "007"), entry(k("txt"), "single_line_text_field", "a"))
	cleared := mfConfig(entry(k("count"), "number_integer", "007"), entry(k("txt"), "single_line_text_field", ""))
	valueOf := func(key, v string) statecheck.StateCheck {
		return statecheck.ExpectKnownValue(addr, tfjsonpath.New("metafields").AtMapKey(key).AtMapKey("value"), knownvalue.StringExact(v))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Destroy removes from state only: the values must still be there.
		CheckDestroy: checkLive(t, map[string]string{k("count"): "7"}),
		Steps: []resource.TestStep{
			{
				Config: both,
				// State keeps the configured "007" although Shopify stores 7.
				ConfigStateChecks: []statecheck.StateCheck{valueOf(k("count"), "007"), statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringRegexp(shopGIDRe))},
				Check:             checkLive(t, map[string]string{k("count"): "7", k("txt"): "a"}),
			},
			{Config: both, ConfigPlanChecks: emptyPlan},
			{
				// An unmanaged key edited elsewhere is no drift.
				PreConfig:        func() { setLive(t, ns, "other", "admin") },
				Config:           both,
				ConfigPlanChecks: emptyPlan,
			},
			{
				PreConfig:          func() { setLive(t, ns, "txt", "admin edit") },
				Config:             both,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				// Apply restores the managed value.
				Config: both,
				Check:  checkLive(t, map[string]string{k("txt"): "a"}),
			},
			{
				Config:            cleared,
				ConfigStateChecks: []statecheck.StateCheck{valueOf(k("txt"), "")},
				Check:             checkLive(t, map[string]string{k("txt"): ""}),
			},
			{Config: cleared, ConfigPlanChecks: emptyPlan},
			{
				// Import takes every key outside app namespaces, the unmanaged ones included.
				Config:          cleared,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateId:   "default",
				ImportStateKind: resource.ImportCommandWithID,
				ImportStateCheck: func(s []*terraform.InstanceState) error {
					a := s[0].Attributes
					for _, key := range []string{k("count"), ns + ".other"} {
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
				Config:          cleared,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				Config:          cleared,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateId:   "other",
				ImportStateKind: resource.ImportCommandWithID,
				ExpectError:     invalidKeyRe,
			},
		},
	})
}

func TestAccListShopMetafields(t *testing.T) {
	acctest.Setup(t)
	ns := mfNamespace()
	cleanup(t, ns)
	cfg := mfConfig(entry(ns+".txt", "single_line_text_field", "listed"))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				Query: true,
				Config: `
provider "shopify" {}

list "shopify_shop_metafields" "all" {
  provider         = shopify
  include_resource = true
}
`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLength("shopify_shop_metafields.all", 1),
					querycheck.ExpectIdentity("shopify_shop_metafields.all", map[string]knownvalue.Check{"id": knownvalue.StringRegexp(shopGIDRe)}),
					querycheck.ExpectResourceKnownValues("shopify_shop_metafields.all", queryfilter.ByDisplayName(knownvalue.StringExact("Shop metafields")), []querycheck.KnownValueCheck{{
						Path: tfjsonpath.New("metafields"),
						KnownValue: knownvalue.MapPartial(map[string]knownvalue.Check{
							ns + ".txt": knownvalue.ObjectExact(map[string]knownvalue.Check{"type": knownvalue.StringExact("single_line_text_field"), "value": knownvalue.StringExact("listed")}),
						}),
					}}),
				},
			},
		},
	})
}
