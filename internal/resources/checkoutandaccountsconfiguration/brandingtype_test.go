package checkoutandaccountsconfiguration

import (
	"context"
	"testing"
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
