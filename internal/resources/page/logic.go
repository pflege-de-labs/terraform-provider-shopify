package page

import (
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// inputAttrs maps pageCreate/pageUpdate input fields to attributes; the rest has no attribute.
var inputAttrs = map[string]string{
	"handle": "handle", "title": "title", "body": "body", "templateSuffix": "template_suffix", "isPublished": "published",
}

// errorPath maps a userError field, relative to `page`, to an attribute.
func errorPath(field []string) (path.Path, bool) {
	if len(field) < 2 || field[0] != "page" {
		return path.Empty(), false
	}
	if a, ok := inputAttrs[field[1]]; ok {
		return path.Root(a), true
	}
	return path.Empty(), false
}

// handleQuery is the pages(query:) filter for a handle; the caller still compares handles, since the search is fuzzy.
func handleQuery(handle string) string {
	return "handle:" + strconv.Quote(handle)
}

func matchHandle(nodes []client.FindPagesPagesPageConnectionNodesPage, handle string) *client.PageFields {
	for i := range nodes {
		if nodes[i].Handle == handle {
			return &nodes[i].PageFields
		}
	}
	return nil
}

// goneOnDelete reports whether pageDelete failed only because the page no longer exists.
func goneOnDelete(errs []client.DeletePagePageDeletePageDeletePayloadUserErrorsPageDeleteUserError) bool {
	for _, e := range errs {
		if e.Code == nil || *e.Code != client.PageDeleteUserErrorCodeNotFound {
			return false
		}
	}
	return len(errs) > 0
}
