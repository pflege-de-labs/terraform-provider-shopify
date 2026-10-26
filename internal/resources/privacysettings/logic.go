package privacysettings

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

// privacyFlags are the live `autoManaged` values; true means Shopify still runs the feature.
type privacyFlags struct {
	CookieBanner, DataSaleOptOutPage, PrivacyPolicy bool
}

// privacyWant holds the configured values; nil means unmanaged.
type privacyWant struct {
	CookieBanner, DataSaleOptOutPage, PrivacyPolicy *bool
}

// featuresToDisable lists every feature that config turns off and that is still on, so one call covers them all.
func featuresToDisable(want privacyWant, live privacyFlags) []client.PrivacyFeaturesEnum {
	var out []client.PrivacyFeaturesEnum
	for _, f := range []struct {
		want *bool
		live bool
		feat client.PrivacyFeaturesEnum
	}{
		{want.CookieBanner, live.CookieBanner, client.PrivacyFeaturesEnumCookieBanner},
		{want.DataSaleOptOutPage, live.DataSaleOptOutPage, client.PrivacyFeaturesEnumDataSaleOptOutPage},
		{want.PrivacyPolicy, live.PrivacyPolicy, client.PrivacyFeaturesEnumPrivacyPolicy},
	} {
		if f.want != nil && !*f.want && f.live {
			out = append(out, f.feat)
		}
	}
	return out
}

// enableConflicts names the attributes config sets to true while the feature is off: the API can't turn features on.
func enableConflicts(want privacyWant, live privacyFlags) []string {
	var out []string
	for _, f := range []struct {
		want *bool
		live bool
		attr string
	}{
		{want.CookieBanner, live.CookieBanner, "cookie_banner_auto_managed"},
		{want.DataSaleOptOutPage, live.DataSaleOptOutPage, "data_sale_opt_out_page_auto_managed"},
		{want.PrivacyPolicy, live.PrivacyPolicy, "privacy_policy_auto_managed"},
	} {
		if f.want != nil && *f.want && !f.live {
			out = append(out, f.attr)
		}
	}
	return out
}

const systemDefaultPrefix = "gid://shopify/ConsentPolicy/SD-"

// isSystemDefault reports Shopify's synthetic default rows; the schema documents the `SD-` prefix.
func isSystemDefault(id string) bool { return strings.HasPrefix(id, systemDefaultPrefix) }

// keyFromSystemDefaultID decodes `SD-US` and `SD-USCA`, because these IDs don't resolve through consentPolicy(id:).
func keyFromSystemDefaultID(id string) (consentKey, bool) {
	code, ok := strings.CutPrefix(id, systemDefaultPrefix)
	if !ok || len(code) < 2 {
		return consentKey{}, false
	}
	if len(code) == 2 {
		return consentKey{Country: code}, true
	}
	return consentKey{Country: code[:2], Region: code}, true
}

var (
	countryRe = regexp.MustCompile(`^[A-Z]{2}$`)
	regionRe  = regexp.MustCompile(`^[A-Z]{2}[A-Z0-9]+$`)
)

// consentKey is the natural key of a consent policy: a country, or a region within it.
type consentKey struct {
	Country, Region string
}

func (k consentKey) String() string {
	if k.Region == "" {
		return k.Country
	}
	return k.Country + "/" + k.Region
}

// parseConsentKey parses `DE` or `US/USCA`.
func parseConsentKey(s string) (consentKey, error) {
	const format = "<COUNTRY> or <COUNTRY>/<REGION>, such as DE or US/USCA"
	if !strings.Contains(s, "/") {
		if !countryRe.MatchString(s) {
			return consentKey{}, fmt.Errorf("import key %q must have the form %s", s, format)
		}
		return consentKey{Country: s}, nil
	}
	parts, err := patterns.SplitImportKey(s, "<COUNTRY>/<REGION>")
	if err != nil {
		return consentKey{}, err
	}
	if !countryRe.MatchString(parts[0]) || !regionRe.MatchString(parts[1]) {
		return consentKey{}, fmt.Errorf("import key %q must have the form %s", s, format)
	}
	return consentKey{Country: parts[0], Region: parts[1]}, nil
}

// findConsent matches the exact key: a country-level key never matches a regional row.
func findConsent(rows []client.ConsentPolicyFields, k consentKey) *client.ConsentPolicyFields {
	i := slices.IndexFunc(rows, func(r client.ConsentPolicyFields) bool {
		if r.CountryCode == nil || string(*r.CountryCode) != k.Country {
			return false
		}
		if r.RegionCode == nil {
			return k.Region == ""
		}
		return *r.RegionCode == k.Region
	})
	if i < 0 {
		return nil
	}
	return &rows[i]
}

func withoutSystemDefaults(rows []client.ConsentPolicyFields) []client.ConsentPolicyFields {
	return slices.DeleteFunc(slices.Clone(rows), func(r client.ConsentPolicyFields) bool { return isSystemDefault(r.Id) })
}

// needsWrite is false when every configured value already matches, so adopting such a row makes no write.
func needsWrite(existing *client.ConsentPolicyFields, consentRequired, optOutRequired *bool) bool {
	if existing == nil {
		return true
	}
	differs := func(want, live *bool) bool { return want != nil && (live == nil || *live != *want) }
	return differs(consentRequired, existing.ConsentRequired) || differs(optOutRequired, existing.DataSaleOptOutRequired)
}

// consentInput omits unset booleans: unset means unmanaged and an omitted value is left unchanged by the API.
func consentInput(k consentKey, consentRequired, optOutRequired *bool) client.ConsentPolicyInput {
	cc := client.PrivacyCountryCode(k.Country)
	in := client.ConsentPolicyInput{CountryCode: &cc, ConsentRequired: consentRequired, DataSaleOptOutRequired: optOutRequired}
	if k.Region != "" {
		in.RegionCode = &k.Region
	}
	return in
}

// consentErrorPath maps userErrors codes to attributes; the API returns `field: []`, so the code is all there is.
func consentErrorPath(code client.ConsentPolicyErrorCode) (path.Path, bool) {
	switch code {
	case client.ConsentPolicyErrorCodeRegionCodeMustMatchCountryCode, client.ConsentPolicyErrorCodeRegionCodeRequired:
		return path.Root("region_code"), true
	case client.ConsentPolicyErrorCodeCountryCodeRequired:
		return path.Root("country_code"), true
	}
	return path.Empty(), false
}
