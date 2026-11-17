package page

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/metafields"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

// metafieldPage pages a page's metafields after a cursor.
func metafieldPage(ctx context.Context, c *client.Client, id string) func(after *string) ([]metafields.Live, *string, error) {
	return func(after *string) ([]metafields.Live, *string, error) {
		res, err := client.GetPageMetafields(ctx, c, id, after)
		if err != nil {
			return nil, nil, err
		}
		if res.Page == nil {
			return nil, nil, fmt.Errorf("page %s was deleted while its metafields were read", id)
		}
		conn := res.Page.Metafields
		out := make([]metafields.Live, len(conn.Nodes))
		for i, n := range conn.Nodes {
			out[i] = metafields.Live{Namespace: n.Namespace, Key: n.Key, Type: n.Type, Value: n.Value}
		}
		return out, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}

// readMetafields sets m.Metafields from the live page: the managed keys, or every key on import (which then stays null when empty).
func readMetafields(ctx context.Context, c *client.Client, m *model, id string, importing bool) diag.Diagnostics {
	prior, diags := metafields.FromValue(ctx, m.Metafields)
	live, err := metafields.List(ctx, metafieldPage(ctx, c, id))
	if err != nil {
		diags.AddError("Failed to read metafields", err.Error())
		return diags
	}
	var d diag.Diagnostics
	m.Metafields, d = metafields.ToValue(ctx, metafields.Read(prior, live), importing)
	return append(diags, d...)
}

// writeMetafields sends the configured metafields that differ from the live ones; after a failure plan holds what the page really has.
func (r *Resource) writeMetafields(ctx context.Context, id string, plan, cfg *model) diag.Diagnostics {
	want, diags := metafields.FromValue(ctx, cfg.Metafields)
	if want == nil || diags.HasError() {
		return diags
	}
	live, err := metafields.List(ctx, metafieldPage(ctx, r.client, id))
	if err != nil {
		diags.AddError("Failed to read metafields", err.Error())
		return diags
	}
	d := metafields.Apply(ctx, r.client, id, want, metafields.Read(want, live))
	diags.Append(d...)
	if d.HasError() {
		if live, err = metafields.List(ctx, metafieldPage(ctx, r.client, id)); err == nil {
			plan.Metafields, d = metafields.ToValue(ctx, metafields.Read(want, live), false)
			diags.Append(d...)
		}
	}
	return diags
}
