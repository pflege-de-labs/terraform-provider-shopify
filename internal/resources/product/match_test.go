package product

import (
	"reflect"
	"strings"
	"testing"
)

func TestMatchNames(t *testing.T) {
	cases := []struct {
		name           string
		prior, planned []string
		want           []int
	}{
		{"same", []string{"S", "M"}, []string{"S", "M"}, []int{0, 1}},
		{"reorder matches by name", []string{"S", "M"}, []string{"M", "S"}, []int{1, 0}},
		{"insert before keeps names", []string{"S", "M"}, []string{"XS", "S", "M"}, []int{-1, 0, 1}},
		{"append", []string{"S"}, []string{"S", "M"}, []int{0, -1}},
		{"rename in place", []string{"S", "M", "L"}, []string{"S", "Medium", "L"}, []int{0, 1, 2}},
		{"remove middle", []string{"S", "M", "L"}, []string{"S", "L"}, []int{0, 2}},
		{"remove one, add another elsewhere is no rename", []string{"S", "M", "L"}, []string{"S", "L", "XL"}, []int{0, 2, -1}},
		{"rename and reorder is no rename", []string{"S", "M"}, []string{"Medium", "S"}, []int{-1, 0}},
		{"rename at end", []string{"S", "M"}, []string{"S", "Medium"}, []int{0, 1}},
		{"from nothing", nil, []string{"S"}, []int{-1}},
		{"to nothing", []string{"S"}, nil, []int{}},
	}
	for _, c := range cases {
		if got := matchNames(c.prior, c.planned); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: matchNames(%v, %v) = %v, want %v", c.name, c.prior, c.planned, got, c.want)
		}
	}
}

func opts(spec ...string) []option {
	out := []option{}
	for _, s := range spec {
		name, values, _ := strings.Cut(s, ":")
		out = append(out, option{Name: name, Values: strings.Split(values, ",")})
	}
	return out
}

// combos parses "Size=S/Color=Red" entries; "" is the default variant.
func combos(spec ...string) []combo {
	out := []combo{}
	for _, s := range spec {
		c := combo{}
		if s != "" {
			for _, kv := range strings.Split(s, "/") {
				k, v, _ := strings.Cut(kv, "=")
				c[k] = v
			}
		}
		out = append(out, c)
	}
	return out
}

