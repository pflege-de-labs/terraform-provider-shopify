package checkoutandaccountsconfiguration

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestBrandingValueSemanticEquals(t *testing.T) {
	ctx := context.Background()
	equal, diags := brandingString(`{"a":"#aabbcc","b":1}`).StringSemanticEquals(ctx, brandingString(`{"b": 1.0, "a": "#AABBCC"}`))
	if diags.HasError() || !equal {
		t.Errorf("want equal, got %v %v", equal, diags)
	}
	equal, _ = brandingString(`{"a":1}`).StringSemanticEquals(ctx, brandingString(`{"a":2}`))
	if equal {
		t.Error("different values must not be equal")
	}
}

func TestBrandingTypeRoundTrip(t *testing.T) {
	ctx := context.Background()
	v, err := brandingType{}.ValueFromTerraform(ctx, tftypes.NewValue(tftypes.String, `{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	bv, ok := v.(brandingValue)
	if !ok || bv.ValueString() != `{"a":1}` {
		t.Fatalf("got %#v", v)
	}
	if !bv.Type(ctx).Equal(brandingType{}) {
		t.Error("value must report the custom type")
	}
	if brandingNull().Type(ctx).Equal(brandingType{}) != true || !brandingNull().IsNull() {
		t.Error("null value must keep the custom type")
	}
}
