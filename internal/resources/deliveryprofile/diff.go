package deliveryprofile

import (
	"fmt"
	"math/big"
	"slices"
	"strconv"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

// restOfWorld stands for Shopify's "rest of world" pseudo-country in zone countries.
const restOfWorld = "*"

// tree is the profile as Go values. IDs are only set on trees read from the API; config trees have none.
type tree struct {
	Groups []group
}

type group struct {
	ID        string
	Locations []string
	Zones     []zone
}

type zone struct {
	ID        string
	Name      string
	Countries []string
	Methods   []method
}

type method struct {
	ID          string
	RateID      string
	Name        string
	Description string
	Active      bool
	Price       string
	Currency    string
	Cond        *cond
}

type cond struct {
	ID       string
	Field    string // TOTAL_PRICE or TOTAL_WEIGHT
	Operator string
	Value    string
	Unit     string // weight unit; empty for price conditions
}

// key identifies a method within a zone: names repeat (e.g. a flat rate and a free-over-X rate), conditions tell them apart.
func (m method) key() string {
	if m.Cond == nil {
		return m.Name
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s", m.Name, m.Cond.Field, m.Cond.Operator, normAmount(m.Cond.Value), m.Cond.Unit)
}

// diff builds the update input that turns current (from the API) into desired (from config).
// Groups match by position, zones by name, methods by key. Changed conditional methods are deleted and
// recreated, because their rate and condition IDs are synthetic and not reliably updatable.
func diff(current, desired tree) (client.DeliveryProfileInput, bool) {
	var in client.DeliveryProfileInput
	for i, d := range desired.Groups {
		if i >= len(current.Groups) {
			in.LocationGroupsToCreate = append(in.LocationGroupsToCreate, client.DeliveryProfileLocationGroupInput{
				Locations: d.Locations, ZonesToCreate: zonesInput(d.Zones),
			})
			continue
		}
		c := current.Groups[i]
		up := client.DeliveryProfileLocationGroupInput{Id: &c.ID}
		up.LocationsToAdd, up.LocationsToRemove = patterns.DiffIDs(c.Locations, d.Locations)

		currentZones := map[string]zone{}
		for _, z := range c.Zones {
			currentZones[z.Name] = z
		}
		for _, dz := range d.Zones {
			cz, ok := currentZones[dz.Name]
			if !ok {
				up.ZonesToCreate = append(up.ZonesToCreate, zoneInput(dz))
				continue
			}
			delete(currentZones, dz.Name)
			if zu, deleted, changed := zoneUpdate(cz, dz); changed {
				up.ZonesToUpdate = append(up.ZonesToUpdate, zu)
				in.MethodDefinitionsToDelete = append(in.MethodDefinitionsToDelete, deleted...)
			}
		}
		for _, z := range c.Zones {
			if _, removed := currentZones[z.Name]; removed {
				in.ZonesToDelete = append(in.ZonesToDelete, z.ID)
			}
		}
		if len(up.LocationsToAdd)+len(up.LocationsToRemove)+len(up.ZonesToCreate)+len(up.ZonesToUpdate) > 0 {
			in.LocationGroupsToUpdate = append(in.LocationGroupsToUpdate, up)
		}
	}
	for _, c := range current.Groups[min(len(desired.Groups), len(current.Groups)):] {
		in.LocationGroupsToDelete = append(in.LocationGroupsToDelete, c.ID)
	}

	changed := len(in.LocationGroupsToCreate)+len(in.LocationGroupsToUpdate)+len(in.LocationGroupsToDelete)+
		len(in.ZonesToDelete)+len(in.MethodDefinitionsToDelete) > 0
	return in, changed
}

// zoneUpdate returns the zone's update entry and the method IDs to delete.
func zoneUpdate(c, d zone) (client.DeliveryLocationGroupZoneInput, []string, bool) {
	up := client.DeliveryLocationGroupZoneInput{Id: &c.ID}
	changed := false
	// Countries are only sent when the set changes, since sending them resets province selections.
	if add, del := patterns.DiffIDs(c.Countries, d.Countries); len(add)+len(del) > 0 {
		up.Countries = countriesInput(d.Countries)
		changed = true
	}

	currentMethods := map[string]method{}
	for _, m := range c.Methods {
		currentMethods[m.key()] = m
	}
	var deleted []string
	for _, dm := range d.Methods {
		cm, ok := currentMethods[dm.key()]
		if !ok {
			up.MethodDefinitionsToCreate = append(up.MethodDefinitionsToCreate, methodInput(dm))
			changed = true
			continue
		}
		delete(currentMethods, dm.key())
		if sameMethod(cm, dm) {
			continue
		}
		changed = true
		if cm.Cond != nil {
			deleted = append(deleted, cm.ID)
			up.MethodDefinitionsToCreate = append(up.MethodDefinitionsToCreate, methodInput(dm))
			continue
		}
		mu := methodInput(dm)
		mu.Id = &cm.ID
		mu.RateDefinition.Id = &cm.RateID
		up.MethodDefinitionsToUpdate = append(up.MethodDefinitionsToUpdate, mu)
	}
	for _, m := range c.Methods {
		if _, removed := currentMethods[m.key()]; removed {
			deleted = append(deleted, m.ID)
			changed = true
		}
	}
	return up, deleted, changed
}

func sameMethod(a, b method) bool {
	return a.Description == b.Description && a.Active == b.Active && a.Currency == b.Currency && sameAmount(a.Price, b.Price)
}

func zonesInput(zones []zone) []client.DeliveryLocationGroupZoneInput {
	out := make([]client.DeliveryLocationGroupZoneInput, len(zones))
	for i, z := range zones {
		out[i] = zoneInput(z)
	}
	return out
}

func zoneInput(z zone) client.DeliveryLocationGroupZoneInput {
	in := client.DeliveryLocationGroupZoneInput{Name: &z.Name, Countries: countriesInput(z.Countries)}
	for _, m := range z.Methods {
		in.MethodDefinitionsToCreate = append(in.MethodDefinitionsToCreate, methodInput(m))
	}
	return in
}

func countriesInput(codes []string) []client.DeliveryCountryInput {
	sorted := slices.Sorted(slices.Values(codes))
	out := make([]client.DeliveryCountryInput, len(sorted))
	all := true
	for i, c := range sorted {
		if c == restOfWorld {
			out[i] = client.DeliveryCountryInput{RestOfWorld: &all}
			continue
		}
		code := client.CountryCode(c)
		out[i] = client.DeliveryCountryInput{Code: &code, IncludeAllProvinces: &all}
	}
	return out
}

func methodInput(m method) client.DeliveryMethodDefinitionInput {
	in := client.DeliveryMethodDefinitionInput{
		Name:           &m.Name,
		Active:         &m.Active,
		RateDefinition: &client.DeliveryRateDefinitionInput{Price: client.MoneyInput{Amount: m.Price, CurrencyCode: client.CurrencyCode(m.Currency)}},
	}
	if m.Description != "" {
		in.Description = &m.Description
	}
	if c := m.Cond; c != nil {
		op := client.DeliveryConditionOperator(c.Operator)
		if c.Field == "TOTAL_WEIGHT" {
			v, _ := strconv.ParseFloat(c.Value, 64)
			in.WeightConditionsToCreate = []client.DeliveryWeightConditionInput{{
				Criteria: &client.WeightInput{Value: v, Unit: client.WeightUnit(c.Unit)}, Operator: &op,
			}}
		} else {
			in.PriceConditionsToCreate = []client.DeliveryPriceConditionInput{{
				Criteria: &client.MoneyInput{Amount: c.Value, CurrencyCode: client.CurrencyCode(m.Currency)}, Operator: &op,
			}}
		}
	}
	return in
}

// duplicates lists zone names repeated within a group and method keys repeated within a zone.
func duplicates(t tree) []string {
	var out []string
	for gi, g := range t.Groups {
		zones := map[string]bool{}
		for _, z := range g.Zones {
			if zones[z.Name] {
				out = append(out, fmt.Sprintf("location_group %d: zone %q", gi, z.Name))
			}
			zones[z.Name] = true
			methods := map[string]bool{}
			for _, m := range z.Methods {
				if methods[m.key()] {
					out = append(out, fmt.Sprintf("zone %q: method %q with the same condition", z.Name, m.Name))
				}
				methods[m.key()] = true
			}
		}
	}
	return out
}

// sameAmount compares decimal strings numerically, since Shopify returns "55.0" for "55.00".
func sameAmount(a, b string) bool {
	x, okx := new(big.Rat).SetString(a)
	y, oky := new(big.Rat).SetString(b)
	if !okx || !oky {
		return a == b
	}
	return x.Cmp(y) == 0
}

func normAmount(a string) string {
	if r, ok := new(big.Rat).SetString(a); ok {
		return r.RatString()
	}
	return a
}
