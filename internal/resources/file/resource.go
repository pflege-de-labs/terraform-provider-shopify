// Package file implements the shopify_file resource.
package file

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

const (
	what           = "file"
	defaultTimeout = 10 * time.Minute
	pollInterval   = 2 * time.Second
	deleteInterval = 500 * time.Millisecond
	deleteWait     = time.Minute
	// Shopify keeps a deleted filename taken for a moment and suffixes it on create, even with RAISE_ERROR.
	deleteSettle = 5 * time.Second
)

var (
	_ resource.ResourceWithConfigure      = &Resource{}
	_ resource.ResourceWithImportState    = &Resource{}
	_ resource.ResourceWithIdentity       = &Resource{}
	_ resource.ResourceWithValidateConfig = &Resource{}
	_ resource.ResourceWithModifyPlan     = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

// Resource manages one file of the store's media library.
type Resource struct {
	client *client.Client
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_file"
	// Read re-adopts a file recreated outside Terraform by its filename, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A file in the store's media library: an image, a document, or an external video (YouTube, Vimeo). " +
			"**A poor fit for Terraform in general:** files are content, uploaded and replaced in the admin and through product media all the time. " +
			"Managing a few assets that are served from a URL, such as a logo or a download, fits; mirroring the whole library does not. " +
			"Shopify fetches the file from `source`, which must be an `http://` or `https://` URL; uploads of local files, and with them new videos and 3D models, are not supported yet (roadmap). " +
			"Videos and 3D models can still be imported or adopted. " +
			"Destroying or replacing a file deletes it for good and breaks every product, page and theme that uses it, so that needs `allow_data_deletion = true` in state first. " +
			"Removing the file from a `shopify_product`'s `media_ids` detaches it and keeps it. " +
			"Destroying a product with `media_ids` set detaches its files and keeps them. " +
			"**A product with `media_ids` unset is different: destroying it deletes the files no other product uses** (Shopify's behaviour); this resource's `allow_data_deletion` can't prevent that, and the next plan shows a create for it. " +
			"A file that already has the filename needs `adopt_existing = true`; destroy then leaves it in place. " +
			"Import with the filename, or with the URL for an external video.",
		Blocks: map[string]schema.Block{
			"timeouts": patterns.TimeoutsBlock("create", "update"),
		},
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "File GID. Its kind follows the content type (`MediaImage`, `GenericFile`, `Video`, `ExternalVideo`, `Model3d`); a product's `media_ids` take it as is, except `GenericFile` (`content_type = \"FILE\"`), which can't be product media.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"content_type": schema.StringAttribute{
				MarkdownDescription: "`IMAGE`, `FILE`, `VIDEO`, `EXTERNAL_VIDEO` or `MODEL_3D`. A file can't change its kind, so changing this replaces it.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.OneOf(patterns.EnumNames(client.AllFileContentType)...)},
			},
			"filename": schema.StringAttribute{
				MarkdownDescription: "The filename with its extension, unique per shop and the import key. Required for every content type except `EXTERNAL_VIDEO`, where it must be unset. " +
					"ASCII letters, digits, `.`, `_` and `-` only, because Shopify rewrites anything else. " +
					"Changing it renames the file in place and **changes its CDN URL**, so links in pages and themes break; images and generic files only. " +
					"A different extension replaces the file, and it must match the extension of the `source` URL (Shopify refuses the file otherwise, which is checked at plan time).",
				Optional: true,
			},
			"source": schema.StringAttribute{
				MarkdownDescription: "The `http://` or `https://` URL Shopify fetches the file from; for `EXTERNAL_VIDEO`, the video URL, which is read back. Shopify rewrites video URLs (`https://youtu.be/<id>` for YouTube), and the duplicate check, adoption and import only match that form, so use it. " +
					"Required to create a file; without it only `adopt_existing` works. Not readable for hosted files: Read keeps the configured value, and an imported file has none (unmanaged). " +
					"A new value replaces the content in place for `IMAGE` and `FILE`; for the other content types it replaces the file. Not allowed for `VIDEO` and `MODEL_3D` yet (roadmap).",
				Optional: true,
			},
			"source_hash": schema.StringAttribute{
				MarkdownDescription: "Not interpreted; any change fetches `source` again, like a change of `source` itself. A file behind a URL that changes later is otherwise invisible to Terraform, because the URL is not fetched at plan time.",
				Optional:            true,
			},
			"alt": schema.StringAttribute{
				MarkdownDescription: "Alt text for screen readers. `\"\"` clears it. Unset means unmanaged: the value set in the admin is kept.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"url": schema.StringAttribute{
				MarkdownDescription: "The public URL of the file; for an external video, the video URL; for a video, Shopify's 480p MP4 rendition, not the uploaded original; for a 3D model, the uploaded model. Changes with the filename and with a content update (`?v=`).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"adopt_existing":      patterns.AdoptExistingAttribute(what),
			"allow_data_deletion": patterns.AllowDataDeletionAttribute(what),
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
	if resp.Diagnostics.HasError() {
		return
	}
	for _, e := range configErrors(&cfg) {
		resp.Diagnostics.AddAttributeError(path.Root(e.attr), "Invalid "+e.attr, e.detail)
	}
}

// replaceAttrs names the attribute each replaceReasons entry blames.
var replaceAttrs = map[string]string{"content_type": "content_type", "filename extension": "filename", "source": "source", "source_hash": "source_hash"}

// ModifyPlan decides replacement, warns about what breaks links or deletes data, and fails a create that has nothing to upload.
func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Terraform plans the create half of a replace again with no prior state, so this also stops a replace that can't recreate the file.
	if req.State.Raw.IsNull() {
		if !plan.ContentType.IsUnknown() && plan.Source.IsNull() && !plan.AdoptExisting.ValueBool() {
			detail := "Without source there is nothing to upload. Set source, or set adopt_existing = true to take over an existing file."
			if ct := plan.ContentType.ValueString(); ct == typeVideo || ct == typeModel3d {
				detail = "Videos and 3D models need a staged upload, which this provider does not support yet (roadmap), so they can't be created. " +
					"Set adopt_existing = true to take over an existing one."
			}
			resp.Diagnostics.AddAttributeError(path.Root("source"), "A file can't be created without a source", detail)
		}
		return
	}
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	reasons := replaceReasons(&state, &plan)
	for _, reason := range reasons {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root(replaceAttrs[reason]))
	}
	if len(reasons) > 0 {
		detail := fmt.Sprintf("Changing %s deletes the file and uploads it again under a new GID and URL; products, pages and themes that use it lose it.", strings.Join(reasons, ", "))
		if !state.AllowDataDeletion.ValueBool() {
			detail += " Apply will fail because allow_data_deletion is not true in state."
		}
		resp.Diagnostics.AddWarning("This change replaces the file", detail)
	}

	newName := differs(state.Filename, plan.Filename)
	renamed := newName && !plan.Filename.IsUnknown() && !state.Filename.IsNull()
	if ct := plan.ContentType.ValueString(); renamed && (ct == typeVideo || ct == typeModel3d) {
		resp.Diagnostics.AddAttributeError(path.Root("filename"), "This file can't be renamed", "Shopify renames images and generic files only.")
	} else if renamed && len(reasons) == 0 {
		resp.Diagnostics.AddAttributeWarning(path.Root("filename"), "This change renames the file and changes its URL",
			"Links to the old URL in pages, themes and blog posts break.")
	}

	// A rename or new content changes the URL (?v= for content); the condition matches what Update sends.
	if (newName || contentChanged(&state, &plan)) && !plan.URL.IsUnknown() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("url"), types.StringUnknown())...)
	}
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, err := patterns.TimeoutOf(plan.Timeouts, "create", defaultTimeout)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("timeouts"), "Invalid timeout", err.Error())
		return
	}

	k := keyOf(&plan)
	adoptMissed := false
	if plan.AdoptExisting.ValueBool() || plan.ContentType.ValueString() == typeExternalVideo {
		existing, err := r.findOne(ctx, k)
		if err != nil {
			resp.Diagnostics.AddError("Failed to look up "+what, err.Error())
			return
		}
		switch {
		case existing != nil && plan.AdoptExisting.ValueBool():
			r.adopt(ctx, resp, &plan, existing, timeout)
			return
		case existing != nil:
			// RAISE_ERROR is not sent for external videos, so a repeated URL would be created twice.
			resp.Diagnostics.AddAttributeError(path.Root("source"), "File already exists",
				"An external video with this URL exists. Set adopt_existing = true to take it over, or import it.")
			return
		}
		adoptMissed = plan.AdoptExisting.ValueBool()
		if adoptMissed && plan.Source.IsNull() {
			resp.Diagnostics.AddError("File not found", fmt.Sprintf("No file matches %s, and without source there is nothing to create. "+
				"Files created moments ago may not be searchable yet; try again in a few seconds.", describeKey(k)))
			return
		}
	}

	hints := map[client.FilesErrorCode]string{}
	if adoptMissed {
		hints[client.FilesErrorCodeFilenameAlreadyExists] = "adopt_existing found no such file: Shopify's search lags behind files created moments ago, so try again in a few seconds."
	}
	res, err := client.CreateFile(ctx, r.client, []client.FileCreateInput{plan.createInput()})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create "+what, err.Error())
		return
	}
	if res.FileCreate == nil {
		resp.Diagnostics.AddError("Failed to create "+what, "Shopify returned no result.")
		return
	}
	resp.Diagnostics.Append(userErrorDiags("Failed to create "+what, res.FileCreate.UserErrors, hints)...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := decodeOne(res.FileCreate.Files)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create "+what, err.Error())
		return
	}

	ready, err := r.poller(created.Id).wait(ctx, timeout)
	var failed *failedError
	if errors.As(err, &failed) {
		// The failed file would block its filename on the next apply.
		resp.Diagnostics.Append(r.removeFile(ctx, failed.file)...)
		resp.Diagnostics.Append(processingDiags(err)...)
		return
	}
	if err != nil {
		// The file exists, so state keeps its ID and the next plan sees it.
		seen := created
		var timedOut *timeoutError
		if errors.As(err, &timedOut) {
			seen = timedOut.file
		}
		setFromAPI(&plan, seen, &seen.OriginUrl)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
		resp.Diagnostics.Append(remember(ctx, resp.Private, seen)...)
		// State was set, so Terraform taints the resource; the next apply would replace the file.
		resp.Diagnostics.AddError("The file is not ready", err.Error()+". Terraform marked the resource as tainted: run `terraform untaint <address of this resource>` before applying again.")
		return
	}
	if name, ok := ready.filename(); ok && plan.ContentType.ValueString() != typeExternalVideo && name != plan.Filename.ValueString() {
		// Shopify suffixes a name that is still in use instead of failing, which would break the key.
		resp.Diagnostics.Append(r.removeFile(ctx, ready)...)
		resp.Diagnostics.AddAttributeError(path.Root("filename"), "Shopify renamed the file",
			fmt.Sprintf("Shopify named it %q because %q was still in use, for example by a file deleted moments ago, or by a file whose name differs only in case. The new file was removed again; apply again, or rename the other file.", name, plan.Filename.ValueString()))
		return
	}
	setFromAPI(&plan, ready, &ready.OriginUrl)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
	resp.Diagnostics.Append(remember(ctx, resp.Private, ready)...)
}

