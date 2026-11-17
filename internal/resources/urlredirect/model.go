package urlredirect

import (
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

type model struct {
	ID            types.String `tfsdk:"id"`
	Path          types.String `tfsdk:"path"`
	Target        types.String `tfsdk:"target"`
	AdoptExisting types.Bool   `tfsdk:"adopt_existing"`
}

func (m *model) input() client.UrlRedirectInput {
	p, t := m.Path.ValueString(), m.Target.ValueString()
	return client.UrlRedirectInput{Path: &p, Target: &t}
}

func setFromAPI(m *model, r *client.UrlRedirectFields) {
	m.ID = types.StringValue(r.Id)
	m.Path = types.StringValue(r.Path)
	m.Target = types.StringValue(r.Target)
}
