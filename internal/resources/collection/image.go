package collection

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

// writtenImageKey is the private state key for the image URL Shopify returned at our last write.
const writtenImageKey = "image_url_written"

type imageModel struct {
	Src     types.String `tfsdk:"src"`
	AltText types.String `tfsdk:"alt_text"`
	URL     types.String `tfsdk:"url"`
}

var imageAttrTypes = map[string]attr.Type{"src": types.StringType, "alt_text": types.StringType, "url": types.StringType}

// imageOf decodes the image attribute; nil when it is null or unknown.
func imageOf(ctx context.Context, o types.Object) (*imageModel, diag.Diagnostics) {
	if o.IsNull() || o.IsUnknown() {
		return nil, nil
	}
	var m imageModel
	diags := o.As(ctx, &m, basetypes.ObjectAsOptions{})
	return &m, diags
}

func imageObject(m *imageModel) (types.Object, diag.Diagnostics) {
	if m == nil {
		return types.ObjectNull(imageAttrTypes), nil
	}
	return types.ObjectValue(imageAttrTypes, map[string]attr.Value{"src": m.Src, "alt_text": m.AltText, "url": m.URL})
}

// readImage maps the live image to state, keeping the configured src while Shopify still holds the image of our last write.
func readImage(prior *imageModel, live *client.CollectionFieldsImage, written *string) *imageModel {
	if live == nil {
		return nil
	}
	src := types.StringNull()
	if prior != nil {
		src = prior.Src
	}
	return &imageModel{
		Src:     patterns.KeepOrTake(src, written, live.Url),
		AltText: types.StringPointerValue(live.AltText),
		URL:     types.StringValue(live.Url),
	}
}

// imageInput is what to send for the configured image given the prior one; nil when nothing changes or it is unmanaged.
// An unset alt_text is unmanaged, so it is never sent.
func imageInput(cfg, prior *imageModel) *client.ImageInput {
	if cfg == nil {
		return nil
	}
	alt := patterns.ConfiguredString(cfg.AltText)
	if prior == nil || !cfg.Src.Equal(prior.Src) {
		return &client.ImageInput{Src: cfg.Src.ValueStringPointer(), AltText: alt}
	}
	if alt != nil && !cfg.AltText.Equal(prior.AltText) {
		return &client.ImageInput{AltText: alt}
	}
	return nil
}
