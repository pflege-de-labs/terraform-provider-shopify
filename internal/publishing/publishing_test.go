package publishing

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func conn(hasNext bool, cursor string, ids ...string) client.PublicationsOfPublishableResourcePublicationsV2ResourcePublicationV2Connection {
	var c client.PublicationsOfPublishableResourcePublicationsV2ResourcePublicationV2Connection
	for _, id := range ids {
		var n client.PublicationsOfPublishableResourcePublicationsV2ResourcePublicationV2ConnectionNodesResourcePublicationV2
		n.Publication.Id = id
		c.Nodes = append(c.Nodes, n)
	}
	c.PageInfo.HasNextPage = hasNext
	c.PageInfo.EndCursor = &cursor
	return c
}

func TestDiff(t *testing.T) {
	tests := []struct {
		name        string
		live, want  []string
		add, remove []string
	}{
		{"same", []string{"a", "b"}, []string{"b", "a"}, nil, nil},
		{"add", []string{"a"}, []string{"a", "b"}, []string{"b"}, nil},
		{"remove", []string{"a", "b"}, []string{"a"}, nil, []string{"b"}},
		{"both, sorted", []string{"c", "a"}, []string{"z", "b"}, []string{"b", "z"}, []string{"a", "c"}},
		{"clear", []string{"a"}, []string{}, nil, []string{"a"}},
		{"from nothing", nil, []string{"a"}, []string{"a"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			add, remove := Diff(tt.live, tt.want)
			if !reflect.DeepEqual(add, tt.add) || !reflect.DeepEqual(remove, tt.remove) {
				t.Errorf("Diff = %v, %v; want %v, %v", add, remove, tt.add, tt.remove)
			}
		})
	}
}

func TestComplete_pagesUntilLast(t *testing.T) {
	c := conn(true, "c1", "a", "b")
	var cursors []string
	err := Complete(context.Background(), &c, func(after *string) ([]string, *string, error) {
		cursors = append(cursors, *after)
		if *after == "c1" {
			next := "c2"
			return []string{"c"}, &next, nil
		}
		return []string{"d"}, nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := IDs(c); !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("IDs = %v", got)
	}
	if !reflect.DeepEqual(cursors, []string{"c1", "c2"}) || c.PageInfo.HasNextPage {
		t.Errorf("cursors = %v, hasNextPage = %v", cursors, c.PageInfo.HasNextPage)
	}
}

func TestComplete_singlePageAndError(t *testing.T) {
	c := conn(false, "", "a")
	if err := Complete(context.Background(), &c, func(*string) ([]string, *string, error) { return nil, nil, errors.New("must not be called") }); err != nil {
		t.Fatal(err)
	}
	c = conn(true, "c1", "a")
	if err := Complete(context.Background(), &c, func(*string) ([]string, *string, error) { return nil, nil, errors.New("boom") }); err == nil {
		t.Error("expected the page error")
	}
}

func TestMismatch(t *testing.T) {
	if err := mismatch([]string{"a", "b"}, []string{"b", "a"}); err != nil {
		t.Errorf("equal sets: %v", err)
	}
	err := mismatch([]string{"a", "m"}, []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "m") || !strings.Contains(err.Error(), "market") {
		t.Errorf("missing ID should name it and the market catalog hint: %v", err)
	}
	err = mismatch([]string{"a"}, []string{"a", "x"})
	if err == nil || !strings.Contains(err.Error(), "x") {
		t.Errorf("extra ID should be named: %v", err)
	}
}

func TestValueAndWant(t *testing.T) {
	ctx := context.Background()
	set, d := Value(ctx, nil)
	if d.HasError() || set.IsNull() || len(set.Elements()) != 0 {
		t.Fatalf("Read value of no publications must be an empty set: %v %v", set, d)
	}
	if !NullIfEmpty(set).IsNull() {
		t.Error("NullIfEmpty(empty) should be null")
	}
	full, _ := Value(ctx, []string{"gid://shopify/Publication/1"})
	if NullIfEmpty(full).IsNull() {
		t.Error("NullIfEmpty(non-empty) should keep the set")
	}
	for _, unmanaged := range []types.Set{types.SetNull(types.StringType), types.SetUnknown(types.StringType)} {
		if got, _ := Want(ctx, unmanaged); got != nil {
			t.Errorf("a null or unknown set must be unmanaged (nil), got %v", got)
		}
	}
	if got, _ := Want(ctx, set); got == nil || len(got) != 0 {
		t.Errorf("an empty set means unpublish everywhere, got %#v", got)
	}
}

func TestIDPattern(t *testing.T) {
	for id, ok := range map[string]bool{
		"gid://shopify/Publication/123": true,
		"gid://shopify/Product/123":     false,
		"123":                           false,
		"":                              false,
	} {
		if idRe.MatchString(id) != ok {
			t.Errorf("idRe.MatchString(%q) = %v, want %v", id, !ok, ok)
		}
	}
}
