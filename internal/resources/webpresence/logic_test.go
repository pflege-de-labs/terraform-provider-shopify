package webpresence

import (
	"testing"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func ptr[T any](v T) *T { return &v }

func TestCheckTarget(t *testing.T) {
	cases := []struct {
		name           string
		suffix, domain bool
		wantErr        bool
	}{
		{"suffix only", true, false, false},
		{"domain only", false, true, false},
		{"neither", false, false, true},
		{"both", true, true, true},
	}
	for _, c := range cases {
		if err := checkTarget(c.suffix, c.domain); (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", c.name, err, c.wantErr)
		}
	}
}

func TestParseImportKey(t *testing.T) {
	if k := parseImportKey("ch"); k != (key{suffix: "ch"}) {
		t.Errorf("suffix key = %+v", k)
	}
	if k := parseImportKey("shop.example.ch"); k != (key{host: "shop.example.ch"}) {
		t.Errorf("host key = %+v", k)
	}
}

func TestKeyMatches(t *testing.T) {
	sub := &client.WebPresenceFields{Id: "gid://shopify/MarketWebPresence/1", SubfolderSuffix: ptr("ch")}
	dom := &client.WebPresenceFields{
		Id:     "gid://shopify/MarketWebPresence/2",
		Domain: &client.WebPresenceFieldsDomain{Id: "gid://shopify/Domain/9", Host: "shop.example.ch"},
	}
	cases := []struct {
		name string
		k    key
		p    *client.WebPresenceFields
		want bool
	}{
		{"suffix hit", key{suffix: "ch"}, sub, true},
		{"suffix miss", key{suffix: "fr"}, sub, false},
		{"suffix vs domain presence", key{suffix: "ch"}, dom, false},
		{"host hit", key{host: "shop.example.ch"}, dom, true},
		{"host vs subfolder presence", key{host: "shop.example.ch"}, sub, false},
		{"domain id hit", key{domainID: "gid://shopify/Domain/9"}, dom, true},
		{"domain id miss", key{domainID: "gid://shopify/Domain/8"}, dom, false},
		{"empty key never matches", key{}, sub, false},
	}
	for _, c := range cases {
		if got := c.k.matches(c.p); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAlternateLocalesJSON(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		set  bool
		want string
	}{
		{"unmanaged is omitted", nil, false, ""},
		{"empty set clears", nil, true, "[]"},
		{"sorted", []string{"it", "fr"}, true, `["fr","it"]`},
	}
	for _, c := range cases {
		if got := string(alternateLocalesJSON(c.in, c.set)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestUpdateInput(t *testing.T) {
	in := updateInput("de", ptr("ch"), ptr("ch"), []string{"fr"}, true)
	if in.SubfolderSuffix != nil {
		t.Errorf("unchanged suffix must be omitted, got %q", *in.SubfolderSuffix)
	}
	if in.DefaultLocale == nil || *in.DefaultLocale != "de" || string(in.AlternateLocales) != `["fr"]` {
		t.Errorf("input = %+v", in)
	}
	in = updateInput("de", ptr("at"), ptr("ch"), nil, false)
	if in.SubfolderSuffix == nil || *in.SubfolderSuffix != "at" {
		t.Errorf("changed suffix must be sent, got %v", in.SubfolderSuffix)
	}
	if in.AlternateLocales != nil {
		t.Errorf("unmanaged alternates must be omitted, got %s", in.AlternateLocales)
	}
	in = updateInput("de", nil, nil, nil, false)
	if in.SubfolderSuffix != nil {
		t.Errorf("domain presence must not send a suffix")
	}
}
