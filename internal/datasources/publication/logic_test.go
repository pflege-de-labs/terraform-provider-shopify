package publication

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

type catalog = client.ListPublicationsPublicationsPublicationConnectionNodesPublicationCatalog

func appCatalog(id string) *catalog {
	var c catalog = &client.ListPublicationsPublicationsPublicationConnectionNodesPublicationCatalogAppCatalog{Id: id}
	return &c
}

func TestFindByName(t *testing.T) {
	pubs := []publication{
		{Id: "gid://shopify/Publication/1", Name: "Online Store"},
		{Id: "gid://shopify/Publication/2", Name: "Point of Sale"},
	}

	got, err := findByName(pubs, "Point of Sale")
	if err != nil || got.Id != "gid://shopify/Publication/2" {
		t.Fatalf("got %+v, %v", got, err)
	}

	// Names are matched exactly, not by prefix or case.
	for _, n := range []string{"Online", "online store", "Online Store "} {
		if _, err := findByName(pubs, n); err == nil {
			t.Errorf("name %q must not match", n)
		}
	}
}

func TestFindByName_skipsEmptyNames(t *testing.T) {
	// A market catalog's publication has an empty name and must never match or be listed.
	pubs := []publication{{Id: "gid://shopify/Publication/9", Name: ""}, {Id: "gid://shopify/Publication/1", Name: "Shop"}}

	if _, err := findByName(pubs, ""); err == nil {
		t.Fatal("empty name must not match an unnamed publication")
	}
	_, err := findByName(pubs, "nope")
	if err == nil || strings.Contains(err.Error(), ", ,") || !strings.Contains(err.Error(), "Shop") {
		t.Errorf("available list must hold named publications only: %v", err)
	}
}

func TestFindByName_duplicateListsMatches(t *testing.T) {
	pubs := []publication{
		{Id: "gid://shopify/Publication/1", Name: "Headless", Catalog: appCatalog("gid://shopify/AppCatalog/10")},
		{Id: "gid://shopify/Publication/2", Name: "Other"},
		{Id: "gid://shopify/Publication/3", Name: "Headless"},
	}
	got, err := findByName(pubs, "Headless")
	if err == nil || got != nil {
		t.Fatalf("want error and no pick, got %+v, %v", got, err)
	}
	for _, want := range []string{`"Headless"`, "gid://shopify/Publication/1", "gid://shopify/AppCatalog/10", "gid://shopify/Publication/3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must contain %s: %q", want, err)
		}
	}
	if strings.Contains(err.Error(), "Publication/2") {
		t.Errorf("error must list the matches only: %q", err)
	}
}

func TestFindByName_notFoundListsAvailable(t *testing.T) {
	_, err := findByName([]publication{{Name: "Online Store"}, {Name: "Shop"}}, "Onlne Store")
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{`"Onlne Store"`, "available: Online Store, Shop"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must contain %s: %q", want, err)
		}
	}

	_, err = findByName(nil, "x")
	if err == nil || !strings.Contains(err.Error(), "none") {
		t.Errorf("empty list must say there are none: %v", err)
	}
}

func TestToModel(t *testing.T) {
	got := toModel(&publication{
		Id: "gid://shopify/Publication/1", Name: "Online Store", AutoPublish: true,
		Catalog: appCatalog("gid://shopify/AppCatalog/5"),
	})
	want := model{
		ID: types.StringValue("gid://shopify/Publication/1"), Name: types.StringValue("Online Store"),
		AutoPublish: types.BoolValue(true), CatalogID: types.StringValue("gid://shopify/AppCatalog/5"),
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestToModel_nullCatalog(t *testing.T) {
	if got := toModel(&publication{Name: "x"}); !got.CatalogID.IsNull() {
		t.Errorf("catalog_id must be null without a catalog: %+v", got)
	}

	// A non-nil pointer to a nil interface must not panic either.
	var c catalog
	if got := toModel(&publication{Name: "x", Catalog: &c}); !got.CatalogID.IsNull() {
		t.Errorf("catalog_id must be null for a nil catalog: %+v", got)
	}
}
