package collection_test

import (
	"fmt"
	"regexp"
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

const (
	image1 = "https://upload.wikimedia.org/wikipedia/commons/c/ca/1x1.png"
	image2 = "https://upload.wikimedia.org/wikipedia/commons/4/47/PNG_transparency_demonstration_1.png"
)

var cdnRe = regexp.MustCompile(`^https://cdn\.shopify\.com/.+/collections/.+\?v=\d+$`)

func imageConfig(handle, title, image string) string {
	return fmt.Sprintf(`
resource "shopify_collection" "test" {
  handle = %q
  title  = %q
%s
}
`, handle, title, image)
}

func imageBlock(src, alt string) string {
	return fmt.Sprintf("  image = {\n    src      = %q\n    alt_text = %q\n  }\n", src, alt)
}

func imagePath(name string) tfjsonpath.Path { return tfjsonpath.New("image").AtMapKey(name) }

// captureURL stores the state's image URL; same reports whether it is still the captured one.
func captureURL(into *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		*into = s.RootModule().Resources[addr].Primary.Attributes["image.url"]
		return nil
	}
}

func sameURL(want *string, same bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		got := s.RootModule().Resources[addr].Primary.Attributes["image.url"]
		if (got == *want) != same {
			return fmt.Errorf("image.url %q, previous %q: want same=%t", got, *want, same)
		}
		return nil
	}
}

func TestAccCollection_image(t *testing.T) {
	acctest.Setup(t)
	handle := testHandle()
	cleanup(t, handle)
	var url string
	first := imageConfig(handle, "Image", imageBlock(image1, "A"))
	altChanged := imageConfig(handle, "Image", imageBlock(image1, "B"))
	titleChanged := imageConfig(handle, "Image renamed", imageBlock(image1, "B"))
	srcChanged := imageConfig(handle, "Image renamed", imageBlock(image2, "B"))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_14_0)},
		CheckDestroy:             checkGone(t, handle),
		Steps: []resource.TestStep{
			{
				Config: first,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, imagePath("src"), str(image1)),
					statecheck.ExpectKnownValue(addr, imagePath("alt_text"), str("A")),
					statecheck.ExpectKnownValue(addr, imagePath("url"), knownvalue.StringRegexp(cdnRe)),
				},
				Check: captureURL(&url),
			},
			{
				// The CDN URL Shopify reads back is no diff against the configured src.
				Config:           first,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: altChanged,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectUnknownValue(addr, imagePath("url"))},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, imagePath("src"), str(image1)),
					statecheck.ExpectKnownValue(addr, imagePath("alt_text"), str("B")),
				},
				Check: captureURL(&url),
			},
			{
				// A title edit leaves the image alone.
				Config: titleChanged,
				Check:  sameURL(&url, true),
			},
			{
				Config: srcChanged,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, imagePath("src"), str(image2)),
					statecheck.ExpectKnownValue(addr, imagePath("alt_text"), str("B")),
				},
				Check: resource.ComposeTestCheckFunc(sameURL(&url, false), captureURL(&url)),
			},
			{
				// An image replaced in the admin shows as a change and is put back.
				PreConfig: func() {
					id := mustLookup(t, handle).Id
					mustUpdate(t, client.CollectionUpdateInput{Id: &id, Image: &client.ImageInput{Src: ptr(image1)}})
				},
				Config: srcChanged,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{statecheck.ExpectKnownValue(addr, imagePath("src"), str(image2))},
				Check:             captureURL(&url),
			},
			{
				// Removing the block stops managing the image and keeps it.
				Config:           imageConfig(handle, "Image renamed", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check: func(*terraform.State) error {
					if c := mustLookup(t, handle); c.Image == nil || c.Image.Url != url {
						return fmt.Errorf("image should be kept, got %+v", c.Image)
					}
					return nil
				},
			},
			{
				// Generated config takes the CDN URL as src and plans import-only.
				Config:          srcChanged,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateId:           handle,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"image.src", "adopt_existing"},
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
						queryfilter.ByDisplayName(knownvalue.StringExact("Image renamed ("+handle+")")),
						[]querycheck.KnownValueCheck{
							{Path: imagePath("src"), KnownValue: knownvalue.StringRegexp(cdnRe)},
							{Path: imagePath("alt_text"), KnownValue: str("B")},
						}),
				},
			},
		},
	})
}

func ptr(s string) *string { return &s }
