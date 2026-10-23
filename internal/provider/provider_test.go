// Copyright IBM Corp. 2021, 2026
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestClientConfig(t *testing.T) {
	env := map[string]string{
		"SHOPIFY_STORE_DOMAIN":  "env.myshopify.com",
		"SHOPIFY_CLIENT_ID":     "env-id",
		"SHOPIFY_CLIENT_SECRET": "env-secret",
	}
	data := ShopifyProviderModel{
		StoreDomain:  types.StringValue("cfg.myshopify.com"),
		ClientID:     types.StringNull(),
		ClientSecret: types.StringNull(),
		AccessToken:  types.StringNull(),
		APIVersion:   types.StringNull(),
		MaxRetries:   types.Int64Null(),
		Concurrency:  types.Int64Value(4),
	}

	cfg, diags := clientConfig(data, func(k string) string { return env[k] })
	if diags.HasError() {
		t.Fatal(diags)
	}
	if cfg.StoreDomain != "cfg.myshopify.com" || cfg.ClientID != "env-id" || cfg.ClientSecret != "env-secret" || cfg.AccessToken != "" {
		t.Errorf("config did not win over env, or env not used: %+v", cfg)
	}
	if cfg.MaxRetries != defaultMaxRetries || cfg.Concurrency != 4 || cfg.APIVersion != "" {
		t.Errorf("defaults: %+v", cfg)
	}

	data.MaxRetries = types.Int64Value(0)
	data.AccessToken = types.StringUnknown()
	cfg, diags = clientConfig(data, func(k string) string { return env[k] })
	if cfg.MaxRetries != 0 {
		t.Errorf("explicit max_retries = 0 not kept: %d", cfg.MaxRetries)
	}
	if !diags.HasError() {
		t.Error("unknown access_token must be an error")
	}
}

func TestClientConfig_StoreDomain(t *testing.T) {
	for domain, wantErr := range map[string]bool{
		"shop.myshopify.com":         false,
		"":                           false, // reported by client.New
		"shop.staging.p4e.io":        true,
		"https://shop.myshopify.com": true,
	} {
		data := ShopifyProviderModel{StoreDomain: types.StringValue(domain), ClientID: types.StringNull(), ClientSecret: types.StringNull(), AccessToken: types.StringNull(), MaxRetries: types.Int64Null()}
		_, diags := clientConfig(data, func(string) string { return "" })
		if diags.HasError() != wantErr {
			t.Errorf("%q: error = %v, want %v", domain, diags.HasError(), wantErr)
		}
	}
}
