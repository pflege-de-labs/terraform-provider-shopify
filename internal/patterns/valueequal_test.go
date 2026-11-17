package patterns

import "testing"

// The number, boolean, json, list and rich text pairs are from the dev store probe (2026-11-17); the rest follow the rules in resource model §9t.
func TestValueEqual(t *testing.T) {
	cases := []struct {
		typ, written, read string
		want               bool
	}{
		{"number_integer", "007", "7", true},
		{"number_decimal", "1.50", "1.5", true},
		{"number_decimal", "1.5", "1.6", false},
		{"boolean", "TRUE", "true", true},
		{"boolean", "true", "false", false},
		{"json", `{ "b": 1,  "a": [1, 2.0] }`, `{"b":1,"a":[1,2.0]}`, true},
		{"json", `{"a": 1}`, `{"a": 2}`, false},
		{"list.single_line_text_field", `["x",  "y"]`, `["x","y"]`, true},
		{"list.single_line_text_field", `["x","y"]`, `["y","x"]`, false},
		{"list.number_integer", "[1,  2]", "[1,2]", true},
		{"rich_text_field", `{"type": "root", "children": []}`, `{"type":"root","children":[]}`, true},
		{"weight", `{"unit": "KILOGRAMS", "value": 1.0}`, `{"unit":"KILOGRAMS","value":1}`, true},
		{"single_line_text_field", "Baumwolle", "Baumwolle", true},
		{"single_line_text_field", "Baumwolle", "baumwolle", false},
		{"single_line_text_field", " a", "a", false},
		{"multi_line_text_field", "a\r\nb\n", "a\r\nb\n", true},
		{"color", "#FFAA00", "#ffaa00", false},
		{"json", "", "", true},
		{"json", "not json", "not json", true},
		{"json", "not json", "not  json", false},
	}
	for _, c := range cases {
		if got := ValueEqual(c.typ, c.written, c.read); got != c.want {
			t.Errorf("ValueEqual(%q, %q, %q) = %v, want %v", c.typ, c.written, c.read, got, c.want)
		}
	}
}
