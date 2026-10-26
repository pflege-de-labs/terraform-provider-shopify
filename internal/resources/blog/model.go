package blog

import (
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

type model struct {
	ID                types.String `tfsdk:"id"`
	Handle            types.String `tfsdk:"handle"`
	Title             types.String `tfsdk:"title"`
	CommentPolicy     types.String `tfsdk:"comment_policy"`
	TemplateSuffix    types.String `tfsdk:"template_suffix"`
	AdoptExisting     types.Bool   `tfsdk:"adopt_existing"`
	AllowDataDeletion types.Bool   `tfsdk:"allow_data_deletion"`
}

func commentPolicy(v types.String) *client.CommentPolicy {
	if s := patterns.ConfiguredString(v); s != nil {
		p := client.CommentPolicy(*s)
		return &p
	}
	return nil
}

// createInput maps the configured attributes; unset ones are not sent, so they keep Shopify's defaults.
func (m *model) createInput() client.BlogCreateInput {
	return client.BlogCreateInput{
		Handle:         patterns.ConfiguredString(m.Handle),
		Title:          m.Title.ValueString(),
		CommentPolicy:  commentPolicy(m.CommentPolicy),
		TemplateSuffix: patterns.ConfiguredString(m.TemplateSuffix),
	}
}

// updateInput sends the configured attributes that differ from prior, and reports whether there are any.
func (m *model) updateInput(prior *model) (client.BlogUpdateInput, bool) {
	var in client.BlogUpdateInput
	in.Handle = changed(m.Handle, prior.Handle)
	in.Title = changed(m.Title, prior.Title)
	in.TemplateSuffix = changed(m.TemplateSuffix, prior.TemplateSuffix)
	if changed(m.CommentPolicy, prior.CommentPolicy) != nil {
		in.CommentPolicy = commentPolicy(m.CommentPolicy)
	}
	return in, in.Handle != nil || in.Title != nil || in.CommentPolicy != nil || in.TemplateSuffix != nil
}

func changed(cfg, prior types.String) *string {
	if cfg.Equal(prior) {
		return nil
	}
	return patterns.ConfiguredString(cfg)
}

func setFromAPI(m *model, b *client.BlogFields) {
	m.ID = types.StringValue(b.Id)
	m.Handle = types.StringValue(b.Handle)
	m.Title = types.StringValue(b.Title)
	m.CommentPolicy = types.StringValue(string(b.CommentPolicy))
	m.TemplateSuffix = types.StringPointerValue(b.TemplateSuffix)
}
