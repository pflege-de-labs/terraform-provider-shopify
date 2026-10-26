package fulfillmentservice

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func TestFindByHandle(t *testing.T) {
	services := []service{{Handle: "manual", Id: "gid://shopify/FulfillmentService/1"}, {Handle: "app-x", Id: "gid://shopify/FulfillmentService/2"}}

	got, err := findByHandle(services, "app-x")
	if err != nil || got.Id != "gid://shopify/FulfillmentService/2" {
		t.Fatalf("got %+v, %v", got, err)
	}

	// Handles are matched exactly, not by prefix or case.
	for _, h := range []string{"app", "MANUAL", ""} {
		if _, err := findByHandle(services, h); err == nil {
			t.Errorf("handle %q must not match", h)
		}
	}
}

func TestFindByHandle_notFoundListsAvailable(t *testing.T) {
	_, err := findByHandle([]service{{Handle: "manual"}, {Handle: "app-x"}}, "nope")
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{`"nope"`, "manual", "app-x"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must contain %s: %q", want, err)
		}
	}

	_, err = findByHandle(nil, "nope")
	if err == nil || !strings.Contains(err.Error(), "none") {
		t.Errorf("empty list must say there are none: %v", err)
	}
}

func TestToModel(t *testing.T) {
	url := "https://example.com/cb"
	got := toModel(&service{
		Id: "gid://shopify/FulfillmentService/2", Handle: "app-x", ServiceName: "App X",
		Type:                   client.FulfillmentServiceTypeThirdParty,
		Location:               &client.GetFulfillmentServicesShopFulfillmentServicesFulfillmentServiceLocation{Id: "gid://shopify/Location/9"},
		CallbackUrl:            &url,
		InventoryManagement:    true,
		RequiresShippingMethod: true,
	})
	want := model{
		ID: types.StringValue("gid://shopify/FulfillmentService/2"), Handle: types.StringValue("app-x"),
		ServiceName: types.StringValue("App X"), Type: types.StringValue("THIRD_PARTY"),
		LocationID: types.StringValue("gid://shopify/Location/9"), CallbackURL: types.StringValue(url),
		InventoryManagement: types.BoolValue(true), TrackingSupport: types.BoolValue(false),
		RequiresShippingMethod: types.BoolValue(true),
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestToModel_nullsForMissingLocationAndCallback(t *testing.T) {
	got := toModel(&service{Handle: "manual", Type: client.FulfillmentServiceTypeManual})
	if !got.LocationID.IsNull() || !got.CallbackURL.IsNull() {
		t.Errorf("location_id and callback_url must be null: %+v", got)
	}
}
