package privacysettings_test

import (
	"context"
	"strings"
	"testing"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func livePrivacy(t *testing.T) *client.GetPrivacySettingsPrivacySettings {
	t.Helper()
	res, err := client.GetPrivacySettings(context.Background(), acctest.Client(t))
	if err != nil {
		t.Fatal(err)
	}
	ps := res.PrivacySettings
	if ps.Banner == nil || ps.DataSaleOptOutPage == nil || ps.PrivacyPolicy == nil {
		t.Fatalf("privacy settings incomplete: %+v", ps)
	}
	return &ps
}

// requireBannerOff is the one precondition for every write step: consent policies can't be written while
// Shopify's banner is on, and the privacy-settings write steps would otherwise switch it off, which is one-way.
func requireBannerOff(t *testing.T) {
	t.Helper()
	if livePrivacy(t).Banner.AutoManaged {
		t.Skip("Shopify's cookie banner is on; consent policies can't be written and the settings write steps would turn it off for good")
	}
}

// consentRow fails the test when the key has no row.
func consentRow(t *testing.T, country, region string) *client.ConsentPolicyFields {
	t.Helper()
	cc := client.PrivacyCountryCode(country)
	var rg *string
	if region != "" {
		rg = &region
	}
	res, err := client.GetConsentPolicyByRegion(context.Background(), acctest.Client(t), cc, rg)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res.ConsentPolicy {
		if (r.RegionCode == nil) == (region == "") {
			return &r.ConsentPolicyFields
		}
	}
	t.Fatalf("no consent policy for %s/%s", country, region)
	return nil
}

// boolOr treats a null live value as false.
func boolOr(b *bool) bool { return b != nil && *b }

// writeConsent returns an error instead of failing so cleanups can report it their way.
func writeConsent(t *testing.T, country, region string, consentRequired, optOutRequired bool) error {
	t.Helper()
	cc := client.PrivacyCountryCode(country)
	in := client.ConsentPolicyInput{CountryCode: &cc, ConsentRequired: &consentRequired, DataSaleOptOutRequired: &optOutRequired}
	if region != "" {
		in.RegionCode = &region
	}
	res, err := client.UpdateConsentPolicy(context.Background(), acctest.Client(t), in)
	if err != nil {
		return err
	}
	if res.ConsentPolicyUpdate == nil {
		t.Fatal("consentPolicyUpdate returned no payload")
	}
	if errs := res.ConsentPolicyUpdate.UserErrors; len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Message
		}
		return &consentWriteError{strings.Join(msgs, "; ")}
	}
	return nil
}

type consentWriteError struct{ msg string }

func (e *consentWriteError) Error() string { return "consentPolicyUpdate: " + e.msg }

// restoreConsent puts the row's current values back after the test. A row written from `SD-` stays a written row.
func restoreConsent(t *testing.T, country, region string) {
	t.Helper()
	before := consentRow(t, country, region)
	cr, ds := boolOr(before.ConsentRequired), boolOr(before.DataSaleOptOutRequired)
	t.Cleanup(func() {
		now := consentRow(t, country, region)
		if boolOr(now.ConsentRequired) == cr && boolOr(now.DataSaleOptOutRequired) == ds {
			return
		}
		if err := writeConsent(t, country, region, cr, ds); err != nil {
			t.Error(err)
		}
	})
}
