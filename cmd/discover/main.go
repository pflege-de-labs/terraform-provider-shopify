// Command discover is a temporary, read-only census of a store's configuration; delete once the import scope is decided.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Khan/genqlient/graphql"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

type row struct {
	Type, Filter string
	Count        int
	Keys         []string
	Status       string
}

// spec describes one census row; root is a QueryRoot field, or "shop.<field>" for a plain list on Shop.
type spec struct {
	typ, filter, root, args string
	fields                  string // node selection; "id" means count only
	key                     func(map[string]any) string
	keep                    func(map[string]any) bool
	// extra derives follow-up rows (per-profile, per-definition) from the collected nodes.
	extra func(context.Context, gql, []map[string]any) []row
}

// Roots that are a plain list or a single object rather than a connection.
var plainRoots = map[string]bool{"shopLocales": true, "consentPolicy": true, "fulfillmentConstraintRules": true}

// Values of MetafieldOwnerType in admin-2026-10.graphql.
var ownerTypes = strings.Fields(`API_PERMISSION COMPANY COMPANY_LOCATION PAYMENT_CUSTOMIZATION VALIDATION CUSTOMER
DELIVERY_CUSTOMIZATION DRAFTORDER GIFT_CARD_TRANSACTION TRANSFER MARKET CARTTRANSFORM COLLECTION MEDIA_IMAGE PRODUCT
PRODUCTVARIANT SELLING_PLAN ARTICLE BLOG PAGE FULFILLMENT_CONSTRAINT_RULE ORDER_ROUTING_LOCATION_RULE DISCOUNT ORDER
LOCATION SHOP`)

func str(k string) func(map[string]any) string {
	return func(n map[string]any) string { s, _ := n[k].(string); return s }
}

func flag(n map[string]any, k string) bool { b, _ := n[k].(bool); return b }

