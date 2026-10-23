package market

import (
	"fmt"
	"testing"
)

func TestSetDiff(t *testing.T) {
	cases := []struct {
		old, new []string
		add, del string
	}{
		{[]string{"DE"}, []string{"DE", "AT"}, "[AT]", "[]"},
		{[]string{"DE", "AT"}, []string{"AT"}, "[]", "[DE]"},
		{[]string{"DE", "AT"}, []string{"CH", "AT"}, "[CH]", "[DE]"},
		{[]string{"DE"}, []string{"DE"}, "[]", "[]"},
		{nil, []string{"B", "A"}, "[A B]", "[]"},
	}
	for _, tc := range cases {
		add, del := setDiff(tc.old, tc.new)
		if fmt.Sprint(add) != tc.add || fmt.Sprint(del) != tc.del {
			t.Errorf("setDiff(%v, %v) = %v, %v; want %s, %s", tc.old, tc.new, add, del, tc.add, tc.del)
		}
	}
}
