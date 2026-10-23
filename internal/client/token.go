package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// refreshMargin renews a token this long before it expires, so long applies keep working.
const refreshMargin = 5 * time.Minute

// tokenSource holds the Admin API token in memory, fetching it via the client credentials grant unless static.
type tokenSource struct {
	url          string
	clientID     string
	clientSecret string
	static       bool
	http         *http.Client
	now          func() time.Time

	mu     sync.Mutex
	token  string
	expiry time.Time
}

// Token returns a valid token, fetching a new one if none is held or it is about to expire.
func (t *tokenSource) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.static || (t.token != "" && t.now().Before(t.expiry)) {
		return t.token, nil
	}

	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {t.clientID}, "client_secret": {t.clientSecret}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := t.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching access token: %w", err)
	}
	defer resp.Body.Close()

	// Only named fields are decoded, so neither token nor raw body can end up in an error.
	var body struct {
		AccessToken      string `json:"access_token"`
		ExpiresIn        int    `json:"expires_in"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body)
	if resp.StatusCode != http.StatusOK || body.AccessToken == "" {
		if body.Error != "" {
			return "", fmt.Errorf("fetching access token: HTTP %d: %s: %s", resp.StatusCode, body.Error, body.ErrorDescription)
		}
		return "", fmt.Errorf("fetching access token: HTTP %d (decode: %v)", resp.StatusCode, decodeErr)
	}

	t.token = body.AccessToken
	t.expiry = t.now().Add(time.Duration(body.ExpiresIn)*time.Second - refreshMargin)
	return t.token, nil
}

func (t *tokenSource) invalidate() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.token = ""
}
