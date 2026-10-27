package blog

import (
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// inputAttrs maps blogCreate/blogUpdate input fields to attributes; the rest has no attribute.
var inputAttrs = map[string]string{
	"handle": "handle", "title": "title", "commentPolicy": "comment_policy", "templateSuffix": "template_suffix",
}

// errorPath maps a userError field, relative to `blog`, to an attribute.
func errorPath(field []string) (path.Path, bool) {
	if len(field) < 2 || field[0] != "blog" {
		return path.Empty(), false
	}
	if a, ok := inputAttrs[field[1]]; ok {
		return path.Root(a), true
	}
	return path.Empty(), false
}

// handleQuery is the blogs(query:) filter for a handle; the caller still compares handles, since the search is fuzzy.
func handleQuery(handle string) string {
	return "handle:" + strconv.Quote(handle)
}

func matchHandle(nodes []client.FindBlogsBlogsBlogConnectionNodesBlog, handle string) *client.BlogFields {
	for i := range nodes {
		if nodes[i].Handle == handle {
			return &nodes[i].BlogFields
		}
	}
	return nil
}

// goneOnDelete reports whether blogDelete failed only because the blog no longer exists.
func goneOnDelete(errs []client.DeleteBlogBlogDeleteBlogDeletePayloadUserErrorsBlogDeleteUserError) bool {
	for _, e := range errs {
		if e.Code == nil || *e.Code != client.BlogDeleteUserErrorCodeNotFound {
			return false
		}
	}
	return len(errs) > 0
}

// articlesAtRisk reports whether deleting the blog may delete articles; an unknown count counts as at risk.
func articlesAtRisk(count *client.GetBlogArticlesCountBlogArticlesCount) (bool, string) {
	switch {
	case count == nil:
		return true, "Shopify did not return the article count, so the blog may hold articles, which are deleted with it."
	case count.Count > 0:
		return true, fmt.Sprintf("It holds %d articles, which are deleted with it.", count.Count)
	}
	return false, ""
}
