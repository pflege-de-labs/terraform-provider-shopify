package customeraccountpage

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

var pages = []page{
	{ID: "gid://shopify/CustomerAccountPage/1", Handle: "orders", Title: "Orders"},
	{ID: "gid://shopify/CustomerAccountPage/2", Handle: "profile", Title: "Profile"},
}

func TestFindByHandle(t *testing.T) {
	got, err := findByHandle(pages, "profile")
	if err != nil || got.ID != "gid://shopify/CustomerAccountPage/2" {
		t.Fatalf("got %+v, %v", got, err)
	}

	// Handles are matched exactly, not by prefix or case; empty never matches.
	for _, h := range []string{"Profile", "prof", ""} {
		if _, err := findByHandle(pages, h); err == nil {
			t.Errorf("handle %q must not match", h)
		}
	}
}

func TestFindByHandle_notFoundListsHandles(t *testing.T) {
	_, err := findByHandle(pages, "missing")
	if err == nil || !strings.Contains(err.Error(), "available: orders, profile") {
		t.Errorf("error must list the handles: %v", err)
	}

	_, err = findByHandle(nil, "x")
	if err == nil || !strings.Contains(err.Error(), "none") {
		t.Errorf("empty list must say there are none: %v", err)
	}
}

func TestToModel(t *testing.T) {
	got := toModel(&pages[0])
	want := model{
		ID: types.StringValue("gid://shopify/CustomerAccountPage/1"), Handle: types.StringValue("orders"), Title: types.StringValue("Orders"),
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFromConnectionNil(t *testing.T) {
	got, next := fromConnection(nil)
	if len(got) != 0 || next != nil {
		t.Fatalf("got %+v, %v", got, next)
	}
}

func TestFromConnection(t *testing.T) {
	conn := &client.ListCustomerAccountPagesCustomerAccountPagesCustomerAccountPageConnection{
		Nodes: []client.ListCustomerAccountPagesCustomerAccountPagesCustomerAccountPageConnectionNodesCustomerAccountPage{
			&client.ListCustomerAccountPagesCustomerAccountPagesCustomerAccountPageConnectionNodesCustomerAccountNativePage{Id: "1", Handle: "orders", Title: "Orders"},
		},
	}
	got, next := fromConnection(conn)
	if len(got) != 1 || got[0].Handle != "orders" || next != nil {
		t.Fatalf("got %+v, %v", got, next)
	}
}
