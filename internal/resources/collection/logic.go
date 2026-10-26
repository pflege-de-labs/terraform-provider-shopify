package collection

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// source is one entry of Collection.sources as CollectionFields reads it.
type source struct {
	Typename   string               `json:"__typename"`
	ID         string               `json:"id"`
	App        *struct{ ID string } `json:"app"`
	Shareable  bool                 `json:"shareable"`
	TargetType string               `json:"targetType"`
	Inclusion  *struct {
		MatchType  *string          `json:"matchType"`
		Conditions []map[string]any `json:"conditions"`
		Selections selectionPage    `json:"selections"`
	} `json:"inclusion"`
}

type selectionPage struct {
	Nodes []struct {
		Product    struct{ ID string } `json:"product"`
		VariantIDs []string            `json:"variantIds"`
	} `json:"nodes"`
	PageInfo struct {
		HasNextPage bool    `json:"hasNextPage"`
		EndCursor   *string `json:"endCursor"`
	} `json:"pageInfo"`
}

func decodeSources(raw json.RawMessage) ([]source, error) {
	var out []source
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decoding collection sources: %w", err)
	}
	return out, nil
}

// membership is the inclusion of the one source the resource manages.
type membership struct {
	SourceID     string
	MatchType    *string
	Conditions   []any // input shape
	ConditionIDs []string
	ProductIDs   []string // sorted
}

// membershipOf returns the managed membership, nil without a source, or an error saying why the sources can't be managed.
func membershipOf(sources []source) (*membership, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	if len(sources) > 1 {
		types := make([]string, len(sources))
		for i, s := range sources {
			types[i] = s.Typename
		}
		return nil, fmt.Errorf("the collection has %d sources (%s); only one conditions source can be managed", len(sources), strings.Join(types, ", "))
	}
	s := sources[0]
	switch {
	case s.Typename != "CollectionConditionsSource" || s.Inclusion == nil:
		return nil, fmt.Errorf("the collection has a %s source; only a conditions source can be managed", s.Typename)
	case s.App != nil:
		return nil, errors.New("the collection's source belongs to an app")
	case s.Shareable:
		return nil, errors.New("the collection's source is shareable")
	case s.TargetType != "PRODUCTS":
		return nil, fmt.Errorf("the collection's source targets %s, not PRODUCTS", s.TargetType)
	}
	conds, ids, err := liveConditions(s.Inclusion.Conditions)
	if err != nil {
		return nil, err
	}
	m := &membership{SourceID: s.ID, MatchType: s.Inclusion.MatchType, Conditions: conds, ConditionIDs: ids, ProductIDs: []string{}}
	for _, n := range s.Inclusion.Selections.Nodes {
		if len(n.VariantIDs) > 0 {
			return nil, fmt.Errorf("the collection selects variants of product %s", n.Product.ID)
		}
		m.ProductIDs = append(m.ProductIDs, n.Product.ID)
	}
	slices.Sort(m.ProductIDs)
	return m, nil
}

// diffIDs returns the IDs to add and to remove to get from live to want, sorted.
func diffIDs(live, want []string) (add, remove []string) {
	for _, id := range want {
		if !slices.Contains(live, id) {
			add = append(add, id)
		}
	}
	for _, id := range live {
		if !slices.Contains(want, id) {
			remove = append(remove, id)
		}
	}
	slices.Sort(add)
	slices.Sort(remove)
	return add, remove
}

// desired is the configured membership; nil fields are unmanaged.
type desired struct {
	MatchType  *string
	Conditions json.RawMessage
	ProductIDs []string
}

// sourceChanges is the source part of a write: at most one of create, update or delete.
type sourceChanges struct {
	Create *client.CollectionCreateSourceInclusionInput
	Update *client.CollectionUpdateSourceInclusionInput
	Delete bool
}

