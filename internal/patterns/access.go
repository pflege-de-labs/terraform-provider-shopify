package patterns

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
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

// UseStateForUnknownIncludingNullInt64, ...Bool and ...Object are the same modifier for other attribute types.
func UseStateForUnknownIncludingNullInt64() planmodifier.Int64   { return useStateIncludingNull{} }
func UseStateForUnknownIncludingNullBool() planmodifier.Bool     { return useStateIncludingNull{} }
func UseStateForUnknownIncludingNullObject() planmodifier.Object { return useStateIncludingNull{} }

// keepState reports whether the plan should take the state value; null state is valid, as Shopify may report a value we map to null.
func keepState(state tfsdk.State, cfg, plan attr.Value) bool {
	return !state.Raw.IsNull() && cfg.IsNull() && plan.IsUnknown()
}

func (useStateIncludingNull) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if keepState(req.State, req.ConfigValue, req.PlanValue) {
		resp.PlanValue = req.StateValue
	}
}

func (useStateIncludingNull) PlanModifyInt64(ctx context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	if keepState(req.State, req.ConfigValue, req.PlanValue) {
		resp.PlanValue = req.StateValue
	}
}

func (useStateIncludingNull) PlanModifyBool(ctx context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if keepState(req.State, req.ConfigValue, req.PlanValue) {
		resp.PlanValue = req.StateValue
	}
}

func (useStateIncludingNull) PlanModifyObject(ctx context.Context, req planmodifier.ObjectRequest, resp *planmodifier.ObjectResponse) {
	if keepState(req.State, req.ConfigValue, req.PlanValue) {
		resp.PlanValue = req.StateValue
	}
}
