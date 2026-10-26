package shoppolicy

import (
	"testing"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func policy(id string, typ client.ShopPolicyType, body string) client.ShopPolicyFields {
	return client.ShopPolicyFields{Id: id, Type: typ, Title: "Title", Body: body}
}

func TestFindPolicy(t *testing.T) {
	ps := []client.ShopPolicyFields{
		policy("gid://1", client.ShopPolicyTypeLegalNotice, "<p>a</p>"),
		policy("gid://2", client.ShopPolicyTypeShippingPolicy, ""),
		policy("gid://3", client.ShopPolicyTypeRefundPolicy, "<p>b</p>"),
	}
	cases := []struct {
		name, id string
		typ      client.ShopPolicyType
		want     string
	}{
		{"by GID", "gid://3", client.ShopPolicyTypeLegalNotice, "gid://3"},
		{"stale GID falls back to type", "gid://gone", client.ShopPolicyTypeLegalNotice, "gid://1"},
		{"no GID uses type", "", client.ShopPolicyTypeRefundPolicy, "gid://3"},
		{"empty body counts as absent", "", client.ShopPolicyTypeShippingPolicy, ""},
		{"empty body by GID counts as absent", "gid://2", client.ShopPolicyTypeShippingPolicy, ""},
		{"missing type", "", client.ShopPolicyTypeTermsOfSale, ""},
	}
	for _, c := range cases {
		got := findPolicy(ps, c.id, c.typ)
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%s: got %+v, want nil", c.name, got)
		case c.want != "" && (got == nil || got.Id != c.want):
			t.Errorf("%s: got %+v, want %s", c.name, got, c.want)
		}
	}
}

func TestWithText(t *testing.T) {
	ps := []client.ShopPolicyFields{
		policy("1", client.ShopPolicyTypeLegalNotice, ""),
		policy("2", client.ShopPolicyTypeRefundPolicy, "<p>b</p>"),
	}
	got := withText(ps)
	if len(got) != 1 || got[0].Id != "2" {
		t.Errorf("got %+v, want only the policy with text", got)
	}
}

func TestPlanCreate(t *testing.T) {
	existing := &client.ShopPolicyFields{Body: "<p>old</p>"}
	cases := []struct {
		name     string
		existing *client.ShopPolicyFields
		body     string
		adopt    bool
		want     createAction
	}{
		{"no policy yet", nil, "<p>new</p>", false, writeBody},
		{"text there, not adopting", existing, "<p>new</p>", false, conflict},
		{"text there, same body, not adopting", existing, "<p>old</p>", false, conflict},
		{"adopt with other body", existing, "<p>new</p>", true, writeBody},
		{"adopt with same body", existing, "<p>old</p>", true, keepBody},
	}
	for _, c := range cases {
		if got := planCreate(c.existing, c.body, c.adopt); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDisplayName(t *testing.T) {
	p := client.ShopPolicyFields{Type: client.ShopPolicyTypeTermsOfService, Title: "Terms of service"}
	if got := displayName(&p); got != "TERMS_OF_SERVICE (Terms of service)" {
		t.Errorf("got %q", got)
	}
}

func TestParseImportKey(t *testing.T) {
	if got, err := parseImportKey("LEGAL_NOTICE"); err != nil || got != client.ShopPolicyTypeLegalNotice {
		t.Errorf("got %q, %v", got, err)
	}
	for _, bad := range []string{"", "legal_notice", "NOPE", "gid://shopify/ShopPolicy/1"} {
		if _, err := parseImportKey(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}
