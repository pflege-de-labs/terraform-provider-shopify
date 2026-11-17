package product

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

var _ datasource.DataSourceWithConfigure = &DataSource{}

func NewDataSource() datasource.DataSource {
	return &DataSource{}
}

// DataSource reads the product that holds the variant with a given SKU.
type DataSource struct {
	client *client.Client
}

func (d *DataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_product"
}

func (d *DataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a product by the SKU of one of its variants, for example to reference products " +
			"that Terraform doesn't manage in a collection's `product_ids`. Product GIDs differ per shop, the SKU doesn't, " +
			"so the same configuration finds the matching product in each shop. " +
			"Fails if no variant has the SKU, and if several variants share it (listing them), even within one product. " +
			"Shopify's search index lags edits by a few seconds, so a SKU changed just before the read may not be found yet. " +
			"Needs the `read_products` scope.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{MarkdownDescription: "Product GID.", Computed: true},
			"sku": schema.StringAttribute{
				MarkdownDescription: "SKU of a variant of the product. Matched exactly, including case.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"variant_id": schema.StringAttribute{MarkdownDescription: "GID of the variant with the SKU.", Computed: true},
			"handle":     schema.StringAttribute{MarkdownDescription: "Handle of the product.", Computed: true},
			"title":      schema.StringAttribute{MarkdownDescription: "Title of the product.", Computed: true},
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
	var sku string
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("sku"), &sku)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var vs []variant
	err := patterns.Paginate(ctx, 0, d.searchPage(ctx, skuQuery(sku)), func(v variant) bool {
		vs = append(vs, v)
		return true
	})
	if client.IsAccessDenied(err) {
		resp.Diagnostics.AddError("Failed to read product variants",
			fmt.Sprintf("The app lacks the read_products scope: %s", err))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read product variants", err.Error())
		return
	}

	v, err := matchSKU(vs, sku)
	if err != nil {
		resp.Diagnostics.AddError("Product not found or ambiguous", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toModel(v))...)
}

func (d *DataSource) searchPage(ctx context.Context, query string) func(after *string) ([]variant, *string, error) {
	return func(after *string) ([]variant, *string, error) {
		res, err := client.FindProductVariantsBySku(ctx, d.client, query, after)
		if err != nil {
			return nil, nil, err
		}
		conn := res.ProductVariants
		vs := make([]variant, len(conn.Nodes))
		for i, n := range conn.Nodes {
			vs[i] = variant{ID: n.Id, ProductID: n.Product.Id, Handle: n.Product.Handle, Title: n.Product.Title}
			if n.Sku != nil {
				vs[i].SKU = *n.Sku
			}
		}
		return vs, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}
