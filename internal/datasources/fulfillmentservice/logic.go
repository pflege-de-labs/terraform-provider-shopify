// Package fulfillmentservice implements the shopify_fulfillment_service data source.
package fulfillmentservice

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

type service = client.GetFulfillmentServicesShopFulfillmentServicesFulfillmentService

type model struct {
	ID                     types.String `tfsdk:"id"`
	Handle                 types.String `tfsdk:"handle"`
	ServiceName            types.String `tfsdk:"service_name"`
	Type                   types.String `tfsdk:"type"`
	LocationID             types.String `tfsdk:"location_id"`
	CallbackURL            types.String `tfsdk:"callback_url"`
	InventoryManagement    types.Bool   `tfsdk:"inventory_management"`
	TrackingSupport        types.Bool   `tfsdk:"tracking_support"`
	RequiresShippingMethod types.Bool   `tfsdk:"requires_shipping_method"`
}

// findByHandle matches exactly; the error lists the existing handles so a typo is easy to spot.
func findByHandle(services []service, handle string) (*service, error) {
	handles := make([]string, len(services))
	for i := range services {
		if services[i].Handle == handle {
			return &services[i], nil
		}
		handles[i] = services[i].Handle
	}
	available := "none"
	if len(handles) > 0 {
		available = strings.Join(handles, ", ")
	}
	return nil, fmt.Errorf("no fulfillment service with handle %q; available handles: %s", handle, available)
}

func toModel(s *service) model {
	m := model{
		ID:                     types.StringValue(s.Id),
		Handle:                 types.StringValue(s.Handle),
		ServiceName:            types.StringValue(s.ServiceName),
		Type:                   types.StringValue(string(s.Type)),
		LocationID:             types.StringNull(),
		CallbackURL:            types.StringPointerValue(s.CallbackUrl),
		InventoryManagement:    types.BoolValue(s.InventoryManagement),
		TrackingSupport:        types.BoolValue(s.TrackingSupport),
		RequiresShippingMethod: types.BoolValue(s.RequiresShippingMethod),
	}
	if s.Location != nil {
		m.LocationID = types.StringValue(s.Location.Id)
	}
	return m
}
