package location

import "testing"

func TestLookupFor(t *testing.T) {
	cases := []struct {
		key, gid, query string
	}{
		{"12345", "gid://shopify/Location/12345", ""},
		{"Lager Hamburg", "", `name:"Lager Hamburg"`},
		{`Say "hi"`, "", `name:"Say \"hi\""`},
		{`back\slash`, "", `name:"back\\slash"`},
		{"12a", "", `name:"12a"`},
	}
	for _, tc := range cases {
		gid, query := lookupFor(tc.key)
		if gid != tc.gid || query != tc.query {
			t.Errorf("lookupFor(%q) = %q, %q; want %q, %q", tc.key, gid, query, tc.gid, tc.query)
		}
	}
}
