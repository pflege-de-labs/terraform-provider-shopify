// Package collection implements the shopify_collection resource.
package collection

import (
	"context"
	"fmt"

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

const (
	what = "collection"
	// sourceTitle is what Shopify names the source of a migrated collection; new sources get the same.
	sourceTitle = "Untitled Source"
)

var (
	_ resource.ResourceWithConfigure      = &Resource{}
	_ resource.ResourceWithImportState    = &Resource{}
	_ resource.ResourceWithIdentity       = &Resource{}
	_ resource.ResourceWithModifyPlan     = &Resource{}
	_ resource.ResourceWithValidateConfig = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one collection.
type Resource struct {
	client *client.Client
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_collection"
	// Read re-adopts a collection recreated outside Terraform by its handle, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

// TODO(verify): whether selections of large collections ever come back only after the collectionUpdate job.
func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	unmanaged := " Unset means unmanaged: the value set in the admin is kept."
	membership := " Unset means unmanaged. Null when the collection's membership can't be managed (see the resource description). A source another app created is managed too, but Shopify may reject changes to it."
	resp.Schema = schema.Schema{
		MarkdownDescription: "A collection: title, handle, description, sort order and which products it holds, by hand (`product_ids`) or by rules (`conditions`). " +
			"**A poor fit for Terraform unless you bootstrap a store or keep a few collections in a repository:** collections back navigation, but their texts and products are edited in the admin all the time, " +
			"and every admin edit to a managed attribute is reverted on the next apply. Attributes you leave unset are not managed; the rules of a smart collection suit Terraform best. " +
			"Membership is managed for collections with at most one non-shareable conditions source, which every collection made in the admin, with the older API or by this provider has. " +
			"That includes a source another app created; Shopify may reject changes to it. " +
			"Collections with several sources, a sub-collection or shareable source, variant-level selections or a condition Shopify can't express as input are read without membership: " +
			"setting `match_type`, `conditions` or `product_ids` for them fails. Exclusions, the product order of `MANUAL` collections, the image, publishing and metafields are not managed. " +
			"Destroying a collection deletes it with its publications and translations; the products stay. " +
			"Menu items pointing at it lose their target, so add `lifecycle { prevent_destroy = true }` to collections that back navigation. " +
			"A collection that already has the handle, such as `frontpage` which every store has, needs `adopt_existing = true`; destroy then leaves it in place. " +
			"Import with the handle.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Collection GID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"handle": schema.StringAttribute{
				MarkdownDescription: "The collection's URL handle, unique per shop. Changing it renames the collection in place and creates no URL redirect. Lower case, no whitespace: Shopify lower-cases anything else.",
				Required:            true,
				Validators:          []validator.String{patterns.HandleValidator()},
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "The collection title.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"description_html": schema.StringAttribute{
				MarkdownDescription: "The description as HTML. Shopify repairs malformed HTML; the configured text is kept while Shopify still holds what it returned at the last apply, so the repair is no diff, but a later admin edit is." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"sort_order": schema.StringAttribute{
				MarkdownDescription: "How products are sorted: `ALPHA_ASC`, `ALPHA_DESC`, `BEST_SELLING`, `MOST_RELEVANT`, `CREATED`, `CREATED_DESC`, `MANUAL`, `PRICE_ASC` or `PRICE_DESC`. A new collection gets `MOST_RELEVANT` when this is unset." + unmanaged,
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.OneOf(patterns.EnumNames(client.AllCollectionSortOrder)...)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"seo_title": schema.StringAttribute{
				MarkdownDescription: "Search engine title. Null while Shopify uses the collection title." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{patterns.UseStateForUnknownIncludingNull()},
			},
			"seo_description": schema.StringAttribute{
				MarkdownDescription: "Search engine description. Null while Shopify uses the collection description." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{patterns.UseStateForUnknownIncludingNull()},
			},
			"template_suffix": schema.StringAttribute{
				MarkdownDescription: "Theme template suffix, such as `special` for `collection.special.json`." + unmanaged,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{patterns.UseStateForUnknownIncludingNull()},
			},
			"match_type": schema.StringAttribute{
				MarkdownDescription: "`ALL` or `ANY`: whether a product must match all `conditions` or any of them. Required when `conditions` is a non-empty list. Null for a collection without a source." + membership,
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.OneOf(patterns.EnumNames(client.AllCollectionConditionMatchType)...)},
				PlanModifiers:       []planmodifier.String{patterns.UseStateForUnknownIncludingNull()},
			},
			"conditions": schema.StringAttribute{
				CustomType: conditionsType{},
				MarkdownDescription: "The rules of a smart collection: a JSON list of " +
					"[`CollectionSourceInclusionConditionInput`](https://shopify.dev/docs/api/admin-graphql/2026-10/input-objects/CollectionSourceInclusionConditionInput) objects in the API's camelCase, one key each, written with `jsonencode([...])`, " +
					"for example `{ productTag = { relation = \"TAGGED_WITH\", values = [\"sale\"], matchType = \"ANY\" } }`. Shopify validates kinds and values. " +
					"Order matters; amounts and values compare numerically, so `\"5\"` matches Shopify's `\"5.0\"`. Reference metafield definitions and metaobjects by their resource or data source IDs, which differ per store. " +
					"Products match asynchronously, so a new rule can take a moment to fill the collection. `jsonencode([])` removes all conditions. Unset means unmanaged. A source another app created is managed too, but Shopify may reject changes to it.",
				Optional: true,
			},
			"product_ids": schema.SetAttribute{
				MarkdownDescription: "Product GIDs included by hand, whatever the conditions say. Reference `shopify_product` IDs, so products are created first and their GIDs never sit in the configuration. " +
					"A product deleted elsewhere drops out and shows as drift. `[]` removes them all." + membership,
				ElementType:   types.StringType,
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"source_id": schema.StringAttribute{
				MarkdownDescription: "The `CollectionConditionsSource` GID that holds `match_type`, `conditions` and `product_ids`; null while the collection has none. " +
					"The first condition or product creates it, and removing the last one deletes it.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{patterns.UseStateForUnknownIncludingNull()},
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

// ValidateConfig checks the shape of conditions and that a match type comes with them, as Shopify requires.
func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg model
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.Conditions.IsNull() || cfg.Conditions.IsUnknown() {
		return
	}
	conds, err := parseConditions(cfg.Conditions.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("conditions"), "Invalid conditions", err.Error())
		return
	}
	if len(conds) > 0 && cfg.MatchType.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("match_type"), "Missing match_type", "Set match_type to ALL or ANY together with conditions.")
	}
}

// ModifyPlan fails early on membership that can't be managed and marks values a membership change recomputes.
func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var state, plan, cfg model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Read leaves product_ids null only when the membership can't be managed.
	if state.ProductIDs.IsNull() && cfg.membershipConfigured() {
		resp.Diagnostics.AddError("Membership can't be managed",
			"Membership of this collection can't be managed yet: it has sources other than one non-shareable conditions source (see the resource description). Remove match_type, conditions and product_ids from the configuration.")
		return
	}
	if plan.Conditions.Equal(state.Conditions) && plan.ProductIDs.Equal(state.ProductIDs) && plan.MatchType.Equal(state.MatchType) {
		return
	}
	// The source may be created or deleted, which also resets an unmanaged match type.
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("source_id"), types.StringUnknown())...)
	if cfg.MatchType.IsNull() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("match_type"), types.StringUnknown())...)
	}
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// collectionCreate also fails on a taken handle, but this says how to take the collection over.
	existing, err := r.getByHandle(ctx, plan.Handle.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
		return
	}
	if existing != nil && !plan.AdoptExisting.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("handle"), "Collection already exists",
			"A collection with this handle exists. Set adopt_existing = true to take it over, or import it.")
		return
	}
	var c *client.CollectionFields
	if existing != nil {
		c = r.update(ctx, existing, &plan, &resp.Diagnostics)
	} else {
		c = r.create(ctx, &plan, &resp.Diagnostics)
	}
	r.finish(ctx, c, &plan, &resp.Diagnostics, resp.Private)
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

	// GID first; fall back to the handle so a collection recreated in the admin is found again.
	var c *liveCollection
	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		var err error
		if c, err = r.getByID(ctx, state.ID.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	importing := state.Handle.IsNull()
	if c == nil && !importing {
		var err error
		if c, err = r.getByHandle(ctx, state.Handle.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if c == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	// Set on import, where only the ID is known.
	if state.AdoptExisting.IsNull() {
		state.AdoptExisting = types.BoolValue(false)
	}
	written, d := patterns.WrittenHTML(ctx, req.Private)
	resp.Diagnostics.Append(d...)
	d, unsupported := setFromAPI(ctx, &state, c, written, importing)
	resp.Diagnostics.Append(d...)
	if unsupported != nil && importing {
		resp.Diagnostics.AddWarning("Membership is not managed", unsupportedDetail(unsupported))
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: state.ID})...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := plan.ID.ValueString()
	// Condition IDs and the current selections come from the live collection, never from state.
	live := &liveCollection{CollectionFields: &client.CollectionFields{Id: id}}
	if plan.membershipConfigured() {
		var err error
		if live, err = r.getByID(ctx, id); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what+" before the update", err.Error())
			return
		}
		if live == nil {
			resp.Diagnostics.AddError("Failed to update "+what, fmt.Sprintf("Collection %s no longer exists; refresh to recreate it.", id))
			return
		}
	}
	c := r.update(ctx, live, &plan, &resp.Diagnostics)
	r.finish(ctx, c, &plan, &resp.Diagnostics, resp.Private)
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
	// A collection already gone is a plain UserError from collectionDelete, so look first instead of matching its message.
	res, err := client.GetCollection(ctx, r.client, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	if res.Collection == nil {
		return
	}
	del, err := client.DeleteCollection(ctx, r.client, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	resp.Diagnostics.Append(client.UserErrorDiags("Failed to delete "+what, del.CollectionDelete.UserErrors)...)
}

// ImportState takes the handle from `terraform import`, or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		res, err := client.GetCollectionByHandle(ctx, r.client, req.ID)
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+what, err.Error())
			return
		}
		if res.CollectionByIdentifier == nil {
			resp.Diagnostics.AddError("Collection not found", fmt.Sprintf("No collection has the handle %q.", req.ID))
			return
		}
		// Read fills in the rest by GID.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), res.CollectionByIdentifier.Id)...)
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

