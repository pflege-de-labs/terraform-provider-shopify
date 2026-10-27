package patterns

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestSplitImportKey(t *testing.T) {
	got, err := SplitImportKey("PRODUCT/custom/material", "OWNER_TYPE/namespace/key")
	if err != nil || strings.Join(got, ",") != "PRODUCT,custom,material" {
		t.Errorf("got %v, %v", got, err)
	}

	for _, bad := range []string{"", "PRODUCT/custom", "PRODUCT//material", "PRODUCT/custom/material/x", "gid://shopify/MetafieldDefinition/1"} {
		_, err := SplitImportKey(bad, "OWNER_TYPE/namespace/key")
		if err == nil {
			t.Errorf("%q: want error", bad)
			continue
		}
		if !strings.Contains(err.Error(), "OWNER_TYPE/namespace/key") {
			t.Errorf("%q: error should name the expected format: %v", bad, err)
		}
	}
}

func TestGuardDataDeletion(t *testing.T) {
	if d := GuardDataDeletion(types.BoolValue(true), "metafield definition"); d.HasError() {
		t.Errorf("allowed deletion blocked: %v", d)
	}
	for _, v := range []types.Bool{types.BoolValue(false), types.BoolNull()} {
		d := GuardDataDeletion(v, "metafield definition")
		if !d.HasError() || !strings.Contains(d[0].Detail(), "allow_data_deletion = true") {
			t.Errorf("%v: want error naming the flag, got %v", v, d)
		}
	}
}

func TestWarnOnDataDeletingReplace(t *testing.T) {
	ctx := context.Background()
	s := schema.Schema{Attributes: map[string]schema.Attribute{
		"type":                schema.StringAttribute{Required: true},
		"name":                schema.StringAttribute{Required: true},
		"allow_data_deletion": schema.BoolAttribute{Optional: true},
	}}
	obj := func(typ, name string, allow bool) tftypes.Value {
		return tftypes.NewValue(s.Type().TerraformType(ctx), map[string]tftypes.Value{
			"type":                tftypes.NewValue(tftypes.String, typ),
			"name":                tftypes.NewValue(tftypes.String, name),
			"allow_data_deletion": tftypes.NewValue(tftypes.Bool, allow),
		})
	}
	run := func(state, plan tftypes.Value) diag.Diagnostics {
		resp := &resource.ModifyPlanResponse{}
		WarnOnDataDeletingReplace(ctx, resource.ModifyPlanRequest{
			State: tfsdk.State{Schema: s, Raw: state},
			Plan:  tfsdk.Plan{Schema: s, Raw: plan},
		}, resp, "thing", "type")
		return resp.Diagnostics
	}

	if d := run(obj("a", "x", false), obj("a", "y", false)); len(d) != 0 {
		t.Errorf("in-place change warned: %v", d)
	}
	d := run(obj("a", "x", false), obj("b", "x", false))
	if d.WarningsCount() != 1 || !strings.Contains(d[0].Detail(), "Apply will fail") {
		t.Errorf("replace without opt-in: %v", d)
	}
	d = run(obj("a", "x", true), obj("b", "x", true))
	if d.WarningsCount() != 1 || strings.Contains(d[0].Detail(), "Apply will fail") {
		t.Errorf("replace with opt-in: %v", d)
	}
	if d := run(tftypes.NewValue(s.Type().TerraformType(ctx), nil), obj("b", "x", false)); len(d) != 0 {
		t.Errorf("create warned: %v", d)
	}
}

func TestDiffIDs(t *testing.T) {
	tests := []struct {
		name        string
		live, want  []string
		add, remove []string
	}{
		{"same", []string{"a", "b"}, []string{"b", "a"}, nil, nil},
		{"add", []string{"a"}, []string{"a", "b"}, []string{"b"}, nil},
		{"remove", []string{"a", "b"}, []string{"a"}, nil, []string{"b"}},
		{"both, sorted", []string{"d", "b"}, []string{"c", "b", "a"}, []string{"a", "c"}, []string{"d"}},
		{"clear", []string{"a"}, []string{}, nil, []string{"a"}},
		{"from nothing", nil, []string{"a"}, []string{"a"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			add, remove := DiffIDs(tt.live, tt.want)
			if !reflect.DeepEqual(add, tt.add) || !reflect.DeepEqual(remove, tt.remove) {
				t.Errorf("DiffIDs = %v, %v; want %v, %v", add, remove, tt.add, tt.remove)
			}
		})
	}
}

// Shopify silently suffixes a taken natural key; that must surface as an error naming both values.
func TestKeyDrift(t *testing.T) {
	if err := KeyDrift("blog", "handle", "news", "news"); err != nil {
		t.Errorf("same key: got %v", err)
	}
	err := KeyDrift("segment", "name", "Email subscribers", "Email subscribers (2)")
	want := `the segment got the name "Email subscribers (2)" instead of "Email subscribers", because "Email subscribers" is taken`
	if err == nil || err.Error() != want {
		t.Errorf("suffixed key: got %v, want %q", err, want)
	}
}
