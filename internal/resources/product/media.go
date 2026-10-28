package product

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

var mediaIDRe = regexp.MustCompile(`^gid://shopify/(MediaImage|Video|Model3d|ExternalVideo)/\d+$`)

func mediaIDsAttribute() schema.ListAttribute {
	return schema.ListAttribute{
		MarkdownDescription: "File GIDs of the product's media in position order, usually `shopify_file.<name>.id`. " +
			"Images, videos, 3D models and external videos only: a `content_type = \"FILE\"` file can't be product media. " +
			"Removing an ID, or `[]`, detaches the file from the product and keeps it in the media library; media attached in the admin but not listed are detached the same way. " +
			"Unset means unmanaged: the media set in the admin are kept, and a destroy lets Shopify delete the media no other product uses. " +
			"Set, a destroy or replace detaches all the product's media first, so they stay in the library.",
		ElementType:   types.StringType,
		Optional:      true,
		Computed:      true,
		PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
		Validators: []validator.List{
			listvalidator.UniqueValues(),
			listvalidator.ValueStringsAre(stringvalidator.RegexMatches(mediaIDRe, "must be the GID of an image, video, 3D model or external video file")),
		},
	}
}

// mediaInput builds productSet's files from configured IDs (nil when unset) and names the live media to detach first.
func mediaInput(ctx context.Context, cfg types.List, live []string) (json.RawMessage, []string, diag.Diagnostics) {
	if cfg.IsNull() || cfg.IsUnknown() {
		return nil, nil, nil
	}
	ids := []string{}
	diags := cfg.ElementsAs(ctx, &ids, false)
	files := make([]map[string]string, len(ids))
	for i, id := range ids {
		files[i] = map[string]string{"id": id}
	}
	raw, err := json.Marshal(files)
	if err != nil {
		diags.AddError("Failed to encode media_ids", err.Error())
	}
	return raw, removedIDs(live, ids), diags
}

// detachInputs takes files off a product without deleting them, which productSet would do for a file no other product uses.
func detachInputs(ids []string, productID string) []client.FileUpdateInput {
	in := make([]client.FileUpdateInput, len(ids))
	for i, id := range ids {
		in[i] = client.FileUpdateInput{Id: id, ReferencesToRemove: []string{productID}}
	}
	return in
}

// mediaManagedKey records whether the last write managed media_ids, since Delete gets no config.
const mediaManagedKey = "media_managed"

func rememberMediaManaged(ctx context.Context, private patterns.PrivateSetter, managed bool) diag.Diagnostics {
	return private.SetKey(ctx, mediaManagedKey, []byte(strconv.FormatBool(managed)))
}

// detachOnDestroy is false only when the last write left media_ids unset; no record (import) fails safe and detaches.
func detachOnDestroy(ctx context.Context, private patterns.PrivateGetter) (bool, diag.Diagnostics) {
	raw, diags := private.GetKey(ctx, mediaManagedKey)
	return string(raw) != "false", diags
}

// needsMediaRecord is true when config manages media_ids but the last write recorded them as unmanaged; no record (import) already detaches.
func needsMediaRecord(cfgSet bool, record []byte) bool {
	return cfgSet && string(record) == "false"
}

// planMediaManaged forces an update, which records the media as managed, when media_ids (known or not yet) is set and the record is false.
func (r *Resource) planMediaManaged(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var ids types.List
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("media_ids"), &ids)...)
	raw, d := req.Private.GetKey(ctx, mediaManagedKey)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() || !needsMediaRecord(!ids.IsNull(), raw) {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("media_managed"), types.BoolUnknown())...)
	resp.Diagnostics.AddWarning("Updating the product to manage its media",
		"media_ids is set but the provider has recorded it as unmanaged. The update changes no media; it makes a destroy detach them so the files survive.")
}

// mediaRecord is what a write records: sticky once true, since detaching on destroy only keeps files in the library.
func mediaRecord(cfgSet bool, prior []byte) bool {
	return cfgSet || string(prior) == "true"
}

// removedIDs returns the prior IDs that are not planned, in prior order.
func removedIDs(prior, planned []string) []string {
	var out []string
	for _, id := range prior {
		if !slices.Contains(planned, id) {
			out = append(out, id)
		}
	}
	return out
}

// mediaIDs returns the product's media GIDs in position order, never nil.
func mediaIDs(p *client.ProductFields) []string {
	ids := []string{}
	if p == nil {
		return ids
	}
	for _, n := range p.Media.Nodes {
		ids = append(ids, n.GetId())
	}
	return ids
}

// completeMedia appends the media pages after the first one that ProductFields carries.
func completeMedia(p *client.ProductFields, more func(after *string) ([]client.MediaID, *string, error)) error {
	page := &p.Media.PageInfo
	for page.HasNextPage {
		nodes, next, err := more(page.EndCursor)
		if err != nil {
			return err
		}
		p.Media.Nodes = append(p.Media.Nodes, nodes...)
		page.HasNextPage, page.EndCursor = next != nil, next
	}
	return nil
}

// moreMedia pages a product's media after a cursor.
func moreMedia(ctx context.Context, c *client.Client, id string) func(after *string) ([]client.MediaID, *string, error) {
	return func(after *string) ([]client.MediaID, *string, error) {
		res, err := client.GetProductMedia(ctx, c, id, after)
		if err != nil {
			return nil, nil, err
		}
		if res.Product == nil {
			return nil, nil, fmt.Errorf("product %s was deleted while its media were read", id)
		}
		conn := res.Product.Media
		return conn.Nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}