// create sends collectionCreate with the configured attributes and membership.
func (r *Resource) create(ctx context.Context, plan *model, diags *diag.Diagnostics) *client.CollectionFields {
	changes, ok := planChanges(ctx, nil, plan, diags)
	if !ok {
		return nil
	}
	in := plan.createInput()
	if changes.Create != nil {
		in.Sources = []client.CollectionCreateSourceTargetInput{{Source: &client.CollectionCreateConditionsSourceInput{Title: sourceTitle, Inclusion: changes.Create}}}
	}
	res, err := client.CreateCollection(ctx, r.client, in)
	if err != nil {
		diags.AddError("Failed to create "+what, err.Error())
		return nil
	}
	diags.Append(client.UserErrorDiagsBy("Failed to create "+what, res.CollectionCreate.UserErrors, errorPath)...)
	if diags.HasError() {
		return nil
	}
	if res.CollectionCreate.Collection == nil {
		diags.AddError("Failed to create "+what, "Shopify returned no collection.")
		return nil
	}
	return &res.CollectionCreate.Collection.CollectionFields
}

// update sends collectionUpdate for live with the configured attributes and the membership difference.
func (r *Resource) update(ctx context.Context, live *liveCollection, plan *model, diags *diag.Diagnostics) *client.CollectionFields {
	var mem *membership
	if plan.membershipConfigured() {
		var err error
		if mem, err = membershipOf(live.sources); err != nil {
			diags.AddError("Membership can't be managed", unsupportedDetail(err))
			return nil
		}
	}
	changes, ok := planChanges(ctx, mem, plan, diags)
	if !ok {
		return nil
	}
	in := plan.updateInput(live.Id)
	switch {
	case changes.Create != nil:
		in.SourcesToCreate = []client.CollectionCreateSourceTargetInput{{Source: &client.CollectionCreateConditionsSourceInput{Title: sourceTitle, Inclusion: changes.Create}}}
	case changes.Update != nil:
		in.SourcesToUpdate = []client.CollectionUpdateSourceTargetInput{{Condition: &client.CollectionUpdateConditionsSourceInput{Id: mem.SourceID, Inclusion: changes.Update}}}
	case changes.Delete:
		in.SourcesToDelete = []string{mem.SourceID}
	}
	// TODO(verify): whether Shopify rejects writes to a source created by another app; a rejection arrives as a userError below.
	res, err := client.UpdateCollection(ctx, r.client, in)
	if err != nil {
		diags.AddError("Failed to update "+what, err.Error())
		return nil
	}
	diags.Append(client.UserErrorDiagsBy("Failed to update "+what, res.CollectionUpdate.UserErrors, errorPath)...)
	if diags.HasError() {
		return nil
	}
	if res.CollectionUpdate.Collection == nil {
		diags.AddError("Failed to update "+what, "Shopify returned no collection.")
		return nil
	}
	return &res.CollectionUpdate.Collection.CollectionFields
}

