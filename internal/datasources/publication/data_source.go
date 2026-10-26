package publication

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

var _ datasource.DataSourceWithConfigure = &DataSource{}

func NewDataSource() datasource.DataSource {
	return &DataSource{}
}

// DataSource reads one sales channel publication by name.
type DataSource struct {
	client *client.Client
}

func (d *DataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_publication"
}

func (d *DataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a sales channel publication by name, for example to reference its ID. " +
			"Publications belong to sales channel apps and are not managed by this provider. " +
			"Publications have no handle, so the name is the key; it differs per store for headless channels. " +
			"Fails if no publication has the name, and lists the names that exist. " +
			"Fails if several publications share the name. " +
			"Needs the `read_publications` scope.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{MarkdownDescription: "Publication GID.", Computed: true},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the publication, e.g. `Online Store`. Matched exactly, including case.",
				Required:            true,
			},
			"auto_publish": schema.BoolAttribute{
				MarkdownDescription: "Whether new products are published to this publication automatically.",
				Computed:            true,
			},
			"catalog_id": schema.StringAttribute{
				MarkdownDescription: "GID of the catalog the publication belongs to (an app catalog for sales channels). Null if there is none.",
				Computed:            true,
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
	var name string
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("name"), &name)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var pubs []publication
	err := patterns.Paginate(ctx, 0, d.listPage(ctx), func(p publication) bool {
		pubs = append(pubs, p)
		return true
	})
	if client.IsAccessDenied(err) {
		resp.Diagnostics.AddError("Failed to read publications",
			fmt.Sprintf("The app lacks the read_publications scope: %s", err))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read publications", err.Error())
		return
	}

	p, err := findByName(pubs, name)
	if err != nil {
		resp.Diagnostics.AddError("Publication not found or ambiguous", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toModel(p))...)
}

func (d *DataSource) listPage(ctx context.Context) func(after *string) ([]publication, *string, error) {
	return func(after *string) ([]publication, *string, error) {
		res, err := client.ListPublications(ctx, d.client, after)
		if err != nil {
			return nil, nil, err
		}
		conn := res.Publications
		return conn.Nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}
