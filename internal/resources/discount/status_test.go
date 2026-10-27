package discount

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestKeepStatus(t *testing.T) {
	s := schema.Schema{Attributes: map[string]schema.Attribute{
		"title":  schema.StringAttribute{Required: true},
		"status": schema.StringAttribute{Computed: true},
	}}
	typ := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"title": tftypes.String, "status": tftypes.String}}
	val := func(title string, status any) tftypes.Value {
		return tftypes.NewValue(typ, map[string]tftypes.Value{"title": tftypes.NewValue(tftypes.String, title), "status": tftypes.NewValue(tftypes.String, status)})
	}
	state := tfsdk.State{Schema: s, Raw: val("T", "ACTIVE")}
	for name, c := range map[string]struct {
		plan tftypes.Value
		want tftypes.Value
	}{
		// Unset nested objects make the framework mark status unknown on a plan with no real change.
		"no change":   {val("T", tftypes.UnknownValue), val("T", "ACTIVE")},
		"real update": {val("U", tftypes.UnknownValue), val("U", tftypes.UnknownValue)},
	} {
		plan := tfsdk.Plan{Schema: s, Raw: c.plan}
		if d := keepStatus(context.Background(), state, &plan); d.HasError() {
			t.Fatal(d)
		}
		if !plan.Raw.Equal(c.want) {
			t.Errorf("%s: got %s, want %s", name, plan.Raw, c.want)
		}
	}
}
