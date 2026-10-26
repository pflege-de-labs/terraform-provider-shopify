package metaobjectdefinition

import (
	"strings"
	"testing"
)

func TestNotFoundDetail(t *testing.T) {
	got := notFoundDetail("shopify--material")
	if !strings.HasPrefix(got, "no metaobject definition of type shopify--material") {
		t.Errorf("detail must name the type first: %q", got)
	}
	if !strings.Contains(got, "read_metaobject_definitions") {
		t.Errorf("detail must mention the missing-scope case: %q", got)
	}
}
