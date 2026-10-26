package patterns

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ValidateHandle rejects what Shopify would normalise in a product or collection handle, which would otherwise plan forever.
func ValidateHandle(h string) error {
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

// HandleValidator applies ValidateHandle to a string attribute.
func HandleValidator() validator.String { return handleValidator{} }

type handleValidator struct{}

func (handleValidator) Description(context.Context) string {
	return "no upper-case letters and no whitespace"
}
func (v handleValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (handleValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := ValidateHandle(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid handle", err.Error())
	}
}

// EnumNames turns a generated enum list into validator values.
func EnumNames[T ~string](all []T) []string {
	out := make([]string, len(all))
	for i, v := range all {
		out[i] = string(v)
	}
	return out
}

// KeepOrTake keeps the configured HTML while live is what Shopify returned at our last write (repaired, not drifted).
func KeepOrTake(configured types.String, written *string, live string) types.String {
	if configured.IsNull() || configured.IsUnknown() || written == nil || *written != live {
		return types.StringValue(live)
	}
	return configured
}

// WrittenHTMLKey is the private state key for the HTML Shopify returned at the last write.
const WrittenHTMLKey = "description_html_written"

// PrivateSetter is the part of the private state a write sets.
type PrivateSetter interface {
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

// PrivateGetter is the part of the private state Read gets.
type PrivateGetter interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
}

// RememberWrittenHTML stores the HTML Shopify returned at a write, for KeepOrTake.
func RememberWrittenHTML(ctx context.Context, private PrivateSetter, html string) diag.Diagnostics {
	raw, err := json.Marshal(html)
	if err != nil {
		var diags diag.Diagnostics
		diags.AddError("Failed to remember the description", err.Error())
		return diags
	}
	return private.SetKey(ctx, WrittenHTMLKey, raw)
}

// WrittenHTML returns the stored HTML, or nil when none was stored (import).
func WrittenHTML(ctx context.Context, private PrivateGetter) (*string, diag.Diagnostics) {
	raw, diags := private.GetKey(ctx, WrittenHTMLKey)
	if diags.HasError() || len(raw) == 0 {
		return nil, diags
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		diags.AddError("Failed to read the remembered description", err.Error())
		return nil, diags
	}
	return &s, diags
}
