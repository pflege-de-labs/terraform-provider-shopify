package metaobjectdefinition

import (
	"fmt"
	"strings"
	"testing"
)

func f(key, typ, name string) field {
	return field{Key: key, Type: typ, Name: name}
}

// render makes ops comparable: "-key", "+key", "~key".
func render(ops []fieldOp) string {
	out := make([]string, len(ops))
	for i, op := range ops {
		switch {
		case op.Delete != "":
			out[i] = "-" + op.Delete
		case op.Create != nil:
			out[i] = "+" + op.Create.Key
		case op.Update != nil:
			out[i] = "~" + op.Update.Key
		}
	}
	return strings.Join(out, " ")
}

func TestDiffFields(t *testing.T) {
	a, b, c := f("a", "single_line_text_field", "A"), f("b", "number_integer", "B"), f("c", "boolean", "C")
	cases := []struct {
		name        string
		old, new    []field
		ops         string
		destructive string
	}{
		{"unchanged keeps order", []field{a, b}, []field{a, b}, "~a ~b", ""},
		{"add appends", []field{a}, []field{a, b}, "~a +b", ""},
		{"add in the middle keeps position", []field{a, c}, []field{a, b, c}, "~a +b ~c", ""},
		{"rename is an update", []field{a}, []field{f("a", a.Type, "renamed")}, "~a", ""},
		{"remove deletes first", []field{a, b, c}, []field{a, c}, "-b ~a ~c", "b"},
		{"type change is delete + create", []field{a, b}, []field{a, f("b", "boolean", "B")}, "-b ~a +b", "b"},
		{"reorder updates all in new order", []field{a, b, c}, []field{c, a, b}, "~c ~a ~b", ""},
		{"from empty", nil, []field{a, b}, "+a +b", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ops, destructive := diffFields(tc.old, tc.new)
			if got := render(ops); got != tc.ops {
				t.Errorf("ops = %q, want %q", got, tc.ops)
			}
			if got := strings.Join(destructive, ","); got != tc.destructive {
				t.Errorf("destructive = %q, want %q", got, tc.destructive)
			}
		})
	}
}

func TestDuplicateKeys(t *testing.T) {
	if d := duplicateKeys([]field{f("a", "x", "A"), f("b", "x", "B")}); len(d) != 0 {
		t.Errorf("got %v", d)
	}
	d := duplicateKeys([]field{f("a", "x", "A"), f("b", "x", "B"), f("a", "y", "A2"), f("a", "z", "A3")})
	if fmt.Sprint(d) != "[a]" {
		t.Errorf("got %v, want [a]", d)
	}
}
