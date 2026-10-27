package product

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/publishing"
)

// completeProduct pages in what the first page of a product lacks: variants, media and publications.
func completeProduct(ctx context.Context, c *client.Client, p *client.ProductFields) error {
	if err := fetchAllVariants(ctx, c, p); err != nil {
		return err
	}
	if err := completeMedia(p, moreMedia(ctx, c, p.Id)); err != nil {
		return err
	}
	return publishing.Complete(ctx, &p.ResourcePublicationsV2, morePublications(ctx, c, p.Id))
}

// morePublications pages a product's publications after a cursor.
func morePublications(ctx context.Context, c *client.Client, id string) func(after *string) ([]string, *string, error) {
	return func(after *string) ([]string, *string, error) {
		res, err := client.GetProductPublications(ctx, c, id, after)
		if err != nil {
			return nil, nil, err
		}
		if res.Product == nil {
			return nil, nil, fmt.Errorf("product %s was deleted while its publications were read", id)
		}
		conn := res.Product.ResourcePublicationsV2
		ids := make([]string, len(conn.Nodes))
		for i, n := range conn.Nodes {
			ids[i] = n.Publication.Id
		}
		return ids, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}

// publish makes the product's publications match the configured ones and records what it has afterwards in plan; unset publication_ids is left alone.
func (r *Resource) publish(ctx context.Context, p *client.ProductFields, plan, cfg *model) diag.Diagnostics {
	want, diags := publishing.Want(ctx, cfg.PublicationIDs)
	if want == nil || diags.HasError() {
		return diags
	}
	got, d := publishing.Sync(ctx, r.client, publishing.Target{ID: p.Id, More: morePublications(ctx, r.client, p.Id)}, publishing.IDs(p.GetResourcePublicationsV2()), want)
	diags.Append(d...)
	plan.PublicationIDs, d = publishing.Value(ctx, got)
	diags.Append(d...)
	return diags
}
