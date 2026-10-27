package metaobject

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

type model struct {
	Type        types.String `tfsdk:"type"`
	Handle      types.String `tfsdk:"handle"`
	ID          types.String `tfsdk:"id"`
	DisplayName types.String `tfsdk:"display_name"`
	Fields      types.Map    `tfsdk:"fields"`
	Status      types.String `tfsdk:"status"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

// toModel keeps fields without a value as null entries, so fields["x"] works for every field of the definition.
func toModel(typ, handle string, m *client.MetaobjectFields) (model, diag.Diagnostics) {
	fields := make(map[string]attr.Value, len(m.Fields))
	for _, f := range m.Fields {
		fields[f.Key] = types.StringPointerValue(f.Value)
	}
	fieldsMap, diags := types.MapValue(types.StringType, fields)

	status := types.StringNull()
	if p := m.Capabilities.Publishable; p != nil {
		status = types.StringValue(string(p.Status))
	}
	return model{
		Type:        types.StringValue(typ),
		Handle:      types.StringValue(handle),
		ID:          types.StringValue(m.Id),
		DisplayName: types.StringValue(m.DisplayName),
		Fields:      fieldsMap,
		Status:      status,
		UpdatedAt:   types.StringValue(m.UpdatedAt),
	}, diags
}

// notFoundDetail names the other causes too, because Shopify returns null for an unknown type and for an unreadable one.
func notFoundDetail(typ, handle string) string {
	return fmt.Sprintf("no metaobject %s/%s. The type may not exist, or the app may lack access to it (scope read_metaobjects, see docs/bootstrapping.md).", typ, handle)
}
