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
				MarkdownDescription: "Owner resource type, e.g. `PRODUCT`. For an owner type the app has no read scope for, Shopify returns an empty list, not an error, so an empty result comes with a warning.",
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
		listed := 0
		page := func(after *string) ([]client.MetafieldDefinitionFields, *string, error) {
			res, err := client.ListMetafieldDefinitions(ctx, r.client, ownerType, after)
			if err != nil {
				return nil, nil, err
			}
			conn := res.MetafieldDefinitions
			listed += len(conn.Nodes)
			return conn.Nodes, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor), nil
		}
		err := patterns.Paginate(ctx, req.Limit, page, func(d client.MetafieldDefinitionFields) bool {
			return push(listResult(ctx, req, &d))
		})
		if err == nil {
			if diags := emptyListWarning(string(ownerType), listed); diags.WarningsCount() > 0 {
				push(list.ListResult{Diagnostics: diags})
			}
			return
		}
		var diags diag.Diagnostics
		// TODO(verify): which error, if any, a missing read scope gives; the dev store returned an empty list for CUSTOMER/ORDER instead.
		if client.IsAccessDenied(err) {
			diags.AddError("Failed to list "+what+"s",
				fmt.Sprintf("The app lacks the read scope for owner type %s: %s", ownerType, err))
		} else {
			diags.AddError("Failed to list "+what+"s", err.Error())
		}
		push(list.ListResult{Diagnostics: diags})
	}
}

// emptyListWarning warns on an empty listing, because Shopify returns one, not an error, when the app lacks the owner type's read scope.
func emptyListWarning(ownerType string, listed int) diag.Diagnostics {
	var diags diag.Diagnostics
	if listed == 0 {
		diags.AddWarning("No "+what+"s for owner type "+ownerType,
			fmt.Sprintf("Shopify listed no %ss for owner type %s. If some exist, the app may lack the read scope for that owner type; see the scope table in docs/bootstrapping.md.", what, ownerType))
	}
	return diags
}

// listResult fills the resource as Read would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, d *client.MetafieldDefinitionFields) list.ListResult {
	res := req.NewListResult(ctx)
	res.DisplayName = fmt.Sprintf("%s/%s/%s", d.OwnerType, d.Namespace, d.Key)
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(d.Id)})...)
	if req.IncludeResource {
		m := model{AllowDataDeletion: types.BoolValue(false)}
		res.Diagnostics.Append(SetFromAPI(ctx, &m.Definition, d)...)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
