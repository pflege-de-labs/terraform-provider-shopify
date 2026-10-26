package page

import (
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

type model struct {
	ID             types.String `tfsdk:"id"`
	Handle         types.String `tfsdk:"handle"`
	Title          types.String `tfsdk:"title"`
	Body           types.String `tfsdk:"body"`
	Published      types.Bool   `tfsdk:"published"`
	TemplateSuffix types.String `tfsdk:"template_suffix"`
	AdoptExisting  types.Bool   `tfsdk:"adopt_existing"`
}

// createInput maps the configured attributes; unset ones are not sent, so they keep Shopify's defaults.
func (m *model) createInput() client.PageCreateInput {
	return client.PageCreateInput{
		Handle:         patterns.ConfiguredString(m.Handle),
		Title:          m.Title.ValueString(),
		Body:           patterns.ConfiguredString(m.Body),
		IsPublished:    patterns.ConfiguredBool(m.Published),
		TemplateSuffix: patterns.ConfiguredString(m.TemplateSuffix),
	}
}

// updateInput sends the configured attributes that differ from prior, and reports whether there are any.
func (m *model) updateInput(prior *model) (client.PageUpdateInput, bool) {
	var in client.PageUpdateInput
	in.Handle = changed(m.Handle, prior.Handle)
	in.Title = changed(m.Title, prior.Title)
	in.Body = changed(m.Body, prior.Body)
	in.TemplateSuffix = changed(m.TemplateSuffix, prior.TemplateSuffix)
	if b := patterns.ConfiguredBool(m.Published); b != nil && !m.Published.Equal(prior.Published) {
		in.IsPublished = b
	}
	return in, in.Handle != nil || in.Title != nil || in.Body != nil || in.TemplateSuffix != nil || in.IsPublished != nil
}

func changed(cfg, prior types.String) *string {
	if cfg.Equal(prior) {
		return nil
	}
	return patterns.ConfiguredString(cfg)
}

// setFromAPI copies the live page into m. written is the body Shopify returned at our last write, if known.
func setFromAPI(m *model, p *client.PageFields, written *string) {
	m.ID = types.StringValue(p.Id)
	m.Handle = types.StringValue(p.Handle)
	m.Title = types.StringValue(p.Title)
	m.Body = patterns.KeepOrTake(m.Body, written, p.Body)
	m.Published = types.BoolValue(p.IsPublished)
	m.TemplateSuffix = types.StringPointerValue(p.TemplateSuffix)
}
