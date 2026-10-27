package customersegment

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// errorPath maps a userError field to an attribute; the segment mutations take name and query as top-level arguments.
func errorPath(field []string) (path.Path, bool) {
	if len(field) == 1 && (field[0] == "name" || field[0] == "query") {
		return path.Root(field[0]), true
	}
	return path.Empty(), false
}

// goneOnDelete reports whether segmentDelete failed only because the segment no longer exists; the error has no code, so the message decides.
func goneOnDelete(errs []client.DeleteSegmentSegmentDeleteSegmentDeletePayloadUserErrorsUserError) bool {
	for _, e := range errs {
		if !strings.Contains(strings.ToLower(e.Message), "does not exist") {
			return false
		}
	}
	return len(errs) > 0
}
