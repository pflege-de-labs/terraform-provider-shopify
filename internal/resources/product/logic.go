package product

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// validateHandle rejects what Shopify would normalise, which would otherwise plan forever.
func validateHandle(h string) error {
	switch {
	case h == "":
		return fmt.Errorf("must not be empty")
	case strings.IndexFunc(h, unicode.IsUpper) >= 0:
		return fmt.Errorf("must not contain upper-case letters, because Shopify lower-cases handles")
	case strings.IndexFunc(h, unicode.IsSpace) >= 0:
		return fmt.Errorf("must not contain whitespace")
	}
	return nil
}

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

type handleValidator struct{}

func (handleValidator) Description(context.Context) string {
	return "no upper-case letters and no whitespace"
}
func (v handleValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (handleValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := validateHandle(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid handle", err.Error())
	}
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

// keepOrTake keeps the configured HTML while live is what Shopify returned at our last write (repaired, not drifted).
func keepOrTake(configured types.String, written *string, live string) types.String {
	if configured.IsNull() || configured.IsUnknown() || written == nil || *written != live {
		return types.StringValue(live)
	}
	return configured
}

// inputAttrs maps ProductSetInput keys to the attribute that sets them.
var inputAttrs = map[string]string{
	"handle": "handle", "title": "title", "status": "status", "descriptionHtml": "description_html",
	"vendor": "vendor", "productType": "product_type", "category": "category_id", "tags": "tags",
	"templateSuffix": "template_suffix", "giftCard": "gift_card",
}

// errorPath maps a productSet userError field such as ["input", "seo", "title"] to the attribute that set it.
func errorPath(field []string) (path.Path, bool) {
	if len(field) < 2 {
		return path.Empty(), false
	}
	if field[1] == "seo" && len(field) > 2 && (field[2] == "title" || field[2] == "description") {
		return path.Root("seo_" + field[2]), true
	}
	if a, ok := inputAttrs[field[1]]; ok {
		return path.Root(a), true
	}
	return path.Empty(), false
}

// userErrorDiags scopes userErrors to attributes where errorPath knows them; the rest stay resource-level.
func userErrorDiags[E any, P interface {
	*E
	client.UserError
}](summary string, errs []E) diag.Diagnostics {
	var diags diag.Diagnostics
	for i := range errs {
		e := P(&errs[i])
		if p, ok := errorPath(e.GetField()); ok {
			diags.AddAttributeError(p, summary, e.GetMessage())
		} else {
			diags.AddError(summary, e.GetMessage())
		}
	}
	return diags
}
