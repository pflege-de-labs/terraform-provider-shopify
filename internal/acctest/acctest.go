// Package acctest holds shared setup for acceptance tests in resource and data source packages.
package acctest

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
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

// Client returns an API client for the dev store, for CheckDestroy and similar out-of-band checks.
func Client(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.New(client.Config{
		StoreDomain:  os.Getenv("SHOPIFY_STORE_DOMAIN"),
		ClientID:     os.Getenv("SHOPIFY_CLIENT_ID"),
		ClientSecret: os.Getenv("SHOPIFY_CLIENT_SECRET"),
		AccessToken:  os.Getenv("SHOPIFY_ACCESS_TOKEN"),
		MaxRetries:   3,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// PublicationID returns the ID of the publication with this exact name, for out-of-band checks.
func PublicationID(t *testing.T, name string) string {
	t.Helper()
	res, err := client.ListPublications(context.Background(), Client(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.Publications.Nodes {
		if p.Name == name {
			return p.Id
		}
	}
	t.Fatalf("no publication named %q on the dev store", name)
	return ""
}

// Publish publishes a product or collection to a publication, outside Terraform.
func Publish(t *testing.T, resourceID, publicationID string) {
	t.Helper()
	res, err := client.PublishPublishable(context.Background(), Client(t), resourceID, []client.PublicationInput{{PublicationId: &publicationID}})
	if err != nil {
		t.Fatal(err)
	}
	if res.PublishablePublish == nil {
		t.Fatal("publishablePublish returned no payload")
	}
	if errs := res.PublishablePublish.UserErrors; len(errs) > 0 {
		t.Fatalf("publishablePublish: %v", errs)
	}
}

// Setup skips without TF_ACC, because tests that call the API directly would otherwise run before resource.Test skips them.
func Setup(t *testing.T) {
	t.Helper()
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skip("Acceptance tests skipped unless env 'TF_ACC' set")
	}
	PreCheck(t)
}
