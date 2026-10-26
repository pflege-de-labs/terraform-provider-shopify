// Package publication implements the shopify_publication data source.
package publication

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

type publication = client.ListPublicationsPublicationsPublicationConnectionNodesPublication

type model struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	AutoPublish types.Bool   `tfsdk:"auto_publish"`
	CatalogID   types.String `tfsdk:"catalog_id"`
}

// catalogID returns "" when the publication has no catalog.
func catalogID(p *publication) string {
	if p.Catalog == nil || *p.Catalog == nil {
		return ""
	}
	return (*p.Catalog).GetId()
}

// findByName matches exactly and skips unnamed publications (market catalogs); duplicates are an error, never a pick.
func findByName(pubs []publication, name string) (*publication, error) {
	var matches []*publication
	var names []string
	for i := range pubs {
		p := &pubs[i]
		if p.Name == "" {
			continue
		}
		names = append(names, p.Name)
		if p.Name == name && name != "" {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		available := "none"
		if len(names) > 0 {
			available = strings.Join(names, ", ")
		}
		return nil, fmt.Errorf("no publication named %q; available: %s", name, available)
	}
	found := make([]string, len(matches))
	for i, p := range matches {
		found[i] = fmt.Sprintf("%s (catalog %s)", p.Id, orNone(catalogID(p)))
	}
	return nil, fmt.Errorf("%d publications are named %q, so the name is ambiguous: %s", len(matches), name, strings.Join(found, ", "))
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func toModel(p *publication) model {
	m := model{
		ID:          types.StringValue(p.Id),
		Name:        types.StringValue(p.Name),
		AutoPublish: types.BoolValue(p.AutoPublish),
		CatalogID:   types.StringNull(),
	}
	if id := catalogID(p); id != "" {
		m.CatalogID = types.StringValue(id)
	}
	return m
}
