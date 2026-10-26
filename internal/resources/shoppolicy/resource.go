// Package shoppolicy implements the shopify_shop_policy resource.
package shoppolicy

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const what = "shop policy"

var (
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
	_ resource.ResourceWithIdentity    = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages the text of one shop policy type.
type Resource struct {
	client *client.Client
}

type model struct {
	ID            types.String `tfsdk:"id"`
	Type          types.String `tfsdk:"type"`
	Body          types.String `tfsdk:"body"`
	Title         types.String `tfsdk:"title"`
	URL           types.String `tfsdk:"url"`
	AdoptExisting types.Bool   `tfsdk:"adopt_existing"`
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_shop_policy"
	// Read re-adopts a policy row recreated outside Terraform by its type, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

// TODO(verify): whether shopPolicyUpdate on an auto-managed PRIVACY_POLICY is rejected, flips the flag, or is regenerated over.
func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The text of one shop policy, such as the terms of service or the legal notice. " +
			"**A poor fit for Terraform unless the texts are maintained in the repository:** policies are legal texts that people outside the infrastructure team often edit in the admin, and Shopify generates the privacy policy. " +
			"The API can't delete policies, so destroy only removes the resource from state and leaves the text in place. " +
			"A type that already has text needs `adopt_existing = true`, so a text written by hand isn't overwritten silently. " +
			"Shopify generates the `PRIVACY_POLICY` text while it is auto-managed; turn that off first (`shopify_privacy_settings`), and expect Shopify to reject or overwrite a write until then. " +
			"Import with the policy type, such as `TERMS_OF_SERVICE`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Shop policy GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Policy type. Changing it manages a different policy and leaves the old one as it is.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.OneOf(policyTypeNames()...)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"body": schema.StringAttribute{
				MarkdownDescription: "The policy text as HTML, stored and returned as written. Can't be empty, because an empty text means no policy.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "Title Shopify gives the policy, translated into the shop's language.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"url": schema.StringAttribute{
				MarkdownDescription: "Public URL of the policy page.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
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

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ps, err := listAll(ctx, r.client)
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
		return
	}
	existing := findPolicy(ps, "", client.ShopPolicyType(plan.Type.ValueString()))
	body := plan.Body.ValueString()
	var p *client.ShopPolicyFields
	switch planCreate(existing, body, plan.AdoptExisting.ValueBool()) {
	case conflict:
		resp.Diagnostics.AddAttributeError(path.Root("type"), "Shop policy already has text",
			"This policy type already has text. Set adopt_existing = true to overwrite it with body, or import it.")
		return
	case keepBody:
		p = existing
	case writeBody:
		if p = r.write(ctx, &plan, &resp.Diagnostics); p == nil {
			return
		}
	}
	setFromAPI(&plan, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ps, err := listAll(ctx, r.client)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read "+what, err.Error())
		return
	}
	// GID first; fall back to the type so a policy row recreated outside Terraform is found again.
	p := findPolicy(ps, state.ID.ValueString(), client.ShopPolicyType(state.Type.ValueString()))
	if p == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	// Set on import, where only the ID is known.
	if state.AdoptExisting.IsNull() {
		state.AdoptExisting = types.BoolValue(false)
	}
	setFromAPI(&state, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Only body can change in place; adopt_existing is a provider-side flag.
	p := r.write(ctx, &plan, &resp.Diagnostics)
	if p == nil {
		return
	}
	setFromAPI(&plan, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

// Delete never writes an empty body: that would blank the policy without removing the row.
func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, what, "the Admin API can't delete shop policies")
}

// ImportState takes the policy type from `terraform import` or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		typ, err := parseImportKey(req.ID)
		if err != nil {
			resp.Diagnostics.AddError("Invalid import key", err.Error())
			return
		}
		ps, err := listAll(ctx, r.client)
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+what, err.Error())
			return
		}
		p := findPolicy(ps, "", typ)
		if p == nil {
			resp.Diagnostics.AddError("Shop policy not found", fmt.Sprintf("The policy type %s has no text on this shop.", typ))
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

// write returns nil after adding diagnostics.
func (r *Resource) write(ctx context.Context, m *model, diags *diag.Diagnostics) *client.ShopPolicyFields {
	res, err := client.UpdateShopPolicy(ctx, r.client, client.ShopPolicyInput{
		Type: client.ShopPolicyType(m.Type.ValueString()),
		Body: m.Body.ValueString(),
	})
	if err != nil {
		diags.AddError("Failed to write "+what, err.Error())
		return nil
	}
	diags.Append(client.UserErrorDiags("Failed to write "+what, res.ShopPolicyUpdate.UserErrors)...)
	if diags.HasError() {
		return nil
	}
	return &res.ShopPolicyUpdate.ShopPolicy.ShopPolicyFields
}

func listAll(ctx context.Context, c *client.Client) ([]client.ShopPolicyFields, error) {
	res, err := client.ListShopPolicies(ctx, c)
	if err != nil {
		return nil, err
	}
	out := make([]client.ShopPolicyFields, len(res.Shop.ShopPolicies))
	for i, p := range res.Shop.ShopPolicies {
		out[i] = p.ShopPolicyFields
	}
	return out, nil
}

func setFromAPI(m *model, p *client.ShopPolicyFields) {
	m.ID = types.StringValue(p.Id)
	m.Type = types.StringValue(string(p.Type))
	m.Body = types.StringValue(p.Body)
	m.Title = types.StringValue(p.Title)
	m.URL = types.StringValue(p.Url)
}