// adopt takes over a file that exists: only what differs from it is sent.
func (r *Resource) adopt(ctx context.Context, resp *resource.CreateResponse, plan *model, existing *apiFile, timeout time.Duration) {
	if ct, _ := existing.contentType(); ct != plan.ContentType.ValueString() {
		resp.Diagnostics.AddAttributeError(path.Root("content_type"), "The existing file has another content type",
			fmt.Sprintf("The file %s is %s, not %s.", existing.Id, ct, plan.ContentType.ValueString()))
		return
	}
	var live model
	setFromAPI(&live, existing, nil)
	file := existing
	if in, has := plan.updateInput(existing.Id, &live); has {
		var ok bool
		if file, ok = r.update(ctx, in, &resp.Diagnostics); !ok {
			return
		}
		ready, err := r.poller(file.Id).wait(ctx, timeout)
		if err != nil {
			resp.Diagnostics.Append(processingDiags(err)...)
			return
		}
		file = ready
	}
	setFromAPI(plan, file, &file.OriginUrl)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
	resp.Diagnostics.Append(remember(ctx, resp.Private, file)...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GID first; fall back to the filename so a file recreated in the admin is found again.
	var f *apiFile
	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		var err error
		if f, err = r.getByID(ctx, state.ID.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if k := keyOf(&state); f == nil && (k.filename != "" || k.externalURL != "") {
		var err error
		if f, err = r.findOne(ctx, k); err != nil {
			resp.Diagnostics.AddError("Failed to read "+what, err.Error())
			return
		}
	}
	if f == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	// Set on import, where only the ID is known.
	if state.AdoptExisting.IsNull() {
		state.AdoptExisting = types.BoolValue(false)
	}
	if state.AllowDataDeletion.IsNull() {
		state.AllowDataDeletion = types.BoolValue(false)
	}
	written, d := patterns.WrittenHTML(ctx, req.Private)
	resp.Diagnostics.Append(d...)
	setFromAPI(&state, f, written)
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
	timeout, err := patterns.TimeoutOf(plan.Timeouts, "update", defaultTimeout)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("timeouts"), "Invalid timeout", err.Error())
		return
	}
	in, has := plan.updateInput(state.ID.ValueString(), &state)
	if !has {
		// Only adopt_existing, allow_data_deletion or timeouts changed.
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	file, ok := r.update(ctx, in, &resp.Diagnostics)
	if !ok {
		return
	}
	ready, err := r.poller(file.Id).wait(ctx, timeout)
	if err != nil {
		// A failed file is not deleted: it exists and may hold content. State keeps the old source, so the next plan tries again.
		seen := file
		var failed *failedError
		var timedOut *timeoutError
		if errors.As(err, &failed) {
			seen = failed.file
		} else if errors.As(err, &timedOut) {
			seen = timedOut.file
		}
		written, d := patterns.WrittenHTML(ctx, req.Private)
		resp.Diagnostics.Append(d...)
		setFromAPI(&state, seen, written)
		state.AdoptExisting, state.AllowDataDeletion, state.Timeouts = plan.AdoptExisting, plan.AllowDataDeletion, plan.Timeouts
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		resp.Diagnostics.Append(processingDiags(err)...)
		return
	}
	setFromAPI(&plan, ready, &ready.OriginUrl)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, patterns.IDIdentity{ID: plan.ID})...)
	resp.Diagnostics.Append(remember(ctx, resp.Private, ready)...)
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
	// Look first, so a file already gone (e.g. with its product) never trips the guard.
	f, err := r.getByID(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete "+what, err.Error())
		return
	}
	if f == nil {
		return
	}
	// Deleting is permanent and breaks every reference, used or not.
	if guard := patterns.GuardDataDeletion(state.AllowDataDeletion, what); guard.HasError() {
		resp.Diagnostics.Append(guard...)
		return
	}
	resp.Diagnostics.Append(r.deleteFile(ctx, state.ID.ValueString())...)
}

