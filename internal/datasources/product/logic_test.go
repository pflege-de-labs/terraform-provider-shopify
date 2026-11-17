package product

import (
	"strings"
	"testing"
)

func v(id, sku, productID string) variant {
	return variant{ID: id, SKU: sku, ProductID: productID, Handle: "h-" + productID, Title: "T " + productID}
}

func TestMatchSKU(t *testing.T) {
	vs := []variant{v("V/1", "ABC-1", "P/1"), v("V/2", "ABC-10", "P/2"), v("V/3", "abc-1", "P/3")}

	got, err := matchSKU(vs, "ABC-1")
	if err != nil || got.ID != "V/1" || got.ProductID != "P/1" {
		t.Fatalf("got %+v, %v", got, err)
	}

	// The search is fuzzy; only an exact, case-sensitive SKU matches.
	for _, s := range []string{"ABC", "ABC-1 ", "Abc-1"} {
		if _, err := matchSKU(vs, s); err == nil {
			t.Errorf("sku %q must not match", s)
		}
	}
}

func TestMatchSKU_noneSaysSo(t *testing.T) {
	_, err := matchSKU([]variant{v("V/2", "ABC-10", "P/2")}, "ABC-1")
	if err == nil || !strings.Contains(err.Error(), `"ABC-1"`) {
		t.Fatalf("want not-found error naming the SKU, got %v", err)
	}
}

func TestMatchSKU_duplicateListsMatches(t *testing.T) {
	vs := []variant{v("V/1", "DUP", "P/1"), v("V/2", "OTHER", "P/2"), v("V/3", "DUP", "P/3")}
	got, err := matchSKU(vs, "DUP")
	if err == nil || got != nil {
		t.Fatalf("want error and no pick, got %+v, %v", got, err)
	}
	for _, want := range []string{"V/1", "h-P/1", "V/3", "h-P/3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must contain %s: %q", want, err)
		}
	}
	if strings.Contains(err.Error(), "V/2") {
		t.Errorf("error must list the matches only: %q", err)
	}
}

func TestSKUQuery(t *testing.T) {
	if got := skuQuery(`A "B"`); got != `sku:"A \"B\""` {
		t.Errorf("got %s", got)
	}
}
