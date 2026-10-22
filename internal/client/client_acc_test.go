package client

import (
	"context"
	"os"
	"testing"
)

// TestAccGetShop is a read-only smoke test of the auth path against the dev store.
func TestAccGetShop(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	c, err := New(Config{
		StoreDomain:  os.Getenv("SHOPIFY_STORE_DOMAIN"),
		ClientID:     os.Getenv("SHOPIFY_CLIENT_ID"),
		ClientSecret: os.Getenv("SHOPIFY_CLIENT_SECRET"),
		AccessToken:  os.Getenv("SHOPIFY_ACCESS_TOKEN"),
		MaxRetries:   3,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := GetShop(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Shop.MyshopifyDomain != os.Getenv("SHOPIFY_STORE_DOMAIN") {
		t.Errorf("myshopifyDomain = %q", resp.Shop.MyshopifyDomain)
	}
}
