package metaobject

import (
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

// readFields maps the live fields to state: every field with a value after import (prior nil), else only the managed keys,
// keeping a written value Shopify merely reformatted. A cleared value reads as "", the value that clears it.
func readFields(prior map[string]string, api []client.MetaobjectFieldsFieldsMetaobjectField) map[string]string {
	out := map[string]string{}
	for _, f := range api {
		live := ""
		if f.Value != nil {
			live = *f.Value
		}
		if prior == nil {
			if f.Value != nil {
				out[f.Key] = live
			}
			continue
		}
		if was, ok := prior[f.Key]; ok {
			if patterns.ValueEqual(f.Type, was, live) {
				out[f.Key] = was
			} else {
				out[f.Key] = live
			}
		}
	}
	return out
}

// changedFields returns the values in want that differ from have, sorted by key; nil when there are none.
func changedFields(want, have map[string]string) []client.MetaobjectFieldInput {
	var out []client.MetaobjectFieldInput
	for k, v := range want {
		if old, ok := have[k]; !ok || old != v {
			out = append(out, client.MetaobjectFieldInput{Key: k, Value: v})
		}
	}
	slices.SortFunc(out, func(a, b client.MetaobjectFieldInput) int { return strings.Compare(a.Key, b.Key) })
	return out
}

// errorPath maps a metaobjectCreate/metaobjectUpdate userError to an attribute; field errors name the key in elementKey.
func errorPath(e client.UserError) (path.Path, bool) {
	f := e.GetField()
	if len(f) < 2 || f[0] != "metaobject" {
		return path.Empty(), false
	}
	switch f[1] {
	case "fields":
		if k, ok := e.(interface{ GetElementKey() *string }); ok && k.GetElementKey() != nil {
			return path.Root("fields").AtMapKey(*k.GetElementKey()), true
		}
		return path.Root("fields"), true
	case "handle", "type":
		return path.Root(f[1]), true
	case "capabilities":
		return path.Root("status"), true
	}
	return path.Empty(), false
}