// ImportState takes the filename (or the URL of an external video) from `terraform import`, or the GID from an import block with identity.
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		k := parseImportKey(req.ID)
		f, err := r.findOne(ctx, k)
		if err != nil {
			resp.Diagnostics.AddError("Failed to import "+what, err.Error())
			return
		}
		if f == nil {
			resp.Diagnostics.AddError("File not found", fmt.Sprintf("No file matches %s. Shopify's search lags behind files created moments ago, so try again in a few seconds.", describeKey(k)))
			return
		}
		// Read fills in the rest by GID.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), f.Id)...)
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

// update sends one fileUpdate and returns the file from its payload.
func (r *Resource) update(ctx context.Context, in client.FileUpdateInput, diags *diag.Diagnostics) (*apiFile, bool) {
	res, err := client.UpdateFile(ctx, r.client, []client.FileUpdateInput{in})
	if err != nil {
		diags.AddError("Failed to update "+what, err.Error())
		return nil, false
	}
	if res.FileUpdate == nil {
		diags.AddError("Failed to update "+what, "Shopify returned no result.")
		return nil, false
	}
	diags.Append(userErrorDiags("Failed to update "+what, res.FileUpdate.UserErrors, nil)...)
	if diags.HasError() {
		return nil, false
	}
	f, err := decodeOne(res.FileUpdate.Files)
	if err != nil {
		diags.AddError("Failed to update "+what, err.Error())
		return nil, false
	}
	return f, true
}

