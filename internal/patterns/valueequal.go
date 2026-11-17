package patterns

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
)

// jsonTypes are the non-list field types whose values Shopify stores as minified JSON.
var jsonTypes = map[string]bool{
	"json": true, "rich_text_field": true, "dimension": true, "volume": true, "weight": true, "rating": true, "money": true, "link": true,
}

// ValueEqual reports whether Shopify's read-back b of a metafield or metaobject field of type typ is equivalent to the written a (resource model §9t, §9v).
func ValueEqual(typ, a, b string) bool {
	if a == b {
		return true
	}
	switch {
	case typ == "number_integer" || typ == "number_decimal":
		return DecimalEqual(a, b)
	case typ == "boolean":
		x, err1 := strconv.ParseBool(a)
		y, err2 := strconv.ParseBool(b)
		return err1 == nil && err2 == nil && x == y
	case jsonTypes[typ] || strings.HasPrefix(typ, "list."):
		var x, y any
		return json.Unmarshal([]byte(a), &x) == nil && json.Unmarshal([]byte(b), &y) == nil && reflect.DeepEqual(x, y)
	}
	return false
}
