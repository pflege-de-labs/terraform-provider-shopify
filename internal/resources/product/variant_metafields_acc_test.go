package product_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

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

// variantID returns the live variant with the given Size value.
func variantID(t *testing.T, handle, size string) string {
	t.Helper()
	for _, v := range mustLookup(t, handle).Variants.Nodes {
		for _, so := range v.SelectedOptions {
			if so.Name == "Size" && so.Value == size {
				return v.Id
			}
		}
	}
	t.Fatalf("no variant of %s with Size=%s", handle, size)
	return ""
}

func liveVariantMetafields(t *testing.T, handle, size string) map[string]string {
	t.Helper()
	res, err := client.GetVariantMetafields(context.Background(), acctest.Client(t), variantID(t, handle, size), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, n := range res.ProductVariant.Metafields.Nodes {
		out[n.Namespace+"."+n.Key] = n.Value
	}
	return out
}

func setLiveVariant(t *testing.T, handle, size, ns, key, typ, value string) {
	t.Helper()
	res, err := client.SetMetafields(context.Background(), acctest.Client(t),
		[]client.MetafieldsSetInput{{OwnerId: variantID(t, handle, size), Namespace: &ns, Key: key, Type: &typ, Value: value}})
	if err != nil {
		t.Fatal(err)
	}
	if errs := res.MetafieldsSet.UserErrors; len(errs) > 0 {
		t.Fatalf("metafieldsSet: %v", errs)
	}
}

func checkLiveVariant(t *testing.T, handle, size string, want map[string]string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got := liveVariantMetafields(t, handle, size)
		for k, v := range want {
			if got[k] != v {
				return fmt.Errorf("variant %s metafield %s = %q, want %q", size, k, got[k], v)
			}
		}
		return nil
	}
}

// vmfConfig has the sizes S and M, and L when withL; mfS and mfL are the metafield entries of S and L, M is unmanaged.
func vmfConfig(handle, mfS, mfL string, withL bool) string {
	sizes, extra := `"S", "M"`, ""
	if withL {
		sizes = `"S", "M", "L"`
		extra = fmt.Sprintf(`, { option_values = { Size = "L" }, metafields = {
%s } }`, mfL)
	}
	return config(fmt.Sprintf(`
  handle              = %q
  title               = "tf-acc variant metafields"
  status              = "DRAFT"
  allow_data_deletion = true
  options             = [{ name = "Size", values = [%s] }]
  variants = [
    { option_values = { Size = "S" }, metafields = {
%s } },
    { option_values = { Size = "M" } }%s
  ]
`, handle, sizes, mfS, extra))
}

func variantMF(i int, key string, check knownvalue.Check) statecheck.StateCheck {
	return stateValue("variants", knownvalue.ListPartial(map[int]knownvalue.Check{i: knownvalue.ObjectPartial(map[string]knownvalue.Check{
		"metafields": knownvalue.MapPartial(map[string]knownvalue.Check{key: knownvalue.ObjectPartial(map[string]knownvalue.Check{"value": check})}),
	})}))
}

