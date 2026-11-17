package discount

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

// kindModel is what the shared CRUD needs from a method's state model M.
type kindModel[M any] interface {
	*M
	common() *commonModel
	// setFromAPI copies n into the model; shapes the schema can't hold are errors, so import fails and list skips them.
	setFromAPI(ctx context.Context, n *node) (string, diag.Diagnostics)
	// write creates the discount, or updates live; the receiver is the config, so unset attributes are never sent. It returns the GID, or "" on error.
	write(ctx context.Context, c *client.Client, plan *M, live *node, diags *diag.Diagnostics) string
}

// discountResource implements everything the methods share; each method adds its schema, model and write.
type discountResource[M any, P kindModel[M]] struct {
	client *client.Client
	method string
	what   string
}

func (r *discountResource[M, P]) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *discountResource[M, P]) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	modifyPlan(ctx, req, resp)
}

func (r *discountResource[M, P]) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, cfg P = new(M), new(M)
	resp.Diagnostics.Append(req.Plan.Get(ctx, plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var live *node
	if plan.common().AdoptExisting.ValueBool() {
		isCode, key := naturalKey(cfg.common())
		n, err := findByKey(ctx, r.client, isCode, key)
		if err != nil {
			resp.Diagnostics.AddError("Failed to look up "+r.what, err.Error())
			return
		}
		if n != nil {
			if err := checkMethod(n.Typename, r.method); err != nil {
				resp.Diagnostics.AddAttributeError(path.Root("adopt_existing"), "Can't adopt the discount", err.Error())
				return
			}
			live = n
		}
	}
	id := cfg.write(ctx, r.client, plan, live, &resp.Diagnostics)
	if id == "" {
		return
	}
	if !r.finish(ctx, id, cfg, plan, &resp.Diagnostics, resp.Private) {
		// The discount exists, so its ID goes into state: Terraform taints it instead of orphaning it, and Read fills the rest.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("adopt_existing"), plan.common().AdoptExisting)...)
		resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(id)})...)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.common().ID})...)
}

func (r *discountResource[M, P]) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state P = new(M)
	resp.Diagnostics.Append(req.State.Get(ctx, state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cm := state.common()
	// GID first; fall back to the natural key so a discount recreated in the admin is found again.
	var n *node
	var err error
	if !cm.ID.IsNull() && !cm.ID.IsUnknown() {
		if n, err = getNode(ctx, r.client, cm.ID.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+r.what, err.Error())
			return
		}
	}
	importing := cm.Title.IsNull()
	if n == nil && !importing {
		isCode, key := naturalKey(cm)
		if n, err = findByKey(ctx, r.client, isCode, key); err != nil {
			resp.Diagnostics.AddError("Failed to read "+r.what, err.Error())
			return
		}
	}
	if n == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	if err := checkMethod(n.Typename, r.method); err != nil {
		resp.Diagnostics.AddError("Wrong discount type", fmt.Sprintf("Discount %s: %s.", n.ID, err))
		return
	}
	if err := resolveCode(ctx, r.client, n, patterns.ConfiguredString(cm.Code)); err != nil {
		resp.Diagnostics.AddError("Failed to read "+r.what, err.Error())
		return
	}
	// Set on import, where only the ID is known.
	if cm.AdoptExisting.IsNull() {
		cm.AdoptExisting = types.BoolValue(false)
	}
	wasManaged := !cm.Context.IsNull()
	unsupported, d := state.setFromAPI(ctx, n)
	resp.Diagnostics.Append(d...)
	// Warn once: on import, or when the context stops being manageable; ModifyPlan errors if config still sets it.
	if unsupported != "" && (importing || wasManaged) {
		resp.Diagnostics.AddWarning("Context is not managed", unsupportedContextDetail(unsupported))
	}
	resp.Diagnostics.Append(rememberContext(ctx, resp.Private, unsupported)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: cm.ID})...)
}

