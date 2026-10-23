package client

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
)

func TestFieldPath(t *testing.T) {
	cases := []struct {
		field []string
		want  path.Path
		ok    bool
	}{
		{[]string{"definition", "namespace"}, path.Root("namespace"), true},
		{[]string{"definition", "ownerType"}, path.Root("owner_type"), true},
		{[]string{"input", "fieldDefinitions", "2", "key"}, path.Root("field_definitions").AtListIndex(2).AtName("key"), true},
		{[]string{"definition"}, path.Empty(), false},
		{nil, path.Empty(), false},
	}
	for _, tc := range cases {
		got, ok := FieldPath(tc.field)
		if ok != tc.ok || !got.Equal(tc.want) {
			t.Errorf("FieldPath(%v) = %s, %v; want %s, %v", tc.field, got, ok, tc.want, tc.ok)
		}
	}
}

func TestUserErrorDiags(t *testing.T) {
	diags := UserErrorDiags("Create failed", []CreateMetafieldDefinitionMetafieldDefinitionCreateMetafieldDefinitionCreatePayloadUserErrorsMetafieldDefinitionCreateUserError{
		{Field: []string{"definition", "key"}, Message: "Key is taken"},
		{Message: "Something else"},
	})
	if len(diags) != 2 || !diags.HasError() {
		t.Fatalf("got %v", diags)
	}
	if d, ok := diags[0].(interface{ Path() path.Path }); !ok || !d.Path().Equal(path.Root("key")) {
		t.Errorf("first diag not attribute-scoped: %v", diags[0])
	}
	if diags[1].Detail() != "Something else" {
		t.Errorf("second diag detail = %q", diags[1].Detail())
	}
}
