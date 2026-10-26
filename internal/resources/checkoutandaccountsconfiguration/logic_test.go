package checkoutandaccountsconfiguration

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func cfg(id, name string, published bool) client.CheckoutAndAccountsConfigurationFields {
	return client.CheckoutAndAccountsConfigurationFields{Id: id, Name: name, IsPublished: published}
}

func TestMatchConfiguration(t *testing.T) {
	all := []client.CheckoutAndAccountsConfigurationFields{
		cfg("gid://1", "Shop configuration", true),
		cfg("gid://2", "Draft", false),
		cfg("gid://3", "Twin", false),
		cfg("gid://4", "Twin", false),
	}
	for name, tc := range map[string]struct {
		key     configKey
		wantID  string
		wantErr string
	}{
		"published":       {key: configKey{}, wantID: "gid://1"},
		"by name":         {key: configKey{name: "Draft"}, wantID: "gid://2"},
		"by exact name":   {key: configKey{name: "draft"}},
		"not found":       {key: configKey{name: "Nope"}},
		"ambiguous names": {key: configKey{name: "Twin"}, wantErr: "gid://3, gid://4"},
	} {
		got, err := matchConfiguration(all, tc.key)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s: want error naming %s, got %v", name, tc.wantErr, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		gotID := ""
		if got != nil {
			gotID = got.Id
		}
		if gotID != tc.wantID {
			t.Errorf("%s: got %q, want %q", name, gotID, tc.wantID)
		}
	}
	if _, err := matchConfiguration([]client.CheckoutAndAccountsConfigurationFields{cfg("gid://1", "A", true), cfg("gid://2", "B", true)}, configKey{}); err == nil {
		t.Error("two published configurations must be an error")
	}
}

func TestParseImportKey(t *testing.T) {
	if k := parseImportKey("published"); k != (configKey{}) {
		t.Errorf("published: got %+v", k)
	}
	if k := parseImportKey("terraform-provider-test configuration"); k.name != "terraform-provider-test configuration" {
		t.Errorf("name: got %+v", k)
	}
}

func TestBrandingErrorDiags(t *testing.T) {
	code := func(c client.CheckoutAndAccountsConfigurationUserErrorCode) *client.CheckoutAndAccountsConfigurationUserErrorCode {
		return &c
	}
	diags := brandingErrorDiags("Failed to write", []userError{
		{Code: code(client.CheckoutAndAccountsConfigurationUserErrorCodeInvalid), Field: []string{"configuration", "branding", "designTokens", "colors", "palette", "color1"},
			Message: "Color is invalid", Reason: "branding_invalid_hex_color"},
		{Code: code(client.CheckoutAndAccountsConfigurationUserErrorCodeTransient), Message: "Try again later", Reason: "transient"},
		{Code: code(client.CheckoutAndAccountsConfigurationUserErrorCodeNotFound), Field: []string{"id"}, Message: "Configuration not found", Reason: "not_found"},
	})
	errs := diags.Errors()
	if len(errs) != 3 {
		t.Fatalf("got %v", diags)
	}
	onBranding := func(d diag.Diagnostic) bool {
		p, ok := d.(diag.DiagnosticWithPath)
		return ok && p.Path().Equal(path.Root("branding"))
	}
	if !onBranding(errs[0]) {
		t.Errorf("a branding error must land on branding: %#v", errs[0])
	}
	for _, want := range []string{"Color is invalid", "designTokens.colors.palette.color1", "branding_invalid_hex_color"} {
		if !strings.Contains(errs[0].Detail(), want) {
			t.Errorf("detail %q must contain %q", errs[0].Detail(), want)
		}
	}
	if onBranding(errs[1]) || !strings.Contains(errs[1].Detail(), "apply again") {
		t.Errorf("TRANSIENT stays unmapped and asks to re-apply: %#v", errs[1])
	}
	if onBranding(errs[2]) || !strings.Contains(errs[2].Detail(), "not_found") {
		t.Errorf("a non-branding field stays unmapped and keeps its reason: %#v", errs[2])
	}
}
