package metafielddefinition

import (
	"context"
	"fmt"

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

// ListResource lists metafield definitions; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

type listConfig struct {
	OwnerType types.String `tfsdk:"owner_type"`
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists the metafield definitions of one owner type.",
		Attributes: map[string]listschema.Attribute{
			"owner_type": listschema.StringAttribute{
				MarkdownDescription: "Owner resource type, e.g. `PRODUCT`. Without the app's read scope for that type the list can come back empty.",
				Required:            true,
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
	ownerType := client.MetafieldOwnerType(cfg.OwnerType.ValueString())

	stream.Results = func(push func(list.ListResult) bool) {
		page := func(after *string) ([]client.MetafieldDefinitionFields, *string, error) {
			res, err := client.ListMetafieldDefinitions(ctx, r.client, ownerType, after)
			if err != nil {
				return nil, nil, err
			}
			conn := res.MetafieldDefinitions
			if !conn.PageInfo.HasNextPage {
				return conn.Nodes, nil, nil
			}
			return conn.Nodes, conn.PageInfo.EndCursor, nil
		}
		err := patterns.Paginate(ctx, req.Limit, page, func(d client.MetafieldDefinitionFields) bool {
			return push(listResult(ctx, req, &d))
		})
		if err == nil {
			return
		}
		var diags diag.Diagnostics
		// TODO(verify): the dev store returns an empty list, not ACCESS_DENIED, for CUSTOMER/ORDER without their read scopes.
		if client.IsAccessDenied(err) {
			diags.AddError("Failed to list "+what+"s",
				fmt.Sprintf("The app lacks the read scope for owner type %s: %s", ownerType, err))
		} else {
			diags.AddError("Failed to list "+what+"s", err.Error())
		}
		push(list.ListResult{Diagnostics: diags})
	}
}

// listResult fills the resource as Read would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, d *client.MetafieldDefinitionFields) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = fmt.Sprintf("%s/%s/%s", d.OwnerType, d.Namespace, d.Key)
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(d.Id)})...)
	if req.IncludeResource {
		m := model{AllowDataDeletion: types.BoolValue(false)}
		res.Diagnostics.Append(setFromAPI(ctx, &m, d)...)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
