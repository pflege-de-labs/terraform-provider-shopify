package fulfillmentservice

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

var _ datasource.DataSourceWithConfigure = &DataSource{}

func NewDataSource() datasource.DataSource {
	return &DataSource{}
}

// DataSource reads one installed fulfillment service by handle.
type DataSource struct {
	client *client.Client
}

func (d *DataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_fulfillment_service"
}

func (d *DataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc, Computed: true}
	}
	computedBool := func(desc string) schema.BoolAttribute {
		return schema.BoolAttribute{MarkdownDescription: desc, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads an installed fulfillment service by handle. " +
			"Use it for services owned by Shopify (`manual`) or by other apps, which this provider doesn't manage, " +
			"for example to reference the service's location. " +
			"Fails if no service has the handle, and lists the handles that exist. " +
			"Needs the `read_fulfillments` scope.",
		Attributes: map[string]schema.Attribute{
			"id": computed("Fulfillment service GID."),
			"handle": schema.StringAttribute{
				MarkdownDescription: "Handle of the service, e.g. `manual`. Matched exactly.",
				Required:            true,
			},
			"service_name":             computed("Name of the service as merchants see it."),
			"type":                     computed("`MANUAL`, `THIRD_PARTY` or `GIFT_CARD`."),
			"location_id":              computed("GID of the location associated with the service. Null if there is none."),
			"callback_url":             computed("URL Shopify calls for fulfillment requests, tracking numbers and stock. Null if the service has none, e.g. `manual`."),
			"inventory_management":     computedBool("Whether the service tracks inventory and reports it to Shopify."),
			"tracking_support":         computedBool("Whether the service supplies tracking numbers."),
			"requires_shipping_method": computedBool("Whether products must be physically shipped."),
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

	res, err := client.GetFulfillmentServices(ctx, d.client)
	if client.IsAccessDenied(err) {
		resp.Diagnostics.AddError("Failed to read fulfillment services",
			fmt.Sprintf("The app lacks the read_fulfillments scope: %s", err))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read fulfillment services", err.Error())
		return
	}

	svc, err := findByHandle(res.Shop.FulfillmentServices, handle)
	if err != nil {
		resp.Diagnostics.AddError("Fulfillment service not found", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toModel(svc))...)
}
