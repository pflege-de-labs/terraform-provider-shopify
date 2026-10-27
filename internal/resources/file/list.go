package file

import (
	"context"
	"fmt"
	"slices"

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

// ListResource lists files; Metadata and Configure come from the embedded Resource.
type ListResource struct {
	Resource
}

type listConfig struct {
	Query types.String `tfsdk:"query"`
}

func (r *ListResource) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists the files of the store, 250 per request. Generated config carries no `source` for hosted files, so it plans as import-only. " +
			"Files that are still processing or failed have no URL, and files whose filename this provider rejects (no extension, spaces, non-ASCII characters) can't be imported by filename; both are skipped and don't count toward the limit.",
		Attributes: map[string]listschema.Attribute{
			"query": listschema.StringAttribute{
				MarkdownDescription: "A Shopify search filter, such as `media_type:IMAGE` or `filename:logo`, to import a large library in parts.",
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
		page := func(after *string) ([]apiFile, *string, error) {
			res, err := client.ListFiles(ctx, r.client, after, patterns.ConfiguredString(cfg.Query))
			if err != nil {
				return nil, nil, err
			}
			fs, err := decodeFiles(res.Files.Nodes)
			return filterListable(fs), patterns.NextCursor(res.Files.PageInfo.HasNextPage, res.Files.PageInfo.EndCursor), err
		}
		err := patterns.Paginate(ctx, req.Limit, page, func(f apiFile) bool {
			return push(listResult(ctx, req, &f))
		})
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list files", err.Error())
			push(list.ListResult{Diagnostics: diags})
		}
	}
}

// filterListable drops what could not be imported by filename: no URL yet, or a filename the validator rejects.
// It runs before Paginate, so skipped files don't count toward the limit.
func filterListable(files []apiFile) []apiFile {
	return slices.DeleteFunc(slices.Clone(files), func(f apiFile) bool {
		if f.Typename == "ExternalVideo" {
			return false
		}
		name, ok := f.filename()
		return !ok || validateFilename(name) != nil
	})
}

// listResult fills the resource as an import would; provider-only attributes get their schema defaults.
func listResult(ctx context.Context, req list.ListRequest, f *apiFile) list.ListResult {
	res := req.NewListResult(ctx)
	ct, _ := f.contentType()
	if ct == typeExternalVideo {
		res.DisplayName = fmt.Sprintf("%s (%s)", f.OriginUrl, ct)
	} else {
		name, _ := f.filename()
		res.DisplayName = fmt.Sprintf("%s (%s)", name, ct)
	}
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(f.Id)})...)
	if req.IncludeResource {
		m := model{
			AdoptExisting: types.BoolValue(false), AllowDataDeletion: types.BoolValue(false),
			Timeouts: patterns.TimeoutsNull("create", "update"),
			Filename: types.StringNull(), Source: types.StringNull(), SourceHash: types.StringNull(), Alt: types.StringNull(),
		}
		setFromAPI(&m, f, nil)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
