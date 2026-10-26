package collection

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

// conditionsEquality makes conditions a semantic JSON attribute.
type conditionsEquality struct{}

func (conditionsEquality) Equal(a, b string) bool { return conditionsEqual(a, b) }

type (
	conditionsType  = patterns.JSONType[conditionsEquality]
	conditionsValue = patterns.JSONValue[conditionsEquality]
)

var (
	conditionsString = patterns.JSONString[conditionsEquality]
	conditionsNull   = patterns.JSONNull[conditionsEquality]
)

type model struct {
	ID              types.String    `tfsdk:"id"`
	Handle          types.String    `tfsdk:"handle"`
	Title           types.String    `tfsdk:"title"`
	DescriptionHTML types.String    `tfsdk:"description_html"`
	SortOrder       types.String    `tfsdk:"sort_order"`
	SEOTitle        types.String    `tfsdk:"seo_title"`
	SEODescription  types.String    `tfsdk:"seo_description"`
	TemplateSuffix  types.String    `tfsdk:"template_suffix"`
	MatchType       types.String    `tfsdk:"match_type"`
	Conditions      conditionsValue `tfsdk:"conditions"`
	ProductIDs      types.Set       `tfsdk:"product_ids"`
	SourceID        types.String    `tfsdk:"source_id"`
	AdoptExisting   types.Bool      `tfsdk:"adopt_existing"`
}

// liveCollection is a collection with its sources decoded and their selections fully paged.
type liveCollection struct {
	*client.CollectionFields
	sources []source
}

// membershipConfigured reports whether config sets any membership attribute.
func (m *model) membershipConfigured() bool {
	return !m.MatchType.IsNull() || !m.Conditions.IsNull() || !m.ProductIDs.IsNull()
}

// desired is the configured membership; unset attributes stay nil, so they are unmanaged.
func (m *model) desired(ctx context.Context) (desired, diag.Diagnostics) {
	var d desired
	var diags diag.Diagnostics
	d.MatchType = patterns.ConfiguredString(m.MatchType)
	if !m.Conditions.IsNull() && !m.Conditions.IsUnknown() {
		d.Conditions = json.RawMessage(m.Conditions.ValueString())
	}
	if !m.ProductIDs.IsNull() && !m.ProductIDs.IsUnknown() {
		d.ProductIDs = []string{}
		diags.Append(m.ProductIDs.ElementsAs(ctx, &d.ProductIDs, false)...)
	}
	return d, diags
}

// createInput maps the configured attributes; unset ones are not sent, so they keep their admin values.
func (m *model) createInput() client.CollectionCreateInput {
	in := client.CollectionCreateInput{
		Title:           m.Title.ValueString(),
		Handle:          patterns.ConfiguredString(m.Handle),
		DescriptionHtml: patterns.ConfiguredString(m.DescriptionHTML),
		TemplateSuffix:  patterns.ConfiguredString(m.TemplateSuffix),
	}
	if s := patterns.ConfiguredString(m.SortOrder); s != nil {
		o := client.CollectionSortOrder(*s)
		in.SortOrder = &o
	}
	if t, d := patterns.ConfiguredString(m.SEOTitle), patterns.ConfiguredString(m.SEODescription); t != nil || d != nil {
		in.Seo = &client.SEOInput{Title: t, Description: d}
	}
	return in
}

// updateInput is createInput for the collection with the given ID.
func (m *model) updateInput(id string) client.CollectionUpdateInput {
	c := m.createInput()
	return client.CollectionUpdateInput{
		Id: &id, Title: &c.Title, Handle: c.Handle, DescriptionHtml: c.DescriptionHtml,
		TemplateSuffix: c.TemplateSuffix, SortOrder: c.SortOrder, Seo: c.Seo,
	}
}

// setFromAPI copies the live collection into m. written is the description Shopify returned at our last write, if known.
// importing fills conditions from live; otherwise they are only refreshed while managed.
// It returns why membership can't be managed, if it can't; the three membership attributes are then null.
func setFromAPI(ctx context.Context, m *model, c *liveCollection, written *string, importing bool) (diag.Diagnostics, error) {
	var diags diag.Diagnostics
	m.ID = types.StringValue(c.Id)
	m.Handle = types.StringValue(c.Handle)
	m.Title = types.StringValue(c.Title)
	m.DescriptionHTML = patterns.KeepOrTake(m.DescriptionHTML, written, c.DescriptionHtml)
	m.SortOrder = types.StringValue(string(c.SortOrder))
	m.SEOTitle = types.StringPointerValue(c.Seo.Title)
	m.SEODescription = types.StringPointerValue(c.Seo.Description)
	m.TemplateSuffix = types.StringPointerValue(c.TemplateSuffix)

	mem, unsupported := membershipOf(c.sources)
	if unsupported != nil {
		m.MatchType, m.Conditions, m.ProductIDs, m.SourceID = types.StringNull(), conditionsNull(), types.SetNull(types.StringType), types.StringNull()
		return diags, unsupported
	}
	if mem == nil {
		mem = &membership{Conditions: []any{}, ProductIDs: []string{}}
		m.SourceID = types.StringNull()
	} else {
		m.SourceID = types.StringValue(mem.SourceID)
	}
	m.MatchType = types.StringPointerValue(mem.MatchType)
	var d diag.Diagnostics
	m.ProductIDs, d = types.SetValueFrom(ctx, types.StringType, mem.ProductIDs)
	diags.Append(d...)
	if !m.Conditions.IsNull() || (importing && len(mem.Conditions) > 0) {
		b, err := json.Marshal(mem.Conditions)
		if err != nil {
			diags.AddError("Failed to encode conditions", err.Error())
		}
		m.Conditions = conditionsString(string(b))
	}
	return diags, nil
}

func unsupportedDetail(err error) string {
	return fmt.Sprintf("Membership of this collection can't be managed yet: %s. match_type, conditions and product_ids are left null and Shopify keeps the membership as it is; leave them out of the configuration.", err)
}