func planChanges(ctx context.Context, mem *membership, plan *model, diags *diag.Diagnostics) (sourceChanges, bool) {
	want, d := plan.desired(ctx)
	diags.Append(d...)
	if diags.HasError() {
		return sourceChanges{}, false
	}
	changes, err := planSource(mem, want)
	if err != nil {
		diags.AddAttributeError(path.Root("match_type"), "Invalid membership", err.Error())
		return sourceChanges{}, false
	}
	return changes, true
}

// finish writes the collection a write returned into plan and remembers its description.
func (r *Resource) finish(ctx context.Context, c *client.CollectionFields, plan *model, diags *diag.Diagnostics, private patterns.PrivateSetter) {
	if c == nil || diags.HasError() {
		return
	}
	l, err := completeCollection(ctx, r.client, c)
	if err != nil {
		diags.AddError("Failed to read the written "+what, err.Error())
		return
	}
	d, _ := setFromAPI(ctx, plan, l, &c.DescriptionHtml, false)
	diags.Append(d...)
	diags.Append(patterns.RememberWrittenHTML(ctx, private, c.DescriptionHtml)...)
}

// getByID returns the collection, or nil when it doesn't exist.
func (r *Resource) getByID(ctx context.Context, id string) (*liveCollection, error) {
	res, err := client.GetCollection(ctx, r.client, id)
	if err != nil || res.Collection == nil {
		return nil, err
	}
	return completeCollection(ctx, r.client, &res.Collection.CollectionFields)
}

