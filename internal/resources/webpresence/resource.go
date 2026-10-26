// Package webpresence implements the shopify_web_presence resource.
package webpresence

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const what = "web presence"

var (
	_ resource.ResourceWithConfigure      = &Resource{}
	_ resource.ResourceWithImportState    = &Resource{}
	_ resource.ResourceWithIdentity       = &Resource{}
	_ resource.ResourceWithValidateConfig = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one market web presence: a subfolder or a domain with its locales.
type Resource struct {
	client *client.Client
}

type model struct {
	ID               types.String `tfsdk:"id"`
	SubfolderSuffix  types.String `tfsdk:"subfolder_suffix"`
	DomainID         types.String `tfsdk:"domain_id"`
	DefaultLocale    types.String `tfsdk:"default_locale"`
	AlternateLocales types.Set    `tfsdk:"alternate_locales"`
	RootURLs         types.Map    `tfsdk:"root_urls"`
	AdoptExisting    types.Bool   `tfsdk:"adopt_existing"`
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_web_presence"
	// Read re-adopts an object recreated outside Terraform by natural key, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A market web presence: the subfolder (such as `/fr-ch`) or domain a market is served from, with its locales. " +
			"Link it to markets with `web_presence_ids` on `shopify_market`. Set exactly one of `subfolder_suffix` and `domain_id`. " +
			"The locales must already be enabled and published on the shop. " +
			"The shop's primary-domain presence can't be deleted: set `adopt_existing = true` to manage it. " +
			"Import with the subfolder suffix or, for a domain presence, the domain host.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Web presence GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"subfolder_suffix": schema.StringAttribute{
				MarkdownDescription: "Market-specific suffix of the subfolder, e.g. `ch` for `/fr-ch`. Two or more lowercase letters. Can't be combined with `domain_id`.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^[a-z]{2,}$`), "must be two or more lowercase ASCII letters")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(
					// A suffix can't be added to or removed from an existing presence.
					func(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
						resp.RequiresReplace = req.StateValue.IsNull() != req.PlanValue.IsNull()
					},
					"Switching between a subfolder and a domain replaces the web presence.",
					"Switching between a subfolder and a domain replaces the web presence.",
				)},
			},
			"domain_id": schema.StringAttribute{
				MarkdownDescription: "GID of an existing `Domain` to serve from. Domains themselves are not managed. Can't be combined with `subfolder_suffix`.",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"default_locale": schema.StringAttribute{
				MarkdownDescription: "Locale served at the root, e.g. `de`. Must be enabled and published on the shop.",
				Required:            true,
			},
			"alternate_locales": schema.SetAttribute{
				MarkdownDescription: "Further locales, served as language-specific subfolders. Must be enabled and published on the shop. Unset means unmanaged; `[]` clears them.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"root_urls": schema.MapAttribute{
				MarkdownDescription: "Root URL for each locale.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"adopt_existing": patterns.AdoptExistingAttribute(what),
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

func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg model
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.SubfolderSuffix.IsUnknown() || cfg.DomainID.IsUnknown() {
		return
	}
	if err := checkTarget(!cfg.SubfolderSuffix.IsNull(), !cfg.DomainID.IsNull()); err != nil {
		resp.Diagnostics.AddError("Invalid web presence", err.Error())
	}
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, cfg model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	alternates, managed := alternatesOf(ctx, cfg.AlternateLocales, &resp.Diagnostics)

	// Create is idempotent for a taken suffix (it returns the existing presence), so existence is checked here.
	existing, err := r.findByKey(ctx, keyOf(&plan))
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
		return
	}
	if existing != nil {
		if !plan.AdoptExisting.ValueBool() {
			resp.Diagnostics.AddAttributeError(naturalKeyPath(&plan), "Web presence already exists",
				"A web presence with this subfolder suffix or domain exists. Set adopt_existing = true to take it over, or import it.")
			return
		}
		plan.ID = types.StringValue(existing.Id)
		r.update(ctx, &plan, existing.SubfolderSuffix, alternates, managed, &resp.Diagnostics)
	} else {
		in := client.WebPresenceCreateInput{
			DefaultLocale:    plan.DefaultLocale.ValueString(),
			SubfolderSuffix:  plan.SubfolderSuffix.ValueStringPointer(),
			DomainId:         plan.DomainID.ValueStringPointer(),
			AlternateLocales: alternates,
		}
		res, err := client.CreateWebPresence(ctx, r.client, in)
		if err != nil {
			resp.Diagnostics.AddError("Failed to create "+what, err.Error())
			return
		}
		resp.Diagnostics.Append(client.UserErrorDiags("Failed to create "+what, res.WebPresenceCreate.UserErrors)...)
		if resp.Diagnostics.HasError() {
			return
		}
		resp.Diagnostics.Append(setFromAPI(ctx, &plan, &res.WebPresenceCreate.WebPresence.WebPresenceFields)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GID first; fall back to the natural key so a presence recreated in the admin is found again.
	var p *client.WebPresenceFields
	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		var err error
		if p, err = r.getByID(ctx, state.ID.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if p == nil {
		var err error
		if p, err = r.findByKey(ctx, keyOf(&state)); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if p == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	if state.AdoptExisting.IsNull() {
		state.AdoptExisting = types.BoolValue(false)
	}
	resp.Diagnostics.Append(setFromAPI(ctx, &state, p)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state, cfg model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	alternates, managed := alternatesOf(ctx, cfg.AlternateLocales, &resp.Diagnostics)
	r.update(ctx, &plan, state.SubfolderSuffix.ValueStringPointer(), alternates, managed, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.AdoptExisting.ValueBool() {
		patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, what, "adopt_existing is true")
		return
	}

	// TODO(verify): deleting a presence that is still linked to a market.
	res, err := client.DeleteWebPresence(ctx, r.client, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	for _, e := range res.WebPresenceDelete.UserErrors {
		if e.Code != nil && *e.Code == client.MarketUserErrorCodeShopMustHavePrimaryDomainWebPresence {
			patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, what, "the shop must keep a web presence on its primary domain")
			return
		}
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to delete "+what, res.WebPresenceDelete.UserErrors)...)
}

// ImportState takes the suffix or domain host from `terraform import`, or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		p, err := r.findByKey(ctx, parseImportKey(req.ID))
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+what, err.Error())
			return
		}
		if p == nil {
			resp.Diagnostics.AddError("Web presence not found", fmt.Sprintf("No web presence has the subfolder suffix or domain host %q.", req.ID))
			return
		}
		// Read fills in the rest by GID.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), p.Id)...)
		return
	}
	var id patterns.IDIdentity
	resp.Diagnostics.Append(req.Identity.Get(ctx, &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id.ID)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, id)...)
}

// update sends the planned values to plan.ID and writes the result back into plan.
func (r *Resource) update(ctx context.Context, plan *model, stateSuffix *string, alternates []string, managed bool, diags *diag.Diagnostics) {
	in := updateInput(plan.DefaultLocale.ValueString(), plan.SubfolderSuffix.ValueStringPointer(), stateSuffix, alternates, managed)
	res, err := client.UpdateWebPresence(ctx, r.client, plan.ID.ValueString(), in)
	if err != nil {
		diags.AddError("Failed to update "+what, err.Error())
		return
	}
	diags.Append(client.UserErrorDiags("Failed to update "+what, res.WebPresenceUpdate.UserErrors)...)
	if diags.HasError() {
		return
	}
	diags.Append(setFromAPI(ctx, plan, &res.WebPresenceUpdate.WebPresence.WebPresenceFields)...)
}

func (r *Resource) getByID(ctx context.Context, id string) (*client.WebPresenceFields, error) {
	res, err := client.GetWebPresence(ctx, r.client, id)
	if err != nil {
		return nil, err
	}
	var p client.WebPresenceFields
	if err := json.Unmarshal(res.Node, &p); err != nil {
		return nil, err
	}
	// A null node or a GID of another type decodes to an empty struct.
	if p.Id == "" {
		return nil, nil
	}
	return &p, nil
}

func (r *Resource) findByKey(ctx context.Context, k key) (*client.WebPresenceFields, error) {
	var found *client.WebPresenceFields
	err := patterns.Paginate(ctx, 0, listPage(ctx, r.client), func(p client.WebPresenceFields) bool {
		if k.matches(&p) {
			found = &p
			return false
		}
		return true
	})
	return found, err
}

func keyOf(m *model) key {
	return key{suffix: m.SubfolderSuffix.ValueString(), domainID: m.DomainID.ValueString()}
}

func naturalKeyPath(m *model) path.Path {
	if m.DomainID.IsNull() {
		return path.Root("subfolder_suffix")
	}
	return path.Root("domain_id")
}

// alternatesOf returns the configured locales; unset config means the attribute is unmanaged.
func alternatesOf(ctx context.Context, s types.Set, diags *diag.Diagnostics) ([]string, bool) {
	if s.IsNull() || s.IsUnknown() {
		return nil, false
	}
	var out []string
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	return out, true
}

func setFromAPI(ctx context.Context, m *model, p *client.WebPresenceFields) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(p.Id)
	m.SubfolderSuffix = types.StringPointerValue(p.SubfolderSuffix)
	m.DomainID = types.StringNull()
	if p.Domain != nil {
		m.DomainID = types.StringValue(p.Domain.Id)
	}
	m.DefaultLocale = types.StringValue(p.DefaultLocale.Locale)

	alternates := make([]string, len(p.AlternateLocales))
	for i, l := range p.AlternateLocales {
		alternates[i] = l.Locale
	}
	var d diag.Diagnostics
	m.AlternateLocales, d = types.SetValueFrom(ctx, types.StringType, alternates)
	diags.Append(d...)

	urls := make(map[string]string, len(p.RootUrls))
	for _, u := range p.RootUrls {
		urls[u.Locale] = u.Url
	}
	m.RootURLs, d = types.MapValueFrom(ctx, types.StringType, urls)
	diags.Append(d...)
	return diags
}
