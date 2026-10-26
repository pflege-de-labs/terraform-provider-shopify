package product

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

type model struct {
	ID                types.String `tfsdk:"id"`
	Handle            types.String `tfsdk:"handle"`
	Title             types.String `tfsdk:"title"`
	Status            types.String `tfsdk:"status"`
	DescriptionHTML   types.String `tfsdk:"description_html"`
	Vendor            types.String `tfsdk:"vendor"`
	ProductType       types.String `tfsdk:"product_type"`
	CategoryID        types.String `tfsdk:"category_id"`
	Tags              types.Set    `tfsdk:"tags"`
	SEOTitle          types.String `tfsdk:"seo_title"`
	SEODescription    types.String `tfsdk:"seo_description"`
	TemplateSuffix    types.String `tfsdk:"template_suffix"`
	GiftCard          types.Bool   `tfsdk:"gift_card"`
	AdoptExisting     types.Bool   `tfsdk:"adopt_existing"`
	AllowDataDeletion types.Bool   `tfsdk:"allow_data_deletion"`
}

// buildInput is the one place that maps config to productSet; later steps add their keys here, and unset ones stay out.
func buildInput(ctx context.Context, cfg *model) (client.ProductSetInput, diag.Diagnostics) {
	var diags diag.Diagnostics
	in := client.ProductSetInput{
		Handle:          patterns.ConfiguredString(cfg.Handle),
		Title:           patterns.ConfiguredString(cfg.Title),
		DescriptionHtml: patterns.ConfiguredString(cfg.DescriptionHTML),
		Vendor:          patterns.ConfiguredString(cfg.Vendor),
		ProductType:     patterns.ConfiguredString(cfg.ProductType),
		Category:        patterns.ConfiguredString(cfg.CategoryID),
		TemplateSuffix:  patterns.ConfiguredString(cfg.TemplateSuffix),
		GiftCard:        patterns.ConfiguredBool(cfg.GiftCard),
	}
	if s := patterns.ConfiguredString(cfg.Status); s != nil {
		st := client.ProductStatus(*s)
		in.Status = &st
	}
	if t, d := patterns.ConfiguredString(cfg.SEOTitle), patterns.ConfiguredString(cfg.SEODescription); t != nil || d != nil {
		in.Seo = &client.SEOInput{Title: t, Description: d}
	}
	if !cfg.Tags.IsNull() && !cfg.Tags.IsUnknown() {
		tags := []string{}
		diags.Append(cfg.Tags.ElementsAs(ctx, &tags, false)...)
		slices.Sort(tags)
		raw, err := json.Marshal(tags)
		if err != nil {
			diags.AddError("Failed to encode tags", err.Error())
		}
		in.Tags = raw
	}
	return in, diags
}

// setFromAPI copies the live product into m. written is the description Shopify returned at our last write, if known.
func setFromAPI(ctx context.Context, m *model, p *client.ProductFields, written *string) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(p.Id)
	m.Handle = types.StringValue(p.Handle)
	m.Title = types.StringValue(p.Title)
	m.Status = types.StringValue(string(p.Status))
	m.DescriptionHTML = keepOrTake(m.DescriptionHTML, written, p.DescriptionHtml)
	m.Vendor = types.StringValue(p.Vendor)
	m.ProductType = types.StringValue(p.ProductType)
	m.CategoryID = types.StringNull()
	if p.Category != nil {
		m.CategoryID = types.StringValue(p.Category.Id)
	}
	var d diag.Diagnostics
	m.Tags, d = types.SetValueFrom(ctx, types.StringType, p.Tags)
	diags.Append(d...)
	m.SEOTitle = types.StringPointerValue(p.Seo.Title)
	m.SEODescription = types.StringPointerValue(p.Seo.Description)
	m.TemplateSuffix = types.StringPointerValue(p.TemplateSuffix)
	m.GiftCard = types.BoolValue(p.IsGiftCard)
	return diags
}
