package discount

import (
	"context"
	"math/big"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestKeepStatus(t *testing.T) {
	s := schema.Schema{Attributes: map[string]schema.Attribute{
		"title":      schema.StringAttribute{Required: true},
		"percentage": schema.Float64Attribute{Required: true},
		"status":     schema.StringAttribute{Computed: true},
	}}
	typ := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"title": tftypes.String, "percentage": tftypes.Number, "status": tftypes.String}}
	val := func(title string, pct *big.Float, status any) tftypes.Value {
		return tftypes.NewValue(typ, map[string]tftypes.Value{
			"title":      tftypes.NewValue(tftypes.String, title),
			"percentage": tftypes.NewValue(tftypes.Number, pct),
			"status":     tftypes.NewValue(tftypes.String, status),
		})
	}
	// Config numbers arrive as exact decimals; state holds what Read stored as float64.
	decimal := func(s string) *big.Float {
		f, _, err := big.ParseFloat(s, 10, 512, big.ToNearestEven)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	state := tfsdk.State{Schema: s, Raw: val("T", big.NewFloat(0.1), "ACTIVE")}
	for name, c := range map[string]struct {
		plan tftypes.Value
		want tftypes.Value
	}{
		// Unset nested objects make the framework mark status unknown on a plan with no real change.
		"no change":      {val("T", big.NewFloat(0.1), tftypes.UnknownValue), val("T", big.NewFloat(0.1), "ACTIVE")},
		"real update":    {val("U", big.NewFloat(0.1), tftypes.UnknownValue), val("U", big.NewFloat(0.1), tftypes.UnknownValue)},
		"config decimal": {val("T", decimal("0.1"), tftypes.UnknownValue), val("T", decimal("0.1"), "ACTIVE")},
		"number change":  {val("T", decimal("0.2"), tftypes.UnknownValue), val("T", decimal("0.2"), tftypes.UnknownValue)},
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
