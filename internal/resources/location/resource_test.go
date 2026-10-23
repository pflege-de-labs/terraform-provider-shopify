package location_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	sdkacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const addr = "shopify_location.test"

var expectIdentity = statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{
	"id": knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/Location/\d+$`)),
})

// testName keeps test locations apart from real ones; only these are ever deleted by tests.
func testName() string {
	return "tfacc-" + strings.ToLower(sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlpha))
}

func config(name, body string) string {
	return fmt.Sprintf(`
resource "shopify_location" "test" {
  name = %q
%s
}
`, name, body)
}

const deleteOnDestroy = `
  deletion_policy     = "delete"
  allow_data_deletion = true
`

func TestAccLocation_basic(t *testing.T) {
	name := testName()
	var gid string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkLocation(t, &gid, false),
		Steps: []resource.TestStep{
			{
				Config: config(name, `
  address = {
    address1     = "Musterstrasse 1"
    city         = "Hamburg"
    zip          = "20095"
    country_code = "DE"
  }
  fulfills_online_orders = false
`+deleteOnDestroy),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringFunc(func(v string) error {
						gid = v
						if !regexp.MustCompile(`^gid://shopify/Location/\d+$`).MatchString(v) {
							return fmt.Errorf("unexpected id %s", v)
						}
						return nil
					})),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("legacy_resource_id"), knownvalue.StringRegexp(regexp.MustCompile(`^\d+$`))),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("active"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("fulfills_online_orders"), knownvalue.Bool(false)),
				},
			},
			{
				// Rename, move and deactivate; address1 and zip are dropped from config and stay unmanaged.
				Config: config(name+"-renamed", `
  address = {
    city         = "Berlin"
    country_code = "DE"
  }
  active = false
`+deleteOnDestroy),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("active"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("address").AtMapKey("city"), knownvalue.StringExact("Berlin")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("address").AtMapKey("address1"), knownvalue.StringExact("Musterstrasse 1")),
				},
			},
			{
				Config: config(name+"-renamed", `
  address = {
    city         = "Berlin"
    country_code = "DE"
  }
  active = true
`+deleteOnDestroy),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("active"), knownvalue.Bool(true)),
				},
			},
			{RefreshState: true},
			{
				// Refresh steps can't carry state checks; this no-op step checks identity survives the refresh.
				Config: config(name+"-renamed", `
  address = {
    city         = "Berlin"
    country_code = "DE"
  }
`+deleteOnDestroy),
				ConfigStateChecks: []statecheck.StateCheck{expectIdentity},
			},
			{
				PreConfig:               func() { waitSearchable(t, name+"-renamed") },
				ResourceName:            addr,
				ImportState:             true,
				ImportStateId:           name + "-renamed",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"deletion_policy", "allow_data_deletion"},
			},
			{
				ResourceName: addr,
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources[addr].Primary.Attributes["legacy_resource_id"], nil
				},
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"deletion_policy", "allow_data_deletion"},
			},
			{
				// Names are unique per shop: a second location with the same name is rejected.
				Config: config(name+"-renamed", `
  address = {
    city         = "Berlin"
    country_code = "DE"
  }
`+deleteOnDestroy) + fmt.Sprintf(`
resource "shopify_location" "dup" {
  name    = %q
  address = { country_code = "DE" }
`+deleteOnDestroy+`
}
`, name+"-renamed"),
				ExpectError: regexp.MustCompile(`already have a location with this name`),
			},
		},
	})
}

func TestAccLocation_identityImport(t *testing.T) {
	name := testName()
	var gid string
	cfg := config(name, `
  address = {
    address1     = "Musterstrasse 1"
    city         = "Hamburg"
    zip          = "20095"
    country_code = "DE"
  }
`)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Default policy only deactivates; the check confirms that and then deletes the location.
		CheckDestroy: checkLocation(t, &gid, true),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringFunc(func(v string) error { gid = v; return nil })),
				},
			},
			{Config: cfg, ResourceName: addr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				// Generated config must pass validation and plan as import-only.
				Config:          cfg,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
		},
	})
}

func TestAccLocation_deletionPolicy(t *testing.T) {
	name := testName()
	var gid string
	body := func(policy string) string {
		return fmt.Sprintf(`
  address         = { country_code = "DE" }
  deletion_policy = %q
`, policy)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// The default policy deactivates; the check confirms that and then deletes the location.
		CheckDestroy: checkLocation(t, &gid, true),
		Steps: []resource.TestStep{
			{
				Config: config(name, body("delete")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringFunc(func(v string) error { gid = v; return nil })),
				},
			},
			{
				Config:      config(name, body("delete")),
				Destroy:     true,
				ExpectError: regexp.MustCompile(`allow_data_deletion = true`),
			},
			{
				Config: config(name, body("deactivate")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
			},
		},
	})
}

// waitSearchable waits until the locations search, which lags behind writes, finds name.
func waitSearchable(t *testing.T, name string) {
	t.Helper()
	for range 30 {
		res, err := client.FindLocations(context.Background(), acctest.Client(t), `name:"`+name+`"`)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range res.Locations.Nodes {
			if l.Name == name {
				return
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("location %s not searchable after 30s", name)
}

// checkLocation verifies the location is gone, or for wantInactive that it was only deactivated, deleting it afterwards.
func checkLocation(t *testing.T, gid *string, wantInactive bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		ctx := context.Background()
		c := acctest.Client(t)
		res, err := client.GetLocation(ctx, c, *gid)
		if err != nil {
			return err
		}
		if !wantInactive {
			if res.Location != nil {
				return fmt.Errorf("location %s still exists", *gid)
			}
			return nil
		}
		if res.Location == nil || res.Location.IsActive {
			return fmt.Errorf("location %s: want it kept and inactive, got %+v", *gid, res.Location)
		}
		del, err := client.DeleteLocation(ctx, c, *gid)
		if err != nil {
			return err
		}
		if len(del.LocationDelete.LocationDeleteUserErrors) > 0 {
			return fmt.Errorf("cleanup: %s", del.LocationDelete.LocationDeleteUserErrors[0].Message)
		}
		return nil
	}
}
