// Package shop implements the shopify_shop data source.
package shop

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

// DataSource reads the shop the provider is configured for.
type DataSource struct {
	client *client.Client
}

type model struct {
	ID                           types.String `tfsdk:"id"`
	Name                         types.String `tfsdk:"name"`
	MyshopifyDomain              types.String `tfsdk:"myshopify_domain"`
	PrimaryDomain                types.String `tfsdk:"primary_domain"`
	CurrencyCode                 types.String `tfsdk:"currency_code"`
	PlanName                     types.String `tfsdk:"plan_name"`
	EnabledPresentmentCurrencies types.List   `tfsdk:"enabled_presentment_currencies"`
	Timezone                     types.String `tfsdk:"timezone"`
}

func (d *DataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_shop"
}

func (d *DataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the shop the provider is configured for.",
		Attributes: map[string]schema.Attribute{
			"id":               schema.StringAttribute{MarkdownDescription: "Shop GID.", Computed: true},
			"name":             schema.StringAttribute{MarkdownDescription: "Shop name.", Computed: true},
			"myshopify_domain": schema.StringAttribute{MarkdownDescription: "The `*.myshopify.com` domain.", Computed: true},
			"primary_domain":   schema.StringAttribute{MarkdownDescription: "Host of the primary domain.", Computed: true},
			"currency_code":    schema.StringAttribute{MarkdownDescription: "Base currency.", Computed: true},
			"plan_name":        schema.StringAttribute{MarkdownDescription: "Public display name of the Shopify plan.", Computed: true},
			"enabled_presentment_currencies": schema.ListAttribute{
				MarkdownDescription: "Currencies customers can pay in.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"timezone": schema.StringAttribute{MarkdownDescription: "IANA time zone.", Computed: true},
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
	res, err := client.GetShop(ctx, d.client)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read shop", err.Error())
		return
	}
	s := res.Shop

	currencies := make([]string, len(s.EnabledPresentmentCurrencies))
	for i, c := range s.EnabledPresentmentCurrencies {
		currencies[i] = string(c)
	}
	list, diags := types.ListValueFrom(ctx, types.StringType, currencies)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model{
		ID:                           types.StringValue(s.Id),
		Name:                         types.StringValue(s.Name),
		MyshopifyDomain:              types.StringValue(s.MyshopifyDomain),
		PrimaryDomain:                types.StringValue(s.PrimaryDomain.Host),
		CurrencyCode:                 types.StringValue(string(s.CurrencyCode)),
		PlanName:                     types.StringValue(s.Plan.PublicDisplayName),
		EnabledPresentmentCurrencies: list,
		Timezone:                     types.StringValue(s.IanaTimezone),
	})...)
}