// deleteFile treats a file that is already gone as deleted.
func (r *Resource) deleteFile(ctx context.Context, id string) diag.Diagnostics {
	var diags diag.Diagnostics
	res, err := client.DeleteFile(ctx, r.client, []string{id})
	if err != nil {
		diags.AddError("Failed to delete "+what, err.Error())
		return diags
	}
	if res.FileDelete == nil {
		diags.AddError("Failed to delete "+what, "Shopify returned no result.")
		return diags
	}
	if gone(res.FileDelete.UserErrors) {
		return diags
	}
	diags.Append(userErrorDiags("Failed to delete "+what, res.FileDelete.UserErrors, nil)...)
	if diags.HasError() {
		return diags
	}
	// Deleting is asynchronous, and the name stays taken for a moment, which a replace would hit.
	if !r.waitGone(ctx, id) {
		diags.AddWarning("The deleted file still exists", fmt.Sprintf("Shopify has not finished deleting %s after %s.", id, deleteWait))
	}
	return diags
}

// waitGone reports whether the file disappeared within deleteWait, and then lets the name settle.
func (r *Resource) waitGone(ctx context.Context, id string) bool {
	p := r.poller(id)
	deadline := p.now().Add(deleteWait)
	for p.now().Before(deadline) {
		if f, err := p.fetch(ctx); err == nil && f == nil {
			return p.sleep(ctx, deleteSettle) == nil
		}
		if p.sleep(ctx, deleteInterval) != nil {
			return false
		}
	}
	return false
}

