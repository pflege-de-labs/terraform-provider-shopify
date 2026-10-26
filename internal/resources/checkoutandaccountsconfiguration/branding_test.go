package checkoutandaccountsconfiguration

import (
	"strings"
	"testing"
)

// The dev store's branding as read: three leaves, the rest null or empty containers.
const devStoreLive = `{"components":{"checkbox":null,"control":null},"designTokens":{"colors":{"palette":{"color1":null}},"cornerRadius":{"base":null,"large":null,"small":null},"typography":null},` +
	`"surfaces":{"checkout":null,"customerAccounts":{"components":{"header":{"colors":{"base":{"accent":null,"background":"#FFFFFF"}},"logo":null},"main":{"colors":{"base":{"background":"#FFFFFF"}}}}},` +
	`"signIn":{"components":{"header":{"logo":null},"main":{"colors":{"base":{"background":"#FFFFFF"}}}}}}}`

const devStoreNormalised = `{"surfaces":{"customerAccounts":{"components":{"header":{"colors":{"base":{"background":"#FFFFFF"}}},"main":{"colors":{"base":{"background":"#FFFFFF"}}}}},` +
	`"signIn":{"components":{"main":{"colors":{"base":{"background":"#FFFFFF"}}}}}}}`

func TestLiveBranding(t *testing.T) {
	for name, tc := range map[string]struct{ live, want string }{
		"drops nulls and empty containers": {devStoreLive, devStoreNormalised},
		"null branding":                    {`null`, ""},
		"missing branding":                 {``, ""},
		"only empty containers":            {`{"components":{"checkbox":null},"designTokens":{"cornerRadius":{"base":null}}}`, ""},
		"keeps false and zero":             {`{"a":{"b":false,"c":0,"d":""}}`, `{"a":{"b":false,"c":0,"d":""}}`},
	} {
		got, err := liveBranding([]byte(tc.live))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, tc.want)
		}
	}
}

func TestProjectBranding(t *testing.T) {
	for name, tc := range map[string]struct{ state, live, want string }{
		"equal keeps the state string": {
			state: `{"designTokens": {"cornerRadius": {"base": 6}}}`,
			live:  `{"designTokens":{"cornerRadius":{"base":6,"small":null}}}`,
			want:  `{"designTokens": {"cornerRadius": {"base": 6}}}`,
		},
		"Review Focus 2: unmanaged paths are ignored": {
			state: `{"designTokens":{"cornerRadius":{"base":6}}}`,
			live:  `{"designTokens":{"cornerRadius":{"base":6,"small":3}},"components":{"checkbox":{"cornerRadius":"LARGE"}}}`,
			want:  `{"designTokens":{"cornerRadius":{"base":6}}}`,
		},
		"hex colours compare case-insensitively": {
			state: `{"designTokens":{"colors":{"palette":{"color1":"#1a4d8f"}}}}`,
			live:  `{"designTokens":{"colors":{"palette":{"color1":"#1A4D8F"}}}}`,
			want:  `{"designTokens":{"colors":{"palette":{"color1":"#1a4d8f"}}}}`,
		},
		"numbers compare numerically": {
			state: `{"a":{"b":6.0,"c":1.50}}`,
			live:  `{"a":{"b":6,"c":1.5}}`,
			want:  `{"a":{"b":6.0,"c":1.50}}`,
		},
		"a changed leaf shows the live value": {
			state: `{"designTokens":{"cornerRadius":{"base":6,"small":2}}}`,
			live:  `{"designTokens":{"cornerRadius":{"base":8,"small":2,"large":9}}}`,
			want:  `{"designTokens":{"cornerRadius":{"base":8,"small":2}}}`,
		},
		"a removed leaf becomes null": {
			state: `{"designTokens":{"cornerRadius":{"base":6}}}`,
			live:  `{"designTokens":{"cornerRadius":{"base":null}}}`,
			want:  `{"designTokens":{"cornerRadius":{"base":null}}}`,
		},
		"null in state matches an absent value": {
			state: `{"designTokens":{"cornerRadius":{"base":null}},"components":{"checkbox":null}}`,
			live:  `{"designTokens":{"cornerRadius":{"base":null,"small":null}},"components":{"checkbox":{"cornerRadius":null}}}`,
			want:  `{"designTokens":{"cornerRadius":{"base":null}},"components":{"checkbox":null}}`,
		},
		"null in state against a live subtree shows the subtree": {
			state: `{"components":{"checkbox":null}}`,
			live:  `{"components":{"checkbox":{"cornerRadius":"LARGE"}}}`,
			want:  `{"components":{"checkbox":{"cornerRadius":"LARGE"}}}`,
		},
		"different strings are drift, even hex-like ones of other lengths": {
			state: `{"a":{"b":"#abc","c":"LARGE"}}`,
			live:  `{"a":{"b":"#ABC","c":"large"}}`,
			want:  `{"a":{"b":"#ABC","c":"large"}}`,
		},
		"an empty object in state is kept": {
			state: `{"components":{}}`,
			live:  `{"components":{"checkbox":{"cornerRadius":"LARGE"}}}`,
			want:  `{"components":{}}`,
		},
		"write-only paths keep the state value": {
			state: `{"components":{"favicon":{"mediaImageId":"gid://shopify/MediaImage/1"},"merchandiseThumbnail":{"aspectRatio":1.5,"fit":"COVER"}},` +
				`"designTokens":{"typography":{"primary":{"shopifyFontGroup":{"baseFontHandle":"assistant_n4"}},"secondary":null}}}`,
			live: `{"components":{"merchandiseThumbnail":{"fit":"COVER"}}}`,
			want: `{"components":{"favicon":{"mediaImageId":"gid://shopify/MediaImage/1"},"merchandiseThumbnail":{"aspectRatio":1.5,"fit":"COVER"}},` +
				`"designTokens":{"typography":{"primary":{"shopifyFontGroup":{"baseFontHandle":"assistant_n4"}},"secondary":null}}}`,
		},
		"a write-only path doesn't hide drift next to it": {
			state: `{"components":{"merchandiseThumbnail":{"aspectRatio":1.5,"fit":"COVER"}}}`,
			live:  `{"components":{"merchandiseThumbnail":{"fit":"CONTAIN"}}}`,
			want:  `{"components":{"merchandiseThumbnail":{"aspectRatio":1.5,"fit":"CONTAIN"}}}`,
		},
		"nothing live at all": {
			state: `{"designTokens":{"cornerRadius":{"base":6}}}`,
			live:  `null`,
			want:  `{"designTokens":{"cornerRadius":{"base":null}}}`,
		},
	} {
		got, err := projectBranding(tc.state, []byte(tc.live))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// Equal results must return the state string itself, so formatting in state never changes.
		if brandingEqual(tc.state, tc.want) && got != tc.state {
			t.Errorf("%s: state string must be kept unchanged, got %s", name, got)
		}
		if !brandingEqual(got, tc.want) {
			t.Errorf("%s:\n got %s\nwant %s", name, got, tc.want)
		}
	}
}

