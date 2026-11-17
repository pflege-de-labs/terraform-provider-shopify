package metafields

import (
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
)

func TestSplitKey(t *testing.T) {
	cases := []struct {
		in, ns, key string
		ok          bool
	}{
		{"custom.material", "custom", "material", true},
		{"my_ns-1.some-key_2", "my_ns-1", "some-key_2", true},
		{"custom.a.b", "", "", false},
		{"custom", "", "", false},
		{".key", "", "", false},
		{"custom.", "", "", false},
		{"ab.key", "", "", false},
		{"custom.k", "", "", false},
		{"app--123--x.key", "", "", false},
		{"$app:x.key", "", "", false},
		{"global.title_tag", "", "", false},
	}
	for _, c := range cases {
		ns, key, err := SplitKey(c.in)
		if (err == nil) != c.ok || ns != c.ns || key != c.key {
			t.Errorf("SplitKey(%q) = %q, %q, %v; want %q, %q, ok=%v", c.in, ns, key, err, c.ns, c.key, c.ok)
		}
	}
}

func TestAppOwned(t *testing.T) {
	if !AppOwned("app--123--x") || AppOwned("custom") || AppOwned("my_app--x") {
		t.Error("AppOwned: only the app-- prefix counts")
	}
}

var live = []Live{
	{"custom", "count", "number_integer", "7"},
	{"custom", "flag", "boolean", "true"},
	{"custom", "txt", "single_line_text_field", "hi"},
	{"app--1--x", "secret", "single_line_text_field", "s"},
	{"global", "title_tag", "string", "seo"},
}

func TestReadImportTakesAllButAppOwned(t *testing.T) {
	got := Read(nil, live)
	want := map[string]Entry{
		"custom.count": {"number_integer", "7"},
		"custom.flag":  {"boolean", "true"},
		"custom.txt":   {"single_line_text_field", "hi"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestReadPriorKeepsManagedKeysOnly(t *testing.T) {
	prior := map[string]Entry{
		"custom.count":   {"number_integer", "007"},
		"custom.flag":    {"boolean", "false"},
		"custom.missing": {"single_line_text_field", "gone"},
	}
	got := Read(prior, live)
	want := map[string]Entry{
		"custom.count":   {"number_integer", "007"},
		"custom.flag":    {"boolean", "true"},
		"custom.missing": {"single_line_text_field", ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestReadEmptyPriorStaysEmpty(t *testing.T) {
	if got := Read(map[string]Entry{}, live); got == nil || len(got) != 0 {
		t.Errorf("got %v, want empty non-nil map", got)
	}
}

func TestPlan(t *testing.T) {
	want := map[string]Entry{
		"custom.same":    {"single_line_text_field", "a"},
		"custom.changed": {"single_line_text_field", "new"},
		"custom.typed":   {"json", "{}"},
		"custom.added":   {"single_line_text_field", "x"},
		"custom.clear":   {"single_line_text_field", ""},
		"custom.absent":  {"single_line_text_field", ""},
	}
	have := map[string]Entry{
		"custom.same":    {"single_line_text_field", "a"},
		"custom.changed": {"single_line_text_field", "old"},
		"custom.typed":   {"single_line_text_field", "{}"},
		"custom.clear":   {"single_line_text_field", "v"},
		"custom.absent":  {"single_line_text_field", ""},
	}
	set, del := Plan(want, have)
	if !slices.Equal(set, []string{"custom.added", "custom.changed", "custom.typed"}) || !slices.Equal(del, []string{"custom.clear"}) {
		t.Errorf("set %v, delete %v", set, del)
	}
}

func TestBatchesChunkAt25(t *testing.T) {
	var keys []string
	for i := range 60 {
		keys = append(keys, "custom.k"+strconv.Itoa(100+i))
	}
	var sizes []int
	for b := range slices.Chunk(keys, maxPerCall) {
		sizes = append(sizes, len(b))
	}
	if !slices.Equal(sizes, []int{25, 25, 10}) {
		t.Errorf("sizes %v", sizes)
	}
}

func TestErrorPath(t *testing.T) {
	keys := []string{"custom.a", "custom.b"}
	idx := 0
	cases := []struct {
		field []string
		elem  *int
		want  path.Path
		ok    bool
	}{
		{[]string{"metafields", "1", "value"}, nil, path.Root("metafields").AtMapKey("custom.b"), true},
		{[]string{"metafields", "0", "type"}, nil, path.Root("metafields").AtMapKey("custom.a"), true},
		{[]string{"metafields"}, &idx, path.Root("metafields").AtMapKey("custom.a"), true},
		{[]string{"metafields", "9", "value"}, nil, path.Root("metafields"), true},
		{[]string{"metafields"}, nil, path.Root("metafields"), true},
		{[]string{"other"}, nil, path.Empty(), false},
	}
	for _, c := range cases {
		got, ok := ErrorPath(keys, c.field, c.elem)
		if ok != c.ok || !got.Equal(c.want) {
			t.Errorf("ErrorPath(%v, %v) = %v, %v; want %v, %v", c.field, c.elem, got, ok, c.want, c.ok)
		}
	}
}
