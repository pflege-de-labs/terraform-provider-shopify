package product

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/metafields"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

// variantMetafieldPage pages a variant's metafields after a cursor.
func variantMetafieldPage(ctx context.Context, c *client.Client, id string) func(after *string) ([]metafields.Live, *string, error) {
	return func(after *string) ([]metafields.Live, *string, error) {
		res, err := client.GetVariantMetafields(ctx, c, id, after)
		if err != nil {
			return nil, nil, err
		}
		if res.ProductVariant == nil {
			return nil, nil, fmt.Errorf("variant %s was deleted while its metafields were read", id)
		}
		conn := res.ProductVariant.Metafields
		out := make([]metafields.Live, len(conn.Nodes))
		for i, n := range conn.Nodes {
			out[i] = metafields.Live{Namespace: n.Namespace, Key: n.Key, Type: n.Type, Value: n.Value}
		}
		return out, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
}

// fillVariantMetafields sets each variant's metafields from lookup: the managed keys, or every key on import (null when empty).
// A product with a linked option is skipped, since variants can't be written for it (linkedOptionError) and Shopify backs the option with variant metafields.
// TODO(verify): which variant metafields back a linked option; the dev store has no such product.
func fillVariantMetafields(ctx context.Context, m *model, p *client.ProductFields, importing bool, lookup func(variantID string) ([]metafields.Live, error)) diag.Diagnostics {
	var ms []variantModel
	if linkedOptionError(p) != nil || m.Variants.IsNull() || m.Variants.IsUnknown() {
		return nil
	}
	diags := m.Variants.ElementsAs(ctx, &ms, false)
	if diags.HasError() {
		return diags
	}
	for i := range ms {
		if !importing && ms[i].Metafields.IsNull() {
			continue
		}
		prior, d := metafields.FromValue(ctx, ms[i].Metafields)
		diags.Append(d...)
		live, err := lookup(ms[i].ID.ValueString())
		if err != nil {
			diags.AddError("Failed to read variant metafields", err.Error())
			return diags
		}
		ms[i].Metafields, d = metafields.ToValue(ctx, metafields.Read(prior, live), importing)
		diags.Append(d...)
	}
	var d diag.Diagnostics
	m.Variants, d = types.ListValueFrom(ctx, variantType, ms)
	return append(diags, d...)
}

func (r *Resource) readVariantMetafields(ctx context.Context, m *model, p *client.ProductFields, importing bool) diag.Diagnostics {
	return fillVariantMetafields(ctx, m, p, importing, func(id string) ([]metafields.Live, error) {
		return metafields.List(ctx, variantMetafieldPage(ctx, r.client, id))
	})
}

// writeVariantMetafields sends the planned metafields of each variant, whose IDs plan holds after productSet; after a failure plan holds what the variant really has.
func (r *Resource) writeVariantMetafields(ctx context.Context, plan *model) diag.Diagnostics {
	var ms []variantModel
	if plan.Variants.IsNull() || plan.Variants.IsUnknown() {
		return nil
	}
	diags := plan.Variants.ElementsAs(ctx, &ms, false)
	for i := range ms {
		want, d := metafields.FromValue(ctx, ms[i].Metafields)
		diags.Append(d...)
		if want == nil || diags.HasError() {
			continue
		}
		id := ms[i].ID.ValueString()
		live, err := metafields.List(ctx, variantMetafieldPage(ctx, r.client, id))
		if err != nil {
			diags.AddError("Failed to read variant metafields", err.Error())
			return diags
		}
		base := path.Root("variants").AtListIndex(i).AtName("metafields")
		d = metafields.ApplyAt(ctx, r.client, base, id, want, metafields.Read(want, live))
		diags.Append(d...)
		if d.HasError() {
			if live, err = metafields.List(ctx, variantMetafieldPage(ctx, r.client, id)); err == nil {
				ms[i].Metafields, d = metafields.ToValue(ctx, metafields.Read(want, live), false)
				diags.Append(d...)
				plan.Variants, d = types.ListValueFrom(ctx, variantType, ms)
				diags.Append(d...)
			}
			return diags
		}
	}
	return diags
}
