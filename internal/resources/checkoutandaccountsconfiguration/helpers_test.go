package checkoutandaccountsconfiguration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// published fails the test unless the dev store has exactly one published configuration.
func published(t *testing.T) *client.CheckoutAndAccountsConfigurationFields {
	t.Helper()
	q := "is_published:true"
	res, err := client.ListCheckoutAndAccountsConfigurations(context.Background(), acctest.Client(t), &q, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.CheckoutAndAccountsConfigurations == nil || len(res.CheckoutAndAccountsConfigurations.Nodes) != 1 {
		t.Fatalf("want exactly one published configuration, got %+v", res.CheckoutAndAccountsConfigurations)
	}
	return &res.CheckoutAndAccountsConfigurations.Nodes[0].CheckoutAndAccountsConfigurationFields
}

// liveTree returns the published configuration's branding without nulls and empty objects.
func liveTree(t *testing.T) map[string]any {
	t.Helper()
	var v any
	if err := json.Unmarshal(published(t).Branding, &v); err != nil {
		t.Fatal(err)
	}
	m, _ := prune(v).(map[string]any)
	return m
}

func prune(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := map[string]any{}
	for k, c := range m {
		if c = prune(c); c != nil {
			out[k] = c
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// leaf returns the value at a dotted path, or nil.
func leaf(tree map[string]any, p string) any {
	var v any = tree
	for _, k := range strings.Split(p, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

// nest turns {"a.b": 1} into {"a": {"b": 1}}.
func nest(leaves map[string]any) map[string]any {
	out := map[string]any{}
	for p, v := range leaves {
		parts := strings.Split(p, ".")
		m := out
		for _, k := range parts[:len(parts)-1] {
			next, ok := m[k].(map[string]any)
			if !ok {
				next = map[string]any{}
				m[k] = next
			}
			m = next
		}
		m[parts[len(parts)-1]] = v
	}
	return out
}

// writeBranding sends dotted-path leaves to the published configuration; nil resets a leaf.
func writeBranding(t *testing.T, leaves map[string]any) error {
	t.Helper()
	raw, err := json.Marshal(nest(leaves))
	if err != nil {
		return err
	}
	res, err := client.UpdateCheckoutAndAccountsConfiguration(context.Background(), acctest.Client(t), published(t).Id,
		client.CheckoutAndAccountsConfigurationInput{Branding: raw})
	if err != nil {
		return err
	}
	if res.CheckoutAndAccountsConfigurationUpdate == nil {
		return errors.New("checkoutAndAccountsConfigurationUpdate returned no payload")
	}
	if errs := res.CheckoutAndAccountsConfigurationUpdate.UserErrors; len(errs) > 0 {
		return fmt.Errorf("checkoutAndAccountsConfigurationUpdate: %+v", errs)
	}
	return nil
}

func mustWrite(t *testing.T, leaves map[string]any) {
	t.Helper()
	if err := writeBranding(t, leaves); err != nil {
		t.Fatal(err)
	}
}

// ownBranding refuses paths the dev store sets, resets them with null after the test, and fails unless the branding is as recorded.
func ownBranding(t *testing.T, paths ...string) {
	t.Helper()
	before := liveTree(t)
	for _, p := range paths {
		if leaf(before, p) != nil {
			t.Fatalf("dev store branding already sets %s; not touching it", p)
		}
	}
	t.Logf("branding before: %s", mustJSON(before))
	t.Cleanup(func() {
		restore := map[string]any{}
		for _, p := range paths {
			restore[p] = nil
		}
		if err := writeBranding(t, restore); err != nil {
			t.Error(err)
			return
		}
		after := liveTree(t)
		t.Logf("branding after: %s", mustJSON(after))
		if !reflect.DeepEqual(before, after) {
			t.Errorf("branding not restored:\nbefore %s\nafter  %s", mustJSON(before), mustJSON(after))
		}
	})
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// checkLive fails unless every dotted path has the given live value (nil = unset).
func checkLive(t *testing.T, want map[string]any) resource.TestCheckFunc {
	return func(*terraform.State) error {
		tree := liveTree(t)
		for p, w := range want {
			if got := leaf(tree, p); !reflect.DeepEqual(got, w) {
				return fmt.Errorf("live %s = %v, want %v", p, got, w)
			}
		}
		return nil
	}
}
