package checkoutandaccountsconfiguration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// fakeAPI answers with canned JSON per operation and records the variables sent.
type fakeAPI struct {
	answers map[string][]string // per operation, one answer per call
	sent    []string
}

func (f *fakeAPI) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	vars, _ := json.Marshal(req.Variables)
	f.sent = append(f.sent, req.OpName+" "+string(vars))
	out := f.answers[req.OpName][0]
	f.answers[req.OpName] = f.answers[req.OpName][1:]
	return json.Unmarshal([]byte(out), resp.Data)
}

func nodeJSON(id, name, published string) string {
	return `{"id":"gid://shopify/CheckoutAndAccountsConfiguration/` + id + `","name":"` + name + `","isPublished":` + published + `,"branding":null}`
}

func TestUpdateBrandingSendsTheJSONAsWritten(t *testing.T) {
	f := &fakeAPI{answers: map[string][]string{"UpdateCheckoutAndAccountsConfiguration": {
		`{"checkoutAndAccountsConfigurationUpdate":{"configuration":` + nodeJSON("1", "Shop", "true") + `,"userErrors":[]}}`,
	}}}
	var diags diag.Diagnostics
	branding := `{"designTokens":{"cornerRadius":{"base":null}},"components":{"checkbox":{"cornerRadius":"LARGE"}}}`
	c := updateBranding(context.Background(), f, "gid://1", branding, &diags)
	if diags.HasError() || c == nil || c.Name != "Shop" {
		t.Fatalf("got %+v, %v", c, diags)
	}
	want := `UpdateCheckoutAndAccountsConfiguration {"id":"gid://1","configuration":{"branding":` + branding + `}}`
	if len(f.sent) != 1 || f.sent[0] != want {
		t.Errorf("sent %q,\nwant %q: nulls must be sent and overrides never", f.sent, want)
	}
}

func TestUpdateBrandingReportsErrors(t *testing.T) {
	for name, answer := range map[string]string{
		"null payload":       `{"checkoutAndAccountsConfigurationUpdate":null}`,
		"null configuration": `{"checkoutAndAccountsConfigurationUpdate":{"configuration":null,"userErrors":[]}}`,
		"user error": `{"checkoutAndAccountsConfigurationUpdate":{"configuration":null,"userErrors":[` +
			`{"code":"INVALID","field":["configuration","branding","designTokens","colors","palette","color1"],"message":"Invalid","reason":"branding_invalid_hex_color"}]}}`,
	} {
		f := &fakeAPI{answers: map[string][]string{"UpdateCheckoutAndAccountsConfiguration": {answer}}}
		var diags diag.Diagnostics
		if c := updateBranding(context.Background(), f, "gid://1", `{}`, &diags); c != nil || !diags.HasError() {
			t.Errorf("%s: want nil and an error, got %+v %v", name, c, diags)
		}
		if name == "user error" && !strings.Contains(diags.Errors()[0].Detail(), "branding_invalid_hex_color") {
			t.Errorf("user error lost its reason: %v", diags)
		}
	}
}

func TestFindByKey(t *testing.T) {
	page := func(nodes, hasNext, cursor string) string {
		return `{"checkoutAndAccountsConfigurations":{"nodes":[` + nodes + `],"pageInfo":{"hasNextPage":` + hasNext + `,"endCursor":` + cursor + `}}}`
	}
	f := &fakeAPI{answers: map[string][]string{"ListCheckoutAndAccountsConfigurations": {
		page(nodeJSON("1", "Shop", "true"), "true", `"c1"`),
		page(nodeJSON("2", "Draft", "false"), "false", "null"),
	}}}
	c, err := findByKey(context.Background(), f, configKey{name: "Draft"})
	if err != nil || c == nil || c.Name != "Draft" {
		t.Fatalf("got %+v, %v", c, err)
	}
	if !strings.Contains(f.sent[0], `"query":null`) || !strings.Contains(f.sent[1], `"after":"c1"`) {
		t.Errorf("a name lookup must page through everything unfiltered: %q", f.sent)
	}

	f = &fakeAPI{answers: map[string][]string{"ListCheckoutAndAccountsConfigurations": {page(nodeJSON("1", "Shop", "true"), "false", "null")}}}
	if c, err = findByKey(context.Background(), f, configKey{}); err != nil || c == nil || !c.IsPublished {
		t.Fatalf("got %+v, %v", c, err)
	}
	if !strings.Contains(f.sent[0], `"query":"is_published:true"`) {
		t.Errorf("the published lookup must filter: %q", f.sent)
	}

	f = &fakeAPI{answers: map[string][]string{"ListCheckoutAndAccountsConfigurations": {`{"checkoutAndAccountsConfigurations":null}`}}}
	if c, err = findByKey(context.Background(), f, configKey{}); err != nil || c != nil {
		t.Errorf("a null connection is not found, got %+v, %v", c, err)
	}
}

func TestImportModel(t *testing.T) {
	live := client.CheckoutAndAccountsConfigurationFields{Id: "gid://1", Name: "Shop", IsPublished: true,
		Branding: []byte(`{"components":{"checkbox":{"cornerRadius":"LARGE"},"control":null},"surfaces":{"checkout":null}}`)}
	m, err := importModel(&live)
	if err != nil {
		t.Fatal(err)
	}
	if m.Branding.ValueString() != `{"components":{"checkbox":{"cornerRadius":"LARGE"}}}` || m.Name.ValueString() != "Shop" || !m.IsPublished.ValueBool() {
		t.Errorf("got %+v", m)
	}
	empty := live
	empty.Branding = []byte(`{"components":{"checkbox":null}}`)
	if m, _ = importModel(&empty); !m.Branding.IsNull() {
		t.Errorf("empty live branding must import as unmanaged, got %s", m.Branding)
	}
}
