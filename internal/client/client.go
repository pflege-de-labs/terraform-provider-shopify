// Package client talks to the Shopify Admin GraphQL API: auth, version pinning, throttling and retries.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// DefaultAPIVersion is the Admin API version the generated code is built against.
const DefaultAPIVersion = "2026-10"

// DefaultConcurrency caps in-flight requests when Config.Concurrency is unset.
const DefaultConcurrency = 2

// Config selects the store and one auth mode: ClientID + ClientSecret, or AccessToken.
type Config struct {
	StoreDomain  string
	ClientID     string
	ClientSecret string
	AccessToken  string
	APIVersion   string
	MaxRetries   int
	Concurrency  int
	HTTPClient   *http.Client
}

// Client implements graphql.Client for the generated operations.
type Client struct {
	gql        graphql.Client
	tokens     *tokenSource
	sem        chan struct{}
	maxRetries int
	sleep      func(context.Context, time.Duration) error
}

var _ graphql.Client = (*Client)(nil)

// New validates cfg and returns a client. No request is made until the first call.
func New(cfg Config) (*Client, error) {
	creds := cfg.ClientID != "" || cfg.ClientSecret != ""
	switch {
	case cfg.StoreDomain == "":
		return nil, errors.New("store domain is required")
	case creds && cfg.AccessToken != "":
		return nil, errors.New("set either client_id and client_secret, or access_token, not both")
	case creds && (cfg.ClientID == "" || cfg.ClientSecret == ""):
		return nil, errors.New("client_id and client_secret must be set together")
	case !creds && cfg.AccessToken == "":
		return nil, errors.New("set client_id and client_secret, or access_token")
	case cfg.MaxRetries < 0:
		return nil, errors.New("max_retries must not be negative")
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = DefaultAPIVersion
	}
	if cfg.Concurrency < 1 {
		cfg.Concurrency = DefaultConcurrency
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}

	tokens := &tokenSource{
		url:          fmt.Sprintf("https://%s/admin/oauth/access_token", cfg.StoreDomain),
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		token:        cfg.AccessToken,
		static:       cfg.AccessToken != "",
		http:         cfg.HTTPClient,
		now:          time.Now,
	}
	endpoint := fmt.Sprintf("https://%s/admin/api/%s/graphql.json", cfg.StoreDomain, cfg.APIVersion)
	return &Client{
		gql:        graphql.NewClient(endpoint, &authDoer{http: cfg.HTTPClient, tokens: tokens}),
		tokens:     tokens,
		sem:        make(chan struct{}, cfg.Concurrency),
		maxRetries: cfg.MaxRetries,
		sleep:      sleepCtx,
	}, nil
}

// MakeRequest sends req, retrying throttling and 429/5xx responses up to MaxRetries times.
func (c *Client) MakeRequest(ctx context.Context, req *graphql.Request, resp *graphql.Response) error {
	data := resp.Data
	refetched := false
	for attempt := 0; ; attempt++ {
		// A throttled reply decodes "data": null over the caller's pointer, and errors from it would survive a later success.
		resp.Data, resp.Errors, resp.Extensions = data, nil, nil
		err := c.do(ctx, req, resp)
		if err == nil {
			return nil
		}

		var httpErr *graphql.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnauthorized && !c.tokens.static && !refetched {
			refetched = true
			c.tokens.invalidate()
			attempt--
			continue
		}
		wait, retry := retryWait(err, resp, attempt)
		if !retry || attempt >= c.maxRetries {
			return err
		}
		tflog.Debug(ctx, "retrying Shopify request", map[string]any{"operation": req.OpName, "attempt": attempt + 1, "wait": wait.String(), "error": err.Error()})
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
	}
}

func (c *Client) do(ctx context.Context, req *graphql.Request, resp *graphql.Response) error {
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.sem }()
	return c.gql.MakeRequest(ctx, req, resp)
}

// retryWait reports whether err is transient and how long to wait before the next attempt.
func retryWait(err error, resp *graphql.Response, attempt int) (time.Duration, bool) {
	var httpErr *graphql.HTTPError
	if errors.As(err, &httpErr) {
		return backoff(attempt), httpErr.StatusCode == http.StatusTooManyRequests || httpErr.StatusCode >= 500
	}
	for _, e := range resp.Errors {
		if e.Extensions["code"] == "THROTTLED" {
			return throttleWait(resp.Extensions), true
		}
	}
	return 0, false
}

// IsAccessDenied reports whether Shopify rejected the request for a missing access scope.
func IsAccessDenied(err error) bool {
	var errs gqlerror.List
	if !errors.As(err, &errs) {
		return false
	}
	for _, e := range errs {
		if e.Extensions["code"] == "ACCESS_DENIED" {
			return true
		}
	}
	return false
}

func backoff(attempt int) time.Duration {
	if attempt >= 5 {
		return 30 * time.Second
	}
	return min(time.Second<<attempt, 30*time.Second)
}

// throttleWait returns the time until the leaky bucket holds enough points for the request again.
func throttleWait(ext map[string]any) time.Duration {
	var cost struct {
		Cost struct {
			RequestedQueryCost float64 `json:"requestedQueryCost"`
			ThrottleStatus     struct {
				CurrentlyAvailable float64 `json:"currentlyAvailable"`
				RestoreRate        float64 `json:"restoreRate"`
			} `json:"throttleStatus"`
		} `json:"cost"`
	}
	raw, _ := json.Marshal(ext)
	if json.Unmarshal(raw, &cost) != nil || cost.Cost.ThrottleStatus.RestoreRate <= 0 {
		return time.Second
	}
	missing := cost.Cost.RequestedQueryCost - cost.Cost.ThrottleStatus.CurrentlyAvailable
	secs := math.Ceil(missing / cost.Cost.ThrottleStatus.RestoreRate)
	return time.Duration(max(secs, 1)) * time.Second
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type authDoer struct {
	http   *http.Client
	tokens *tokenSource
}

func (d *authDoer) Do(req *http.Request) (*http.Response, error) {
	token, err := d.tokens.Token(req.Context())
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Shopify-Access-Token", token)
	return d.http.Do(req)
}