// getByHandle returns the collection, or nil when no collection has the handle.
func (r *Resource) getByHandle(ctx context.Context, handle string) (*liveCollection, error) {
	res, err := client.GetCollectionByHandle(ctx, r.client, handle)
	if err != nil || res.CollectionByIdentifier == nil {
		return nil, err
	}
	return completeCollection(ctx, r.client, &res.CollectionByIdentifier.CollectionFields)
}

// completeCollection decodes the sources and pages a single source's selections after the first 250.
func completeCollection(ctx context.Context, cl *client.Client, c *client.CollectionFields) (*liveCollection, error) {
	sources, err := decodeSources(c.Sources)
	if err != nil {
		return nil, err
	}
	l := &liveCollection{CollectionFields: c, sources: sources}
	if len(sources) != 1 || sources[0].Inclusion == nil {
		return l, nil
	}
	inc := sources[0].Inclusion
	for inc.Selections.PageInfo.HasNextPage {
		res, err := client.GetCollectionSelections(ctx, cl, c.Id, inc.Selections.PageInfo.EndCursor)
		if err != nil {
			return nil, err
		}
		if res.Collection == nil {
			return nil, fmt.Errorf("collection %s disappeared while reading its products", c.Id)
		}
		page, err := decodeSources(res.Collection.Sources)
		if err != nil {
			return nil, err
		}
		if len(page) != 1 || page[0].ID != sources[0].ID || page[0].Inclusion == nil {
			return nil, fmt.Errorf("the sources of collection %s changed while reading its products", c.Id)
		}
		inc.Selections.Nodes = append(inc.Selections.Nodes, page[0].Inclusion.Selections.Nodes...)
		inc.Selections.PageInfo = page[0].Inclusion.Selections.PageInfo
	}
	return l, nil
}
