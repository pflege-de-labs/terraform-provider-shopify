package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Khan/genqlient/graphql"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

func TestClassify(t *testing.T) {
	denied := gqlerror.List{{Message: "denied", Extensions: map[string]any{"code": "ACCESS_DENIED"}}}
	other := gqlerror.List{{Message: "boom", Extensions: map[string]any{"code": "INTERNAL"}}}
	cases := map[string]struct {
		err  error
		want string
	}{
		"nil":     {nil, "ok"},
		"denied":  {denied, "no scope"},
		"wrapped": {fmt.Errorf("x: %w", denied), "no scope"},
		"other":   {other, "error: input error: boom"},
		"plain":   {errors.New("net down"), "error: net down"},
	}
	for name, c := range cases {
		got := classify(c.err)
		if name == "other" {
			if !strings.HasPrefix(got, "error: ") || !strings.Contains(got, "boom") {
				t.Errorf("%s: got %q", name, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}

func TestRender(t *testing.T) {
	var keys []string
	for i := range 22 {
		keys = append(keys, fmt.Sprintf("k%d", i))
	}
	out := render([]row{
		{Type: "menus", Filter: "-", Count: 2, Keys: []string{"main", "footer"}, Status: "ok"},
		{Type: "markets", Filter: "-", Count: 22, Keys: keys, Status: "ok"},
		{Type: "segments", Filter: "-", Status: "no scope"},
	})
	for _, want := range []string{
		"| menus | - | 2 | main, footer | ok |",
		"k19 … (+2) | ok |",
		"| segments | - | 0 |  | no scope |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "k20,") || strings.Contains(out, "k20 ") {
		t.Errorf("keys not truncated:\n%s", out)
	}
}

func TestSpecsCoverPhase2(t *testing.T) {
	got := map[string]bool{}
	for _, s := range specs() {
		got[s.typ] = true
	}
	for _, want := range strings.Fields(`webPresences shopLocales shopPolicies privacySettings consentPolicy checkoutAndAccountsConfigurations
		metafieldDefinitions deliveryProfiles carrierServices fulfillmentServices urlRedirects publications catalogs priceLists
		sellingPlanGroups paymentCustomizations deliveryCustomizations validations cartTransforms fulfillmentConstraintRules
		products collections pages blogs articles discountNodes metaobjectDefinitions files segments returnPolicyProfiles`) {
		if !got[want] {
			t.Errorf("no spec for %s", want)
		}
	}
}

type failSecondPage struct{ calls int }

func (f *failSecondPage) MakeRequest(_ context.Context, _ *graphql.Request, resp *graphql.Response) error {
	f.calls++
	if f.calls > 1 {
		return errors.New("net down")
	}
	data, _ := resp.Data.(*map[string]any)
	*data = map[string]any{"products": map[string]any{
		"nodes":    []any{map[string]any{"id": "1"}},
		"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "c"},
	}}
	return nil
}

func TestRunDropsPartialCountOnError(t *testing.T) {
	for _, r := range run(context.Background(), &failSecondPage{}) {
		if r.Type == "products" && (r.Count != 0 || !strings.HasPrefix(r.Status, "error")) {
			t.Errorf("got count %d status %q", r.Count, r.Status)
		}
	}
}
