package deliveryprofile_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
)

const listConfig = `
provider "shopify" {}

list "shopify_delivery_profile" "all" {
  provider         = shopify
  include_resource = true
}
`

// Applying the unchanged snapshot leaves the store as it is; the generated-config step proves the listed model round-trips.
func TestAccListDeliveryProfile(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	acctest.PreCheck(t)
	cfg := strings.Replace(snapshot(t), "#ZONES#", "", 1)
	gid := knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/DeliveryProfile/\d+$`))
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				Query:  true,
				Config: listConfig,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLength("shopify_delivery_profile.all", 1),
					querycheck.ExpectIdentity("shopify_delivery_profile.all", map[string]knownvalue.Check{"id": gid}),
					querycheck.ExpectResourceKnownValues("shopify_delivery_profile.all", nil, []querycheck.KnownValueCheck{
						{Path: tfjsonpath.New("name"), KnownValue: knownvalue.NotNull()},
						{Path: tfjsonpath.New("location_group"), KnownValue: knownvalue.NotNull()},
					}),
				},
			},
			{
				Config:          cfg,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
		},
	})
}