func TestProjectBrandingRejectsBadState(t *testing.T) {
	if _, err := projectBranding(`not json`, []byte(`{}`)); err == nil {
		t.Error("want an error for invalid state")
	}
	if _, err := projectBranding(`{}`, []byte(`{`)); err == nil {
		t.Error("want an error for invalid live JSON")
	}
}

func TestBrandingEqual(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{`{"a":1,"b":{"c":"x"}}`, "{\n  \"b\": {\"c\": \"x\"},\n  \"a\": 1.0\n}", true},
		{`{"a":"#aabbcc"}`, `{"a":"#AABBCC"}`, true},
		{`{"a":"#aabbcc"}`, `{"a":"#AABBCD"}`, false},
		{`{"a":"abc"}`, `{"a":"ABC"}`, false},
		{`{"a":null}`, `{}`, false},
		{`{"a":null}`, `{"a":null}`, true},
		{`{"a":{}}`, `{}`, true},
		{`{"a":1}`, `{"a":1,"b":2}`, false},
		{`{"a":1}`, `{"a":"1"}`, false},
		{`{"a":true}`, `{"a":true}`, true},
		{`{"a":{"b":1}}`, `{"a":1}`, false},
		{`not json`, `not json`, true},
		{`not json`, `{}`, false},
	} {
		if got := brandingEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("brandingEqual(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
		if got := brandingEqual(tc.b, tc.a); got != tc.want {
			t.Errorf("brandingEqual must be symmetric: (%s, %s) = %v", tc.b, tc.a, got)
		}
	}
}

func TestIsWriteOnly(t *testing.T) {
	for p, want := range map[string]bool{
		"components.favicon":                                         true,
		"components.favicon.mediaImageId":                            true,
		"components.header.logo.image.mediaImageId":                  true,
		"components.header.logo.maxWidth":                            false,
		"surfaces.checkout.components.header.cartLink.image":         true,
		"surfaces.checkout.components.orderSummary.backgroundImage":  true,
		"surfaces.signIn.components.main.backgroundImage":            true,
		"surfaces.customerAccounts.components.header.logo.image":     true,
		"designTokens.typography.primary.customFontGroup.base":       true,
		"designTokens.typography.secondary":                          true,
		"designTokens.typography.size":                               false,
		"components.merchandiseThumbnail.aspectRatio":                true,
		"components.merchandiseThumbnail.aspectRatioX":               false,
		"components.merchandiseThumbnail":                            false,
		"surfaces.checkout.components.orderSummary.backgroundImageX": false,
	} {
		if got := isWriteOnly(strings.Split(p, ".")); got != want {
			t.Errorf("isWriteOnly(%s) = %v, want %v", p, got, want)
		}
	}
}

func TestBrandingObjectError(t *testing.T) {
	for in, wantErr := range map[string]bool{
		`{}`:                   false,
		`{"a":{"b":null}}`:     false,
		`[]`:                   true,
		`null`:                 true,
		`"x"`:                  true,
		`{"a":1} trailing`:     true,
		`{"a":`:                true,
		` {"a": 1} `:           false,
		`{"a":1}{"b":2}`:       true,
		`{"components":"red"}`: false,
	} {
		if err := brandingObjectError(in); (err != nil) != wantErr {
			t.Errorf("brandingObjectError(%q) = %v, want error %v", in, err, wantErr)
		}
	}
}
