package patterns

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TimeoutsBlock is a `timeouts` block with one optional duration string per operation; it avoids the terraform-plugin-framework-timeouts dependency.
func TimeoutsBlock(ops ...string) schema.SingleNestedBlock {
	attrs := make(map[string]schema.Attribute, len(ops))
	for _, op := range ops {
		attrs[op] = schema.StringAttribute{
			MarkdownDescription: fmt.Sprintf("How long to wait for the %s, as a duration such as `30s` or `10m`.", op),
			Optional:            true,
			Validators:          []validator.String{DurationValidator()},
		}
	}
	return schema.SingleNestedBlock{Attributes: attrs}
}

// TimeoutsAttrTypes is the object type of the block, for the model's types.Object.
func TimeoutsAttrTypes(ops ...string) map[string]attr.Type {
	out := make(map[string]attr.Type, len(ops))
	for _, op := range ops {
		out[op] = types.StringType
	}
	return out
}

// TimeoutsNull is the value of an absent block.
func TimeoutsNull(ops ...string) types.Object {
	return types.ObjectNull(TimeoutsAttrTypes(ops...))
}

// TimeoutOf returns the configured duration for op, or def when the block or the attribute is unset.
func TimeoutOf(obj types.Object, op string, def time.Duration) (time.Duration, error) {
	if obj.IsNull() || obj.IsUnknown() {
		return def, nil
	}
	s, ok := obj.Attributes()[op].(types.String)
	if !ok || s.IsNull() || s.IsUnknown() {
		return def, nil
	}
	return parsePositiveDuration(s.ValueString())
}

func parsePositiveDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	switch {
	case err != nil:
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	case d <= 0:
		return 0, fmt.Errorf("duration %q must be positive", s)
	}
	return d, nil
}

// DurationValidator accepts what time.ParseDuration accepts, if positive.
func DurationValidator() validator.String { return durationValidator{} }

type durationValidator struct{}

func (durationValidator) Description(context.Context) string {
	return "a positive duration such as 30s or 10m"
}
func (v durationValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (durationValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := parsePositiveDuration(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid timeout", err.Error())
	}
}