func (r *discountResource[M, P]) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, cfg, state P = new(M), new(M), new(M)
	resp.Diagnostics.Append(req.Plan.Get(ctx, plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, cfg)...)
	resp.Diagnostics.Append(req.State.Get(ctx, state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Items, context and codes are diffed against the live discount, never against state.
	sc := state.common()
	live, err := getNode(ctx, r.client, sc.ID.ValueString())
	if err == nil && live != nil {
		err = resolveCode(ctx, r.client, live, patterns.ConfiguredString(sc.Code))
	}
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Failed to read "+r.what+" before the update", err.Error())
		return
	case live == nil:
		resp.Diagnostics.AddError("Failed to update "+r.what, fmt.Sprintf("Discount %s no longer exists; refresh to recreate it.", sc.ID.ValueString()))
		return
	}
	if err := checkMethod(live.Typename, r.method); err != nil {
		resp.Diagnostics.AddError("Wrong discount type", fmt.Sprintf("Discount %s: %s.", live.ID, err))
		return
	}
	if cfg.write(ctx, r.client, plan, live, &resp.Diagnostics) == "" || !r.finish(ctx, live.ID, cfg, plan, &resp.Diagnostics, resp.Private) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.common().ID})...)
}

func (r *discountResource[M, P]) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state P = new(M)
	resp.Diagnostics.Append(req.State.Get(ctx, state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.common().AdoptExisting.ValueBool() {
		patterns.WarnRemovedFromStateOnly(&resp.Diagnostics, "discount", "adopt_existing is true")
		return
	}
	resp.Diagnostics.Append(deleteDiscount(ctx, r.client, state.common().ID.ValueString())...)
}

// ImportState takes code/<code> or automatic/<title> from `terraform import`, or the GID from an import block with identity.
func (r *discountResource[M, P]) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		isCode, key, err := parseImportKey(req.ID)
		if err != nil {
			resp.Diagnostics.AddError("Invalid import key", err.Error())
			return
		}
		n, err := findByKey(ctx, r.client, isCode, key)
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+r.what, err.Error())
			return
		}
		if n == nil {
			resp.Diagnostics.AddError("Discount not found", fmt.Sprintf("No discount matches %q.", req.ID))
			return
		}
		// Read fills in the rest by GID; the code lets it keep the one imported by when the discount has several.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), n.ID)...)
		if isCode {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("code"), key)...)
		}
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

// naturalKey is the code of a code discount, or the title of an automatic one.
func naturalKey(m *commonModel) (bool, string) {
	if !m.Code.IsNull() {
		return true, m.Code.ValueString()
	}
	return false, m.Title.ValueString()
}

// finish reads the written discount back into plan; it reports whether that worked.
func (r *discountResource[M, P]) finish(ctx context.Context, id string, cfg, plan P, diags *diag.Diagnostics, private patterns.PrivateSetter) bool {
	n, err := getNode(ctx, r.client, id)
	if err == nil && n != nil {
		err = resolveCode(ctx, r.client, n, patterns.ConfiguredString(cfg.common().Code))
	}
	switch {
	case err != nil:
		diags.AddError("Failed to read the written "+r.what, err.Error())
		return false
	case n == nil:
		diags.AddError("Failed to read the written "+r.what, fmt.Sprintf("Discount %s disappeared after the write.", id))
		return false
	}
	unsupported, d := plan.setFromAPI(ctx, n)
	diags.Append(d...)
	diags.Append(rememberContext(ctx, private, unsupported)...)
	return !diags.HasError()
}

// writeOutcome turns a mutation's result into the discount's GID, or "" after adding the errors to diags.
func writeOutcome(diags *diag.Diagnostics, summary string, err error, errs []client.DiscountUserErrorFields, id *string) string {
	if err != nil {
		diags.AddError(summary, err.Error())
		return ""
	}
	if diags.Append(userErrorDiags(summary, errs)...); diags.HasError() {
		return ""
	}
	if id == nil {
		diags.AddError(summary, "Shopify returned no discount.")
		return ""
	}
	return *id
}

// writeSummary names a write for its error messages.
func writeSummary(what string, live *node) string {
	if live != nil {
		return "Failed to update " + what
	}
	return "Failed to create " + what
}
