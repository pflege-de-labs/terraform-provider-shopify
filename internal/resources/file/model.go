package file

import (
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

type model struct {
	ID                types.String `tfsdk:"id"`
	ContentType       types.String `tfsdk:"content_type"`
	Filename          types.String `tfsdk:"filename"`
	Source            types.String `tfsdk:"source"`
	SourceHash        types.String `tfsdk:"source_hash"`
	Alt               types.String `tfsdk:"alt"`
	URL               types.String `tfsdk:"url"`
	AdoptExisting     types.Bool   `tfsdk:"adopt_existing"`
	AllowDataDeletion types.Bool   `tfsdk:"allow_data_deletion"`
	Timeouts          types.Object `tfsdk:"timeouts"`
}

// createInput maps the configured attributes; unset ones are not sent.
func (m *model) createInput() client.FileCreateInput {
	ct := client.FileContentType(m.ContentType.ValueString())
	in := client.FileCreateInput{ContentType: &ct, Alt: patterns.ConfiguredString(m.Alt), OriginalSource: m.Source.ValueString()}
	// RAISE_ERROR keeps filenames unique so the key holds; external videos have no filename.
	// TODO(verify): whether duplicateResolutionMode: RAISE_ERROR is rejected for external videos; it is not sent.
	if ct != client.FileContentTypeExternalVideo {
		mode := client.FileCreateInputDuplicateResolutionModeRaiseError
		in.Filename, in.DuplicateResolutionMode = patterns.ConfiguredString(m.Filename), &mode
	}
	return in
}

// updateInput sends the configured attributes that differ from prior, and reports whether there are any.
func (m *model) updateInput(id string, prior *model) (client.FileUpdateInput, bool) {
	in := client.FileUpdateInput{Id: id}
	if differs(prior.Alt, m.Alt) {
		in.Alt = patterns.ConfiguredString(m.Alt)
	}
	if differs(prior.Filename, m.Filename) {
		in.Filename = patterns.ConfiguredString(m.Filename)
	}
	if contentChanged(prior, m) {
		in.OriginalSource = patterns.ConfiguredString(m.Source)
	}
	return in, in.Alt != nil || in.Filename != nil || in.OriginalSource != nil
}

// setFromAPI keeps source and source_hash: Shopify never returns them for hosted files.
func setFromAPI(m *model, f *apiFile) {
	ct, _ := f.contentType()
	m.ID = types.StringValue(f.Id)
	m.ContentType = types.StringValue(ct)
	m.URL = types.StringPointerValue(f.publicURL())
	if name, ok := f.filename(); ok {
		m.Filename = types.StringValue(name)
	} else if ct == typeExternalVideo {
		m.Filename = types.StringNull()
	}
	// A cleared alt may read back as null; the configured "" must survive.
	if f.Alt == nil && !m.Alt.IsNull() && !m.Alt.IsUnknown() && m.Alt.ValueString() == "" {
		m.Alt = types.StringValue("")
	} else {
		m.Alt = types.StringPointerValue(f.Alt)
	}
	if ct == typeExternalVideo {
		m.Source = types.StringValue(f.OriginUrl)
	}
}
