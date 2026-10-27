package customersegment_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	sdkacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/acctest"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const addr = "shopify_customer_segment.test"

const (
	someQuery  = "number_of_orders = 0"
	otherQuery = "number_of_orders > 1"
)

var gidRe = regexp.MustCompile(`^gid://shopify/Segment/\d+$`)

var expectIdentity = statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.StringRegexp(gidRe)})

// testName keeps test segments apart from real ones.
func testName() string {
	return "tf-acc-segment-" + strings.ToLower(sdkacctest.RandStringFromCharSet(6, sdkacctest.CharSetAlphaNum))
}

// accSetup skips without TF_ACC, because these tests call the API before resource.Test would skip them.
func accSetup(t *testing.T) {
	t.Helper()
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skip("Acceptance tests skipped unless env 'TF_ACC' set")
	}
	acctest.PreCheck(t)
}

// all lists every segment; the list is unfiltered because the search index lags behind writes.
func all(t *testing.T) []client.SegmentFields {
	t.Helper()
	var out []client.SegmentFields
	var after *string
	for {
		res, err := client.ListSegments(context.Background(), acctest.Client(t), after)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range res.Segments.Nodes {
			out = append(out, n.SegmentFields)
		}
		if !res.Segments.PageInfo.HasNextPage {
			return out
		}
		after = res.Segments.PageInfo.EndCursor
	}
}

// lookup returns nil when no segment has the name.
func lookup(t *testing.T, name string) *client.SegmentFields {
	t.Helper()
	for _, s := range all(t) {
		if s.Name == name {
			return &s
		}
	}
	return nil
}

func mustLookup(t *testing.T, name string) *client.SegmentFields {
	t.Helper()
	s := lookup(t, name)
	if s == nil {
		t.Fatalf("segment %q does not exist", name)
	}
	return s
}

// mustCreate makes a segment through the client, outside Terraform.
func mustCreate(t *testing.T, name string) {
	t.Helper()
	res, err := client.CreateSegment(context.Background(), acctest.Client(t), name, someQuery)
	if err != nil {
		t.Fatal(err)
	}
	if res.SegmentCreate == nil || res.SegmentCreate.Segment == nil || len(res.SegmentCreate.UserErrors) > 0 {
		t.Fatalf("segmentCreate: %+v", res.SegmentCreate)
	}
}

// mustUpdate changes a segment through the client, outside Terraform.
func mustUpdate(t *testing.T, id string, query string) {
	t.Helper()
	res, err := client.UpdateSegment(context.Background(), acctest.Client(t), id, nil, &query)
	if err != nil {
		t.Fatal(err)
	}
	if res.SegmentUpdate == nil || len(res.SegmentUpdate.UserErrors) > 0 {
		t.Fatalf("segmentUpdate: %+v", res.SegmentUpdate)
	}
}

func deleteByID(t *testing.T, id string) error {
	t.Helper()
	res, err := client.DeleteSegment(context.Background(), acctest.Client(t), id)
	if err != nil {
		return err
	}
	if res.SegmentDelete == nil || len(res.SegmentDelete.UserErrors) > 0 {
		return fmt.Errorf("segmentDelete %s: %+v", id, res.SegmentDelete)
	}
	return nil
}

// leftovers returns the segments named like a test name or its suffixed copy ("name (2)").
func leftovers(t *testing.T, names []string) []client.SegmentFields {
	t.Helper()
	var out []client.SegmentFields
	for _, s := range all(t) {
		for _, n := range names {
			if s.Name == n || strings.HasPrefix(s.Name, n+" (") {
				out = append(out, s)
			}
		}
	}
	return out
}

// cleanup deletes the test segments with these names (and their suffixed copies) when the test ends, whatever state they are in.
func cleanup(t *testing.T, names ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, s := range leftovers(t, names) {
			if err := deleteByID(t, s.Id); err != nil {
				t.Error(err)
			}
		}
	})
}

// checkGone fails, and removes the leftovers, when a test segment or a suffixed copy still exists.
func checkGone(t *testing.T, names ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		left := leftovers(t, names)
		for _, s := range left {
			_ = deleteByID(t, s.Id)
		}
		if len(left) > 0 {
			return fmt.Errorf("segments still exist: %+v", left)
		}
		return nil
	}
}

func str(s string) knownvalue.Check { return knownvalue.StringExact(s) }

