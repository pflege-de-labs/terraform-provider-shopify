// Package customersegment implements the shopify_customer_segment resource.
package customersegment

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

const what = "customer segment"

var (
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
	_ resource.ResourceWithIdentity    = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one saved customer query.
type Resource struct {
	client *client.Client
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_customer_segment"
	// Read re-adopts a segment recreated outside Terraform by its name, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A customer segment: a saved customer query in ShopifyQL segment syntax, which discounts and marketing target. " +
			"**A poor fit for Terraform in general:** marketing builds and changes segments in the admin, so manage only those that discounts or automations depend on. " +
			"Segments hold a query, not customers, so recreating one restores the same members. " +
			"**Deleting a segment expires every discount that targets only that segment** and resets its context to all buyers, and may break marketing automations and campaigns that use it. " +
			"Reference segments as `shopify_customer_segment.<name>.id` from discounts, so a change to the segment updates the discount first. " +
			"Shopify gives a taken name a suffix (`Email subscribers (2)`) instead of failing; the provider checks for that and fails instead. " +
			"The five segments every store starts with (such as `Email subscribers`) need `adopt_existing = true`; destroy then leaves them in place. " +
			"Import with the name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Segment GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The segment name, unique per shop, without leading or trailing whitespace (Shopify trims it). Changing it renames the segment in place. Shopify gives a taken name a suffix instead of failing; the provider fails instead.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.RegexMatches(nameRe, "must not start or end with whitespace"),
				},
			},
			"query": schema.StringAttribute{
				MarkdownDescription: "The segment definition in [segment query syntax](https://shopify.dev/docs/api/shopifyql/segment-query-language-reference), such as `email_subscription_status = 'SUBSCRIBED'`. " +
					"Shopify validates it on write and stores it verbatim, so a changed spelling shows as a change.",
				Required:   true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
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

	// Shopify suffixes a taken name instead of failing, so existence is checked here and again after create.
	existing, err := r.getByName(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
		return
	}
	if existing != nil && !plan.AdoptExisting.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Customer segment already exists",
			"A customer segment with this name exists. Set adopt_existing = true to take it over, or import it.")
		return
	}
	var s *client.SegmentFields
	if existing != nil {
		// Only what differs is sent, so a segment that already matches stays untouched.
		var live model
		setFromAPI(&live, existing)
		_, query, has := plan.updateArgs(&live)
		if s = existing; has {
			s = r.update(ctx, existing.Id, nil, query, &resp.Diagnostics)
		}
	} else if s = r.create(ctx, plan.Name.ValueString(), plan.Query.ValueString(), &resp.Diagnostics); s != nil {
		if err := patterns.KeyDrift(what, "name", plan.Name.ValueString(), s.Name); err != nil {
			r.removeDuplicate(ctx, s, plan.Name.ValueString(), &resp.Diagnostics)
			return
		}
	}
	if s == nil {
		return
	}
	setFromAPI(&plan, s)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GID first; fall back to the name so a segment recreated in the admin is found again.
	var s *client.SegmentFields
	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		var err error
		if s, err = r.getByID(ctx, state.ID.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if s == nil && !state.Name.IsNull() {
		var err error
		if s, err = r.getByName(ctx, state.Name.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if s == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	// Set on import, where only the ID is known.
	if state.AdoptExisting.IsNull() {
		state.AdoptExisting = types.BoolValue(false)
	}
	setFromAPI(&state, s)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name, query, has := plan.updateArgs(&state)
	if !has {
		// Only adopt_existing changed.
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	// segmentUpdate suffixes a taken name instead of failing, so a taken one is refused up front.
	if name != nil {
		other, err := r.getByName(ctx, *name)
		if err != nil {
			resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
			return
		}
		if other != nil && other.Id != plan.ID.ValueString() {
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Name already taken",
				fmt.Sprintf("Another customer segment is named %q.", *name))
			return
		}
	}
	s := r.update(ctx, plan.ID.ValueString(), name, query, &resp.Diagnostics)
	if s == nil {
		return
	}
	// State takes the name Shopify returned, so the next plan shows the drift.
	setFromAPI(&plan, s)
	if name != nil {
		if err := patterns.KeyDrift(what, "name", *name, s.Name); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Segment name was changed by Shopify", err.Error())
		}
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

	del, err := client.DeleteSegment(ctx, r.client, state.ID.ValueString())
	// segmentDelete reports a missing segment as a userError (checked); a NOT_FOUND error, as segment(id:) gives, is tolerated too.
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	if del.SegmentDelete == nil {
		resp.Diagnostics.AddError("Failed to delete "+what, "Shopify returned no result.")
		return
	}
	// Already gone is what delete wants.
	if goneOnDelete(del.SegmentDelete.UserErrors) {
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiagsWith("Failed to delete "+what, del.SegmentDelete.UserErrors, errorPath)...)
}

// ImportState takes the name from `terraform import`, or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		s, err := r.getByName(ctx, req.ID)
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+what, err.Error())
			return
		}
		if s == nil {
			resp.Diagnostics.AddError("Customer segment not found", fmt.Sprintf("No customer segment is named %q.", req.ID))
			return
		}
		// Read fills in the rest by GID.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), s.Id)...)
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