func specs() []spec {
	var s []spec
	for _, o := range ownerTypes {
		s = append(s, spec{typ: "metafieldDefinitions", filter: o, root: "metafieldDefinitions", args: "ownerType: " + o,
			fields: "namespace key", key: func(n map[string]any) string { return str("namespace")(n) + "." + str("key")(n) }})
	}
	general := func(want bool) func(map[string]any) bool {
		return func(n map[string]any) bool { return flag(n, "default") == want }
	}
	return append(s,
		spec{typ: "metaobjectDefinitions", filter: "-", root: "metaobjectDefinitions", fields: "type", key: str("type"), extra: metaobjects},
		spec{typ: "menus", filter: "-", root: "menus", fields: "handle isDefault", key: func(n map[string]any) string {
			if flag(n, "isDefault") {
				return str("handle")(n) + " (default)"
			}
			return str("handle")(n)
		}},
		spec{typ: "locations", filter: "includeInactive", root: "locations", args: "includeInactive: true, includeLegacy: false",
			fields: "name isActive", key: func(n map[string]any) string {
				if !flag(n, "isActive") {
					return str("name")(n) + " (inactive)"
				}
				return str("name")(n)
			}},
		spec{typ: "markets", filter: "-", root: "markets", fields: "handle type status", key: func(n map[string]any) string {
			return fmt.Sprintf("%s (%s/%s)", str("handle")(n), str("type")(n), str("status")(n))
		}},
		spec{typ: "deliveryProfiles", filter: "general", root: "deliveryProfiles", args: "merchantOwnedOnly: true", fields: "default", keep: general(true)},
		spec{typ: "deliveryProfiles", filter: "custom", root: "deliveryProfiles", args: "merchantOwnedOnly: true", fields: "default", keep: general(false)},
		spec{typ: "webPresences", filter: "-", root: "webPresences", fields: "id"},
		spec{typ: "shopLocales", filter: "-", root: "shopLocales", fields: "locale", key: str("locale")},
		spec{typ: "shopPolicies", filter: "-", root: "shop.shopPolicies", fields: "type", key: str("type")},
		spec{typ: "privacySettings", filter: "-", root: "privacySettings", fields: "__typename"},
		spec{typ: "consentPolicy", filter: "-", root: "consentPolicy", fields: "id"},
		spec{typ: "checkoutProfiles", filter: "-", root: "checkoutProfiles", fields: "id name", key: str("name"), extra: brandings},
		spec{typ: "urlRedirects", filter: "-", root: "urlRedirects", fields: "id"},
		spec{typ: "publications", filter: "-", root: "publications", fields: "name", key: str("name")},
		spec{typ: "catalogs", filter: "-", root: "catalogs", fields: "id"},
		spec{typ: "priceLists", filter: "-", root: "priceLists", fields: "name", key: str("name")},
		spec{typ: "paymentCustomizations", filter: "-", root: "paymentCustomizations", fields: "title", key: str("title")},
		spec{typ: "deliveryCustomizations", filter: "-", root: "deliveryCustomizations", fields: "title", key: str("title")},
		spec{typ: "validations", filter: "-", root: "validations", fields: "title", key: str("title")},
		spec{typ: "cartTransforms", filter: "-", root: "cartTransforms", fields: "functionId", key: str("functionId")},
		spec{typ: "fulfillmentConstraintRules", filter: "-", root: "fulfillmentConstraintRules", fields: "function { id }", key: func(n map[string]any) string {
			f, _ := n["function"].(map[string]any)
			return str("id")(f)
		}},
		spec{typ: "products", filter: "-", root: "products", fields: "id"},
		spec{typ: "collections", filter: "-", root: "collections", fields: "id"},
		spec{typ: "pages", filter: "-", root: "pages", fields: "id"},
		spec{typ: "blogs", filter: "-", root: "blogs", fields: "id"},
		spec{typ: "articles", filter: "-", root: "articles", fields: "id"},
		spec{typ: "discountNodes", filter: "-", root: "discountNodes", fields: "id"},
		spec{typ: "files", filter: "-", root: "files", fields: "id"},
		spec{typ: "carrierServices", filter: "-", root: "carrierServices", fields: "name", key: str("name")},
		spec{typ: "fulfillmentServices", filter: "-", root: "shop.fulfillmentServices", fields: "handle", key: str("handle")},
		spec{typ: "sellingPlanGroups", filter: "-", root: "sellingPlanGroups", fields: "id"},
		spec{typ: "segments", filter: "-", root: "segments", fields: "id"},
		spec{typ: "returnPolicyProfiles", filter: "-", root: "returnPolicyProfiles", fields: "name", key: str("name")},
	)
}

// classify maps a request error to a table status; missing scopes are expected, not fatal.
func classify(err error) string {
	if err == nil {
		return "ok"
	}
	var list gqlerror.List
	if errors.As(err, &list) {
		for _, e := range list {
			if e.Extensions["code"] == "ACCESS_DENIED" {
				return "no scope"
			}
		}
	}
	return "error: " + strings.Join(strings.Fields(strings.ReplaceAll(err.Error(), "|", "/")), " ")
}

