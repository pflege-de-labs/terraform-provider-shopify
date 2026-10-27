package product

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func mediaList(t *testing.T, ids ...string) types.List {
	t.Helper()
	l, d := types.ListValueFrom(context.Background(), types.StringType, append([]string{}, ids...))
	if d.HasError() {
		t.Fatal(d)
	}
	return l
}

func withMedia(ids ...string) *client.ProductFields {
	p := &client.ProductFields{Id: "gid://shopify/Product/1", Handle: "h"}
	for _, id := range ids {
		p.Media.Nodes = append(p.Media.Nodes, &client.MediaIDMediaImage{Id: id})
	}
	return p
}

func TestBuildInputMedia(t *testing.T) {
	cases := []struct {
		name  string
		media types.List
		want  string
	}{
		{"ordered", mediaList(t, "gid://shopify/MediaImage/2", "gid://shopify/ExternalVideo/1"), `[{"id":"gid://shopify/MediaImage/2"},{"id":"gid://shopify/ExternalVideo/1"}]`},
		{"empty clears", mediaList(t), `[]`},
	}
	for _, c := range cases {
		m := unsetModel()
		m.MediaIDs = c.media
		if got := string(inputJSON(t, m)["files"]); got != c.want {
			t.Errorf("%s: files = %s, want %s", c.name, got, c.want)
		}
	}
	m := unsetModel()
	m.MediaIDs = types.ListNull(types.StringType)
	if _, ok := inputJSON(t, m)["files"]; ok {
		t.Error("files was sent for unset media_ids, want it left out so the admin's media stay")
	}
}

func TestBuildInputNamesRemovedMedia(t *testing.T) {
	cases := []struct {
		name  string
		media types.List
		live  *client.ProductFields
		want  []string
	}{
		{"removed and reordered", mediaList(t, "c", "a"), withMedia("a", "b", "c"), []string{"b"}},
		{"cleared", mediaList(t), withMedia("a", "b"), []string{"a", "b"}},
		{"added only", mediaList(t, "a", "b"), withMedia("a"), nil},
		{"unset never removes", types.ListNull(types.StringType), withMedia("a"), nil},
		{"create", mediaList(t, "a"), nil, nil},
	}
	for _, c := range cases {
		m := unsetModel()
		m.MediaIDs = c.media
		_, _, removed, d := buildInput(context.Background(), &m, c.live)
		if d.HasError() {
			t.Fatalf("%s: %v", c.name, d)
		}
		if !slices.Equal(removed, c.want) {
			t.Errorf("%s: removed = %v, want %v", c.name, removed, c.want)
		}
	}
}

func TestSetFromAPIMedia(t *testing.T) {
	ctx := context.Background()
	p := withMedia("gid://shopify/MediaImage/2")
	p.Media.Nodes = append(p.Media.Nodes, &client.MediaIDExternalVideo{Id: "gid://shopify/ExternalVideo/1"})
	var m model
	if d := setFromAPI(ctx, &m, p, nil); d.HasError() {
		t.Fatal(d)
	}
	if !m.MediaIDs.Equal(mediaList(t, "gid://shopify/MediaImage/2", "gid://shopify/ExternalVideo/1")) {
		t.Errorf("media_ids = %s, want position order", m.MediaIDs)
	}
	if d := setFromAPI(ctx, &m, withMedia(), nil); d.HasError() {
		t.Fatal(d)
	}
	if !m.MediaIDs.Equal(mediaList(t)) {
		t.Errorf("media_ids = %s, want [] for a product without media", m.MediaIDs)
	}
}

func TestListModelMedia(t *testing.T) {
	ctx := context.Background()
	m, _ := listModel(ctx, withMedia())
	if !m.MediaIDs.IsNull() {
		t.Errorf("media_ids = %s, want null so generated config leaves media unmanaged", m.MediaIDs)
	}
	m, _ = listModel(ctx, withMedia("a"))
	if !m.MediaIDs.Equal(mediaList(t, "a")) {
		t.Errorf("media_ids = %s, want [a]", m.MediaIDs)
	}
}

func TestCompleteMedia(t *testing.T) {
	p := withMedia("a")
	p.Media.PageInfo.HasNextPage, p.Media.PageInfo.EndCursor = true, ptr("c1")
	var afters []string
	more := func(after *string) ([]client.MediaID, *string, error) {
		afters = append(afters, *after)
		if *after == "c1" {
			return []client.MediaID{&client.MediaIDVideo{Id: "b"}}, ptr("c2"), nil
		}
		return []client.MediaID{&client.MediaIDModel3d{Id: "c"}}, nil, nil
	}
	if err := completeMedia(p, more); err != nil {
		t.Fatal(err)
	}
	if got := mediaIDs(p); !slices.Equal(got, []string{"a", "b", "c"}) || !slices.Equal(afters, []string{"c1", "c2"}) {
		t.Errorf("media = %v after %v, want [a b c] after [c1 c2]", got, afters)
	}
	if p.Media.PageInfo.HasNextPage {
		t.Error("HasNextPage still set after the last page")
	}
	failing := withMedia()
	failing.Media.PageInfo.HasNextPage, failing.Media.PageInfo.EndCursor = true, ptr("c1")
	if err := completeMedia(failing, func(*string) ([]client.MediaID, *string, error) { return nil, nil, errors.New("boom") }); err == nil {
		t.Error("want the paging error")
	}
}

func TestRemovedIDs(t *testing.T) {
	if got := removedIDs([]string{"a", "b", "c"}, []string{"c", "x"}); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("removedIDs = %v, want [a b] in prior order", got)
	}
	if got := removedIDs(nil, []string{"a"}); got != nil {
		t.Errorf("removedIDs = %v, want nil", got)
	}
}

func TestMediaIDPattern(t *testing.T) {
	for id, want := range map[string]bool{
		"gid://shopify/MediaImage/1":    true,
		"gid://shopify/Video/1":         true,
		"gid://shopify/Model3d/1":       true,
		"gid://shopify/ExternalVideo/1": true,
		"gid://shopify/GenericFile/1":   false,
		"gid://shopify/Product/1":       false,
		"gid://shopify/MediaImage/":     false,
	} {
		if got := mediaIDRe.MatchString(id); got != want {
			t.Errorf("mediaIDRe(%q) = %t, want %t", id, got, want)
		}
	}
}