// removeFile deletes a file Create must not keep; a failure to do so is reported with the GID so it can be removed by hand.
func (r *Resource) removeFile(ctx context.Context, f *apiFile) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, d := range r.deleteFile(ctx, f.Id) {
		diags.AddError("Failed to remove the failed file "+f.Id, d.Detail())
	}
	return diags
}

// remember stores the origin URL Shopify returned, for external videos; the helper is generic despite its name.
func remember(ctx context.Context, private patterns.PrivateSetter, f *apiFile) diag.Diagnostics {
	if f.Typename != "ExternalVideo" {
		return nil
	}
	return patterns.RememberWrittenHTML(ctx, private, f.OriginUrl)
}

// processingDiags reports why a file isn't ready, one error per Shopify file error.
func processingDiags(err error) diag.Diagnostics {
	var diags diag.Diagnostics
	var failed *failedError
	if errors.As(err, &failed) {
		for _, fe := range failed.file.FileErrors {
			diags.AddError("Shopify could not process the file", fe.String())
		}
		return diags
	}
	var timedOut *timeoutError
	if errors.As(err, &timedOut) {
		diags.AddError("The file is not ready", err.Error()+"; apply again once Shopify has finished.")
		return diags
	}
	diags.AddError("The file is not ready", err.Error())
	return diags
}

func (r *Resource) poller(id string) poller {
	return poller{
		fetch: func(ctx context.Context) (*apiFile, error) { return r.getByID(ctx, id) },
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		},
		now:      time.Now,
		interval: pollInterval,
	}
}

// getByID returns the file, or nil when it doesn't exist or isn't a file.
func (r *Resource) getByID(ctx context.Context, id string) (*apiFile, error) {
	res, err := client.GetFile(ctx, r.client, id)
	if err != nil {
		return nil, err
	}
	return decodeFile(res.Node)
}

// findOne returns the file with the exact key, nil when none has it, and an error when several do.
func (r *Resource) findOne(ctx context.Context, k key) (*apiFile, error) {
	q := findQuery(k)
	var all []apiFile
	err := patterns.Paginate(ctx, 0, func(after *string) ([]apiFile, *string, error) {
		res, err := client.ListFiles(ctx, r.client, after, &q)
		if err != nil {
			return nil, nil, err
		}
		fs, err := decodeFiles(res.Files.Nodes)
		return fs, patterns.NextCursor(res.Files.PageInfo.HasNextPage, res.Files.PageInfo.EndCursor), err
	}, func(f apiFile) bool {
		all = append(all, f)
		return true
	})
	if err != nil {
		return nil, err
	}
	matches := matchFiles(all, k)
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	}
	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = m.Id
	}
	return nil, fmt.Errorf("%d files match %s, so none is picked: %s", len(matches), describeKey(k), strings.Join(ids, ", "))
}

// keyOf prefers url for an external video: it is the form Shopify stores, while source may be the configured one.
func keyOf(m *model) key {
	if m.ContentType.ValueString() == typeExternalVideo {
		if !m.URL.IsNull() && !m.URL.IsUnknown() {
			return key{externalURL: m.URL.ValueString()}
		}
		return key{externalURL: m.Source.ValueString()}
	}
	return key{filename: m.Filename.ValueString()}
}

func describeKey(k key) string {
	if k.externalURL != "" {
		return "the external video " + k.externalURL
	}
	return fmt.Sprintf("the filename %q", k.filename)
}
