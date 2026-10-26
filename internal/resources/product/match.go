package product

import (
	"fmt"
	"math/big"
	"slices"
	"strings"
)

// maxOptions is Shopify's limit per product (OPTIONS_OVER_LIMIT).
const maxOptions = 3

// option is a product option by name, values in position order.
type option struct {
	Name   string
	Values []string
}

// combo is a variant's option values: option name to value.
type combo map[string]string

// optionMatch holds, per planned option and per planned value, the index of the prior one it keeps, or -1.
type optionMatch struct {
	Options []int
	Values  [][]int
}

// matchNames pairs planned names with prior ones: equal names first, then an unmatched planned name takes an unmatched prior name at the same index (a rename).
func matchNames(prior, planned []string) []int {
	out := make([]int, len(planned))
	used := make([]bool, len(prior))
	for j, n := range planned {
		out[j] = -1
		if i := slices.Index(prior, n); i >= 0 && !used[i] {
			out[j], used[i] = i, true
		}
	}
	for j := range planned {
		if out[j] == -1 && j < len(prior) && !used[j] {
			out[j], used[j] = j, true
		}
	}
	return out
}

func matchOptions(prior, planned []option) optionMatch {
	m := optionMatch{Options: matchNames(names(prior), names(planned)), Values: make([][]int, len(planned))}
	for j, i := range m.Options {
		if i >= 0 {
			m.Values[j] = matchNames(prior[i].Values, planned[j].Values)
			continue
		}
		m.Values[j] = slices.Repeat([]int{-1}, len(planned[j].Values))
	}
	return m
}

func names(opts []option) []string {
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.Name
	}
	return out
}

// matchVariants returns, per planned variant, the prior variant it keeps (-1 for a new one), and the prior variants nothing keeps.
// Combinations are compared on the options both sides share, through renames, so adding or removing an option keeps variants.
func matchVariants(priorOpts []option, prior []combo, plannedOpts []option, planned []combo) (keep, removed []int) {
	m := matchOptions(priorOpts, plannedOpts)
	var shared []int
	for j, i := range m.Options {
		if i >= 0 {
			shared = append(shared, j)
		}
	}
	priorKeys := make([]*string, len(prior))
	for k, c := range prior {
		parts, ok := make([]string, len(shared)), true
		for n, j := range shared {
			po := priorOpts[m.Options[j]]
			v, has := c[po.Name]
			pj := -1
			if vi := slices.Index(po.Values, v); has && vi >= 0 {
				pj = slices.Index(m.Values[j], vi)
			}
			if pj < 0 {
				ok = false // its value is gone, so nothing can keep this variant
				break
			}
			parts[n] = plannedOpts[j].Values[pj]
		}
		if ok {
			key := strings.Join(parts, "\x00")
			priorKeys[k] = &key
		}
	}
	used := make([]bool, len(prior))
	keep = make([]int, len(planned))
	for p, c := range planned {
		parts := make([]string, len(shared))
		for n, j := range shared {
			parts[n] = c[plannedOpts[j].Name]
		}
		key := strings.Join(parts, "\x00")
		keep[p] = -1
		for k, pk := range priorKeys {
			if pk != nil && !used[k] && *pk == key {
				keep[p], used[k] = k, true
				break
			}
		}
	}
	removed = []int{}
	for k := range prior {
		if !used[k] {
			removed = append(removed, k)
		}
	}
	return keep, removed
}

// describe names a variant by its option values, in option order.
func describe(c combo, opts []option) string {
	var parts []string
	for _, o := range opts {
		if v, ok := c[o.Name]; ok {
			parts = append(parts, o.Name+" = "+v)
		}
	}
	if len(parts) == 0 {
		return "the default variant"
	}
	return strings.Join(parts, ", ")
}

// validateOptionsAndVariants rejects what productSet would refuse or what could not be matched back on Read.
func validateOptionsAndVariants(opts []option, variants []combo) error {
	if len(opts) > maxOptions {
		return fmt.Errorf("a product has at most %d options", maxOptions)
	}
	if len(opts) == 0 {
		if len(variants) != 1 || len(variants[0]) != 0 {
			return fmt.Errorf("a product without options has exactly one variant, with option_values = {}")
		}
		return nil
	}
	seen := map[string]bool{}
	for _, o := range opts {
		switch {
		case o.Name == "":
			return fmt.Errorf("option name must not be empty")
		case seen[o.Name]:
			return fmt.Errorf("duplicate option %q", o.Name)
		case len(o.Values) == 0:
			return fmt.Errorf("option %q needs at least one value", o.Name)
		}
		seen[o.Name] = true
		vals := map[string]bool{}
		for _, v := range o.Values {
			switch {
			case v == "":
				return fmt.Errorf("option %q has an empty value", o.Name)
			case vals[v]:
				return fmt.Errorf("option %q has the duplicate value %q", o.Name, v)
			}
			vals[v] = true
		}
	}
	if len(variants) == 0 {
		return fmt.Errorf("a product with options needs at least one variant")
	}
	keys := map[string]bool{}
	for i, c := range variants {
		if len(c) != len(opts) {
			return fmt.Errorf("variant %d must set a value for every option and no others", i)
		}
		parts := make([]string, len(opts))
		for n, o := range opts {
			v, ok := c[o.Name]
			if !ok {
				return fmt.Errorf("variant %d must set a value for every option and no others", i)
			}
			if !slices.Contains(o.Values, v) {
				return fmt.Errorf("variant %d: %q is not one of the values of option %q", i, v, o.Name)
			}
			parts[n] = v
		}
		key := strings.Join(parts, "\x00")
		if keys[key] {
			return fmt.Errorf("variant %d has the same option values as an earlier one (%s)", i, describe(c, opts))
		}
		keys[key] = true
	}
	return nil
}

// decimalEqual compares decimal strings numerically, since Shopify returns "10" as "10.00".
func decimalEqual(a, b string) bool {
	x, ok1 := new(big.Rat).SetString(a)
	y, ok2 := new(big.Rat).SetString(b)
	return ok1 && ok2 && x.Cmp(y) == 0
}
