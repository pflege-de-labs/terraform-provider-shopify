package discount

import (
	"context"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

// instantEquality compares RFC 3339 times by instant, since Shopify returns them in UTC.
type instantEquality struct{}

func (instantEquality) Equal(a, b string) bool {
	x, err1 := time.Parse(time.RFC3339, a)
	y, err2 := time.Parse(time.RFC3339, b)
	return err1 == nil && err2 == nil && x.Equal(y)
}

// decimalEquality compares decimal strings numerically, since Shopify returns "50" as "50.0".
type decimalEquality struct{}

func (decimalEquality) Equal(a, b string) bool { return patterns.DecimalEqual(a, b) }

// The semantic string type is named for JSON, its first use; it only needs a comparison.
type (
	instantType  = patterns.JSONType[instantEquality]
	instantValue = patterns.JSONValue[instantEquality]
	decimalType  = patterns.JSONType[decimalEquality]
)

var (
	instantString = patterns.JSONString[instantEquality]
	instantNull   = patterns.JSONNull[instantEquality]
)

// now is replaced in tests.
var now = time.Now

var decimalRe = regexp.MustCompile(`^\d+(\.\d+)?$`)

// rfc3339Validator accepts what the instant type can compare.
type rfc3339Validator struct{}

func (rfc3339Validator) Description(context.Context) string {
	return "an RFC 3339 time such as 2026-11-01T00:00:00+01:00"
}

func (v rfc3339Validator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v rfc3339Validator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := time.Parse(time.RFC3339, req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid time", "Must be "+v.Description(ctx)+": "+err.Error())
	}
}
