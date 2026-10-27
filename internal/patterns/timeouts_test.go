package patterns

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func timeoutsValue(t *testing.T, create, update types.String) types.Object {
	t.Helper()
	obj, diags := types.ObjectValue(TimeoutsAttrTypes("create", "update"), map[string]attr.Value{"create": create, "update": update})
	if diags.HasError() {
		t.Fatal(diags)
	}
	return obj
}

func TestTimeoutOf(t *testing.T) {
	cases := []struct {
		name    string
		obj     types.Object
		want    time.Duration
		wantErr bool
	}{
		{"no block", TimeoutsNull("create", "update"), 10 * time.Minute, false},
		{"unknown block", types.ObjectUnknown(TimeoutsAttrTypes("create", "update")), 10 * time.Minute, false},
		{"unset attribute", timeoutsValue(t, types.StringNull(), types.StringValue("1m")), 10 * time.Minute, false},
		{"set", timeoutsValue(t, types.StringValue("90s"), types.StringNull()), 90 * time.Second, false},
		{"invalid", timeoutsValue(t, types.StringValue("soon"), types.StringNull()), 0, true},
		{"not positive", timeoutsValue(t, types.StringValue("0s"), types.StringNull()), 0, true},
	}
	for _, c := range cases {
		got, err := TimeoutOf(c.obj, "create", 10*time.Minute)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("%s: got %v, %v; want %v, error: %t", c.name, got, err, c.want, c.wantErr)
		}
	}
}

func TestDurationValidator(t *testing.T) {
	for v, wantErr := range map[string]bool{"10m": false, "1h30m": false, "10": true, "-1m": true, "": true} {
		resp := &validator.StringResponse{}
		DurationValidator().ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue(v)}, resp)
		if resp.Diagnostics.HasError() != wantErr {
			t.Errorf("%q: error = %t, want %t", v, resp.Diagnostics.HasError(), wantErr)
		}
	}
	resp := &validator.StringResponse{}
	DurationValidator().ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringUnknown()}, resp)
	if resp.Diagnostics.HasError() {
		t.Error("an unknown value must pass")
	}
}

func TestTimeoutsBlockHasOneAttributePerOperation(t *testing.T) {
	b := TimeoutsBlock("create", "update")
	if got := len(b.GetNestedObject().GetAttributes()); got != 2 {
		t.Errorf("attributes = %d, want 2", got)
	}
}
