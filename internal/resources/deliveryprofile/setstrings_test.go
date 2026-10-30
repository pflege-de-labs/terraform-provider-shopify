package deliveryprofile

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestSetStrings_unknownElement(t *testing.T) {
	s := types.SetValueMust(types.StringType, []attr.Value{types.StringValue("a"), types.StringUnknown()})
	var diags diag.Diagnostics
	if _, ok := setStrings(context.Background(), s, &diags); ok || diags.HasError() {
		t.Errorf("a set with an unknown element must be not-yet-known without diagnostics: ok=%v diags=%v", ok, diags)
	}
}
