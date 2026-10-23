package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const shopJSON = `{"data":{"shop":{"id":"gid://shopify/Shop/1","name":"Dev","myshopifyDomain":"dev.myshopify.com","primaryDomain":{"host":"dev.example"},"currencyCode":"EUR","plan":{"publicDisplayName":"Development"},"enabledPresentmentCurrencies":["EUR"],"ianaTimezone":"Europe/Berlin"}}}`

const throttledJSON = `{"errors":[{"message":"Throttled","extensions":{"code":"THROTTLED"}}],"extensions":{"cost":{"requestedQueryCost":101,"actualQueryCost":null,"throttleStatus":{"maximumAvailable":1000.0,"currentlyAvailable":1,"restoreRate":50.0}}}}`

// fakeShopify serves the token endpoint and answers GraphQL calls from graphql in order, then repeats the last one.
type fakeShopify struct {
	t           *testing.T
	srv         *httptest.Server
	tokenCalls  atomic.Int32
	gqlCalls    atomic.Int32
	expiresIn   int
	tokenStatus int
	mu          sync.Mutex
	graphql     []func(w http.ResponseWriter, r *http.Request)
}

func newFakeShopify(t *testing.T, graphql ...func(w http.ResponseWriter, r *http.Request)) *fakeShopify {
	f := &fakeShopify{t: t, expiresIn: 86399, tokenStatus: http.StatusOK, graphql: graphql}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeShopify) handle(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/admin/oauth/access_token":
		n := f.tokenCalls.Add(1)
		if err := r.ParseForm(); err != nil {
			f.t.Error(err)
		}
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != "id" || r.Form.Get("client_secret") != "secret" {
			f.t.Errorf("unexpected token request form: %v", r.Form)
		}
		if f.tokenStatus != http.StatusOK {
			w.WriteHeader(f.tokenStatus)
			_, _ = io.WriteString(w, `{"error":"shop_not_permitted","error_description":"Client credentials cannot be performed on this shop."}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": fmt.Sprintf("tok%d", n), "scope": "", "expires_in": f.expiresIn})
	case "/admin/api/2026-10/graphql.json":
		i := int(f.gqlCalls.Add(1)) - 1
		f.mu.Lock()
		h := f.graphql[min(i, len(f.graphql)-1)]
		f.mu.Unlock()
		h(w, r)
	default:
		f.t.Errorf("unexpected path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeShopify) newClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	cfg.StoreDomain = f.srv.Listener.Addr().String()
	cfg.HTTPClient = f.srv.Client()
	if cfg.AccessToken == "" {
		cfg.ClientID, cfg.ClientSecret = "id", "secret"
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
	return c
}

func reply(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func TestThrottleWait(t *testing.T) {
	cases := []struct {
		name string
		ext  string
		want time.Duration
	}{
		{"needs points", `{"cost":{"requestedQueryCost":101,"throttleStatus":{"currentlyAvailable":1,"restoreRate":50}}}`, 2 * time.Second},
		{"rounds up", `{"cost":{"requestedQueryCost":120,"throttleStatus":{"currentlyAvailable":0,"restoreRate":50}}}`, 3 * time.Second},
		{"enough available waits minimum", `{"cost":{"requestedQueryCost":10,"throttleStatus":{"currentlyAvailable":100,"restoreRate":50}}}`, time.Second},
		{"missing cost falls back", `{}`, time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ext map[string]any
			if err := json.Unmarshal([]byte(tc.ext), &ext); err != nil {
				t.Fatal(err)
			}
			if got := throttleWait(ext); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	for attempt, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		if got := backoff(attempt); got != want {
			t.Errorf("attempt %d: got %s, want %s", attempt, got, want)
		}
	}
	if got := backoff(20); got != 30*time.Second {
		t.Errorf("cap: got %s", got)
	}
}

func TestMakeRequest_ThrottledThenOK(t *testing.T) {
	f := newFakeShopify(t, reply(200, throttledJSON), reply(200, shopJSON))
	c := f.newClient(t, Config{MaxRetries: 3})

	resp, err := GetShop(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Shop.Name != "Dev" || resp.Shop.PrimaryDomain.Host != "dev.example" {
		t.Errorf("typed data not populated after retry: %+v", resp.Shop)
	}
	if n := f.gqlCalls.Load(); n != 2 {
		t.Errorf("got %d calls, want 2", n)
	}
}

func TestMakeRequest_RetriesServerErrorsUpToMax(t *testing.T) {
	f := newFakeShopify(t, reply(503, `{"errors":"unavailable"}`))
	c := f.newClient(t, Config{MaxRetries: 2})

	if _, err := GetShop(context.Background(), c); err == nil {
		t.Fatal("want error")
	}
	if n := f.gqlCalls.Load(); n != 3 {
		t.Errorf("got %d calls, want 3 (1 + 2 retries)", n)
	}
}

func TestMakeRequest_DoesNotRetrySemanticErrors(t *testing.T) {
	f := newFakeShopify(t, reply(200, `{"errors":[{"message":"Access denied for shop field.","extensions":{"code":"ACCESS_DENIED"}}]}`))
	c := f.newClient(t, Config{MaxRetries: 5})

	_, err := GetShop(context.Background(), c)
	if err == nil || !strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("got %v", err)
	}
	if n := f.gqlCalls.Load(); n != 1 {
		t.Errorf("got %d calls, want 1", n)
	}
	if !IsAccessDenied(err) {
		t.Errorf("IsAccessDenied(%v) = false", err)
	}
	if IsAccessDenied(errors.New("Access denied")) {
		t.Error("IsAccessDenied matched a plain error")
	}
}

func TestMakeRequest_SendsTokenHeader(t *testing.T) {
	f := newFakeShopify(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Shopify-Access-Token"); got != "static" {
			t.Errorf("token header = %q", got)
		}
		_, _ = io.WriteString(w, shopJSON)
	})
	c := f.newClient(t, Config{AccessToken: "static"})

	if _, err := GetShop(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if n := f.tokenCalls.Load(); n != 0 {
		t.Errorf("static token mode fetched %d tokens", n)
	}
}

func TestToken_FetchedOnceForConcurrentCalls(t *testing.T) {
	f := newFakeShopify(t, reply(200, shopJSON))
	c := f.newClient(t, Config{Concurrency: 4})

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := GetShop(context.Background(), c); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := f.tokenCalls.Load(); n != 1 {
		t.Errorf("got %d token fetches, want 1", n)
	}
}

func TestToken_RefreshedBeforeExpiry(t *testing.T) {
	var seen []string
	f := newFakeShopify(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("X-Shopify-Access-Token"))
		_, _ = io.WriteString(w, shopJSON)
	})
	c := f.newClient(t, Config{})
	now := time.Now()
	c.tokens.now = func() time.Time { return now }

	for _, advance := range []time.Duration{0, 23 * time.Hour, 58 * time.Minute} {
		now = now.Add(advance)
		if _, err := GetShop(context.Background(), c); err != nil {
			t.Fatal(err)
		}
	}
	// 23h is still valid; 23h58m is inside the refresh margin of a 24h token.
	if strings.Join(seen, ",") != "tok1,tok1,tok2" {
		t.Errorf("tokens used: %v", seen)
	}
}

func TestToken_RefetchedOnUnauthorized(t *testing.T) {
	f := newFakeShopify(t, reply(401, `{"errors":"[API] Invalid API key or access token"}`), reply(200, shopJSON))
	c := f.newClient(t, Config{MaxRetries: 0})

	if _, err := GetShop(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if n := f.tokenCalls.Load(); n != 2 {
		t.Errorf("got %d token fetches, want 2", n)
	}
}

func TestToken_ErrorNamesReasonNotSecret(t *testing.T) {
	f := newFakeShopify(t, reply(200, shopJSON))
	f.tokenStatus = http.StatusBadRequest
	c := f.newClient(t, Config{MaxRetries: 3})

	_, err := GetShop(context.Background(), c)
	if err == nil || !strings.Contains(err.Error(), "shop_not_permitted") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error leaks secret: %v", err)
	}
	if n := f.tokenCalls.Load(); n != 1 {
		t.Errorf("token errors must not be retried, got %d fetches", n)
	}
}

func TestNew_Validation(t *testing.T) {
	cases := map[string]Config{
		"no domain":   {AccessToken: "x"},
		"no auth":     {StoreDomain: "d"},
		"both modes":  {StoreDomain: "d", AccessToken: "x", ClientID: "a", ClientSecret: "b"},
		"half creds":  {StoreDomain: "d", ClientID: "a"},
		"bad retries": {StoreDomain: "d", AccessToken: "x", MaxRetries: -1},
	}
	for name, cfg := range cases {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}
