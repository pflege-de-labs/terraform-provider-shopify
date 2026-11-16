package metafielddefinition

import (
	"strings"
	"testing"
)

func TestEmptyListWarning(t *testing.T) {
	if d := emptyListWarning("ORDER", 3); d.HasError() || d.WarningsCount() != 0 {
		t.Fatalf("listed definitions: want no diagnostics, got %v", d)
	}
	d := emptyListWarning("ORDER", 0)
	if d.HasError() || d.WarningsCount() != 1 {
		t.Fatalf("empty list: want one warning, got %v", d)
	}
	if detail := d[0].Detail(); !strings.Contains(detail, "ORDER") || !strings.Contains(detail, "read scope") {
		t.Errorf("warning must name the owner type and the scope hint, got %q", detail)
	}
}
