package shoplocale

import (
	"strings"
	"testing"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func ptr[T any](v T) *T { return &v }

func TestFindLocale(t *testing.T) {
	ls := []client.ShopLocaleFields{{Locale: "en", Primary: true}, {Locale: "pt-BR"}}
	if l := findLocale(ls, "pt-BR"); l == nil || l.Locale != "pt-BR" {
		t.Errorf("pt-BR: got %+v", l)
	}
	// Shopify treats codes as case-sensitive: "pt-br" is invalid, so it must not match.
	if l := findLocale(ls, "pt-br"); l != nil {
		t.Errorf("pt-br: got %+v, want nil", l)
	}
	if l := findLocale(ls, "fr"); l != nil {
		t.Errorf("fr: got %+v, want nil", l)
	}
}

func TestNeedsPublish(t *testing.T) {
	cases := []struct {
		name    string
		want    *bool
		current bool
		expect  bool
	}{
		{"unset is unmanaged", nil, true, false},
		{"unset is unmanaged, unpublished", nil, false, false},
		{"same", ptr(true), true, false},
		{"publish", ptr(true), false, true},
		{"unpublish", ptr(false), true, true},
	}
	for _, c := range cases {
		if got := needsPublish(c.want, c.current); got != c.expect {
			t.Errorf("%s: got %v, want %v", c.name, got, c.expect)
		}
	}
}

func TestKeepReason(t *testing.T) {
	cases := []struct {
		name            string
		primary, adopt  bool
		wantKeep        bool
		wantReasonMatch string
	}{
		{"plain", false, false, false, ""},
		{"adopted", false, true, true, "adopt_existing"},
		{"primary", true, false, true, "primary"},
		{"adopted primary", true, true, true, "primary"},
	}
	for _, c := range cases {
		got := keepReason(c.primary, c.adopt)
		if (got != "") != c.wantKeep {
			t.Errorf("%s: reason %q, want keep=%v", c.name, got, c.wantKeep)
		}
		if c.wantReasonMatch != "" && !strings.Contains(got, c.wantReasonMatch) {
			t.Errorf("%s: reason %q lacks %q", c.name, got, c.wantReasonMatch)
		}
	}
}

func TestDisplayName(t *testing.T) {
	if got := displayName(&client.ShopLocaleFields{Locale: "fr", Name: "French"}); got != "fr (French)" {
		t.Errorf("got %q", got)
	}
	if got := displayName(&client.ShopLocaleFields{Locale: "en", Name: "English", Primary: true}); got != "en (English, primary)" {
		t.Errorf("got %q", got)
	}
}
