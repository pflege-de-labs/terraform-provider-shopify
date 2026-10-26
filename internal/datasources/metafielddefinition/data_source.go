// Package metafielddefinition implements the shopify_metafield_definition data source.
package metafielddefinition

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	resource "github.com/pflege-de-labs/terraform-provider-shopify/internal/resources/metafielddefinition"
)

var _ datasource.DataSourceWithConfigure = &DataSource{}

func NewDataSource() datasource.DataSource {
	return &DataSource{}
}

// DataSource reads one metafield definition by its natural key.
type DataSource struct {
	client *client.Client
}

func (d *DataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metafield_definition"
}

func (d *DataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computedString := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a metafield definition by owner type, namespace and key. " +
			"Use it for definitions owned by Shopify (namespace `shopify`) or by an app, which can't be imported " +
			"into `shopify_metafield_definition`; it works for merchant-owned definitions too. " +
			"Fails if no such definition exists. Shopify also returns nothing when the app lacks the read scope for the owner type.",
		Attributes: map[string]schema.Attribute{
			"id": computedString("Metafield definition GID."),
			"owner_type": schema.StringAttribute{
				MarkdownDescription: "Owner resource type, e.g. `PRODUCT`, `ORDER` or `SHOP`.",
				Required:            true,
			},
			"namespace":   schema.StringAttribute{MarkdownDescription: "Namespace, e.g. `shopify`.", Required: true},
			"key":         schema.StringAttribute{MarkdownDescription: "Key within the namespace.", Required: true},
			"type":        computedString("Metafield type, e.g. `number_integer`."),
			"name":        computedString("Human-readable name."),
			"description": computedString("Description. Empty when none is set."),
			"validations": schema.MapAttribute{
				MarkdownDescription: "Validation name to value. JSON-shaped values are JSON strings.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"access": schema.SingleNestedAttribute{
				MarkdownDescription: "API access settings.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"admin":            computedString("Admin API access. Null when Shopify reports a value that `shopify_metafield_definition` can't set, e.g. the default `PUBLIC_READ_WRITE`."),
					"storefront":       computedString("Storefront API access."),
					"customer_account": computedString("Customer account API access."),
				},
			},
			"pin": schema.BoolAttribute{MarkdownDescription: "Whether the definition is pinned in the admin.", Computed: true},
			"use_as_collection_condition": schema.BoolAttribute{
				MarkdownDescription: "Whether the definition can be used in smart collection conditions.",
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
	var ownerType, namespace, key string
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("owner_type"), &ownerType)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("namespace"), &namespace)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("key"), &key)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := client.GetMetafieldDefinition(ctx, d.client, client.MetafieldDefinitionIdentifierInput{
		OwnerType: client.MetafieldOwnerType(ownerType),
		Namespace: &namespace,
		Key:       key,
	})
	if client.IsAccessDenied(err) {
		resp.Diagnostics.AddError("Failed to read metafield definition",
			fmt.Sprintf("The app lacks the read scope for owner type %s: %s", ownerType, err))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read metafield definition", err.Error())
		return
	}
	if res.MetafieldDefinition == nil {
		resp.Diagnostics.AddError("Metafield definition not found", notFoundDetail(ownerType, namespace, key))
		return
	}

	var def resource.Definition
	resp.Diagnostics.Append(resource.SetFromAPI(ctx, &def, &res.MetafieldDefinition.MetafieldDefinitionFields)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &def)...)
}

// notFoundDetail also names the scope case, because Shopify returns null for an owner type the app can't read.
func notFoundDetail(ownerType, namespace, key string) string {
	return fmt.Sprintf("no metafield definition %s/%s/%s. If it exists, the app may lack the read scope for owner type %s; see docs/bootstrapping.md.",
		ownerType, namespace, key, ownerType)
}
