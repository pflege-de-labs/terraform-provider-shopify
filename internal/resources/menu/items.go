package menu

import (
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// itemFields are the attributes shared by all three item levels.
type itemFields struct {
	Title      types.String `tfsdk:"title"`
	Type       types.String `tfsdk:"type"`
	ResourceID types.String `tfsdk:"resource_id"`
	URL        types.String `tfsdk:"url"`
	Tags       types.List   `tfsdk:"tags"`
}

// The schema has no recursion, so each of the three levels has its own type.
type level1 struct {
	itemFields
	Items []level2 `tfsdk:"item"`
}

type level2 struct {
	itemFields
	Items []level3 `tfsdk:"item"`
}

type level3 struct {
	itemFields
}

// resourceTypes need resource_id; menuCreate fails with "<type> not found" without one.
var resourceTypes = []string{"COLLECTION", "PRODUCT", "PAGE", "BLOG", "ARTICLE", "METAOBJECT", "SHOP_POLICY", "CUSTOMER_ACCOUNT_PAGE"}

// checkItem enforces url for HTTP only (Shopify computes it for other types) and resource_id where one is needed.
func checkItem(typ string, hasResourceID, hasURL bool) error {
	switch {
	case typ == "HTTP" && (!hasURL || hasResourceID):
		return fmt.Errorf("HTTP items need url and no resource_id")
	case typ != "HTTP" && hasURL:
		return fmt.Errorf("url is only allowed for HTTP items; Shopify derives it for %s", typ)
	case slices.Contains(resourceTypes, typ) && !hasResourceID:
		return fmt.Errorf("%s items need resource_id", typ)
	}
	return nil
}

func fieldsFromAPI(f client.MenuItemFields) itemFields {
	out := itemFields{
		Title:      types.StringValue(f.Title),
		Type:       types.StringValue(string(f.Type)),
		ResourceID: types.StringPointerValue(f.ResourceId),
		URL:        types.StringNull(),
		Tags:       types.ListNull(types.StringType),
	}
	if f.Type == client.MenuItemTypeHttp {
		out.URL = types.StringPointerValue(f.Url)
	}
	if len(f.Tags) > 0 {
		tags := make([]attr.Value, len(f.Tags))
		for i, t := range f.Tags {
			tags[i] = types.StringValue(t)
		}
		out.Tags = types.ListValueMust(types.StringType, tags)
	}
	return out
}

func level1FromAPI(items []client.MenuFieldsItemsMenuItem) []level1 {
	out := make([]level1, len(items))
	for i, it := range items {
		out[i] = level1{itemFields: fieldsFromAPI(it.MenuItemFields), Items: make([]level2, len(it.Items))}
		for j, c := range it.Items {
			out[i].Items[j] = level2{itemFields: fieldsFromAPI(c.MenuItemFields), Items: make([]level3, len(c.Items))}
			for k, g := range c.Items {
				out[i].Items[j].Items[k] = level3{itemFields: fieldsFromAPI(g.MenuItemFields)}
			}
		}
	}
	return out
}

func (f itemFields) tags() []string {
	var out []string
	for _, t := range f.Tags.Elements() {
		if s, ok := t.(types.String); ok {
			out = append(out, s.ValueString())
		}
	}
	return out
}

func (f itemFields) createInput(children []client.MenuItemCreateInput) client.MenuItemCreateInput {
	return client.MenuItemCreateInput{
		Title: f.Title.ValueString(), Type: client.MenuItemType(f.Type.ValueString()),
		ResourceId: f.ResourceID.ValueStringPointer(), Url: f.URL.ValueStringPointer(), Tags: f.tags(), Items: children,
	}
}

func (f itemFields) updateInput(children []client.MenuItemUpdateInput) client.MenuItemUpdateInput {
	return client.MenuItemUpdateInput{
		Title: f.Title.ValueString(), Type: client.MenuItemType(f.Type.ValueString()),
		ResourceId: f.ResourceID.ValueStringPointer(), Url: f.URL.ValueStringPointer(), Tags: f.tags(), Items: children,
	}
}

func createInputs(items []level1) []client.MenuItemCreateInput {
	out := []client.MenuItemCreateInput{}
	for _, a := range items {
		var l2 []client.MenuItemCreateInput
		for _, b := range a.Items {
			var l3 []client.MenuItemCreateInput
			for _, c := range b.Items {
				l3 = append(l3, c.createInput(nil))
			}
			l2 = append(l2, b.createInput(l3))
		}
		out = append(out, a.createInput(l2))
	}
	return out
}

// updateInputs sends the whole tree without item IDs, so Shopify replaces all items.
func updateInputs(items []level1) []client.MenuItemUpdateInput {
	out := []client.MenuItemUpdateInput{}
	for _, a := range items {
		var l2 []client.MenuItemUpdateInput
		for _, b := range a.Items {
			var l3 []client.MenuItemUpdateInput
			for _, c := range b.Items {
				l3 = append(l3, c.updateInput(nil))
			}
			l2 = append(l2, b.updateInput(l3))
		}
		out = append(out, a.updateInput(l2))
	}
	return out
}
