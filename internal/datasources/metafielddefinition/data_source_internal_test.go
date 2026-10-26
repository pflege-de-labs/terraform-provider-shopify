package metafielddefinition

import (
	"strings"
	"testing"
)

func TestNotFoundDetail(t *testing.T) {
	got := notFoundDetail("ORDER", "checkoutblocks", "x")
	if !strings.HasPrefix(got, "no metafield definition ORDER/checkoutblocks/x") {
		t.Errorf("detail must name the key first: %q", got)
	}
	if !strings.Contains(got, "read scope for owner type ORDER") {
		t.Errorf("detail must mention the missing-scope case: %q", got)
	}
}
