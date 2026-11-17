package metaobject

import (
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func field(key, typ string, value *string) client.MetaobjectFieldsFieldsMetaobjectField {
	return client.MetaobjectFieldsFieldsMetaobjectField{Key: key, Type: typ, Value: value}
}

func ptr(s string) *string { return &s }

func TestReadFields(t *testing.T) {
	api := []client.MetaobjectFieldsFieldsMetaobjectField{
		field("title", "single_line_text_field", ptr("Baumwolle")),
		field("num", "number_integer", ptr("7")),
		field("tags", "list.single_line_text_field", ptr(`["x","y"]`)),
		field("body", "multi_line_text_field", nil),
		field("other", "single_line_text_field", ptr("unmanaged")),
	}
	t.Run("import takes every field with a value", func(t *testing.T) {
		got := readFields(nil, api)
		want := map[string]string{"title": "Baumwolle", "num": "7", "tags": `["x","y"]`, "other": "unmanaged"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("managed keys keep equivalent written values", func(t *testing.T) {
		prior := map[string]string{"title": "Baumwolle", "num": "007", "tags": `["x", "y"]`, "body": ""}
		got := readFields(prior, api)
		if !reflect.DeepEqual(got, prior) {
			t.Errorf("got %v, want %v", got, prior)
		}
	})
	t.Run("drift takes the live value", func(t *testing.T) {
		prior := map[string]string{"title": "Leinen", "num": "8", "body": "was set"}
		got := readFields(prior, api)
		want := map[string]string{"title": "Baumwolle", "num": "7", "body": ""}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("a key removed from the definition is dropped", func(t *testing.T) {
		got := readFields(map[string]string{"title": "Baumwolle", "gone": "x"}, api)
		want := map[string]string{"title": "Baumwolle"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestChangedFields(t *testing.T) {
	got := changedFields(
		map[string]string{"title": "Leinen", "num": "7", "body": ""},
		map[string]string{"title": "Baumwolle", "num": "7"},
	)
	want := []client.MetaobjectFieldInput{{Key: "body", Value: ""}, {Key: "title", Value: "Leinen"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := changedFields(map[string]string{"a": "1"}, map[string]string{"a": "1"}); got != nil {
		t.Errorf("no change: got %v, want nil", got)
	}
}

type userError struct {
	field      []string
	elementKey *string
}

func (e userError) GetField() []string     { return e.field }
func (e userError) GetMessage() string     { return "msg" }
func (e userError) GetElementKey() *string { return e.elementKey }

func TestErrorPath(t *testing.T) {
	cases := []struct {
		err  userError
		want path.Path
		ok   bool
	}{
		{userError{[]string{"metaobject", "fields", "10"}, ptr("nums")}, path.Root("fields").AtMapKey("nums"), true},
		{userError{[]string{"metaobject", "fields", "0"}, nil}, path.Root("fields"), true},
		{userError{[]string{"metaobject", "handle"}, nil}, path.Root("handle"), true},
		{userError{[]string{"metaobject", "capabilities", "publishable"}, nil}, path.Root("status"), true},
		{userError{[]string{"id"}, nil}, path.Empty(), false},
	}
	for _, c := range cases {
		got, ok := errorPath(c.err)
		if ok != c.ok || !got.Equal(c.want) {
			t.Errorf("errorPath(%v) = %v, %v; want %v, %v", c.err.field, got, ok, c.want, c.ok)
		}
	}
}
