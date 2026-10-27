package acctest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// FrontpageSources are the IDs of frontpage's sources with whether each is empty.
func FrontpageSources(t *testing.T) map[string]bool {
	t.Helper()
	res, err := client.GetCollectionByHandle(context.Background(), Client(t), "frontpage")
	if err != nil {
		t.Fatal(err)
	}
	if res.CollectionByIdentifier == nil {
		return nil
	}
	var sources []struct {
		ID        string
		Inclusion struct {
			Conditions []json.RawMessage
			Selections struct{ Nodes []json.RawMessage }
		}
	}
	if err := json.Unmarshal(res.CollectionByIdentifier.Sources, &sources); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, s := range sources {
		out[s.ID] = len(s.Inclusion.Conditions) == 0 && len(s.Inclusion.Selections.Nodes) == 0
	}
	return out
}

// KeepFrontpage removes the empty source Shopify adds to frontpage when a test creates a draft product (§9k.1).
func KeepFrontpage(t *testing.T) {
	t.Helper()
	before := FrontpageSources(t)
	t.Cleanup(func() {
		var stray []string
		for id, empty := range FrontpageSources(t) {
			if _, ok := before[id]; !ok && empty {
				stray = append(stray, id)
			}
		}
		if len(stray) == 0 {
			return
		}
		res, err := client.GetCollectionByHandle(context.Background(), Client(t), "frontpage")
		if err != nil || res.CollectionByIdentifier == nil {
			t.Errorf("frontpage: %v", err)
			return
		}
		up, err := client.UpdateCollection(context.Background(), Client(t), client.CollectionUpdateInput{Id: &res.CollectionByIdentifier.Id, SourcesToDelete: stray})
		if err == nil && up.CollectionUpdate != nil && up.CollectionUpdate.Collection != nil && len(up.CollectionUpdate.UserErrors) == 0 {
			return
		}
		// Packages run in parallel, so another test's cleanup may have removed the same source first.
		now := FrontpageSources(t)
		for _, id := range stray {
			if _, ok := now[id]; ok {
				var userErrors any
				if err == nil && up.CollectionUpdate != nil {
					userErrors = up.CollectionUpdate.UserErrors
				}
				t.Errorf("removing frontpage sources %v: %v %+v", stray, err, userErrors)
				return
			}
		}
	})
}
