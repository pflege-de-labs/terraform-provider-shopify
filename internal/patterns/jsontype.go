package patterns

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// JSONEquality compares two JSON strings the way the API's read-back differs from what was written.
type JSONEquality interface {
	Equal(a, b string) bool
}

var (
	_ basetypes.StringTypable                    = JSONType[JSONEquality]{}
	_ basetypes.StringValuableWithSemanticEquals = JSONValue[JSONEquality]{}
)

// JSONType is a JSON string attribute compared with E, so API formatting never shows as drift.
type JSONType[E JSONEquality] struct {
	basetypes.StringType
}

func (t JSONType[E]) Equal(o attr.Type) bool {
	_, ok := o.(JSONType[E])
	return ok
}

func (t JSONType[E]) String() string { return fmt.Sprintf("JSONType[%T]", *new(E)) }

func (t JSONType[E]) ValueType(context.Context) attr.Value { return JSONValue[E]{} }

func (t JSONType[E]) ValueFromString(_ context.Context, in basetypes.StringValue) (basetypes.StringValuable, diag.Diagnostics) {
	return JSONValue[E]{StringValue: in}, nil
}

func (t JSONType[E]) ValueFromTerraform(ctx context.Context, in tftypes.Value) (attr.Value, error) {
	v, err := t.StringType.ValueFromTerraform(ctx, in)
	if err != nil {
		return nil, err
	}
	s, ok := v.(basetypes.StringValue)
	if !ok {
		return nil, fmt.Errorf("unexpected value type %T", v)
	}
	return JSONValue[E]{StringValue: s}, nil
}

// JSONValue is a value of JSONType[E].
type JSONValue[E JSONEquality] struct {
	basetypes.StringValue
}

func JSONString[E JSONEquality](s string) JSONValue[E] {
	return JSONValue[E]{StringValue: basetypes.NewStringValue(s)}
}

func JSONNull[E JSONEquality]() JSONValue[E] {
	return JSONValue[E]{StringValue: basetypes.NewStringNull()}
}

func (v JSONValue[E]) Type(context.Context) attr.Type { return JSONType[E]{} }

func (v JSONValue[E]) Equal(o attr.Value) bool {
	other, ok := o.(JSONValue[E])
	return ok && v.StringValue.Equal(other.StringValue)
}

func (v JSONValue[E]) StringSemanticEquals(_ context.Context, o basetypes.StringValuable) (bool, diag.Diagnostics) {
	other, ok := o.(JSONValue[E])
	if !ok {
		return false, nil
	}
	var e E
	return e.Equal(v.ValueString(), other.ValueString()), nil
}