// planSource turns the configured membership into source changes against live (nil: no source).
// Shopify rejects a source without conditions and products, so emptying one deletes it.
func planSource(live *membership, want desired) (sourceChanges, error) {
	if want.MatchType == nil && want.Conditions == nil && want.ProductIDs == nil {
		return sourceChanges{}, nil
	}
	var cur membership
	if live != nil {
		cur = *live
	}
	conds := cur.Conditions
	if want.Conditions != nil {
		var err error
		if conds, err = parseConditions(string(want.Conditions)); err != nil {
			return sourceChanges{}, err
		}
	}
	products := cur.ProductIDs
	if want.ProductIDs != nil {
		products = want.ProductIDs
	}
	if len(conds) == 0 && len(products) == 0 {
		if want.MatchType != nil {
			return sourceChanges{}, errors.New("match_type needs at least one condition or product in the collection; Shopify keeps no source without them")
		}
		// An empty source, such as frontpage's, stays: Shopify made it and it can't be recreated.
		return sourceChanges{Delete: live != nil && (len(cur.Conditions) > 0 || len(cur.ProductIDs) > 0)}, nil
	}
	matchType := (*client.CollectionConditionMatchType)(want.MatchType)
	if live == nil {
		in := &client.CollectionCreateSourceInclusionInput{MatchType: matchType, Selections: selections(sorted(products))}
		if len(conds) > 0 {
			in.Conditions = want.Conditions
		}
		return sourceChanges{Create: in}, nil
	}
	in := client.CollectionUpdateSourceInclusionInput{}
	if want.MatchType != nil && (cur.MatchType == nil || *cur.MatchType != *want.MatchType) {
		in.MatchType = matchType
	}
	if want.Conditions != nil && !jsonEqual(cur.Conditions, conds, "") {
		in.ConditionsToDelete = cur.ConditionIDs
		if len(conds) > 0 {
			in.ConditionsToCreate = want.Conditions
		}
	}
	if want.ProductIDs != nil {
		add, remove := diffIDs(cur.ProductIDs, want.ProductIDs)
		in.SelectionsToAdd, in.SelectionsToRemove = selections(add), selections(remove)
	}
	if in.MatchType == nil && in.ConditionsToDelete == nil && in.ConditionsToCreate == nil && in.SelectionsToAdd == nil && in.SelectionsToRemove == nil {
		return sourceChanges{}, nil
	}
	return sourceChanges{Update: &in}, nil
}

func sorted(ids []string) []string {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

func selections(ids []string) []client.CollectionInclusionProductSelectionInput {
	if len(ids) == 0 {
		return nil
	}
	out := make([]client.CollectionInclusionProductSelectionInput, len(ids))
	for i, id := range ids {
		out[i].ProductId = id
	}
	return out
}

// inputAttrs maps CollectionCreateInput/CollectionUpdateInput keys to attributes.
var inputAttrs = map[string]string{
	"title": "title", "handle": "handle", "descriptionHtml": "description_html",
	"sortOrder": "sort_order", "templateSuffix": "template_suffix",
}

// sourceAttrs maps keys below a source's inclusion to attributes.
var sourceAttrs = map[string]string{
	"conditions": "conditions", "conditionsToCreate": "conditions", "conditionsToDelete": "conditions",
	"selections": "product_ids", "selectionsToAdd": "product_ids", "selectionsToRemove": "product_ids",
	"matchType": "match_type",
}

// errorPath maps a collectionCreate/Update userError to an attribute; a taken handle names only the input, so its message decides.
func errorPath(e client.UserError) (path.Path, bool) {
	field := e.GetField()
	switch {
	case len(field) == 1 && field[0] == "collection" && strings.Contains(e.GetMessage(), "Handle has already been taken"):
		return path.Root("handle"), true
	case len(field) < 2 || field[0] != "collection":
		return path.Empty(), false
	case field[1] == "seo" && len(field) > 2 && (field[2] == "title" || field[2] == "description"):
		return path.Root("seo_" + field[2]), true
	case strings.HasPrefix(field[1], "sources"):
		for _, f := range field[2:] {
			if a, ok := sourceAttrs[f]; ok {
				return path.Root(a), true
			}
		}
		return path.Empty(), false
	}
	if a, ok := inputAttrs[field[1]]; ok {
		return path.Root(a), true
	}
	return path.Empty(), false
}
