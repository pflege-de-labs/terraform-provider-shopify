package privacysettings

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func ptr[T any](v T) *T { return &v }

func eq(t *testing.T, got, want any, msg ...any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v: got %+v, want %+v", msg, got, want)
	}
}

func TestFeaturesToDisable(t *testing.T) {
	allOn := privacyFlags{CookieBanner: true, DataSaleOptOutPage: true, PrivacyPolicy: true}
	tests := []struct {
		name string
		want privacyWant
		live privacyFlags
		out  []client.PrivacyFeaturesEnum
	}{
		{"nothing configured", privacyWant{}, allOn, nil},
		{"true matches live true", privacyWant{CookieBanner: ptr(true)}, allOn, nil},
		{"false already off", privacyWant{CookieBanner: ptr(false)}, privacyFlags{}, nil},
		{"one feature", privacyWant{CookieBanner: ptr(false)}, allOn, []client.PrivacyFeaturesEnum{client.PrivacyFeaturesEnumCookieBanner}},
		{"all in one call", privacyWant{CookieBanner: ptr(false), DataSaleOptOutPage: ptr(false), PrivacyPolicy: ptr(false)}, allOn, []client.PrivacyFeaturesEnum{
			client.PrivacyFeaturesEnumCookieBanner, client.PrivacyFeaturesEnumDataSaleOptOutPage, client.PrivacyFeaturesEnumPrivacyPolicy,
		}},
		{"only the ones still on", privacyWant{CookieBanner: ptr(false), PrivacyPolicy: ptr(false)}, privacyFlags{PrivacyPolicy: true}, []client.PrivacyFeaturesEnum{client.PrivacyFeaturesEnumPrivacyPolicy}},
		{"unmanaged ones are left alone", privacyWant{PrivacyPolicy: ptr(false)}, allOn, []client.PrivacyFeaturesEnum{client.PrivacyFeaturesEnumPrivacyPolicy}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eq(t, featuresToDisable(tt.want, tt.live), tt.out)
		})
	}
}

func TestEnableConflicts(t *testing.T) {
	eq(t, enableConflicts(privacyWant{}, privacyFlags{}), []string(nil), "unset")
	eq(t, enableConflicts(privacyWant{CookieBanner: ptr(true)}, privacyFlags{CookieBanner: true}), []string(nil), "true on true")
	eq(t, enableConflicts(privacyWant{CookieBanner: ptr(false)}, privacyFlags{}), []string(nil), "false on false")
	got := enableConflicts(
		privacyWant{CookieBanner: ptr(true), DataSaleOptOutPage: ptr(true), PrivacyPolicy: ptr(true)},
		privacyFlags{DataSaleOptOutPage: true},
	)
	eq(t, got, []string{"cookie_banner_auto_managed", "privacy_policy_auto_managed"})
}

func TestIsSystemDefault(t *testing.T) {
	eq(t, isSystemDefault("gid://shopify/ConsentPolicy/SD-DE"), true)
	eq(t, isSystemDefault("gid://shopify/ConsentPolicy/SD-USCA"), true)
	eq(t, isSystemDefault("gid://shopify/ConsentPolicy/1611960680779"), false)
}

