package collection

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/publishing"
)

// morePublications pages a collection's publications after a cursor.
func morePublications(ctx context.Context, c *client.Client, id string) func(after *string) ([]string, *string, error) {
	return func(after *string) ([]string, *string, error) {
		res, err := client.GetCollectionPublications(ctx, c, id, after)
		if err != nil {
			return nil, nil, err
		}
		if res.Collection == nil {
			return nil, nil, fmt.Errorf("collection %s was deleted while its publications were read", id)
		}
		conn := res.Collection.ResourcePublicationsV2
		ids := make([]string, len(conn.Nodes))
		for i, n := range conn.Nodes {
			ids[i] = n.Publication.Id
		}
		return ids, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}

// publish makes the collection's publications match the configured ones and records what it has afterwards in plan; unset publication_ids is left alone.
func (r *Resource) publish(ctx context.Context, l *liveCollection, plan, cfg *model) diag.Diagnostics {
	want, diags := publishing.Want(ctx, cfg.PublicationIDs)
	if want == nil || diags.HasError() {
		return diags
	}
	got, d := publishing.Sync(ctx, r.client, publishing.Target{ID: l.Id, More: morePublications(ctx, r.client, l.Id)}, publishing.IDs(l.GetResourcePublicationsV2()), want)
	diags.Append(d...)
	plan.PublicationIDs, d = publishing.Value(ctx, got)
	diags.Append(d...)
	return diags
}
