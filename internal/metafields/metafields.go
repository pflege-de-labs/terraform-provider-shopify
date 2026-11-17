// Package metafields manages the metafield values of one owner as a map attribute, shared by every owner type (resource model §9v).
package metafields

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

// maxPerCall is Shopify's limit for metafieldsSet (checked: 26 are refused); the same size is used for metafieldsDelete.
// TODO(verify): the metafieldsDelete limit.
const maxPerCall = 25

var keyRe = regexp.MustCompile(`^([A-Za-z0-9_-]{3,255})\.([A-Za-z0-9_-]{2,64})$`)

// Entry is one managed metafield: its type and its value in Shopify's string form.
type Entry struct{ Type, Value string }

// Live is a metafield as the API returns it.
type Live struct{ Namespace, Key, Type, Value string }

// seoOwned are the metafields behind seo_title and seo_description (found by the import acceptance test), so those attributes own them.
func seoOwned(ns, key string) bool {
	return ns == "global" && (key == "title_tag" || key == "description_tag")
}

// AppOwned reports whether a namespace belongs to an app (`app--<id>--…`), which the provider neither reads nor writes.
func AppOwned(ns string) bool { return strings.HasPrefix(ns, "app--") }

// SplitKey splits "namespace.key" on its first dot; neither part can contain one.
func SplitKey(k string) (ns, key string, err error) {
	if strings.HasPrefix(k, "app--") || strings.HasPrefix(k, "$app") {
		return "", "", fmt.Errorf("%q is in an app-owned namespace, which the provider doesn't manage", k)
	}
	m := keyRe.FindStringSubmatch(k)
	if m != nil && seoOwned(m[1], m[2]) {
		return "", "", fmt.Errorf("%q is set through seo_title and seo_description", k)
	}
	if m == nil {
		return "", "", fmt.Errorf("%q must be \"namespace.key\": a namespace of 3-255 and a key of 2-64 letters, digits, hyphens or underscores", k)
	}
	return m[1], m[2], nil
}

// Read maps live metafields to state. With prior nil (import, list) it takes every one outside app namespaces and the SEO ones;
// otherwise only the prior keys, keeping a written value Shopify merely reformatted. A managed key the API lacks reads as "".
func Read(prior map[string]Entry, live []Live) map[string]Entry {
	byKey := map[string]Live{}
	for _, l := range live {
		if !AppOwned(l.Namespace) && !seoOwned(l.Namespace, l.Key) {
			byKey[l.Namespace+"."+l.Key] = l
		}
	}
	out := map[string]Entry{}
	if prior == nil {
		for k, l := range byKey {
			out[k] = Entry{l.Type, l.Value}
		}
		return out
	}
	for k, was := range prior {
		l, ok := byKey[k]
		switch {
		case !ok:
			out[k] = Entry{was.Type, ""}
		case l.Type == was.Type && patterns.ValueEqual(l.Type, was.Value, l.Value):
			out[k] = was
		default:
			out[k] = Entry{l.Type, l.Value}
		}
	}
	return out
}

// Plan returns the sorted keys of want to write and to delete, given the current state have (from Read with want as prior).
func Plan(want, have map[string]Entry) (set, del []string) {
	for k, w := range want {
		h, exists := have[k]
		switch {
		case w.Value == "":
			if exists && h.Value != "" {
				del = append(del, k)
			}
		case h != w:
			set = append(set, k)
		}
	}
	slices.Sort(set)
	slices.Sort(del)
	return set, del
}

// ErrorPath maps a userError of metafieldsSet to the key at its position in the call; keys are the call's keys in order.
// Shopify names the position in field ("metafields", "<i>", …) and leaves elementIndex null (checked), so both are read.
func ErrorPath(keys []string, field []string, elementIndex *int) (path.Path, bool) {
	if len(field) == 0 || field[0] != "metafields" {
		return path.Empty(), false
	}
	i := -1
	if elementIndex != nil {
		i = *elementIndex
	} else if len(field) > 1 {
		if n, err := strconv.Atoi(field[1]); err == nil {
			i = n
		}
	}
	if i < 0 || i >= len(keys) {
		return path.Root("metafields"), true
	}
	return path.Root("metafields").AtMapKey(keys[i]), true
}

