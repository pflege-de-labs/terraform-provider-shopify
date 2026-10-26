// Copyright IBM Corp. 2021, 2026
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/datasources/shop"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/resources/checkoutandaccountsconfiguration"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/resources/deliveryprofile"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/resources/location"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/resources/market"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/resources/menu"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/resources/metafielddefinition"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/resources/metaobjectdefinition"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/resources/privacysettings"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/resources/shoplocale"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/resources/shoppolicy"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/resources/webpresence"
)

// Ensure ShopifyProvider satisfies various provider interfaces.
var _ provider.Provider = &ShopifyProvider{}
var _ provider.ProviderWithListResources = &ShopifyProvider{}

// defaultMaxRetries applies when max_retries is unset.
const defaultMaxRetries = 5

// ShopifyProvider defines the provider implementation.
type ShopifyProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// ShopifyProviderModel describes the provider data model.
type ShopifyProviderModel struct {
	StoreDomain  types.String `tfsdk:"store_domain"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	AccessToken  types.String `tfsdk:"access_token"`
	APIVersion   types.String `tfsdk:"api_version"`
	MaxRetries   types.Int64  `tfsdk:"max_retries"`
	Concurrency  types.Int64  `tfsdk:"concurrency"`
}

func (p *ShopifyProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "shopify"
	resp.Version = p.version
}

func (p *ShopifyProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	// Only ConflictsWith here: an exactly-one check on config would reject credentials set via env vars.
	conflictsWithToken := []validator.String{stringvalidator.ConflictsWith(path.MatchRoot("access_token"))}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages Shopify store configuration through the Admin GraphQL API. " +
			"Authenticate with `client_id` and `client_secret` (client credentials grant, preferred) or a static `access_token`.",
		Attributes: map[string]schema.Attribute{
			"store_domain": schema.StringAttribute{
				MarkdownDescription: "Store domain, `<handle>.myshopify.com` (not a custom domain). Env: `SHOPIFY_STORE_DOMAIN`.",
				Optional:            true,
			},
			"client_id": schema.StringAttribute{
				MarkdownDescription: "Client ID of the Dev Dashboard app. Env: `SHOPIFY_CLIENT_ID`.",
				Optional:            true,
				Sensitive:           true,
				Validators:          conflictsWithToken,
			},
			"client_secret": schema.StringAttribute{
				MarkdownDescription: "Client secret of the Dev Dashboard app. Env: `SHOPIFY_CLIENT_SECRET`.",
				Optional:            true,
				Sensitive:           true,
				Validators:          conflictsWithToken,
			},
			"access_token": schema.StringAttribute{
				MarkdownDescription: "Static Admin API access token, instead of client credentials. Env: `SHOPIFY_ACCESS_TOKEN`.",
				Optional:            true,
				Sensitive:           true,
			},
			"api_version": schema.StringAttribute{
				MarkdownDescription: "Admin API version. Defaults to `" + client.DefaultAPIVersion + "`, the version the provider is built against.",
				Optional:            true,
			},
			"max_retries": schema.Int64Attribute{
				MarkdownDescription: "Retries for throttled and 429/5xx responses. Defaults to `5`.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.AtLeast(0)},
			},
			"concurrency": schema.Int64Attribute{
				MarkdownDescription: "Maximum in-flight API requests, regardless of Terraform parallelism. Defaults to `2`.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.AtLeast(1)},
			},
		},
	}
}

func (p *ShopifyProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data ShopifyProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	cfg, diags := clientConfig(data, os.Getenv)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	c, err := client.New(cfg)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Shopify provider configuration", err.Error()+" (directly or via SHOPIFY_* environment variables)")
		return
	}
	resp.DataSourceData = c
	resp.ResourceData = c
	resp.ListResourceData = c
}

// clientConfig merges provider config with env var fallbacks; config wins.
func clientConfig(data ShopifyProviderModel, getenv func(string) string) (client.Config, diag.Diagnostics) {
	var diags diag.Diagnostics
	str := func(v types.String, attr, env string) string {
		if v.IsUnknown() {
			diags.AddAttributeError(path.Root(attr), "Unknown provider value", attr+" must be known at plan time.")
		}
		if v.IsNull() {
			return getenv(env)
		}
		return v.ValueString()
	}
	cfg := client.Config{
		StoreDomain:  str(data.StoreDomain, "store_domain", "SHOPIFY_STORE_DOMAIN"),
		ClientID:     str(data.ClientID, "client_id", "SHOPIFY_CLIENT_ID"),
		ClientSecret: str(data.ClientSecret, "client_secret", "SHOPIFY_CLIENT_SECRET"),
		AccessToken:  str(data.AccessToken, "access_token", "SHOPIFY_ACCESS_TOKEN"),
		APIVersion:   data.APIVersion.ValueString(),
		MaxRetries:   defaultMaxRetries,
		Concurrency:  int(data.Concurrency.ValueInt64()),
	}
	if !data.MaxRetries.IsNull() {
		cfg.MaxRetries = int(data.MaxRetries.ValueInt64())
	}
	// Custom domains answer the token request with an HTML page, which surfaces as an opaque decode error.
	if d := cfg.StoreDomain; d != "" && (!strings.HasSuffix(d, ".myshopify.com") || strings.Contains(d, "/")) {
		diags.AddAttributeError(path.Root("store_domain"), "Invalid store domain",
			fmt.Sprintf("%q must be the store's bare <handle>.myshopify.com domain (admin: Settings → Domains), not a custom domain or URL. Set via store_domain or SHOPIFY_STORE_DOMAIN.", d))
	}
	return cfg, diags
}

func (p *ShopifyProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		metafielddefinition.NewResource,
		metaobjectdefinition.NewResource,
		menu.NewResource,
		location.NewResource,
		market.NewResource,
		deliveryprofile.NewResource,
		shoplocale.NewResource,
		privacysettings.NewPrivacySettings,
		privacysettings.NewConsentPolicy,
		shoppolicy.NewResource,
		webpresence.NewResource,
		checkoutandaccountsconfiguration.NewResource,
	}
}

func (p *ShopifyProvider) ListResources(ctx context.Context) []func() list.ListResource {
	return []func() list.ListResource{
		metafielddefinition.NewListResource,
		metaobjectdefinition.NewListResource,
		menu.NewListResource,
		location.NewListResource,
		deliveryprofile.NewListResource,
		market.NewListResource,
		shoplocale.NewListResource,
		privacysettings.NewPrivacySettingsList,
		privacysettings.NewConsentPolicyList,
		shoppolicy.NewListResource,
		webpresence.NewListResource,
		checkoutandaccountsconfiguration.NewListResource,
	}
}

func (p *ShopifyProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		shop.NewDataSource,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &ShopifyProvider{
			version: version,
		}
	}
}
