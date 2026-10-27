package collection

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const conditionPrefix = "CollectionSourceInclusionCondition"

var decimalRe = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

// inputKinds are the keys of CollectionSourceInclusionConditionInput.
var inputKinds = map[string]bool{
	"productTag": true, "productTitle": true, "productCategory": true, "productType": true,
	"productVendor": true, "productStatus": true, "variantTitle": true, "variantPrice": true,
	"variantCompareAtPrice": true, "variantInventory": true, "variantWeight": true,
	"metafieldBoolean": true, "metafieldInt": true, "metafieldDecimal": true, "metafieldString": true,
	"metafieldStringList": true, "metafieldMetaobject": true, "metafieldMetaobjectList": true,
}

// parseConditions checks the configured shape: a JSON list of objects with one key each; Shopify validates the rest.
func parseConditions(s string) ([]any, error) {
	var list []any
	if err := json.Unmarshal([]byte(s), &list); err != nil || list == nil {
		return nil, errors.New("conditions must be a JSON list, such as jsonencode([...])")
	}
	for i, c := range list {
		m, ok := c.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("condition %d must be an object", i)
		}
		if len(m) != 1 {
			return nil, fmt.Errorf("condition %d must have exactly one key, the condition kind such as productTag", i)
		}
	}
	return list, nil
}

// liveConditions maps conditions as CollectionFields reads them to their input shape, with their IDs in the same order.
func liveConditions(conds []map[string]any) ([]any, []string, error) {
	out := make([]any, 0, len(conds))
	var ids []string
	for _, c := range conds {
		typename, _ := c["__typename"].(string)
		kind := strings.TrimPrefix(typename, conditionPrefix)
		key := lowerFirst(kind)
		if kind == typename || !inputKinds[key] {
			return nil, nil, fmt.Errorf("the condition type %s can't be expressed as input", typename)
		}
		id, _ := c["id"].(string)
		ids = append(ids, id)
		body := map[string]any{}
		for k, v := range c {
			if k != "__typename" && k != "id" {
				mapField(body, k, v)
			}
		}
		out = append(out, map[string]any{key: body})
	}
	return out, ids, nil
}

// mapField writes one output field in input shape: aliases lose their `__<kind>` suffix, `{id}` objects become IDs, nulls are dropped.
func mapField(out map[string]any, key string, v any) {
	key, _, _ = strings.Cut(key, "__")
	if v == nil {
		return
	}
	if id, ok := idOnly(v); ok {
		if key != "value" {
			key += "Id"
		}
		out[key] = id
		return
	}
	switch t := v.(type) {
	case []any:
		list := make([]any, len(t))
		for i, e := range t {
			list[i] = e
			if id, ok := idOnly(e); ok {
				list[i] = id
			} else if m, ok := e.(map[string]any); ok {
				obj := map[string]any{}
				for k, f := range m {
					mapField(obj, k, f)
				}
				list[i] = obj
			}
		}
		out[key] = list
	default:
		out[key] = v
	}
}

func idOnly(v any) (string, bool) {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 1 {
		return "", false
	}
	id, ok := m["id"].(string)
	return id, ok
}

func lowerFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[n:]
}

// conditionsEqual compares two condition lists in order; amounts and values compare numerically, nulls equal missing keys.
func conditionsEqual(a, b string) bool {
	var av, bv any
	if json.Unmarshal([]byte(a), &av) != nil || json.Unmarshal([]byte(b), &bv) != nil {
		return a == b
	}
	return jsonEqual(av, bv, "")
}

func jsonEqual(a, b any, key string) bool {
	switch at := a.(type) {
	case map[string]any:
		bt, ok := b.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range at {
			if !jsonEqual(v, bt[k], k) {
				return false
			}
		}
		for k, v := range bt {
			if _, seen := at[k]; !seen && v != nil {
				return false
			}
		}
		return true
	case []any:
		bt, ok := b.([]any)
		if !ok || len(at) != len(bt) {
			return false
		}
		for i := range at {
			if !jsonEqual(at[i], bt[i], key) {
				return false
			}
		}
		return true
	}
	if key == "amount" || key == "value" {
		if x, ok := number(a); ok {
			if y, ok := number(b); ok {
				return x == y
			}
		}
	}
	return a == b
}

// number reads a JSON number or a decimal string such as Shopify's "5.0".
func number(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		if !decimalRe.MatchString(t) {
			return 0, false
		}
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	}
	return 0, false
}