func render(rows []row) string {
	var b strings.Builder
	b.WriteString("| type | filter | count | keys | status |\n|---|---|---|---|---|\n")
	for _, r := range rows {
		keys := r.Keys
		extra := ""
		if len(keys) > 20 {
			extra = fmt.Sprintf(" … (+%d)", len(keys)-20)
			keys = keys[:20]
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %s%s | %s |\n", r.Type, r.Filter, r.Count, strings.Join(keys, ", "), extra, r.Status)
	}
	return b.String()
}

type gql interface {
	MakeRequest(context.Context, *graphql.Request, *graphql.Response) error
}

func query(ctx context.Context, c gql, q string, vars map[string]any) (map[string]any, error) {
	var data map[string]any
	resp := &graphql.Response{Data: &data}
	err := c.MakeRequest(ctx, &graphql.Request{OpName: "Discover", Query: q, Variables: vars}, resp)
	return data, err
}

// collect returns every node of s; a connection is paged 250 at a time, shop.* roots are plain lists.
func collect(ctx context.Context, c gql, s spec) ([]map[string]any, error) {
	var nodes []map[string]any
	toNodes := func(v any) {
		items, _ := v.([]any)
		for _, it := range items {
			if n, ok := it.(map[string]any); ok {
				nodes = append(nodes, n)
			}
		}
	}
	if field, ok := strings.CutPrefix(s.root, "shop."); ok {
		d, err := query(ctx, c, fmt.Sprintf("query Discover { shop { %s { %s } } }", field, s.fields), nil)
		if err != nil {
			return nil, err
		}
		shop, _ := d["shop"].(map[string]any)
		toNodes(shop[field])
		return nodes, nil
	}
	if plainRoots[s.root] {
		d, err := query(ctx, c, fmt.Sprintf("query Discover { %s { %s } }", s.root, s.fields), nil)
		if err != nil {
			return nil, err
		}
		toNodes(d[s.root])
		return nodes, nil
	}
	if s.root == "privacySettings" {
		d, err := query(ctx, c, "query Discover { privacySettings { "+s.fields+" } }", nil)
		if err != nil {
			return nil, err
		}
		toNodes([]any{d[s.root]})
		return nodes, nil
	}
	args := "first: 250, after: $after"
	if s.args != "" {
		args += ", " + s.args
	}
	q := fmt.Sprintf("query Discover($after: String) { %s(%s) { nodes { %s } pageInfo { hasNextPage endCursor } } }", s.root, args, s.fields)
	var after any
	for {
		d, err := query(ctx, c, q, map[string]any{"after": after})
		if err != nil {
			return nodes, err
		}
		conn, _ := d[s.root].(map[string]any)
		toNodes(conn["nodes"])
		pi, _ := conn["pageInfo"].(map[string]any)
		if !flag(pi, "hasNextPage") {
			return nodes, nil
		}
		after = pi["endCursor"]
	}
}

func run(ctx context.Context, c gql) []row {
	var rows []row
	for _, s := range specs() {
		nodes, err := collect(ctx, c, s)
		r := row{Type: s.typ, Filter: s.filter, Status: classify(err)}
		for _, n := range nodes {
			if s.keep != nil && !s.keep(n) {
				continue
			}
			r.Count++
			if s.key != nil {
				r.Keys = append(r.Keys, s.key(n))
			}
		}
		rows = append(rows, r)
		if s.extra != nil && err == nil {
			rows = append(rows, s.extra(ctx, c, nodes)...)
		}
	}
	return rows
}

// brandings counts profiles that have checkout branding; the root needs a profile ID.
func brandings(ctx context.Context, c gql, profiles []map[string]any) []row {
	r := row{Type: "checkoutBranding", Filter: "per profile"}
	var err error
	for _, p := range profiles {
		d, e := query(ctx, c, "query Discover($id: ID!) { checkoutBranding(checkoutProfileId: $id) { __typename } }", map[string]any{"id": str("id")(p)})
		if e != nil {
			err = e
			break
		}
		if d["checkoutBranding"] != nil {
			r.Count++
		}
	}
	r.Status = classify(err)
	return []row{r}
}

// metaobjects counts entries per definition type; the root requires a type.
func metaobjects(ctx context.Context, c gql, defs []map[string]any) []row {
	var rows []row
	for _, d := range defs {
		t := str("type")(d)
		nodes, err := collect(ctx, c, spec{root: "metaobjects", args: "type: " + strconv.Quote(t), fields: "id"})
		rows = append(rows, row{Type: "metaobjects", Filter: t, Count: len(nodes), Status: classify(err)})
	}
	return rows
}

func main() {
	cl, err := client.New(client.Config{
		StoreDomain:  os.Getenv("SHOPIFY_STORE_DOMAIN"),
		ClientID:     os.Getenv("SHOPIFY_CLIENT_ID"),
		ClientSecret: os.Getenv("SHOPIFY_CLIENT_SECRET"),
		AccessToken:  os.Getenv("SHOPIFY_ACCESS_TOKEN"),
		MaxRetries:   5,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	rows := run(context.Background(), cl)
	fmt.Print(render(rows))
	// Every row failing the same non-scope way means bad credentials or store, not a census.
	for _, r := range rows {
		if r.Status == "ok" || r.Status == "no scope" {
			return
		}
	}
	fmt.Fprintln(os.Stderr, "all queries failed")
	os.Exit(1)
}
