package patterns

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestAdminAccess(t *testing.T) {
	for in, want := range map[string]types.String{
		"MERCHANT_READ":       types.StringValue("MERCHANT_READ"),
		"MERCHANT_READ_WRITE": types.StringValue("MERCHANT_READ_WRITE"),
		"PRIVATE":             types.StringNull(),
		"PUBLIC_READ":         types.StringNull(),
		"PUBLIC_READ_WRITE":   types.StringNull(),
		"":                    types.StringNull(),
	} {
		if got := AdminAccess(in); !got.Equal(want) {
			t.Errorf("AdminAccess(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestUseStateForUnknownIncludingNull(t *testing.T) {
	existing := tfsdk.State{Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})}
	created := tfsdk.State{Raw: tftypes.NewValue(tftypes.Object{}, nil)}
	unknown := types.StringUnknown()
	for name, tc := range map[string]struct {
		state  tfsdk.State
		cfg    types.String
		plan   types.String
		stateV types.String
		want   types.String
	}{
		"config null, state null":  {existing, types.StringNull(), unknown, types.StringNull(), types.StringNull()},
		"config null, state value": {existing, types.StringNull(), unknown, types.StringValue("x"), types.StringValue("x")},
		"create":                   {created, types.StringNull(), unknown, types.StringNull(), unknown},
		"config set":               {existing, types.StringValue("y"), types.StringValue("y"), types.StringValue("x"), types.StringValue("y")},
	} {
		t.Run(name, func(t *testing.T) {
			req := planmodifier.StringRequest{State: tc.state, ConfigValue: tc.cfg, PlanValue: tc.plan, StateValue: tc.stateV}
			resp := &planmodifier.StringResponse{PlanValue: tc.plan}
			UseStateForUnknownIncludingNull().PlanModifyString(context.Background(), req, resp)
			if !resp.PlanValue.Equal(tc.want) {
				t.Errorf("got %s, want %s", resp.PlanValue, tc.want)
			}
		})
	}
}