func TestParseConsentKey(t *testing.T) {
	for in, want := range map[string]consentKey{
		"DE":      {Country: "DE"},
		"US/USCA": {Country: "US", Region: "USCA"},
	} {
		k, err := parseConsentKey(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		eq(t, k, want, in)
		eq(t, k.String(), in, "round trip")
	}
	for _, bad := range []string{"", "de", "DEU", "US/", "/USCA", "US/USCA/x", "gid://shopify/ConsentPolicy/1", "US/usca"} {
		if _, err := parseConsentKey(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestKeyFromSystemDefaultID(t *testing.T) {
	k, ok := keyFromSystemDefaultID("gid://shopify/ConsentPolicy/SD-DE")
	eq(t, ok, true)
	eq(t, k, consentKey{Country: "DE"})

	k, ok = keyFromSystemDefaultID("gid://shopify/ConsentPolicy/SD-USCA")
	eq(t, ok, true)
	eq(t, k, consentKey{Country: "US", Region: "USCA"})

	_, ok = keyFromSystemDefaultID("gid://shopify/ConsentPolicy/123")
	eq(t, ok, false)
}

func row(id, country string, region *string, cr, ds *bool) client.ConsentPolicyFields {
	c := client.PrivacyCountryCode(country)
	return client.ConsentPolicyFields{Id: id, CountryCode: &c, RegionCode: region, ConsentRequired: cr, DataSaleOptOutRequired: ds}
}

func TestFindConsent(t *testing.T) {
	const idDE, idUS, idCA = "gid://shopify/ConsentPolicy/1", "gid://shopify/ConsentPolicy/SD-US", "gid://shopify/ConsentPolicy/2"
	rows := []client.ConsentPolicyFields{
		row(idDE, "DE", nil, ptr(true), ptr(false)),
		row(idUS, "US", nil, ptr(false), ptr(false)),
		row(idCA, "US", ptr("USCA"), ptr(false), ptr(true)),
	}
	for key, want := range map[consentKey]string{
		// A country-level key must not match a regional row.
		{Country: "US"}:                 idUS,
		{Country: "US", Region: "USCA"}: idCA,
		{Country: "DE"}:                 idDE,
	} {
		got := findConsent(rows, key)
		if got == nil || got.Id != want {
			t.Errorf("%s: got %+v, want %s", key, got, want)
		}
	}
	for _, key := range []consentKey{{Country: "FR"}, {Country: "DE", Region: "DEXX"}} {
		if got := findConsent(rows, key); got != nil {
			t.Errorf("%s: got %+v, want nil", key, got)
		}
	}
	if findConsent(nil, consentKey{Country: "DE"}) != nil {
		t.Error("empty rows must give nil")
	}
}

func TestWithoutSystemDefaults(t *testing.T) {
	rows := []client.ConsentPolicyFields{
		row("gid://shopify/ConsentPolicy/1", "DE", nil, ptr(true), ptr(false)),
		row("gid://shopify/ConsentPolicy/SD-AT", "AT", nil, ptr(false), ptr(false)),
		row("gid://shopify/ConsentPolicy/SD-USCA", "US", ptr("USCA"), ptr(false), ptr(false)),
		row("gid://shopify/ConsentPolicy/2", "US", ptr("USCA"), ptr(false), ptr(true)),
	}
	got := withoutSystemDefaults(rows)
	if len(got) != 2 || got[0].Id != "gid://shopify/ConsentPolicy/1" || got[1].Id != "gid://shopify/ConsentPolicy/2" {
		t.Errorf("got %+v", got)
	}
	if len(rows) != 4 || rows[1].Id != "gid://shopify/ConsentPolicy/SD-AT" {
		t.Error("input must not be modified")
	}
}

func TestNeedsWrite(t *testing.T) {
	existing := row("gid://shopify/ConsentPolicy/1", "DE", nil, ptr(true), ptr(false))
	unset := row("gid://shopify/ConsentPolicy/SD-X", "AT", nil, nil, nil)
	tests := []struct {
		name     string
		existing *client.ConsentPolicyFields
		cr, ds   *bool
		want     bool
	}{
		{"nothing configured", &existing, nil, nil, false},
		{"both match", &existing, ptr(true), ptr(false), false},
		{"one matches, other unset", &existing, ptr(true), nil, false},
		{"consent differs", &existing, ptr(false), nil, true},
		{"opt-out differs", &existing, nil, ptr(true), true},
		{"no row", nil, nil, nil, true},
		{"null live value differs from a configured one", &unset, ptr(false), nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eq(t, needsWrite(tt.existing, tt.cr, tt.ds), tt.want)
		})
	}
}

func TestConsentInputOmitsUnsetValues(t *testing.T) {
	in := consentInput(consentKey{Country: "US", Region: "USCA"}, nil, ptr(true))
	eq(t, *in.CountryCode, client.PrivacyCountryCodeUs)
	eq(t, *in.RegionCode, "USCA")
	if in.ConsentRequired != nil {
		t.Error("unset means unmanaged: consentRequired must not be sent")
	}
	eq(t, *in.DataSaleOptOutRequired, true)

	in = consentInput(consentKey{Country: "DE"}, ptr(false), nil)
	if in.RegionCode != nil || in.DataSaleOptOutRequired != nil {
		t.Errorf("got %+v", in)
	}
	eq(t, *in.ConsentRequired, false)
}

func TestConsentErrorPath(t *testing.T) {
	tests := []struct {
		code client.ConsentPolicyErrorCode
		path path.Path
		ok   bool
	}{
		{client.ConsentPolicyErrorCodeRegionCodeMustMatchCountryCode, path.Root("region_code"), true},
		{client.ConsentPolicyErrorCodeRegionCodeRequired, path.Root("region_code"), true},
		{client.ConsentPolicyErrorCodeCountryCodeRequired, path.Root("country_code"), true},
		{client.ConsentPolicyErrorCodeShopifyCookieBannerNotDisabled, path.Empty(), false},
		{client.ConsentPolicyErrorCodeUnsuportedConsentPolicy, path.Empty(), false},
	}
	for _, tt := range tests {
		p, ok := consentErrorPath(tt.code)
		eq(t, ok, tt.ok, tt.code)
		eq(t, p, tt.path, tt.code)
	}
}

func TestKeepIDUnlessDefault(t *testing.T) {
	const numeric, sd = "gid://shopify/ConsentPolicy/1", "gid://shopify/ConsentPolicy/SD-DE"
	tests := []struct {
		name        string
		state, plan types.String
		want        types.String
	}{
		{"numeric id is kept", types.StringValue(numeric), types.StringUnknown(), types.StringValue(numeric)},
		{"system default stays unknown: its first write changes the ID", types.StringValue(sd), types.StringUnknown(), types.StringUnknown()},
		{"create has no state", types.StringNull(), types.StringUnknown(), types.StringUnknown()},
		{"a known plan value is left alone", types.StringValue(numeric), types.StringValue("x"), types.StringValue("x")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &planmodifier.StringResponse{PlanValue: tt.plan}
			keepIDUnlessDefault{}.PlanModifyString(context.Background(), planmodifier.StringRequest{StateValue: tt.state, PlanValue: tt.plan}, resp)
			eq(t, resp.PlanValue, tt.want)
		})
	}
}
