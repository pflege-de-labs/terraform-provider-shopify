package urlredirect_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/querycheck/queryfilter"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

func TestAccListURLRedirect(t *testing.T) {
	acctest.Setup(t)
	a, b := testPath(), testPath()
	cleanup(t, a, b)
	redirects := fmt.Sprintf(`
resource "shopify_url_redirect" "a" {
  path   = %q
  target = "/tf-acc-list-a"
}

resource "shopify_url_redirect" "b" {
  path   = %q
  target = "https://example.com/tf-acc-list-b"
}
`, a, b)
	gid := knownvalue.StringRegexp(gidRe)
	byName := func(p, target string) queryfilter.QueryFilter {
		return queryfilter.ByDisplayName(knownvalue.StringExact(p + " -> " + target))
	}
	query := func(filter string) string {
		return fmt.Sprintf("provider \"shopify\" {}\n\nlist \"shopify_url_redirect\" \"all\" {\n  provider         = shopify\n  include_resource = true\n%s}\n", filter)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkGone(t, a, b),
		Steps: []resource.TestStep{
			{Config: redirects},
			{
				Query:  true,
				Config: query(""),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("shopify_url_redirect.all", 2),
					querycheck.ExpectIdentity("shopify_url_redirect.all", map[string]knownvalue.Check{"id": gid}),
					querycheck.ExpectResourceKnownValues("shopify_url_redirect.all", byName(a, "/tf-acc-list-a"), []querycheck.KnownValueCheck{
						{Path: tfjsonpath.New("id"), KnownValue: gid},
						{Path: tfjsonpath.New("path"), KnownValue: knownvalue.StringExact(a)},
						{Path: tfjsonpath.New("target"), KnownValue: knownvalue.StringExact("/tf-acc-list-a")},
						{Path: tfjsonpath.New("adopt_existing"), KnownValue: knownvalue.Bool(false)},
					}),
				},
			},
			{
				// The filter narrows the list to one redirect.
				Query:  true,
				Config: query(fmt.Sprintf("  config {\n    query = %q\n  }\n", fmt.Sprintf("path:%q", b))),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLength("shopify_url_redirect.all", 1),
					querycheck.ExpectResourceKnownValues("shopify_url_redirect.all", byName(b, "https://example.com/tf-acc-list-b"), []querycheck.KnownValueCheck{
						{Path: tfjsonpath.New("target"), KnownValue: knownvalue.StringExact("https://example.com/tf-acc-list-b")},
					}),
				},
			},
			// GenerateConfig is a no-op on Query steps in terraform-plugin-testing v1.16.0; the round-trip is covered by the identity import step in _basic.
		},
	})
}
