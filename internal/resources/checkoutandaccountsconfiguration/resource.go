// Package checkoutandaccountsconfiguration implements the shopify_checkout_and_accounts_configuration resource.
package checkoutandaccountsconfiguration

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Khan/genqlient/graphql"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const (
	what = "checkout and accounts configuration"
	// publishedKey in private state: the name was unset, so Read re-adopts the published configuration when the GID is gone.
	publishedKey = "follows_published"
)

var (
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
	_ resource.ResourceWithIdentity    = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages the branding of one checkout and accounts configuration.
type Resource struct {
	client *client.Client
}

type model struct {
	ID          types.String  `tfsdk:"id"`
	Name        types.String  `tfsdk:"name"`
	IsPublished types.Bool    `tfsdk:"is_published"`
	Branding    brandingValue `tfsdk:"branding"`
}

func (m *model) setFromAPI(c *client.CheckoutAndAccountsConfigurationFields) {
	m.ID = types.StringValue(c.Id)
	m.Name = types.StringValue(c.Name)
	m.IsPublished = types.BoolValue(c.IsPublished)
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_checkout_and_accounts_configuration"
	// Read re-adopts by name or as the published configuration when the GID is gone.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The branding of a checkout and accounts configuration: colours, corner radii, typography and component styles of checkout and customer accounts. " +
			"**A poor fit for Terraform:** branding is usually edited in the admin's checkout editor, and every edit there to a managed value shows as drift. " +
			"Configurations are created and published in the checkout editor; the API can't create, delete or publish them, so this resource adopts an existing one " +
			"and destroy only removes it from state, leaving the branding as it is. " +
			"If another configuration is published later, the resource keeps following its own one (`is_published` turns `false`); import again to follow the new one. " +
			"Overrides are not managed. Import with `published` or the configuration name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Configuration GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the configuration to manage, as shown in the checkout editor. Unset means the published configuration. " +
					"Names come from the shop name, so leaving it unset is what works on every store. " +
					"Changing it manages a different configuration; the old one keeps its branding.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"is_published": schema.BoolAttribute{
				MarkdownDescription: "Whether the configuration is the one live on checkout.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"branding": schema.StringAttribute{
				CustomType: brandingType{},
				MarkdownDescription: "Branding as a JSON object in the API's camelCase, following " +
					"[`CheckoutAndAccountsConfigurationBrandingInput`](https://shopify.dev/docs/api/admin-graphql/2026-10/input-objects/CheckoutAndAccountsConfigurationBrandingInput); write it with `jsonencode()`. " +
					"Only the keys you write are managed: a missing key is left alone, and `null` resets that value (or a whole subtree) to Shopify's default. " +
					"Removing a key from the config stops managing it but keeps its value in Shopify; set it to `null` and apply first to reset it. " +
					"Unset means branding is not managed. Colours compare case-insensitively and numbers numerically, and edits in the admin to keys you didn't write never show as drift. " +
					"**Write-only, never checked for drift:** images (`components.favicon`, `components.header.logo.image`, " +
					"`surfaces.{checkout,customerAccounts,signIn}.components.header.logo.image`, `surfaces.checkout.components.header.cartLink.image`, " +
					"`surfaces.checkout.components.orderSummary.backgroundImage`, `surfaces.signIn.components.main.backgroundImage`), " +
					"fonts (`designTokens.typography.primary`, `designTokens.typography.secondary`) and `components.merchandiseThumbnail.aspectRatio`. " +
					"Import and `terraform query` leave them out. Shopify checks keys and values; an invalid value rejects the whole update.",
				Optional:   true,
				Validators: []validator.String{jsonObjectValidator{}},
			},
		},
	}
}

func (r *Resource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	r.client = c
}

type privateSetter interface {
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

// rememberSelection records whether the configuration was chosen as the published one.
func rememberSelection(ctx context.Context, p privateSetter, name types.String) diag.Diagnostics {
	if name.IsNull() {
		return p.SetKey(ctx, publishedKey, []byte("true"))
	}
	return p.SetKey(ctx, publishedKey, nil)
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, config model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key := configKey{name: config.Name.ValueString()}
	c, err := findByKey(ctx, r.client, key)
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
		return
	}
	if c == nil {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Checkout and accounts configuration not found",
			fmt.Sprintf("Found no %s on this shop. Configurations are created in the admin's checkout editor.", key))
		return
	}
	if !plan.Branding.IsNull() {
		if c = updateBranding(ctx, r.client, c.Id, plan.Branding.ValueString(), &resp.Diagnostics); c == nil {
			return
		}
	}
	plan.setFromAPI(c)
	resp.Diagnostics.Append(rememberSelection(ctx, resp.Private, config.Name)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := getByID(ctx, r.client, state.ID.ValueString())
	if err == nil && c == nil {
		// GID gone: fall back to the natural key the resource was created with.
		key := configKey{name: state.Name.ValueString()}
		follows, d := req.Private.GetKey(ctx, publishedKey)
		resp.Diagnostics.Append(d...)
		if follows != nil {
			key = configKey{}
		}
		c, err = findByKey(ctx, r.client, key)
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read "+what, err.Error())
		return
	}
	if c == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.setFromAPI(c)
	if !state.Branding.IsNull() {
		b, err := projectBranding(state.Branding.ValueString(), c.Branding)
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("branding"), "Failed to compare branding", err.Error())
			return
		}
		state.Branding = brandingString(b)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, config model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Branding is the only attribute that changes in place; null means no longer managed, so nothing is sent.
	if !plan.Branding.IsNull() {
		c := updateBranding(ctx, r.client, plan.ID.ValueString(), plan.Branding.ValueString(), &resp.Diagnostics)
		if c == nil {
			return
		}
		plan.setFromAPI(c)
	}
	resp.Diagnostics.Append(rememberSelection(ctx, resp.Private, config.Name)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

// Delete never resets branding: removing the resource must not change checkout.
func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, what, "the Admin API can't delete configurations, and destroy leaves the branding as it is")
}

