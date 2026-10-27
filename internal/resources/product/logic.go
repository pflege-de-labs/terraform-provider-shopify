package product

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// validateTags rejects tags Shopify trims or de-duplicates, so the config matches what is read back.
func validateTags(tags []string) error {
	seen := map[string]bool{}
	for _, t := range tags {
		switch {
		case t == "":
			return fmt.Errorf("tags must not be empty")
		case strings.TrimSpace(t) != t:
			return fmt.Errorf("tag %q has leading or trailing whitespace, which Shopify trims", t)
		case seen[strings.ToLower(t)]:
			return fmt.Errorf("tag %q is a case-insensitive duplicate, which Shopify drops", t)
		}
		seen[strings.ToLower(t)] = true
	}
	return nil
}

type tagsValidator struct{}

func (tagsValidator) Description(context.Context) string {
	return "no empty tags, no surrounding whitespace, no case-insensitive duplicates"
}
func (v tagsValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (tagsValidator) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var tags []string
	resp.Diagnostics.Append(req.ConfigValue.ElementsAs(ctx, &tags, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := validateTags(tags); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid tags", err.Error())
	}
}

// inputAttrs maps ProductSetInput keys to the attribute that sets them.
var inputAttrs = map[string]string{
	"handle": "handle", "title": "title", "status": "status", "descriptionHtml": "description_html",
	"vendor": "vendor", "productType": "product_type", "category": "category_id", "tags": "tags",
	"templateSuffix": "template_suffix", "giftCard": "gift_card",
}

// variantAttrs maps ProductVariantSetInput keys, and inventoryItem keys below them, to variant attributes.
var variantAttrs = map[string]string{
	"optionValues": "option_values", "price": "price", "compareAtPrice": "compare_at_price", "sku": "sku",
	"barcodes": "barcodes", "inventoryPolicy": "inventory_policy", "taxable": "taxable",
	"tracked": "tracked", "requiresShipping": "requires_shipping", "measurement": "weight",
}

// errorPath maps a productSet userError field such as ["input", "seo", "title"] to the attribute that set it.
func errorPath(field []string) (path.Path, bool) {
	if len(field) < 2 {
		return path.Empty(), false
	}
	if attr, ok := map[string]string{"productOptions": "options", "variants": "variants", "files": "media_ids"}[field[1]]; ok {
		p := path.Root(attr)
		if len(field) < 3 {
			return p, true
		}
		i, err := strconv.Atoi(field[2])
		if err != nil {
			return p, true
		}
		p = p.AtListIndex(i) // the sent lists follow config order
		rest := field[3:]
		if attr == "variants" && len(rest) > 1 && rest[0] == "inventoryItem" {
			rest = rest[1:]
		}
		switch {
		case len(rest) == 0:
		case attr == "options" && (rest[0] == "name" || rest[0] == "values"):
			p = p.AtName(rest[0])
		case attr == "variants" && variantAttrs[rest[0]] != "":
			p = p.AtName(variantAttrs[rest[0]])
		}
		return p, true
	}
	if field[1] == "seo" && len(field) > 2 && (field[2] == "title" || field[2] == "description") {
		return path.Root("seo_" + field[2]), true
	}
	if a, ok := inputAttrs[field[1]]; ok {
		return path.Root(a), true
	}
	return path.Empty(), false
}
