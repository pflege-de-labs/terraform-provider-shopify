package customeraccountpage

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

// DataSource reads one customer account page by handle.
type DataSource struct {
	client *client.Client
}

func (d *DataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_customer_account_page"
}

func (d *DataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a customer account page by handle, for example to reference its GID in a menu item. " +
			"Customer account pages are created by Shopify and are not managed by this provider. " +
			"The GID is specific to each shop. " +
			"Fails if no page has the handle, and lists the handles that exist. " +
			"Needs the `read_customers` scope.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{MarkdownDescription: "Customer account page GID.", Computed: true},
			"handle": schema.StringAttribute{
				MarkdownDescription: "Handle of the page, e.g. `orders`. Matched exactly, including case.",
				Required:            true,
			},
			"title": schema.StringAttribute{MarkdownDescription: "Title of the page.", Computed: true},
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
	var handle string
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("handle"), &handle)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var pages []page
	err := patterns.Paginate(ctx, 0, d.listPage(ctx), func(p page) bool {
		pages = append(pages, p)
		return true
	})
	// TODO(verify): scope assumed from Shopify docs; the dev store grants all scopes, so a denial was never observed.
	if client.IsAccessDenied(err) {
		resp.Diagnostics.AddError("Failed to read customer account pages",
			fmt.Sprintf("The app lacks the read_customers scope: %s", err))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read customer account pages", err.Error())
		return
	}

	p, err := findByHandle(pages, handle)
	if err != nil {
		resp.Diagnostics.AddError("Customer account page not found", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toModel(p))...)
}

func (d *DataSource) listPage(ctx context.Context) func(after *string) ([]page, *string, error) {
	return func(after *string) ([]page, *string, error) {
		res, err := client.ListCustomerAccountPages(ctx, d.client, after)
		if err != nil {
			return nil, nil, err
		}
		pages, next := fromConnection(res.CustomerAccountPages)
		return pages, next, nil
	}
}
