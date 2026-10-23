package menu_test

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

const addr = "shopify_menu.test"

// testHandle keeps test menus apart from real ones; only these are ever deleted by tests.
func testHandle() string {
	return "tfacc-" + strings.ToLower(sdkacctest.RandStringFromCharSet(8, sdkacctest.CharSetAlpha))
}

func config(handle, body string) string {
	return fmt.Sprintf(`
resource "shopify_menu" "test" {
  handle = %q
%s
}
`, handle, body)
}

func titles(p tfjsonpath.Path, want ...string) statecheck.StateCheck {
	checks := make([]knownvalue.Check, len(want))
	for i, w := range want {
		checks[i] = knownvalue.ObjectPartial(map[string]knownvalue.Check{"title": knownvalue.StringExact(w)})
	}
	return statecheck.ExpectKnownValue(addr, p, knownvalue.ListExact(checks))
}

var expectIdentity = statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{
	"id": knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/Menu/\d+$`)),
})

func TestAccMenu_basic(t *testing.T) {
	handle := testHandle()
	updated := config(handle+"-renamed", `
  title = "Test menu (updated)"

  item {
    title = "Help"
    type  = "HTTP"
    url   = "https://example.com/help"

    item {
      title = "FAQ"
      type  = "HTTP"
      url   = "https://example.com/faq"
    }
  }
  item {
    title = "Search"
    type  = "SEARCH"
  }
`)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkMenu(t, handle, false),
		Steps: []resource.TestStep{
			{
				Config: config(handle, `
  title = "Test menu"

  item {
    title = "Home"
    type  = "FRONTPAGE"
  }
  item {
    title = "Help"
    type  = "HTTP"
    url   = "https://example.com/help"

    item {
      title = "FAQ"
      type  = "HTTP"
      url   = "https://example.com/faq"

      item {
        title = "Shipping"
        type  = "HTTP"
        url   = "https://example.com/faq/shipping"
      }
    }
  }
`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringRegexp(regexp.MustCompile(`^gid://shopify/Menu/\d+$`))),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("is_default"), knownvalue.Bool(false)),
					titles(tfjsonpath.New("item"), "Home", "Help"),
					titles(tfjsonpath.New("item").AtSliceIndex(1).AtMapKey("item").AtSliceIndex(0).AtMapKey("item"), "Shipping"),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("item").AtSliceIndex(0).AtMapKey("url"), knownvalue.Null()),
				},
			},
			{
				// New title and handle, reordered items, a level dropped: the whole tree is replaced.
				Config: updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("handle"), knownvalue.StringExact(handle+"-renamed")),
					titles(tfjsonpath.New("item"), "Help", "Search"),
					titles(tfjsonpath.New("item").AtSliceIndex(0).AtMapKey("item").AtSliceIndex(0).AtMapKey("item")),
				},
			},
			{RefreshState: true},
			{
				// Refresh steps can't carry state checks; this no-op step checks identity survives the refresh.
				Config:            updated,
				ConfigStateChecks: []statecheck.StateCheck{expectIdentity},
			},
			{Config: updated, ResourceName: addr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				// Generated config must pass validation and plan as import-only, without adopt_existing.
				Config:          updated,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				PreConfig:                            func() { waitListed(t, handle+"-renamed") },
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        handle + "-renamed",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "handle",
				ImportStateVerifyIgnore:              []string{"adopt_existing"},
			},
		},
	})
}

func TestAccMenu_adoptExisting(t *testing.T) {
	handle := testHandle()
	var existingID string
	body := func(adopt bool) string {
		return fmt.Sprintf(`
  title          = "Adopted"
  adopt_existing = %t

  item {
    title = "Home"
    type  = "FRONTPAGE"
  }
`, adopt)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Adopted menus survive destroy; the check confirms that and then cleans up.
		CheckDestroy: checkMenu(t, handle, true),
		Steps: []resource.TestStep{
			{
				PreConfig: func() {
					res, err := client.CreateMenu(context.Background(), acctest.Client(t), "Pre-existing", handle, []client.MenuItemCreateInput{
						{Title: "Old", Type: client.MenuItemTypeSearch},
					})
					if err != nil || len(res.MenuCreate.UserErrors) > 0 {
						t.Fatalf("creating menu: %v %v", err, res)
					}
					existingID = res.MenuCreate.Menu.Id
				},
				// Usually runs before the list shows the menu, so this exercises the duplicate-handle guard after create.
				Config:      config(handle, body(false)),
				ExpectError: regexp.MustCompile(`Set adopt_existing = true`),
			},
			{
				PreConfig: func() { waitListed(t, handle) },
				Config:    config(handle, body(true)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringFunc(func(v string) error {
						if v != existingID {
							return fmt.Errorf("adopted id %s, want pre-existing %s", v, existingID)
						}
						return nil
					})),
					titles(tfjsonpath.New("item"), "Home"),
				},
			},
		},
	})
}

// waitListed waits until the menus list, which lags behind writes, shows handle.
func waitListed(t *testing.T, handle string) {
	t.Helper()
	ctx := context.Background()
	for range 30 {
		res, err := client.ListMenus(ctx, acctest.Client(t), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range res.Menus.Nodes {
			if m.Handle == handle {
				return
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("menu %s not listed after 30s", handle)
}

// checkMenu verifies the menu is gone, or for adopted menus that it survived, deleting it afterwards.
func checkMenu(t *testing.T, handle string, wantKept bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		c := acctest.Client(t)
		ctx := context.Background()
		var after *string
		for {
			res, err := client.ListMenus(ctx, c, after)
			if err != nil {
				return err
			}
			for _, m := range res.Menus.Nodes {
				if !strings.HasPrefix(m.Handle, handle) {
					continue
				}
				if _, err := client.DeleteMenu(ctx, c, m.Id); err != nil {
					return err
				}
				if !wantKept {
					return fmt.Errorf("menu %s still exists", m.Handle)
				}
				return nil
			}
			if !res.Menus.PageInfo.HasNextPage {
				break
			}
			after = res.Menus.PageInfo.EndCursor
		}
		if wantKept {
			return fmt.Errorf("adopted menu %s was deleted, want it kept", handle)
		}
		return nil
	}
}
