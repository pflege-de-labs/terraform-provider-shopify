package deliveryprofile

import (
	"encoding/json"
	"strings"
	"testing"
)

func flat(id, rateID, name, price string) method {
	return method{ID: id, RateID: rateID, Name: name, Active: true, Price: price, Currency: "EUR"}
}

func free(id, name, over string) method {
	m := flat(id, "gid://rate/0?source=RateRangeCondition", name, "0.0")
	m.Cond = &cond{ID: id + "-cond", Field: "TOTAL_PRICE", Operator: "GREATER_THAN_OR_EQUAL_TO", Value: over}
	return m
}

func current() tree {
	return tree{Groups: []group{{
		ID:        "g1",
		Locations: []string{"loc1"},
		Zones: []zone{
			{ID: "z1", Name: "DE", Countries: []string{"DE"}, Methods: []method{
				flat("m1", "r1", "Standard", "5.99"),
				free("m2", "Standard", "55.0"),
			}},
			{ID: "z2", Name: "EU", Countries: []string{"AT", "FR"}, Methods: []method{flat("m3", "r3", "Intl", "13.99")}},
		},
	}}}
}

// desired strips IDs, as config has none.
func desired(t tree) tree {
	for gi := range t.Groups {
		g := &t.Groups[gi]
		g.ID = ""
		for zi := range g.Zones {
			z := &g.Zones[zi]
			z.ID = ""
			for mi := range z.Methods {
				m := &z.Methods[mi]
				m.ID, m.RateID = "", ""
				if m.Cond != nil {
					c := *m.Cond
					c.ID = ""
					m.Cond = &c
				}
			}
		}
	}
	return t
}

func js(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDiff_NoChange(t *testing.T) {
	want := desired(current())
	// Equal amounts in another format are no change.
	want.Groups[0].Zones[0].Methods[0].Price = "5.990"
	want.Groups[0].Zones[0].Methods[1].Cond.Value = "55.00"
	in, changed := diff(current(), want)
	if changed {
		t.Errorf("unexpected change: %s", js(t, in))
	}
}

func TestDiff_PriceChangeOnFlatMethodUpdatesInPlace(t *testing.T) {
	want := desired(current())
	want.Groups[0].Zones[0].Methods[0].Price = "6.49"
	in, changed := diff(current(), want)
	got := js(t, in)
	if !changed || !strings.Contains(got, `"methodDefinitionsToUpdate":[{"id":"m1"`) || !strings.Contains(got, `"rateDefinition":{"id":"r1","price":{"amount":"6.49","currencyCode":"EUR"}}`) {
		t.Errorf("got %s", got)
	}
	if strings.Contains(got, "ToDelete") || strings.Contains(got, "ToCreate") {
		t.Errorf("flat price change must not delete or create: %s", got)
	}
}

func TestDiff_ConditionalMethodChangeIsDeleteAndCreate(t *testing.T) {
	want := desired(current())
	want.Groups[0].Zones[0].Methods[1].Cond.Value = "49.00"
	in, _ := diff(current(), want)
	got := js(t, in)
	if !strings.Contains(got, `"methodDefinitionsToDelete":["m2"]`) || !strings.Contains(got, `"methodDefinitionsToCreate":[{"name":"Standard"`) ||
		!strings.Contains(got, `"priceConditionsToCreate":[{"criteria":{"amount":"49.00","currencyCode":"EUR"},"operator":"GREATER_THAN_OR_EQUAL_TO"}]`) {
		t.Errorf("got %s", got)
	}
	if strings.Contains(got, `"id":"m1"`) {
		t.Errorf("same-named flat method must be untouched: %s", got)
	}
}

func TestDiff_ZoneAddRemoveAndCountries(t *testing.T) {
	want := desired(current())
	want.Groups[0].Zones[1] = zone{Name: "CH", Countries: []string{"CH"}, Methods: []method{flat("", "", "Swiss", "15")}}
	want.Groups[0].Zones[0].Countries = []string{"DE", "AT"}
	in, _ := diff(current(), want)
	got := js(t, in)
	for _, s := range []string{
		`"zonesToDelete":["z2"]`,
		`"zonesToCreate":[{"name":"CH","countries":[{"code":"CH","includeAllProvinces":true}]`,
		`"zonesToUpdate":[{"id":"z1","countries":[{"code":"AT","includeAllProvinces":true},{"code":"DE","includeAllProvinces":true}]}]`,
	} {
		if !strings.Contains(got, s) {
			t.Errorf("missing %s in %s", s, got)
		}
	}
}

func TestDiff_LocationsAndGroups(t *testing.T) {
	want := desired(current())
	want.Groups[0].Locations = []string{"loc2"}
	want.Groups = append(want.Groups, group{Locations: []string{"loc3"}, Zones: []zone{{Name: "US", Countries: []string{"*"}, Methods: []method{flat("", "", "World", "30")}}}})
	in, _ := diff(current(), want)
	got := js(t, in)
	for _, s := range []string{
		`"locationGroupsToUpdate":[{"id":"g1","locationsToAdd":["loc2"],"locationsToRemove":["loc1"]}]`,
		`"locationGroupsToCreate":[{"locations":["loc3"],"zonesToCreate":[{"name":"US","countries":[{"restOfWorld":true}]`,
	} {
		if !strings.Contains(got, s) {
			t.Errorf("missing %s in %s", s, got)
		}
	}

	in, _ = diff(current(), tree{})
	if got := js(t, in); !strings.Contains(got, `"locationGroupsToDelete":["g1"]`) {
		t.Errorf("got %s", got)
	}
}

func TestDuplicates(t *testing.T) {
	ok := desired(current())
	if d := duplicates(ok); len(d) != 0 {
		t.Errorf("got %v", d)
	}
	bad := desired(current())
	bad.Groups[0].Zones[0].Methods = append(bad.Groups[0].Zones[0].Methods, flat("", "", "Standard", "7"))
	bad.Groups[0].Zones = append(bad.Groups[0].Zones, zone{Name: "EU"})
	d := duplicates(bad)
	if len(d) != 2 {
		t.Errorf("got %v", d)
	}
}
