package metaobject

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

type model struct {
	ID                types.String `tfsdk:"id"`
	Type              types.String `tfsdk:"type"`
	Handle            types.String `tfsdk:"handle"`
	Fields            types.Map    `tfsdk:"fields"`
	Status            types.String `tfsdk:"status"`
	DisplayName       types.String `tfsdk:"display_name"`
	AdoptExisting     types.Bool   `tfsdk:"adopt_existing"`
	AllowDataDeletion types.Bool   `tfsdk:"allow_data_deletion"`
}

// fields returns the managed values; nil only when state has none yet (after import).
func (m *model) fields(ctx context.Context) (map[string]string, diag.Diagnostics) {
	if m.Fields.IsNull() || m.Fields.IsUnknown() {
		return nil, nil
	}
	out := map[string]string{}
	diags := m.Fields.ElementsAs(ctx, &out, false)
	return out, diags
}

// setFromAPI copies server values into m. Fields are the managed ones (readFields); type stays as configured once known.
func (m *model) setFromAPI(o *client.MetaobjectFields, fields map[string]string) diag.Diagnostics {
	m.ID = types.StringValue(o.Id)
	if m.Type.IsNull() || m.Type.IsUnknown() {
		m.Type = types.StringValue(o.Type)
	}
	m.Handle = types.StringValue(o.Handle)
	m.DisplayName = types.StringValue(o.DisplayName)
	m.Status = statusValue(o)
	var diags diag.Diagnostics
	m.Fields, diags = types.MapValueFrom(context.Background(), types.StringType, fields)
	return diags
}

// statusValue is the publishable status, null for types without that capability.
func statusValue(o *client.MetaobjectFields) types.String {
	if p := o.Capabilities.Publishable; p != nil {
		return types.StringValue(string(p.Status))
	}
	return types.StringNull()
}

// status is the publishable capability to send, or nil when status isn't configured or doesn't change.
func status(cfg, prior types.String) *client.MetaobjectCapabilityDataInput {
	if cfg.IsNull() || cfg.IsUnknown() || cfg.Equal(prior) {
		return nil
	}
	return &client.MetaobjectCapabilityDataInput{
		Publishable: &client.MetaobjectCapabilityDataPublishableInput{Status: client.MetaobjectStatus(cfg.ValueString())},
	}
}