// Apply writes the keys of want that differ from have: values through metafieldsSet, "" through metafieldsDelete.
func Apply(ctx context.Context, c *client.Client, ownerID string, want, have map[string]Entry) diag.Diagnostics {
	var diags diag.Diagnostics
	set, del := Plan(want, have)
	for keys := range slices.Chunk(set, maxPerCall) {
		in := make([]client.MetafieldsSetInput, len(keys))
		for i, k := range keys {
			ns, key, err := SplitKey(k)
			if err != nil {
				diags.AddAttributeError(path.Root("metafields").AtMapKey(k), "Invalid metafield key", err.Error())
				return diags
			}
			typ := want[k].Type
			in[i] = client.MetafieldsSetInput{OwnerId: ownerID, Namespace: &ns, Key: key, Type: &typ, Value: want[k].Value}
		}
		res, err := client.SetMetafields(ctx, c, in)
		if err != nil {
			diags.AddError("Failed to write metafields", err.Error())
			return diags
		}
		diags.Append(client.UserErrorDiagsBy("Failed to write metafields", res.MetafieldsSet.UserErrors, func(e client.UserError) (path.Path, bool) {
			var idx *int
			if x, ok := e.(interface{ GetElementIndex() *int }); ok {
				idx = x.GetElementIndex()
			}
			return ErrorPath(keys, e.GetField(), idx)
		})...)
		if diags.HasError() {
			return diags
		}
	}
	for keys := range slices.Chunk(del, maxPerCall) {
		in := make([]client.MetafieldIdentifierInput, len(keys))
		for i, k := range keys {
			ns, key, _ := SplitKey(k)
			in[i] = client.MetafieldIdentifierInput{OwnerId: ownerID, Namespace: ns, Key: key}
		}
		res, err := client.DeleteMetafields(ctx, c, in)
		if err != nil {
			diags.AddError("Failed to delete metafields", err.Error())
			return diags
		}
		diags.Append(client.UserErrorDiagsWith("Failed to delete metafields", res.MetafieldsDelete.UserErrors, func([]string) (path.Path, bool) { return path.Root("metafields"), true })...)
		if diags.HasError() {
			return diags
		}
	}
	return diags
}

// List pages all metafields of an owner through the owner's page function.
func List(ctx context.Context, page func(after *string) ([]Live, *string, error)) ([]Live, error) {
	var all []Live
	err := patterns.Paginate(ctx, 0, page, func(l Live) bool { all = append(all, l); return true })
	return all, err
}

type object struct {
	Type  types.String `tfsdk:"type"`
	Value types.String `tfsdk:"value"`
}

var objectType = types.ObjectType{AttrTypes: map[string]attr.Type{"type": types.StringType, "value": types.StringType}}

// Null is the unmanaged value of the attribute.
func Null() types.Map { return types.MapNull(objectType) }

// FromValue reads the attribute; nil means unmanaged.
func FromValue(ctx context.Context, m types.Map) (map[string]Entry, diag.Diagnostics) {
	if m.IsNull() || m.IsUnknown() {
		return nil, nil
	}
	var objs map[string]object
	diags := m.ElementsAs(ctx, &objs, false)
	out := make(map[string]Entry, len(objs))
	for k, o := range objs {
		out[k] = Entry{o.Type.ValueString(), o.Value.ValueString()}
	}
	return out, diags
}

// ToValue builds the attribute; nilIfEmpty makes an empty map null, for import and list where nothing is managed yet.
func ToValue(ctx context.Context, e map[string]Entry, nilIfEmpty bool) (types.Map, diag.Diagnostics) {
	if e == nil || (nilIfEmpty && len(e) == 0) {
		return Null(), nil
	}
	objs := make(map[string]object, len(e))
	for k, v := range e {
		objs[k] = object{types.StringValue(v.Type), types.StringValue(v.Value)}
	}
	return types.MapValueFrom(ctx, objectType, objs)
}

// Attribute is the optional `metafields` attribute; unset means unmanaged.
func Attribute(owner string) schema.MapNestedAttribute {
	return schema.MapNestedAttribute{
		MarkdownDescription: "Metafield values of the " + owner + ", keyed `namespace.key` (a namespace of 3-255 and a key of 2-64 letters, digits, hyphens or underscores; the key is split on the first dot). " +
			"Only the keys listed here are managed: a key removed from the map stops being managed and keeps its value in Shopify. Unset means no metafield is managed. " +
			"`type` is a [metafield type](https://shopify.dev/docs/apps/build/custom-data/metafields/list-of-data-types) such as `single_line_text_field` or `list.product_reference`; " +
			"`value` is Shopify's string form: plain text as is, numbers and booleans as strings, list and JSON types as a JSON string (use `jsonencode()`). " +
			"Shopify rewrites some values (`007` becomes `7`, `TRUE` becomes `true`, JSON is minified); the configured text is kept while it means the same. " +
			"`value = \"\"` deletes the metafield, which then reads as `\"\"`. " +
			"Reference types hold store-specific GIDs, so set them from resource references (`shopify_metaobject.x.id`), not literals. " +
			"App-owned namespaces (`app--…`) and the SEO metafields `global.title_tag` and `global.description_tag` (use the SEO attributes) are rejected, and import and listing skip them. A metafield with a definition must be written with the definition's type. " +
			"Writing needs the owner's `write_*` scope (`write_products` for products, not verified alone, see `docs/bootstrapping.md`).",
		Optional:   true,
		Validators: []validator.Map{keysValidator{}},
		NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"type":  schema.StringAttribute{MarkdownDescription: "Metafield type.", Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"value": schema.StringAttribute{MarkdownDescription: "Value in Shopify's string form; `\"\"` deletes the metafield.", Required: true},
		}},
	}
}

type keysValidator struct{}

func (keysValidator) Description(context.Context) string {
	return "keys must be namespace.key outside app-owned namespaces"
}
func (v keysValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (keysValidator) ValidateMap(ctx context.Context, req validator.MapRequest, resp *validator.MapResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for k := range req.ConfigValue.Elements() {
		if _, _, err := SplitKey(k); err != nil {
			resp.Diagnostics.AddAttributeError(req.Path.AtMapKey(k), "Invalid metafield key", err.Error())
		}
	}
}