func TestMatchVariants(t *testing.T) {
	cases := []struct {
		name                   string
		priorOpts, plannedOpts []option
		prior, planned         []combo
		wantKeep               []int
		wantRemoved            []int
	}{
		{
			name:      "unchanged (price change keeps every variant)",
			priorOpts: opts("Size:S,M"), prior: combos("Size=S", "Size=M"),
			plannedOpts: opts("Size:S,M"), planned: combos("Size=S", "Size=M"),
			wantKeep: []int{0, 1}, wantRemoved: []int{},
		},
		{
			name:      "add option keeps the existing variant",
			priorOpts: opts("Size:S"), prior: combos("Size=S"),
			plannedOpts: opts("Size:S", "Color:Red,Blue"), planned: combos("Size=S/Color=Red", "Size=S/Color=Blue"),
			wantKeep: []int{0, -1}, wantRemoved: []int{},
		},
		{
			name:      "rename option and value keeps every variant",
			priorOpts: opts("Size:S,M"), prior: combos("Size=S", "Size=M"),
			plannedOpts: opts("Größe:S,Medium"), planned: combos("Größe=S", "Größe=Medium"),
			wantKeep: []int{0, 1}, wantRemoved: []int{},
		},
		{
			name:      "insert XS before S adds a variant",
			priorOpts: opts("Size:S,M"), prior: combos("Size=S", "Size=M"),
			plannedOpts: opts("Size:XS,S,M"), planned: combos("Size=XS", "Size=S", "Size=M"),
			wantKeep: []int{-1, 0, 1}, wantRemoved: []int{},
		},
		{
			name:      "default variant gets options in place",
			priorOpts: opts(), prior: combos(""),
			plannedOpts: opts("Size:S,M"), planned: combos("Size=S", "Size=M"),
			wantKeep: []int{0, -1}, wantRemoved: []int{},
		},
		{
			name:      "default variant stays",
			priorOpts: opts(), prior: combos(""),
			plannedOpts: opts(), planned: combos(""),
			wantKeep: []int{0}, wantRemoved: []int{},
		},
		{
			name:      "back to the default variant keeps the first and removes the rest",
			priorOpts: opts("Size:S,M"), prior: combos("Size=S", "Size=M"),
			plannedOpts: opts(), planned: combos(""),
			wantKeep: []int{0}, wantRemoved: []int{1},
		},
		{
			name:      "remove middle value removes its variant",
			priorOpts: opts("Size:S,M,L"), prior: combos("Size=S", "Size=M", "Size=L"),
			plannedOpts: opts("Size:S,L"), planned: combos("Size=S", "Size=L"),
			wantKeep: []int{0, 2}, wantRemoved: []int{1},
		},
		{
			name:      "remove a variant but keep its value",
			priorOpts: opts("Size:S,M"), prior: combos("Size=S", "Size=M"),
			plannedOpts: opts("Size:S,M"), planned: combos("Size=S"),
			wantKeep: []int{0}, wantRemoved: []int{1},
		},
		{
			name:      "remove option collapses variants onto the first",
			priorOpts: opts("Size:S", "Color:Red,Blue"), prior: combos("Size=S/Color=Red", "Size=S/Color=Blue"),
			plannedOpts: opts("Size:S"), planned: combos("Size=S"),
			wantKeep: []int{0}, wantRemoved: []int{1},
		},
		{
			name:      "swap variant order keeps both",
			priorOpts: opts("Size:S,M"), prior: combos("Size=S", "Size=M"),
			plannedOpts: opts("Size:M,S"), planned: combos("Size=M", "Size=S"),
			wantKeep: []int{1, 0}, wantRemoved: []int{},
		},
		{
			name:      "swap option order keeps every variant",
			priorOpts: opts("Size:S", "Color:Red"), prior: combos("Size=S/Color=Red"),
			plannedOpts: opts("Color:Red", "Size:S"), planned: combos("Size=S/Color=Red"),
			wantKeep: []int{0}, wantRemoved: []int{},
		},
		{
			name:      "rename plus reorder in one change is a removal and an addition",
			priorOpts: opts("Size:S,M"), prior: combos("Size=S", "Size=M"),
			plannedOpts: opts("Size:Medium,S"), planned: combos("Size=Medium", "Size=S"),
			wantKeep: []int{-1, 0}, wantRemoved: []int{1},
		},
		{
			name:      "rename option and add a value",
			priorOpts: opts("Size:S"), prior: combos("Size=S"),
			plannedOpts: opts("Größe:S,M"), planned: combos("Größe=S", "Größe=M"),
			wantKeep: []int{0, -1}, wantRemoved: []int{},
		},
		{
			name:      "two options, rename one value",
			priorOpts: opts("Size:S,M", "Color:Red"), prior: combos("Size=S/Color=Red", "Size=M/Color=Red"),
			plannedOpts: opts("Size:S,M", "Color:Rot"), planned: combos("Size=S/Color=Rot", "Size=M/Color=Rot"),
			wantKeep: []int{0, 1}, wantRemoved: []int{},
		},
		{
			name:      "prior variant with a value missing from its options is not matched",
			priorOpts: opts("Size:S"), prior: combos("Size=S", "Size=Z"),
			plannedOpts: opts("Size:S"), planned: combos("Size=S"),
			wantKeep: []int{0}, wantRemoved: []int{1},
		},
		{
			name:      "option dropped while another is added at its place keeps nothing",
			priorOpts: opts("Size:S,M", "Color:Red"), prior: combos("Size=S/Color=Red", "Size=M/Color=Red"),
			plannedOpts: opts("Color:Red", "Größe:S,M"), planned: combos("Color=Red/Größe=M", "Color=Red/Größe=S"),
			wantKeep: []int{-1, -1}, wantRemoved: []int{0, 1},
		},
		{
			name:      "option replaced by an unrelated one keeps nothing",
			priorOpts: opts("Size:S,M", "Color:Red"), prior: combos("Size=S/Color=Red", "Size=M/Color=Red"),
			plannedOpts: opts("Color:Red", "Material:Cotton,Wool"), planned: combos("Color=Red/Material=Cotton", "Color=Red/Material=Wool"),
			wantKeep: []int{-1, -1}, wantRemoved: []int{0, 1},
		},
		{
			name:      "everything new",
			priorOpts: opts("Size:S"), prior: combos("Size=S"),
			plannedOpts: opts("Size:S"), planned: combos(),
			wantKeep: []int{}, wantRemoved: []int{0},
		},
	}
	for _, c := range cases {
		keep, removed := matchVariants(c.priorOpts, c.prior, c.plannedOpts, c.planned)
		if !reflect.DeepEqual(keep, c.wantKeep) || !reflect.DeepEqual(removed, c.wantRemoved) {
			t.Errorf("%s: got keep %v removed %v, want keep %v removed %v", c.name, keep, removed, c.wantKeep, c.wantRemoved)
		}
	}
}

