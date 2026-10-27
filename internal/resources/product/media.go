package product

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

// TODO(verify): Video and Model3d GIDs as productSet files; only MediaImage and ExternalVideo were attached on the dev store.
var mediaIDRe = regexp.MustCompile(`^gid://shopify/(MediaImage|Video|Model3d|ExternalVideo)/\d+$`)

func mediaIDsAttribute() schema.ListAttribute {
	return schema.ListAttribute{
		MarkdownDescription: "File GIDs of the product's media in position order, usually `shopify_file.<name>.id`. " +
			"Images, videos, 3D models and external videos only: a `content_type = \"FILE\"` file can't be product media. " +
			"Removing an ID, or `[]`, detaches the file from the product and keeps it in the media library; media attached in the admin but not listed are detached the same way. " +
			"Unset means unmanaged: the media set in the admin are kept.",
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