func stateValue(path string, check knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(addr, tfjsonpath.New(path), check)
}

func config(body string) string {
	return fmt.Sprintf("resource \"shopify_customer_segment\" \"test\" {\n%s\n}\n", body)
}

func segmentConfig(name, query string) string {
	return config(fmt.Sprintf("  name  = %q\n  query = %q\n", name, query))
}

func emptyPlan() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
}

func updatePlan() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)}}
}

func TestAccCustomerSegment_basic(t *testing.T) {
	accSetup(t)
	name := testName()
	renamed := name + "-b"
	cleanup(t, name, renamed)
	created := segmentConfig(name, someQuery)
	requeried := segmentConfig(name, otherQuery)
	rename := segmentConfig(renamed, otherQuery)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, name, renamed),
		Steps: []resource.TestStep{
			{Config: segmentConfig("", someQuery), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Attribute name string length`)},
			{Config: segmentConfig(" x ", someQuery), PlanOnly: true, ExpectError: regexp.MustCompile(`must not start or end with whitespace`)},
			{Config: segmentConfig("x", ""), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Attribute query string length`)},
			{
				// Shopify's message lands on the attribute it names.
				Config:      segmentConfig(name, "number_of_orderz = 0"),
				ExpectError: regexp.MustCompile(`(?s)Failed to create customer segment.*'number_of_orderz' filter cannot be found`),
			},
			{
				Config: created,
				Check: func(*terraform.State) error {
					if s := mustLookup(t, name); s.Query != someQuery {
						return fmt.Errorf("live segment = %+v, want query %q", s, someQuery)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("id", knownvalue.StringRegexp(gidRe)),
					stateValue("name", str(name)),
					stateValue("query", str(someQuery)),
					stateValue("adopt_existing", knownvalue.Bool(false)),
				},
			},
			{Config: created, ConfigPlanChecks: emptyPlan()},
			{
				Config:           requeried,
				ConfigPlanChecks: updatePlan(),
				Check: func(*terraform.State) error {
					if s := mustLookup(t, name); s.Query != otherQuery {
						return fmt.Errorf("live segment = %+v, want query %q", s, otherQuery)
					}
					return nil
				},
				ConfigStateChecks: []statecheck.StateCheck{stateValue("query", str(otherQuery))},
			},
			{
				// A new name renames in place: same GID.
				Config:            rename,
				ConfigPlanChecks:  updatePlan(),
				ConfigStateChecks: []statecheck.StateCheck{stateValue("name", str(renamed))},
			},
			{Config: rename, ConfigPlanChecks: emptyPlan()},
			{
				// A changed query that Shopify rejects leaves the segment and the state as they were.
				Config:      segmentConfig(renamed, "number_of_orderz = 0"),
				ExpectError: regexp.MustCompile(`(?s)Failed to update customer segment.*'number_of_orderz' filter cannot be found`),
			},
			{Config: rename, ConfigPlanChecks: emptyPlan()},
			{Config: rename, ResourceName: addr, ImportState: true, ImportStateKind: resource.ImportBlockWithResourceIdentity},
			{
				Config:                               rename,
				ResourceName:                         addr,
				ImportState:                          true,
				ImportStateId:                        renamed,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				ImportStateVerifyIgnore:              []string{"adopt_existing"},
			},
			{
				// Generated config must pass validation and plan as import-only, without adopt_existing.
				Config:          ` `,
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				GenerateConfig:  true,
			},
		},
	})
}

// Review Focus 2: name and query are both required, so there is nothing unmanaged here; another segment edited through the client must not plan.
func TestAccCustomerSegment_otherSegmentEdit(t *testing.T) {
	accSetup(t)
	name, other := testName(), testName()
	cleanup(t, name, other)
	mustCreate(t, other)
	cfg := segmentConfig(name, someQuery)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, name),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig:        func() { mustUpdate(t, mustLookup(t, other).Id, otherQuery) },
				Config:           cfg,
				ConfigPlanChecks: emptyPlan(),
			},
			{
				// An edit of the managed segment itself is drift and plans.
				PreConfig:        func() { mustUpdate(t, mustLookup(t, name).Id, otherQuery) },
				Config:           cfg,
				ConfigPlanChecks: updatePlan(),
			},
		},
	})
}

