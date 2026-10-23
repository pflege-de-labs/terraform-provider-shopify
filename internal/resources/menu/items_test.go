package menu

import (
	"testing"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func TestCheckItem(t *testing.T) {
	cases := []struct {
		typ             string
		resourceID, url bool
		wantErr         bool
	}{
		{"HTTP", false, true, false},
		{"HTTP", false, false, true},
		{"HTTP", true, true, true},
		{"COLLECTION", true, false, false},
		{"COLLECTION", false, false, true},
		{"PAGE", true, true, true},
		{"FRONTPAGE", false, false, false},
		{"SEARCH", false, true, true},
		{"SHOP_POLICY", false, false, true},
		{"CUSTOMER_ACCOUNT_PAGE", false, false, true},
		{"CATALOG", false, false, false},
	}
	for _, tc := range cases {
		err := checkItem(tc.typ, tc.resourceID, tc.url)
		if (err != nil) != tc.wantErr {
			t.Errorf("checkItem(%s, id=%v, url=%v) = %v, wantErr %v", tc.typ, tc.resourceID, tc.url, err, tc.wantErr)
		}
	}
}

func TestFromAPI_URLOnlyForHTTP(t *testing.T) {
	url, id := "/collections/foo", "gid://shopify/Collection/1"
	items := []client.MenuFieldsItemsMenuItem{
		{MenuItemFields: client.MenuItemFields{Title: "C", Type: client.MenuItemTypeCollection, ResourceId: &id, Url: &url, Tags: []string{}}},
		{MenuItemFields: client.MenuItemFields{Title: "H", Type: client.MenuItemTypeHttp, Url: &url, Tags: []string{"a"}}},
	}
	got := level1FromAPI(items)
	if len(got) != 2 {
		t.Fatalf("got %d items", len(got))
	}
	if !got[0].URL.IsNull() || got[0].ResourceID.ValueString() != id || !got[0].Tags.IsNull() {
		t.Errorf("collection item: url=%s id=%s tags=%s", got[0].URL, got[0].ResourceID, got[0].Tags)
	}
	if got[1].URL.ValueString() != url || len(got[1].Tags.Elements()) != 1 {
		t.Errorf("http item: url=%s tags=%s", got[1].URL, got[1].Tags)
	}
	if got[0].Items == nil {
		t.Error("children must be an empty slice, not nil, so the block list is not null")
	}
}