func TestMatchOptionValues(t *testing.T) {
	m := matchOptions(opts("Size:S,M", "Color:Red"), opts("Color:Rot,Blue", "Größe:XS,S,M"))
	// Color matches by name; Größe sits where Color was, so it is a new option rather than a rename of Size.
	if want := []int{1, -1}; !reflect.DeepEqual(m.Options, want) {
		t.Errorf("options = %v, want %v", m.Options, want)
	}
	if want := [][]int{{0, -1}, {-1, -1, -1}}; !reflect.DeepEqual(m.Values, want) {
		t.Errorf("values = %v, want %v", m.Values, want)
	}
}

func TestDescribe(t *testing.T) {
	o := opts("Size:S", "Color:Red")
	if got := describe(combo{"Color": "Red", "Size": "S"}, o); got != "Size = S, Color = Red" {
		t.Errorf("got %q", got)
	}
	if got := describe(combo{}, nil); got != "the default variant" {
		t.Errorf("got %q", got)
	}
}

func TestValidateOptionsAndVariants(t *testing.T) {
	cases := []struct {
		name     string
		opts     []option
		variants []combo
		wantErr  string
	}{
		{"ok", opts("Size:S,M", "Color:Red"), combos("Size=S/Color=Red", "Size=M/Color=Red"), ""},
		{"value without variant is allowed", opts("Size:S,M"), combos("Size=S"), ""},
		{"default variant", opts(), combos(""), ""},
		{"default needs exactly one variant", opts(), combos("", ""), "exactly one variant"},
		{"default variant has no option values", opts(), combos("Size=S"), "exactly one variant"},
		{"no variants", opts("Size:S"), combos(), "at least one variant"},
		{"too many options", opts("A:1", "B:1", "C:1", "D:1"), combos("A=1/B=1/C=1/D=1"), "at most 3"},
		{"empty option name", opts(":S"), combos("=S"), "name must not be empty"},
		{"duplicate option name", opts("Size:S", "Size:M"), combos("Size=S"), "duplicate option"},
		{"no values", []option{{Name: "Size"}}, combos("Size=S"), "at least one value"},
		{"empty value", opts("Size:S,"), combos("Size=S"), "empty value"},
		{"duplicate value", opts("Size:S,S"), combos("Size=S"), "duplicate value"},
		{"missing option value", opts("Size:S", "Color:Red"), combos("Size=S"), "must set a value for every option"},
		{"unknown option", opts("Size:S"), combos("Size=S/Color=Red"), "must set a value for every option"},
		{"value not in option", opts("Size:S"), combos("Size=M"), `"M" is not one of the values of option "Size"`},
		{"duplicate combination", opts("Size:S,M"), combos("Size=S", "Size=S"), "same option values"},
	}
	for _, c := range cases {
		err := validateOptionsAndVariants(c.opts, c.variants)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("%s: got %v, want error containing %q", c.name, err, c.wantErr)
		}
	}
}
