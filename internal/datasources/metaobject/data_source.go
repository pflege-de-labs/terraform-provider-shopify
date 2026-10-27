// Package metaobject implements the shopify_metaobject data source.
package metaobject

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

var _ datasource.DataSourceWithConfigure = &DataSource{}

func NewDataSource() datasource.DataSource {
	return &DataSource{}
}

// DataSource reads one metaobject entry by type and handle.
type DataSource struct {
	client *client.Client
}

func (d *DataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metaobject"
}

func (d *DataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computedString := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads one metaobject entry by type and handle, for example to reference its `id` from a metafield value or a collection condition. " +
			"Works for entries of Shopify-owned types (`shopify--…`) and merchant-owned types. Fails if no such entry exists. " +
			"Entries are content that merchants edit in the admin; the data source reads the current state on every plan.",
		Attributes: map[string]schema.Attribute{
			"type": schema.StringAttribute{
				MarkdownDescription: "Definition type, e.g. `shopify--material`, exactly as the API returns it.",
				Required:            true,
			},
			"handle": schema.StringAttribute{
				MarkdownDescription: "Entry handle, unique within the type.",
				Required:            true,
			},
			"id":           computedString("Metaobject GID, what references take."),
			"display_name": computedString("The entry's label: the value of the display-name field, or Shopify's fallback."),
			"fields": schema.MapAttribute{
				MarkdownDescription: "Field key to value, as Shopify serialises metafield values: plain text as is, references as GIDs, " +
					"booleans and numbers as strings, list types as a JSON array string (use `jsondecode()`). " +
					"Fields without a value are null entries, so `fields[\"key\"]` works for every field of the definition. " +
					"A field's type comes from `shopify_metaobject_definition`.",
				ElementType: types.StringType,
				Computed:    true,
			},
			"status":     computedString("`ACTIVE` or `DRAFT` when the definition has the publishable capability; null otherwise."),
			"updated_at": computedString("When the entry was last updated (RFC 3339)."),
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
	var cfg model
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	typ, handle := cfg.Type.ValueString(), cfg.Handle.ValueString()

	// TODO(verify): whether `$app:foo` types resolve to the caller's app prefix here.
	res, err := client.GetMetaobjectByHandle(ctx, d.client, typ, handle)
	if client.IsAccessDenied(err) {
		resp.Diagnostics.AddError("Failed to read metaobject",
			fmt.Sprintf("The app lacks the read_metaobjects scope: %s", err))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read metaobject", err.Error())
		return
	}
	if res.MetaobjectByHandle == nil {
		resp.Diagnostics.AddError("Metaobject not found", notFoundDetail(typ, handle))
		return
	}

	state, diags := toModel(typ, handle, &res.MetaobjectByHandle.MetaobjectFields)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
