package patterns

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestValidateHandle(t *testing.T) {
	cases := []struct {
		handle  string
		wantErr bool
	}{
		{"bettschutzeinlage", false},
		{"tf-design-probe_3", false},
		{"größe-m", false},
		{"TF-Design", true},
		{"has space", true},
		{"tab\there", true},
		{"Ä-upper", true},
		{"", true},
	}
	for _, c := range cases {
		if err := ValidateHandle(c.handle); (err != nil) != c.wantErr {
			t.Errorf("ValidateHandle(%q) = %v, want error: %t", c.handle, err, c.wantErr)
		}
	}
}

func TestKeepOrTake(t *testing.T) {
	cfg := types.StringValue("<p>Hi<br>there<b>bold</p>")
	repaired := "<p>Hi<br>there<b>bold</b></p>"
	edited := "<p>edited in the admin</p>"
	cases := []struct {
		name       string
		configured types.String
		written    *string
		live       string
		want       types.String
	}{
		{"live is what Shopify returned at write: keep config", cfg, &repaired, repaired, cfg},
		{"admin edit: take live", cfg, &repaired, edited, types.StringValue(edited)},
		{"no stored write (import): take live", cfg, nil, repaired, types.StringValue(repaired)},
		{"unset: take live", types.StringNull(), &repaired, repaired, types.StringValue(repaired)},
		{"unknown (create without config): take live", types.StringUnknown(), nil, repaired, types.StringValue(repaired)},
	}
	for _, c := range cases {
		if got := KeepOrTake(c.configured, c.written, c.live); !got.Equal(c.want) {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

type fakePrivate map[string][]byte

func (p fakePrivate) SetKey(_ context.Context, k string, v []byte) diag.Diagnostics {
	p[k] = v
	return nil
}

func (p fakePrivate) GetKey(_ context.Context, k string) ([]byte, diag.Diagnostics) {
	return p[k], nil
}

func TestWrittenHTMLRoundTrip(t *testing.T) {
	ctx := context.Background()
	p := fakePrivate{}
	if got, d := WrittenHTML(ctx, p); got != nil || d.HasError() {
		t.Fatalf("nothing stored: got %v %v, want nil", got, d)
	}
	if d := RememberWrittenHTML(ctx, p, `<p>"x"</p>`); d.HasError() {
		t.Fatal(d)
	}
	if got, d := WrittenHTML(ctx, p); got == nil || *got != `<p>"x"</p>` || d.HasError() {
		t.Errorf("got %v %v", got, d)
	}
}
