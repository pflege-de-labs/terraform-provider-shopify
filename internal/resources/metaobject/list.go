package metaobject

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

var _ list.ListResourceWithConfigure = &ListResource{}

func NewListResource() list.ListResource {
	return &ListResource{}
}

// ListResource lists metaobject entries; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

type listConfig struct {
	Type types.String `tfsdk:"type"`
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists metaobject entries of one type, or of every type except app-owned ones (`app--…`). " +
			"Reference values are GIDs of the listed store; replace them before applying generated config to another store.",
		Attributes: map[string]listschema.Attribute{
			"type": listschema.StringAttribute{
				MarkdownDescription: "Definition type to list, such as `shopify--material`. Unset lists every type except app-owned ones.",
				Optional:            true,
			},
		},
	}
}

func (r *ListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	var cfg listConfig
	if diags := req.Config.Get(ctx, &cfg); diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}
	stream.Results = func(push func(list.ListResult) bool) {
		fail := func(err error) {
			var diags diag.Diagnostics
			diags.AddError("Failed to list "+what+"s", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
		typeNames := []string{cfg.Type.ValueString()}
		if cfg.Type.IsNull() {
			var err error
			if typeNames, err = r.listTypes(ctx); err != nil {
				fail(err)
				return
			}
		}
		// The limit counts across types.
		var n int64
		for _, typ := range typeNames {
			stop := false
			page := func(after *string) ([]client.MetaobjectFields, *string, error) {
				res, err := client.ListMetaobjects(ctx, r.client, typ, after)
				if err != nil {
					return nil, nil, err
				}
				conn := res.Metaobjects
				return conn.Nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
			}
			err := patterns.Paginate(ctx, 0, page, func(o client.MetaobjectFields) bool {
				n++
				stop = !push(listResult(ctx, req, typ, &o)) || (req.Limit > 0 && n >= req.Limit)
				return !stop
			})
			if err != nil {
				fail(err)
				return
			}
			if stop {
				return
			}
		}
	}
}

// listTypes returns every definition type except app-owned ones, which D3 keeps out of import.
func (r *ListResource) listTypes(ctx context.Context) ([]string, error) {
	var out []string
	page := func(after *string) ([]client.MetaobjectDefinitionFields, *string, error) {
		res, err := client.ListMetaobjectDefinitions(ctx, r.client, after)
		if err != nil {
			return nil, nil, err
		}
		conn := res.MetaobjectDefinitions
		return conn.Nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
	}
	err := patterns.Paginate(ctx, 0, page, func(d client.MetaobjectDefinitionFields) bool {
		if !strings.HasPrefix(d.Type, "app--") {
			out = append(out, d.Type)
		}
		return true
	})
	return out, err
}

// listResult fills the resource as an import would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, typ string, o *client.MetaobjectFields) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = typ + "/" + o.Handle
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(o.Id)})...)
	if req.IncludeResource {
		m := model{Type: types.StringValue(typ), AdoptExisting: types.BoolValue(false), AllowDataDeletion: types.BoolValue(false)}
		res.Diagnostics.Append(m.setFromAPI(o, readFields(nil, o.Fields))...)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