// Pattern A: an existing segment is only taken over with adopt_existing, and destroy leaves it as Terraform left it.
func TestAccCustomerSegment_adopt(t *testing.T) {
	accSetup(t)
	name := testName()
	cleanup(t, name)
	mustCreate(t, name)
	body := func(adopt bool) string {
		return config(fmt.Sprintf("  name           = %q\n  query          = %q\n  adopt_existing = %t\n", name, otherQuery, adopt))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Destroy only drops state, so the segment is still there with Terraform's query.
		CheckDestroy: func(*terraform.State) error {
			s := lookup(t, name)
			switch {
			case s == nil:
				return fmt.Errorf("adopted segment %s was deleted", name)
			case s.Query != otherQuery:
				return fmt.Errorf("adopted segment changed on destroy: %+v", s)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: body(false), ExpectError: regexp.MustCompile(`Customer segment already exists`)},
			{
				Config: body(true),
				ConfigStateChecks: []statecheck.StateCheck{
					expectIdentity,
					stateValue("query", str(otherQuery)),
					stateValue("adopt_existing", knownvalue.Bool(true)),
				},
			},
		},
	})
}

// The store's own Email subscribers segment is adopted with its live query, so adopt and destroy leave it exactly as it was (Review Focus 1).
func TestAccCustomerSegment_adoptDefault(t *testing.T) {
	accSetup(t)
	const name = "Email subscribers"
	before := lookup(t, name)
	if before == nil {
		t.Skip("the dev store has no Email subscribers segment")
	}
	snapshot := func(s *client.SegmentFields) string {
		out, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	want := snapshot(before)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if got := snapshot(lookup(t, name)); got != want {
				return fmt.Errorf("default segment changed:\n got %s\nwant %s", got, want)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: config(fmt.Sprintf("  name           = %q\n  query          = %q\n  adopt_existing = true\n", name, before.Query)),
			ConfigStateChecks: []statecheck.StateCheck{
				expectIdentity,
				stateValue("id", str(before.Id)),
				stateValue("adopt_existing", knownvalue.Bool(true)),
			},
		}},
	})
}

// Shopify suffixes a taken name instead of failing; neither create nor update may leave a suffixed segment behind.
func TestAccCustomerSegment_takenName(t *testing.T) {
	accSetup(t)
	taken, mine := testName(), testName()
	cleanup(t, taken, mine)
	mustCreate(t, taken)
	noSuffix := func() error {
		for _, s := range leftovers(t, []string{taken, mine}) {
			if s.Name != taken && s.Name != mine {
				return fmt.Errorf("segment %q was created", s.Name)
			}
		}
		return nil
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(s *terraform.State) error {
			if err := noSuffix(); err != nil {
				return err
			}
			return checkGone(t, mine)(s)
		},
		Steps: []resource.TestStep{
			{
				Config:      segmentConfig(taken, someQuery),
				ExpectError: regexp.MustCompile(`Customer segment already exists`),
				Check:       func(*terraform.State) error { return noSuffix() },
			},
			{Config: segmentConfig(mine, someQuery)},
			{
				// The rename is refused before segmentUpdate, which would silently suffix it.
				Config:      segmentConfig(taken, someQuery),
				ExpectError: regexp.MustCompile(`Name already taken`),
			},
			{Config: segmentConfig(mine, someQuery), ConfigPlanChecks: emptyPlan(), Check: func(*terraform.State) error { return noSuffix() }},
		},
	})
}

// Names that differ only in case are distinct segments, so the case-sensitive lookup is right.
func TestAccCustomerSegment_caseOnlyDiffers(t *testing.T) {
	accSetup(t)
	lower := testName()
	upper := strings.ToUpper(lower)
	cleanup(t, lower, upper)
	mustCreate(t, lower)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, upper),
		Steps: []resource.TestStep{{
			Config:            segmentConfig(upper, someQuery),
			ConfigStateChecks: []statecheck.StateCheck{stateValue("name", str(upper))},
		}},
	})
}

// A segment deleted outside Terraform is planned for recreation.
func TestAccCustomerSegment_deletedOutside(t *testing.T) {
	accSetup(t)
	name := testName()
	cleanup(t, name)
	cfg := segmentConfig(name, someQuery)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkGone(t, name),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					if err := deleteByID(t, mustLookup(t, name).Id); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)},
				},
			},
		},
	})
}