func (r *Resource) create(ctx context.Context, name, query string, diags *diag.Diagnostics) *client.SegmentFields {
	res, err := client.CreateSegment(ctx, r.client, name, query)
	if err != nil {
		diags.AddError("Failed to create "+what, err.Error())
		return nil
	}
	if res.SegmentCreate == nil {
		diags.AddError("Failed to create "+what, "Shopify returned no result.")
		return nil
	}
	diags.Append(client.UserErrorDiagsWith("Failed to create "+what, res.SegmentCreate.UserErrors, errorPath)...)
	if diags.HasError() {
		return nil
	}
	if res.SegmentCreate.Segment == nil {
		diags.AddError("Failed to create "+what, "Shopify returned no segment.")
		return nil
	}
	return &res.SegmentCreate.Segment.SegmentFields
}

func (r *Resource) update(ctx context.Context, id string, name, query *string, diags *diag.Diagnostics) *client.SegmentFields {
	res, err := client.UpdateSegment(ctx, r.client, id, name, query)
	if err != nil {
		diags.AddError("Failed to update "+what, err.Error())
		return nil
	}
	if res.SegmentUpdate == nil {
		diags.AddError("Failed to update "+what, "Shopify returned no result.")
		return nil
	}
	diags.Append(client.UserErrorDiagsWith("Failed to update "+what, res.SegmentUpdate.UserErrors, errorPath)...)
	if diags.HasError() {
		return nil
	}
	if res.SegmentUpdate.Segment == nil {
		diags.AddError("Failed to update "+what, "Shopify returned no segment.")
		return nil
	}
	return &res.SegmentUpdate.Segment.SegmentFields
}

// removeDuplicate deletes the suffixed segment a lost name race created, and reports the clash.
func (r *Resource) removeDuplicate(ctx context.Context, created *client.SegmentFields, want string, diags *diag.Diagnostics) {
	if res, err := client.DeleteSegment(ctx, r.client, created.Id); err != nil {
		diags.AddError("Failed to remove duplicate "+what, fmt.Sprintf("Remove customer segment %q in the admin: %s", created.Name, err))
	} else if res.SegmentDelete == nil {
		diags.AddError("Failed to remove duplicate "+what, fmt.Sprintf("Remove customer segment %q in the admin: Shopify returned no result.", created.Name))
	} else if len(res.SegmentDelete.UserErrors) > 0 {
		diags.Append(client.UserErrorDiagsWith(fmt.Sprintf("Failed to remove duplicate %s %q; remove it in the admin", what, created.Name), res.SegmentDelete.UserErrors, errorPath)...)
	}
	diags.AddAttributeError(path.Root("name"), "Customer segment already exists",
		fmt.Sprintf("A customer segment named %q exists. Set adopt_existing = true to take it over, or import it.", want))
}

// getByID returns the segment, or nil when it doesn't exist; segment(id:) reports that as a NOT_FOUND error, not as null.
func (r *Resource) getByID(ctx context.Context, id string) (*client.SegmentFields, error) {
	res, err := client.GetSegment(ctx, r.client, id)
	if client.IsNotFound(err) {
		return nil, nil
	}
	if err != nil || res.Segment == nil {
		return nil, err
	}
	return &res.Segment.SegmentFields, nil
}

// getByName returns the segment, or nil when none has the name. There is no name lookup, and the search index lags, so this scans the list.
// The match is case-sensitive: names differing only in case are distinct segments (checked).
func (r *Resource) getByName(ctx context.Context, name string) (*client.SegmentFields, error) {
	var found *client.SegmentFields
	err := patterns.Paginate(ctx, 0, listSegments(ctx, r.client), func(s *client.SegmentFields) bool {
		if s.Name == name {
			found = s
		}
		return found == nil
	})
	return found, err
}
