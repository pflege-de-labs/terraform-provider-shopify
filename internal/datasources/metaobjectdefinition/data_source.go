// Package metaobjectdefinition implements the shopify_metaobject_definition data source.
package metaobjectdefinition

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	resource "github.com/pflege-de-labs/terraform-provider-shopify/internal/resources/metaobjectdefinition"
)

var _ datasource.DataSourceWithConfigure = &DataSource{}

func NewDataSource() datasource.DataSource {
	return &DataSource{}
}

// DataSource reads one metaobject definition by type.
type DataSource struct {
	client *client.Client
}

func (d *DataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metaobject_definition"
}

func (d *DataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computedString := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc, Computed: true}
	}
	computedBool := func(desc string) schema.BoolAttribute {
		return schema.BoolAttribute{MarkdownDescription: desc, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a metaobject definition and its fields by type. " +
			"Use it for definitions owned by Shopify (`shopify--…`) or by an app, which can't be imported into `shopify_metaobject_definition`; " +
			"it works for merchant-owned definitions too. Fails if no such definition exists.",
		Attributes: map[string]schema.Attribute{
			"id":               computedString("Metaobject definition GID."),
			"type":             schema.StringAttribute{MarkdownDescription: "Definition type, e.g. `shopify--material`, exactly as the API returns it.", Required: true},
			"name":             computedString("Human-readable name."),
			"description":      computedString("Description. Empty when none is set."),
			"display_name_key": computedString("Key of the field used as display name for entries. Empty when none is set."),
			"access": schema.SingleNestedAttribute{
				MarkdownDescription: "API access settings.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"admin":            computedString("Admin API access. Null when Shopify reports a value that `shopify_metaobject_definition` can't set, e.g. the default `PUBLIC_READ_WRITE`."),
					"storefront":       computedString("Storefront API access."),
					"customer_account": computedString("Customer account API access."),
				},
			},
			"capabilities": schema.SingleNestedAttribute{
				MarkdownDescription: "Capabilities of entries of this type.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"publishable":  computedBool("Entries have a draft/active status."),
					"translatable": computedBool("Entries can be translated."),
					"renderable":   computedBool("Entries can be rendered as web pages (SEO data)."),
				},
			},
			// An attribute, not a block: data sources can't compute blocks. Use `dynamic "field"` to feed a resource.
			"field": schema.ListNestedAttribute{
				MarkdownDescription: "Field definitions, in display order.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"key":         computedString("Field key."),
						"type":        computedString("Field type, e.g. `single_line_text_field`."),
						"name":        computedString("Human-readable name."),
						"description": computedString("Description. Null when none is set."),
						"required":    computedBool("Whether entries must set this field."),
						"validations": schema.MapAttribute{
							MarkdownDescription: "Validation name to value. Null when none is set.",
							ElementType:         types.StringType,
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *DataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	d.client = c
}

func (d *DataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var typ string
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("type"), &typ)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := client.GetMetaobjectDefinitionByType(ctx, d.client, typ)
	if client.IsAccessDenied(err) {
		resp.Diagnostics.AddError("Failed to read metaobject definition",
			fmt.Sprintf("The app lacks the read_metaobject_definitions scope: %s", err))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read metaobject definition", err.Error())
		return
	}
	if res.MetaobjectDefinitionByType == nil {
		resp.Diagnostics.AddError("Metaobject definition not found", notFoundDetail(typ))
		return
	}

	var def resource.Definition
	resp.Diagnostics.Append(resource.SetFromAPI(ctx, &def, &res.MetaobjectDefinitionByType.MetaobjectDefinitionFields)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &def)...)
}

// notFoundDetail also names the scope case, because Shopify may return null for a definition the app can't read.
func notFoundDetail(typ string) string {
	return fmt.Sprintf("no metaobject definition of type %s. If it exists, the app may lack the read_metaobject_definitions scope; see docs/bootstrapping.md.", typ)
}
