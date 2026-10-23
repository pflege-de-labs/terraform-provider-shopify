package market

import (
	"context"
	"testing"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func region(handle string) client.MarketFields {
	return client.MarketFields{Handle: handle, Conditions: &client.MarketFieldsConditionsMarketConditions{
		RegionsCondition: &client.MarketFieldsConditionsMarketConditionsRegionsCondition{
			Regions: client.MarketFieldsConditionsMarketConditionsRegionsConditionRegionsMarketRegionConnection{
				Nodes: []client.MarketFieldsConditionsMarketConditionsRegionsConditionRegionsMarketRegionConnectionNodesMarketRegion{
					&client.MarketFieldsConditionsMarketConditionsRegionsConditionRegionsMarketRegionConnectionNodesMarketRegionCountry{},
				},
			},
		},
	}}
}

func TestEachRegionMarketLimitCountsRegionsOnly(t *testing.T) {
	items := []client.MarketFields{{Handle: "x1"}, region("a"), {Handle: "x2"}, region("b"), region("c")}
	page := func(*string) ([]client.MarketFields, *string, error) { return items, nil, nil }
	var pushed, skipped []string
	err := eachRegionMarket(context.Background(), 2, page,
		func(m *client.MarketFields) bool { pushed = append(pushed, m.Handle); return true },
		func(m *client.MarketFields) bool { skipped = append(skipped, m.Handle); return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(pushed) != 2 || pushed[1] != "b" || len(skipped) != 2 {
		t.Fatalf("pushed %v skipped %v", pushed, skipped)
	}
}
