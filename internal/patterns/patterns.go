// Package patterns holds behaviour shared by resources: natural-key import and the data-loss guard (Pattern B).
package patterns

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// SplitImportKey splits a natural import key such as "PRODUCT/custom/material" into the parts named by format.
func SplitImportKey(id, format string) ([]string, error) {
	want := strings.Count(format, "/") + 1
	parts := strings.Split(id, "/")
	if len(parts) != want {
		return nil, fmt.Errorf("import key %q must have the form %s", id, format)
	}
	for _, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("import key %q must have the form %s, with no empty parts", id, format)
		}
	}
	return parts, nil
}

// AllowDataDeletionAttribute is the Pattern B opt-in flag; it never causes an API call.
func AllowDataDeletionAttribute(what string) schema.BoolAttribute {
	return schema.BoolAttribute{
		MarkdownDescription: fmt.Sprintf("Allow destroying or replacing this %s, which deletes the data stored in it. Defaults to `false`. "+
			"Must already be `true` in state when the delete runs, so set it in a separate apply first.", what),
		Optional: true,
		Computed: true,
		Default:  booldefault.StaticBool(false),
	}
}

// GuardDataDeletion errors unless allow (from prior state) is true.
func GuardDataDeletion(allow types.Bool, what string) diag.Diagnostics {
	var diags diag.Diagnostics
	if !allow.ValueBool() {
		diags.AddError(
			fmt.Sprintf("Deleting this %s would delete its data", what),
			fmt.Sprintf("Set allow_data_deletion = true and apply that change first, then destroy or replace the %s.", what),
		)
	}
	return diags
}

// WarnOnDataDeletingReplace adds a plan-time warning when one of the replace-forcing attributes changes.
// Attribute-level RequiresReplace is not visible in resource ModifyPlan, so the attributes are compared here.
func WarnOnDataDeletingReplace(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse, what string, replaceAttrs ...string) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var changed []string
	for _, a := range replaceAttrs {
		var before, after types.String
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root(a), &before)...)
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root(a), &after)...)
		if !before.Equal(after) {
			changed = append(changed, a)
		}
	}
	if len(changed) == 0 {
		return
	}
	var allow types.Bool
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("allow_data_deletion"), &allow)...)
	detail := fmt.Sprintf("Changing %s replaces the %s and deletes its data.", strings.Join(changed, ", "), what)
	if !allow.ValueBool() {
		detail += " Apply will fail because allow_data_deletion is not true in state."
	}
	resp.Diagnostics.AddWarning(fmt.Sprintf("This change replaces the %s", what), detail)
}

// ConfiguredString and ConfiguredBool return nil for unset config values: unset means unmanaged, so nothing is sent.
func ConfiguredString(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueStringPointer()
}

func ConfiguredBool(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueBoolPointer()
}

// AdoptExistingAttribute is the Pattern A opt-in: Create takes over an existing object instead of failing.
func AdoptExistingAttribute(what string) schema.BoolAttribute {
	return schema.BoolAttribute{
		MarkdownDescription: fmt.Sprintf("Take over an existing %s with the same natural key instead of failing. "+
			"Destroy then only removes it from state. Defaults to `false`.", what),
		Optional: true,
		Computed: true,
		Default:  booldefault.StaticBool(false),
	}
}

// WarnRemovedFromStateOnly is the Pattern A delete: the object stays in Shopify.
func WarnRemovedFromStateOnly(diags *diag.Diagnostics, what, reason string) {
	diags.AddWarning(
		fmt.Sprintf("The %s was only removed from Terraform state", what),
		fmt.Sprintf("It still exists in Shopify because %s. Remove or reset it in the admin if needed.", reason),
	)
}

// DiffIDs returns the sorted IDs to add and to remove to get from live to want; nil when there are none.
func DiffIDs(live, want []string) (add, remove []string) {
	for _, id := range want {
		if !slices.Contains(live, id) {
			add = append(add, id)
		}
	}
	for _, id := range live {
		if !slices.Contains(want, id) {
			remove = append(remove, id)
		}
	}
	slices.Sort(add)
	slices.Sort(remove)
	return add, remove
}
