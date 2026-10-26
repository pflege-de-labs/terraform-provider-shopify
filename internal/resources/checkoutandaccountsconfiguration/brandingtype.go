package checkoutandaccountsconfiguration

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var (
	_ basetypes.StringTypable                    = brandingType{}
	_ basetypes.StringValuableWithSemanticEquals = brandingValue{}
)

// brandingType is a JSON string compared with brandingEqual, so the API's casing and number format never show as drift.
type brandingType struct {
	basetypes.StringType
}

func (t brandingType) Equal(o attr.Type) bool {
	_, ok := o.(brandingType)
	return ok
}

func (t brandingType) String() string { return "brandingType" }

func (t brandingType) ValueType(context.Context) attr.Value { return brandingValue{} }

func (t brandingType) ValueFromString(_ context.Context, in basetypes.StringValue) (basetypes.StringValuable, diag.Diagnostics) {
	return brandingValue{StringValue: in}, nil
}

func (t brandingType) ValueFromTerraform(ctx context.Context, in tftypes.Value) (attr.Value, error) {
	v, err := t.StringType.ValueFromTerraform(ctx, in)
	if err != nil {
		return nil, err
	}
	s, ok := v.(basetypes.StringValue)
	if !ok {
		return nil, fmt.Errorf("unexpected value type %T", v)
	}
	return brandingValue{StringValue: s}, nil
}

type brandingValue struct {
	basetypes.StringValue
}

func brandingString(s string) brandingValue {
	return brandingValue{StringValue: basetypes.NewStringValue(s)}
}

func brandingNull() brandingValue {
	return brandingValue{StringValue: basetypes.NewStringNull()}
}

func (v brandingValue) Type(context.Context) attr.Type { return brandingType{} }

func (v brandingValue) Equal(o attr.Value) bool {
	other, ok := o.(brandingValue)
	return ok && v.StringValue.Equal(other.StringValue)
}

func (v brandingValue) StringSemanticEquals(_ context.Context, o basetypes.StringValuable) (bool, diag.Diagnostics) {
	other, ok := o.(brandingValue)
	if !ok {
		return false, nil
	}
	return brandingEqual(v.ValueString(), other.ValueString()), nil
}
