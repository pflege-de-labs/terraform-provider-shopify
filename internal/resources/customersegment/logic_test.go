package customersegment

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func TestErrorPath(t *testing.T) {
	cases := []struct {
		field []string
		want  string
		ok    bool
	}{
		{[]string{"query"}, "query", true},
		{[]string{"name"}, "name", true},
		{[]string{"id"}, "", false},
		{nil, "", false},
		{[]string{}, "", false},
	}
	for _, c := range cases {
		got, ok := errorPath(c.field)
		if ok != c.ok || (ok && !got.Equal(path.Root(c.want))) {
			t.Errorf("errorPath(%v) = %v, %v; want %q, %v", c.field, got, ok, c.want, c.ok)
		}
	}
}

func TestGoneOnDelete(t *testing.T) {
	type e = client.DeleteSegmentSegmentDeleteSegmentDeletePayloadUserErrorsUserError
	cases := []struct {
		name string
		errs []e
		want bool
	}{
		{"no errors", nil, false},
		{"does not exist", []e{{Field: []string{"id"}, Message: "Segment does not exist"}}, true},
		{"other", []e{{Field: []string{"id"}, Message: "Segment is in use"}}, false},
		{"mixed", []e{{Message: "Segment does not exist"}, {Message: "Segment is in use"}}, false},
	}
	for _, c := range cases {
		if got := goneOnDelete(c.errs); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestUpdateArgsSendOnlyChanges(t *testing.T) {
	prior := model{Name: types.StringValue("n"), Query: types.StringValue("q")}
	cases := []struct {
		name         string
		cfg          model
		wantN, wantQ *string
		ok           bool
	}{
		{"nothing", model{Name: prior.Name, Query: prior.Query}, nil, nil, false},
		{"adopt flag only", model{Name: prior.Name, Query: prior.Query, AdoptExisting: types.BoolValue(true)}, nil, nil, false},
		{"name", model{Name: types.StringValue("n2"), Query: prior.Query}, ptr("n2"), nil, true},
		{"query", model{Name: prior.Name, Query: types.StringValue("q2")}, nil, ptr("q2"), true},
		{"both", model{Name: types.StringValue("n2"), Query: types.StringValue("q2")}, ptr("n2"), ptr("q2"), true},
	}
	for _, c := range cases {
		n, q, ok := c.cfg.updateArgs(&prior)
		if ok != c.ok || !eq(n, c.wantN) || !eq(q, c.wantQ) {
			t.Errorf("%s: got %v %v %v", c.name, deref(n), deref(q), ok)
		}
	}
}

func ptr(s string) *string { return &s }

func eq(a, b *string) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestNameRe(t *testing.T) {
	for name, want := range map[string]bool{
		"x": true, "Email subscribers (2)": true, "a  b": true, "a\nb": true,
		"": false, " x": false, "x ": false, " x ": false, "\tx": false, "x\n": false, "x\u00a0": false, " ": false,
	} {
		if got := nameRe.MatchString(name); got != want {
			t.Errorf("nameRe(%q) = %v, want %v", name, got, want)
		}
	}
}
