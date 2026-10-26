// Package publishing manages the publication_ids attribute that shopify_product and shopify_collection share.
package publishing

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// Connection is the publications fragment both resources embed in their fields.
type Connection = client.PublicationsOfPublishableResourcePublicationsV2ResourcePublicationV2Connection

type node = client.PublicationsOfPublishableResourcePublicationsV2ResourcePublicationV2ConnectionNodesResourcePublicationV2

var idRe = regexp.MustCompile(`^gid://shopify/Publication/\d+$`)

// Attribute is the publication_ids schema; what is "product" or "collection".
func Attribute(what string) schema.SetAttribute {
	return schema.SetAttribute{
		MarkdownDescription: fmt.Sprintf("`Publication` GIDs of the sales channels this %[1]s is published to, usually from `data.shopify_publication`. "+
			"**Removing an ID unpublishes the %[1]s from that channel: it disappears there for customers**, so check the plan before applying. "+
			"A new %[1]s is published nowhere until this is set. A channel that publishes automatically (`auto_publish`) and isn't listed here is unpublished again. "+
			"Only sales channel publications are supported; the publication of a market catalog is rejected. Scheduled publishing is not supported. "+
			"Unset means unmanaged: publications set in the admin are kept. `[]` unpublishes everywhere. Needs the `write_publications` scope.", what),
		ElementType: types.StringType,
		Optional:    true,
		Computed:    true,
		Validators: []validator.Set{setvalidator.ValueStringsAre(
			stringvalidator.RegexMatches(idRe, "must be a Publication GID such as gid://shopify/Publication/123"))},
		PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
	}
}

// IDs lists the publication IDs on conn's loaded pages.
func IDs(conn Connection) []string {
	ids := make([]string, len(conn.Nodes))
	for i, n := range conn.Nodes {
		ids[i] = n.Publication.Id
	}
	return ids
}

// Complete appends the pages after conn's first one, so conn holds every publication; more returns the next page's IDs and cursor.
func Complete(ctx context.Context, conn *Connection, more func(after *string) ([]string, *string, error)) error {
	for conn.PageInfo.HasNextPage {
		if err := ctx.Err(); err != nil {
			return err
		}
		ids, next, err := more(conn.PageInfo.EndCursor)
		if err != nil {
			return err
		}
		for _, id := range ids {
			var n node
			n.Publication.Id = id
			conn.Nodes = append(conn.Nodes, n)
		}
		conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor = next != nil, next
	}
	return nil
}

// Diff returns the sorted IDs to publish and to unpublish to turn live into want.
func Diff(live, want []string) (add, remove []string) {
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

// Value is the attribute value for live IDs; no publications is an empty set, not null.
func Value(ctx context.Context, ids []string) (types.Set, diag.Diagnostics) {
	return types.SetValueFrom(ctx, types.StringType, append([]string{}, ids...))
}

// NullIfEmpty is for list results, where an empty set would generate config that claims to manage publications.
func NullIfEmpty(s types.Set) types.Set {
	if len(s.Elements()) == 0 {
		return types.SetNull(types.StringType)
	}
	return s
}

// Want returns the configured IDs, or nil when the attribute is unset (unmanaged).
func Want(ctx context.Context, cfg types.Set) ([]string, diag.Diagnostics) {
	if cfg.IsNull() || cfg.IsUnknown() {
		return nil, nil
	}
	ids := []string{}
	d := cfg.ElementsAs(ctx, &ids, false)
	return ids, d
}

// Target is one product or collection to publish.
type Target struct {
	ID   string
	More func(after *string) ([]string, *string, error) // pages publications after a cursor
}

// Sync publishes and unpublishes the difference between live and want and returns the publications Shopify lists afterwards.
// Publishing goes first, so a rejected ID unpublishes nothing.
func Sync(ctx context.Context, c *client.Client, t Target, live, want []string) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	add, remove := Diff(live, want)
	if len(add) == 0 && len(remove) == 0 {
		return live, nil
	}
	var after Connection
	if len(add) > 0 {
		res, err := client.PublishPublishable(ctx, c, t.ID, inputs(add))
		if err != nil {
			diags.AddError("Failed to publish", err.Error())
			return live, diags
		}
		if res.PublishablePublish == nil {
			diags.AddError("Failed to publish", "Shopify returned no result.")
			return live, diags
		}
		diags.Append(client.UserErrorDiagsWith("Failed to publish", res.PublishablePublish.UserErrors, attributePath)...)
		if diags.HasError() {
			return live, diags
		}
		if p := res.PublishablePublish.Publishable; p != nil && *p != nil {
			after = (*p).GetResourcePublicationsV2()
		}
	}
	if len(remove) > 0 {
		res, err := client.UnpublishPublishable(ctx, c, t.ID, inputs(remove))
		if err != nil {
			diags.AddError("Failed to unpublish", err.Error())
			return live, diags
		}
		if res.PublishableUnpublish == nil {
			diags.AddError("Failed to unpublish", "Shopify returned no result.")
			return live, diags
		}
		diags.Append(client.UserErrorDiagsWith("Failed to unpublish", res.PublishableUnpublish.UserErrors, attributePath)...)
		if diags.HasError() {
			return live, diags
		}
		if p := res.PublishableUnpublish.Publishable; p != nil && *p != nil {
			after = (*p).GetResourcePublicationsV2()
		}
	}
	if err := Complete(ctx, &after, t.More); err != nil {
		diags.AddError("Failed to read the publications", err.Error())
		return live, diags
	}
	got := IDs(after)
	if msg := mismatch(want, got); msg != "" {
		diags.AddAttributeError(path.Root("publication_ids"), "Publications differ from the configuration", msg)
		return got, diags
	}
	// An extra publication (a racing auto-publish channel) would make the result differ from the plan; the next refresh shows it as drift.
	return want, diags
}

func inputs(ids []string) []client.PublicationInput {
	in := make([]client.PublicationInput, len(ids))
	for i := range ids {
		in[i] = client.PublicationInput{PublicationId: &ids[i]}
	}
	return in
}

// attributePath maps every publishing error to publication_ids, the only thing the mutations take.
func attributePath([]string) (path.Path, bool) { return path.Root("publication_ids"), true }

// mismatch explains why Shopify doesn't list a wanted publication, or returns "" when it does; an extra one is no error, since the next plan removes it.
func mismatch(want, got []string) string {
	missing, _ := Diff(got, want)
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("Shopify accepted %s but doesn't list them as sales channel publications. A market catalog's publication can't be managed here.", strings.Join(missing, ", "))
}

// CreateFailedNote tells how to recover when publishing failed right after a create, which leaves the new object tainted.
func CreateFailedNote(what string) string {
	return fmt.Sprintf("The %s was created, but Terraform marked it tainted because publishing failed. Fix publication_ids and run `terraform untaint` for it before the next apply, or the apply replaces it.", what)
}
