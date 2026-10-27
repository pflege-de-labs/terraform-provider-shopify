package customersegment

import (
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

type model struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Query         types.String `tfsdk:"query"`
	AdoptExisting types.Bool   `tfsdk:"adopt_existing"`
}

// updateArgs returns the configured arguments that differ from prior, and reports whether there are any.
func (m *model) updateArgs(prior *model) (name, query *string, has bool) {
	name = changed(m.Name, prior.Name)
	query = changed(m.Query, prior.Query)
	return name, query, name != nil || query != nil
}

func changed(cfg, prior types.String) *string {
	if cfg.Equal(prior) {
		return nil
	}
	return patterns.ConfiguredString(cfg)
}

func setFromAPI(m *model, s *client.SegmentFields) {
	m.ID = types.StringValue(s.Id)
	m.Name = types.StringValue(s.Name)
	m.Query = types.StringValue(s.Query)
}
