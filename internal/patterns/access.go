package patterns

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// AdminAccess keeps only values the input accepts, so generated config passes validation.
func AdminAccess(v string) types.String {
	if v != "MERCHANT_READ" && v != "MERCHANT_READ_WRITE" {
		return types.StringNull()
	}
	return types.StringValue(v)
}

type useStateIncludingNull struct{}

// UseStateForUnknownIncludingNull is UseStateForUnknown that also keeps a null state value.
func UseStateForUnknownIncludingNull() planmodifier.String { return useStateIncludingNull{} }

func (useStateIncludingNull) Description(context.Context) string {
	return "Keeps the prior state value, including null, when the attribute is not configured."
}

func (m useStateIncludingNull) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (useStateIncludingNull) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Null state is valid here: Shopify may report a value we map to null.
	if req.State.Raw.IsNull() || !req.ConfigValue.IsNull() || !req.PlanValue.IsUnknown() {
		return
	}
	resp.PlanValue = req.StateValue
}
