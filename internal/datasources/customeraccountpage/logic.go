// Package customeraccountpage implements the shopify_customer_account_page data source.
package customeraccountpage

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

type page struct {
	ID, Handle, Title string
}

type model struct {
	ID     types.String `tfsdk:"id"`
	Handle types.String `tfsdk:"handle"`
	Title  types.String `tfsdk:"title"`
}

// findByHandle matches exactly; Shopify keeps handles unique, so there is no ambiguity case.
func findByHandle(pages []page, handle string) (*page, error) {
	handles := make([]string, len(pages))
	for i := range pages {
		if pages[i].Handle == handle && handle != "" {
			return &pages[i], nil
		}
		handles[i] = pages[i].Handle
	}
	available := "none"
	if len(handles) > 0 {
		available = strings.Join(handles, ", ")
	}
	return nil, fmt.Errorf("no customer account page with handle %q; available: %s", handle, available)
}

func toModel(p *page) model {
	return model{ID: types.StringValue(p.ID), Handle: types.StringValue(p.Handle), Title: types.StringValue(p.Title)}
}

// fromConnection treats a null connection (nullable in the schema) as no pages.
func fromConnection(conn *client.ListCustomerAccountPagesCustomerAccountPagesCustomerAccountPageConnection) ([]page, *string) {
	if conn == nil {
		return nil, nil
	}
	pages := make([]page, len(conn.Nodes))
	for i, n := range conn.Nodes {
		pages[i] = page{ID: n.GetId(), Handle: n.GetHandle(), Title: n.GetTitle()}
	}
	return pages, patterns.NextCursor(conn.PageInfo.HasNextPage, conn.PageInfo.EndCursor)
}
