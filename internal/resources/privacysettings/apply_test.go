package privacysettings

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// fakeAPI stands in for the Admin API, because privacyFeaturesDisable is one-way and acceptance tests can't exercise it again.
type fakeAPI struct {
	live          privacyFlags
	bannerEnabled bool
	ops           []string
	disabled      [][]string
	disableJSON   string // overrides the disable response when set
}

func (f *fakeAPI) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	f.ops = append(f.ops, req.OpName)
	var out string
	switch req.OpName {
	case "GetPrivacySettings":
		out = `{"shop":{"id":"gid://shopify/Shop/1"},"privacySettings":{` +
			`"banner":{"autoManaged":` + jsonBool(f.live.CookieBanner) + `,"enabled":` + jsonBool(f.bannerEnabled) + `},` +
			`"dataSaleOptOutPage":{"autoManaged":` + jsonBool(f.live.DataSaleOptOutPage) + `},` +
			`"privacyPolicy":{"autoManaged":` + jsonBool(f.live.PrivacyPolicy) + `}}}`
	case "DisablePrivacyFeatures":
		raw, _ := json.Marshal(req.Variables)
		var vars struct{ Features []string }
		_ = json.Unmarshal(raw, &vars)
		f.disabled = append(f.disabled, vars.Features)
		if f.disableJSON != "" {
			out = f.disableJSON
			break
		}
		for _, feat := range vars.Features {
			switch feat {
			case "COOKIE_BANNER":
				f.live.CookieBanner, f.bannerEnabled = false, false
			case "DATA_SALE_OPT_OUT_PAGE":
				f.live.DataSaleOptOutPage = false
			case "PRIVACY_POLICY":
				f.live.PrivacyPolicy = false
			}
		}
		feats, _ := json.Marshal(vars.Features)
		out = `{"privacyFeaturesDisable":{"featuresDisabled":` + string(feats) + `,"userErrors":[]}}`
	}
	return json.Unmarshal([]byte(out), resp.Data)
}

func jsonBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func newFake() *fakeAPI {
	return &fakeAPI{live: privacyFlags{CookieBanner: true, DataSaleOptOutPage: true, PrivacyPolicy: true}, bannerEnabled: true}
}

func TestApplyPrivacyDisablesWhatConfigTurnsOff(t *testing.T) {
	f := newFake()
	got, diags := applyPrivacy(context.Background(), f, privacyWant{CookieBanner: ptr(false)})
	if diags.HasError() {
		t.Fatal(diags)
	}
	eq(t, f.ops, []string{"GetPrivacySettings", "DisablePrivacyFeatures", "GetPrivacySettings"})
	eq(t, f.disabled, [][]string{{"COOKIE_BANNER"}}, "only the configured feature may be disabled")
	eq(t, got.Flags, privacyFlags{DataSaleOptOutPage: true, PrivacyPolicy: true})
	eq(t, got.BannerEnabled, false, "state must carry the re-read value")
	eq(t, got.ShopID, "gid://shopify/Shop/1")
}

func TestApplyPrivacyDisablesAllInOneCall(t *testing.T) {
	f := newFake()
	_, diags := applyPrivacy(context.Background(), f, privacyWant{CookieBanner: ptr(false), DataSaleOptOutPage: ptr(false), PrivacyPolicy: ptr(false)})
	if diags.HasError() {
		t.Fatal(diags)
	}
	eq(t, f.disabled, [][]string{{"COOKIE_BANNER", "DATA_SALE_OPT_OUT_PAGE", "PRIVACY_POLICY"}})
}

func TestApplyPrivacyWritesNothingWhenNothingChanges(t *testing.T) {
	for name, want := range map[string]privacyWant{
		"unmanaged":       {},
		"true on live on": {CookieBanner: ptr(true)},
	} {
		f := newFake()
		got, diags := applyPrivacy(context.Background(), f, want)
		if diags.HasError() {
			t.Fatalf("%s: %v", name, diags)
		}
		eq(t, f.ops, []string{"GetPrivacySettings"}, name)
		eq(t, got.Flags, privacyFlags{true, true, true}, name)
	}
}

func TestApplyPrivacyRefusesToEnable(t *testing.T) {
	f := newFake()
	f.live.CookieBanner = false
	_, diags := applyPrivacy(context.Background(), f, privacyWant{CookieBanner: ptr(true), PrivacyPolicy: ptr(false)})
	if !diags.HasError() {
		t.Fatal("want an error")
	}
	eq(t, f.ops, []string{"GetPrivacySettings"}, "must not disable anything either")
	d := diags.Errors()[0]
	if !strings.Contains(d.Detail(), "turned back on in the Shopify admin") || !strings.Contains(d.Detail(), "cookie_banner_auto_managed") {
		t.Errorf("got %q", d.Detail())
	}
}

func TestApplyPrivacyReportsUserErrorsAndNilPayload(t *testing.T) {
	f := newFake()
	f.disableJSON = `{"privacyFeaturesDisable":{"featuresDisabled":[],"userErrors":[{"code":"FAILED","field":null,"message":"Failed to disable privacy features."}]}}`
	_, diags := applyPrivacy(context.Background(), f, privacyWant{CookieBanner: ptr(false)})
	if !diags.HasError() || !strings.Contains(diags.Errors()[0].Detail(), "Failed to disable") {
		t.Errorf("got %v", diags)
	}

	f = newFake()
	f.disableJSON = `{"privacyFeaturesDisable":null}`
	_, diags = applyPrivacy(context.Background(), f, privacyWant{CookieBanner: ptr(false)})
	if !diags.HasError() {
		t.Error("a null payload must be an error, not a panic")
	}
}

func TestConsentErrorDiags(t *testing.T) {
	code := func(c client.ConsentPolicyErrorCode) *client.ConsentPolicyErrorCode { return &c }
	diags := consentErrorDiags("Failed to write", []consentUserError{
		{Code: code(client.ConsentPolicyErrorCodeRegionCodeMustMatchCountryCode), Field: []string{}, Message: "Region code must match the country code."},
		{Code: code(client.ConsentPolicyErrorCodeShopifyCookieBannerNotDisabled), Message: "Shopify's cookie banner must be disabled."},
		{Code: nil, Message: "Region code is not supported."},
	})
	errs := diags.Errors()
	if len(errs) != 3 {
		t.Fatalf("got %v", diags)
	}
	withPath, ok := errs[0].(diag.DiagnosticWithPath)
	if !ok || !withPath.Path().Equal(path.Root("region_code")) {
		t.Errorf("REGION_CODE_MUST_MATCH_COUNTRY_CODE must land on region_code: %#v", errs[0])
	}
	if !strings.Contains(errs[1].Detail(), "cookie_banner_auto_managed = false") || !strings.Contains(errs[1].Detail(), "depends_on") {
		t.Errorf("the banner error must say how to fix it: %q", errs[1].Detail())
	}
	if _, ok := errs[2].(diag.DiagnosticWithPath); ok || !strings.Contains(errs[2].Detail(), "not supported") {
		t.Errorf("an error without a code stays unmapped: %#v", errs[2])
	}
}
