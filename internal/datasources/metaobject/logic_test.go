package metaobject

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

func str(s string) *string { return &s }

func entry(status *client.MetaobjectStatus) *client.MetaobjectFields {
	e := &client.MetaobjectFields{
		Id:          "gid://shopify/Metaobject/1",
		DisplayName: "Baumwolle",
		UpdatedAt:   "2026-10-27T10:00:00Z",
		Fields: []client.MetaobjectFieldsFieldsMetaobjectField{
			{Key: "label", Value: str("Baumwolle")},
			{Key: "tags", Value: str(`["a","b"]`)},
			{Key: "empty", Value: nil},
		},
	}
	if status != nil {
		e.Capabilities.Publishable = &client.MetaobjectFieldsCapabilitiesMetaobjectCapabilityDataPublishable{Status: *status}
	}
	return e
}

func TestToModel_fieldsKeepNullEntries(t *testing.T) {
	m, diags := toModel("shopify--material", "baumwolle", entry(nil))
	if diags.HasError() {
		t.Fatal(diags)
	}
	got := m.Fields.Elements()
	if len(got) != 3 {
		t.Fatalf("want 3 entries including the one without a value, got %d", len(got))
	}
	if got["label"] != types.StringValue("Baumwolle") {
		t.Errorf("label = %v", got["label"])
	}
	if got["tags"] != types.StringValue(`["a","b"]`) {
		t.Errorf("list value must stay a JSON string: %v", got["tags"])
	}
	if !got["empty"].IsNull() {
		t.Errorf("a field without a value must be a null entry: %v", got["empty"])
	}
	if m.ID.ValueString() != "gid://shopify/Metaobject/1" || m.DisplayName.ValueString() != "Baumwolle" ||
		m.UpdatedAt.ValueString() != "2026-10-27T10:00:00Z" || m.Type.ValueString() != "shopify--material" || m.Handle.ValueString() != "baumwolle" {
		t.Errorf("scalar attributes wrong: %+v", m)
	}
}

func TestToModel_status(t *testing.T) {
	m, _ := toModel("t", "h", entry(nil))
	if !m.Status.IsNull() {
		t.Errorf("no publishable capability must give null status, got %v", m.Status)
	}
	draft := client.MetaobjectStatusDraft
	m, _ = toModel("t", "h", entry(&draft))
	if m.Status.ValueString() != "DRAFT" {
		t.Errorf("status = %v", m.Status)
	}
}

func TestNotFoundDetail(t *testing.T) {
	got := notFoundDetail("shopify--material", "baumwolle")
	if !strings.HasPrefix(got, "no metaobject shopify--material/baumwolle") {
		t.Errorf("detail must name type and handle first: %q", got)
	}
	if !strings.Contains(got, "type may not exist") || !strings.Contains(got, "read_metaobjects") {
		t.Errorf("detail must hint at unknown type and missing access: %q", got)
	}
}
