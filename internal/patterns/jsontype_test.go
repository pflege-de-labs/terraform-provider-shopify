package patterns

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// caseless is a test comparator: equal ignoring case.
type caseless struct{}

func (caseless) Equal(a, b string) bool { return strings.EqualFold(a, b) }

// exact is a second comparator, to check that types with different comparators differ.
type exact struct{}

func (exact) Equal(a, b string) bool { return a == b }

func TestJSONValueSemanticEquals(t *testing.T) {
	ctx := context.Background()
	equal, diags := JSONString[caseless](`{"a":"X"}`).StringSemanticEquals(ctx, JSONString[caseless](`{"A":"x"}`))
	if diags.HasError() || !equal {
		t.Errorf("want equal, got %v %v", equal, diags)
	}
	if equal, _ = JSONString[caseless](`{"a":1}`).StringSemanticEquals(ctx, JSONString[caseless](`{"a":2}`)); equal {
		t.Error("different values must not be equal")
	}
	if equal, _ = JSONString[caseless](`{}`).StringSemanticEquals(ctx, JSONString[exact](`{}`)); equal {
		t.Error("values of different JSON types must not be equal")
	}
}

func TestJSONTypeRoundTrip(t *testing.T) {
	ctx := context.Background()
	v, err := JSONType[caseless]{}.ValueFromTerraform(ctx, tftypes.NewValue(tftypes.String, `{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	jv, ok := v.(JSONValue[caseless])
	if !ok || jv.ValueString() != `{"a":1}` {
		t.Fatalf("got %#v", v)
	}
	if !jv.Type(ctx).Equal(JSONType[caseless]{}) {
		t.Error("value must report its JSON type")
	}
	if jv.Type(ctx).Equal(JSONType[exact]{}) {
		t.Error("JSON types with different comparators must differ")
	}
	if n := JSONNull[caseless](); !n.Type(ctx).Equal(JSONType[caseless]{}) || !n.IsNull() {
		t.Error("null value must keep the JSON type")
	}
}
