package privacysettings

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

var (
	_ list.ListResourceWithConfigure = &PrivacySettingsList{}
	_ list.ListResourceWithConfigure = &ConsentPolicyList{}
)

func NewPrivacySettingsList() list.ListResource {
	return &PrivacySettingsList{}
}

func NewConsentPolicyList() list.ListResource {
	return &ConsentPolicyList{}
}

// PrivacySettingsList lists the shop's one set of privacy settings; Metadata and Configure come from the embedded resource.
type PrivacySettingsList struct {
	PrivacySettings
}

// ConsentPolicyList lists written consent policies; Metadata and Configure come from the embedded resource.
type ConsentPolicyList struct {
	ConsentPolicy
}

func (r *PrivacySettingsList) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists the shop's privacy settings: always one result.",
	}
}

func (r *PrivacySettingsList) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		read, err := readPrivacy(ctx, r.client)
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list "+privacyWhat, err.Error())
			push(list.ListResult{Diagnostics: diags})
			return
		}
		res := req.NewListResult(ctx)
		res.DisplayName = "Privacy settings"
		res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(read.ShopID)})...)
		if req.IncludeResource {
			var m privacyModel
			m.set(read)
			res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
		}
		push(res)
	}
}

func (r *ConsentPolicyList) ListResourceConfigSchema(ctx context.Context, req list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists the consent policies that aren't Shopify's untouched defaults (IDs prefixed `SD-`), so the 300-odd defaults don't bury them. " +
			"Importing a listed policy writes nothing. Once a region of a country has been written, all of that country's regional policies count as written (51 for the US) and are listed too.",
	}
}

func (r *ConsentPolicyList) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	stream.Results = func(push func(list.ListResult) bool) {
		res, err := client.ListConsentPolicies(ctx, r.client)
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Failed to list consent policies", err.Error())
			push(list.ListResult{Diagnostics: diags})
			return
		}
		all := make([]client.ConsentPolicyFields, len(res.ConsentPolicy))
		for i, p := range res.ConsentPolicy {
			all[i] = p.ConsentPolicyFields
		}
		rows := withoutSystemDefaults(all)
		for i := range rows {
			if req.Limit > 0 && int64(i) >= req.Limit {
				return
			}
			if !push(consentListResult(ctx, req, &rows[i])) {
				return
			}
		}
	}
}

func consentListResult(ctx context.Context, req list.ListRequest, row *client.ConsentPolicyFields) list.ListResult {
	var key consentKey
	if row.CountryCode != nil {
		key.Country = string(*row.CountryCode)
	}
	if row.RegionCode != nil {
		key.Region = *row.RegionCode
	}
	res := req.NewListResult(ctx)
	res.DisplayName = key.String()
	res.Diagnostics.Append(res.Identity.Set(ctx, patterns.IDIdentity{ID: types.StringValue(row.Id)})...)
	if req.IncludeResource {
		var m consentModel
		m.set(row, key)
		res.Diagnostics.Append(res.Resource.Set(ctx, &m)...)
	}
	return res
}