// ImportState sets the whole live branding, which Read leaves unmanaged; the key is `published`, a name, or the identity GID.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var c *client.CheckoutAndAccountsConfigurationFields
	var err error
	if req.ID != "" {
		key := parseImportKey(req.ID)
		if c, err = findByKey(ctx, r.client, key); err == nil && c == nil {
			err = fmt.Errorf("found no %s on this shop", key)
		}
		if key.name == "" {
			resp.Diagnostics.Append(resp.Private.SetKey(ctx, publishedKey, []byte("true"))...)
		}
	} else {
		var id patterns.IDIdentity
		resp.Diagnostics.Append(req.Identity.Get(ctx, &id)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if c, err = getByID(ctx, r.client, id.ID.ValueString()); err == nil && c == nil {
			err = fmt.Errorf("found no configuration with ID %s", id.ID.ValueString())
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to import "+what, err.Error())
		return
	}
	m, err := importModel(c)
	if err != nil {
		resp.Diagnostics.AddError("Failed to import "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: m.ID})...)
}

// importModel is the configuration as import and list emit it: the whole live branding, or none.
func importModel(c *client.CheckoutAndAccountsConfigurationFields) (model, error) {
	var m model
	m.setFromAPI(c)
	b, err := liveBranding(c.Branding)
	if err != nil {
		return m, err
	}
	m.Branding = brandingNull()
	if b != "" {
		m.Branding = brandingString(b)
	}
	return m, nil
}

// updateBranding sends the branding JSON as written, nulls included; it returns nil after adding diagnostics.
func updateBranding(ctx context.Context, c graphql.Client, id, branding string, diags *diag.Diagnostics) *client.CheckoutAndAccountsConfigurationFields {
	res, err := client.UpdateCheckoutAndAccountsConfiguration(ctx, c, id, client.CheckoutAndAccountsConfigurationInput{Branding: json.RawMessage(branding)})
	if err != nil {
		diags.AddAttributeError(path.Root("branding"), "Failed to write "+what, err.Error())
		return nil
	}
	p := res.CheckoutAndAccountsConfigurationUpdate
	if p == nil {
		diags.AddError("Failed to write "+what, "The API returned no result.")
		return nil
	}
	diags.Append(brandingErrorDiags("Failed to write "+what, p.UserErrors)...)
	if diags.HasError() {
		return nil
	}
	if p.Configuration == nil {
		diags.AddError("Failed to write "+what, "The API returned no configuration.")
		return nil
	}
	return &p.Configuration.CheckoutAndAccountsConfigurationFields
}

// getByID returns nil when the GID is gone.
func getByID(ctx context.Context, c graphql.Client, id string) (*client.CheckoutAndAccountsConfigurationFields, error) {
	res, err := client.GetCheckoutAndAccountsConfiguration(ctx, c, id)
	if err != nil || res.CheckoutAndAccountsConfiguration == nil {
		return nil, err
	}
	return &res.CheckoutAndAccountsConfiguration.CheckoutAndAccountsConfigurationFields, nil
}

// findByKey returns nil when nothing matches; names have no API filter, so they page through every configuration.
func findByKey(ctx context.Context, c graphql.Client, key configKey) (*client.CheckoutAndAccountsConfigurationFields, error) {
	var query *string
	if key.name == "" {
		q := "is_published:true"
		query = &q
	}
	var all []client.CheckoutAndAccountsConfigurationFields
	err := patterns.Paginate(ctx, 0, listPage(ctx, c, query), func(f client.CheckoutAndAccountsConfigurationFields) bool {
		all = append(all, f)
		return true
	})
	if err != nil {
		return nil, err
	}
	return matchConfiguration(all, key)
}

func listPage(ctx context.Context, c graphql.Client, query *string) func(after *string) ([]client.CheckoutAndAccountsConfigurationFields, *string, error) {
	return func(after *string) ([]client.CheckoutAndAccountsConfigurationFields, *string, error) {
		res, err := client.ListCheckoutAndAccountsConfigurations(ctx, c, query, after)
		if err != nil {
			return nil, nil, err
		}
		conn := res.CheckoutAndAccountsConfigurations
		if conn == nil {
			return nil, nil, nil
		}
		out := make([]client.CheckoutAndAccountsConfigurationFields, len(conn.Nodes))
		for i, n := range conn.Nodes {
			out[i] = n.CheckoutAndAccountsConfigurationFields
		}
		return out, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}

// jsonObjectValidator only checks the shape; Shopify validates keys and values.
type jsonObjectValidator struct{}

func (jsonObjectValidator) Description(context.Context) string { return "value must be a JSON object" }

func (v jsonObjectValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (jsonObjectValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := brandingObjectError(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid branding", err.Error())
	}
}