func TestAccProductVariantMetafields_lifecycle(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	handle, ns := testHandle(), mfNamespace()
	cleanup(t, handle)
	k := func(name string) string { return ns + "." + name }
	both := mfEntry(k("count"), "number_integer", `"007"`) + mfEntry(k("txt"), "single_line_text_field", `"a"`)
	updated := mfEntry(k("count"), "number_integer", `"007"`) + mfEntry(k("txt"), "single_line_text_field", `"b"`)
	dropped := mfEntry(k("txt"), "single_line_text_field", `"b"`)
	cleared := mfEntry(k("txt"), "single_line_text_field", `""`)
	lNew := mfEntry(k("txt"), "single_line_text_field", `"new variant"`)
	empty := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{
				Config: vmfConfig(handle, both, "", false),
				// State keeps the configured "007" although Shopify stores 7; the unmanaged variant stays null.
				ConfigStateChecks: []statecheck.StateCheck{
					variantMF(0, k("count"), str("007")),
					stateValue("variants", knownvalue.ListPartial(map[int]knownvalue.Check{1: knownvalue.ObjectPartial(map[string]knownvalue.Check{"metafields": knownvalue.Null()})})),
				},
				Check: checkLiveVariant(t, handle, "S", map[string]string{k("count"): "7", k("txt"): "a"}),
			},
			{Config: vmfConfig(handle, both, "", false), ConfigPlanChecks: empty},
			{
				// A new variant gets its metafields, the existing one is updated.
				Config:            vmfConfig(handle, updated, lNew, true),
				ConfigStateChecks: []statecheck.StateCheck{variantMF(0, k("txt"), str("b")), variantMF(2, k("txt"), str("new variant"))},
				Check: resource.ComposeTestCheckFunc(
					checkLiveVariant(t, handle, "S", map[string]string{k("txt"): "b"}),
					checkLiveVariant(t, handle, "L", map[string]string{k("txt"): "new variant"}),
				),
			},
			{
				// A dropped key stops being managed and keeps its value.
				Config: vmfConfig(handle, dropped, lNew, true),
				Check:  checkLiveVariant(t, handle, "S", map[string]string{k("count"): "7"}),
			},
			{
				// Unmanaged keys and an unmanaged variant edited elsewhere are no drift.
				PreConfig: func() {
					setLiveVariant(t, handle, "S", ns, "other", "single_line_text_field", "admin")
					setLiveVariant(t, handle, "M", ns, "other", "single_line_text_field", "admin")
				},
				Config:           vmfConfig(handle, dropped, lNew, true),
				ConfigPlanChecks: empty,
			},
			{
				PreConfig:          func() { setLiveVariant(t, handle, "S", ns, "txt", "single_line_text_field", "admin edit") },
				Config:             vmfConfig(handle, dropped, lNew, true),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: vmfConfig(handle, dropped, lNew, true),
				Check:  checkLiveVariant(t, handle, "S", map[string]string{k("txt"): "b"}),
			},
			{
				Config:            vmfConfig(handle, cleared, lNew, true),
				ConfigStateChecks: []statecheck.StateCheck{variantMF(0, k("txt"), str(""))},
				Check: func(*terraform.State) error {
					if v, ok := liveVariantMetafields(t, handle, "S")[k("txt")]; ok {
						return fmt.Errorf("variant metafield txt still has %q", v)
					}
					return nil
				},
			},
			{Config: vmfConfig(handle, cleared, lNew, true), ConfigPlanChecks: empty},
			{
				// Import takes every key outside app namespaces, of every variant.
				Config:          vmfConfig(handle, cleared, lNew, true),
				ResourceName:    addr,
				ImportState:     true,
				ImportStateId:   handle,
				ImportStateKind: resource.ImportCommandWithID,
				ImportStateCheck: func(s []*terraform.InstanceState) error {
					n := 0
					for a := range s[0].Attributes {
						if strings.HasPrefix(a, "variants.") && strings.HasSuffix(a, ".metafields."+ns+".other.type") {
							n++
						}
					}
					if n != 2 {
						return fmt.Errorf("import has %d variants with %s.other, want 2 (S and M): %v", n, ns, s[0].Attributes)
					}
					return nil
				},
			},
			{
				// Generated config must pass validation and plan as import-only.
				Config:          vmfConfig(handle, cleared, lNew, true),
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
		},
	})
}

func TestAccProductVariantMetafields_invalid(t *testing.T) {
	acctest.Setup(t)
	handle, ns := testHandle(), mfNamespace()
	cleanup(t, handle)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{Config: vmfConfig(handle, mfEntry("app--123--x.key", "single_line_text_field", `"v"`), "", false), PlanOnly: true, ExpectError: regexp.MustCompile(`app-owned namespace`)},
			{Config: vmfConfig(handle, mfEntry(ns+".count", "number_integer", `"abc"`), "", false), ExpectError: regexp.MustCompile(`Value must be an integer`)},
		},
	})
}

func TestAccListProduct_variantMetafields(t *testing.T) {
	acctest.Setup(t)
	acctest.KeepFrontpage(t)
	handle, ns := testHandle(), mfNamespace()
	cleanup(t, handle)
	cfg := vmfConfig(handle, mfEntry(ns+".txt", "single_line_text_field", `"listed"`), "", false)
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
						queryfilter.ByDisplayName(knownvalue.StringExact("tf-acc variant metafields ("+handle+")")),
						[]querycheck.KnownValueCheck{{
							Path: tfjsonpath.New("variants").AtSliceIndex(0).AtMapKey("metafields"),
							KnownValue: knownvalue.MapExact(map[string]knownvalue.Check{
								ns + ".txt": knownvalue.ObjectExact(map[string]knownvalue.Check{"type": str("single_line_text_field"), "value": str("listed")}),
							}),
						}}),
				},
			},
		},
	})
}
