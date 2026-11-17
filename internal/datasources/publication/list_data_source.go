package publication

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

var _ datasource.DataSourceWithConfigure = &ListDataSource{}

func NewListDataSource() datasource.DataSource {
	return &ListDataSource{}
}

// ListDataSource reads all named sales channel publications.
type ListDataSource struct {
	client *client.Client
}

type listModel struct {
	Publications []model `tfsdk:"publications"`
}

func (d *ListDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_publications"
}

func (d *ListDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists all sales channel publications of the store, for example to map the `publication_ids` " +
			"of exported products and collections to names. Same elements as `shopify_publication`; " +
			"sorted by name, then ID; publications without a name (market catalogs) are left out. " +
			"Publications belong to sales channel apps and are not managed by this provider. " +
			"Needs the `read_publications` scope.",
		Attributes: map[string]schema.Attribute{
			"publications": schema.ListNestedAttribute{
				MarkdownDescription: "The publications; an empty list if there are none.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":           schema.StringAttribute{MarkdownDescription: "Publication GID.", Computed: true},
						"name":         schema.StringAttribute{MarkdownDescription: "Name of the publication, e.g. `Online Store`.", Computed: true},
						"auto_publish": schema.BoolAttribute{MarkdownDescription: "Whether new products are published to this publication automatically.", Computed: true},
						"catalog_id":   schema.StringAttribute{MarkdownDescription: "GID of the catalog the publication belongs to. Null if there is none.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *ListDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ListDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	pubs, err := listAll(ctx, d.client)
	if !publicationsOK(&resp.Diagnostics, err) {
		return
	}
	sorted := namedSorted(pubs)
	out := listModel{Publications: make([]model, len(sorted))}
	for i := range sorted {
		out.Publications[i] = toModel(&sorted[i])
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, out)...)
}
