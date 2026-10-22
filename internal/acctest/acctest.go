// Package acctest holds shared setup for acceptance tests in resource and data source packages.
package acctest

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/provider"
)

// ProtoV6ProviderFactories starts an in-process provider for each Terraform CLI command.
var ProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"shopify": providerserver.NewProtocol6WithError(provider.New("test")()),
}

// PreCheck fails fast when the dev store credentials are missing.
func PreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("SHOPIFY_STORE_DOMAIN") == "" {
		t.Fatal("SHOPIFY_STORE_DOMAIN must be set to the disposable dev store")
	}
	if os.Getenv("SHOPIFY_ACCESS_TOKEN") == "" && (os.Getenv("SHOPIFY_CLIENT_ID") == "" || os.Getenv("SHOPIFY_CLIENT_SECRET") == "") {
		t.Fatal("set SHOPIFY_CLIENT_ID and SHOPIFY_CLIENT_SECRET, or SHOPIFY_ACCESS_TOKEN")
	}
}
