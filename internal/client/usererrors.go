package client

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
)

// UserError is the common shape of the per-mutation userErrors types Shopify returns inside HTTP 200.
type UserError interface {
	GetField() []string
	GetMessage() string
}

// UserErrorDiags turns generated userErrors into error diagnostics, attribute-scoped where the field maps to a path.
func UserErrorDiags[E any, P interface {
	*E
	UserError
}](summary string, errs []E) diag.Diagnostics {
	var diags diag.Diagnostics
	for i := range errs {
		e := P(&errs[i])
		if p, ok := FieldPath(e.GetField()); ok {
			diags.AddAttributeError(p, summary, e.GetMessage())
		} else {
			diags.AddError(summary, e.GetMessage())
		}
	}
	return diags
}

// FieldPath maps a userErrors field such as ["input", "fieldDefinitions", "2", "key"] to field_definitions[2].key.
// The first element names the mutation argument and is dropped.
// shortcut: assumes attribute names are the snake_case API names, add a per-resource rename map when one differs.
func FieldPath(field []string) (path.Path, bool) {
	if len(field) < 2 {
		return path.Empty(), false
	}
	p := path.Root(snake(field[1]))
	for _, f := range field[2:] {
		if i, err := strconv.Atoi(f); err == nil {
			p = p.AtListIndex(i)
		} else {
			p = p.AtName(snake(f))
		}
	}
	return p, true
}

func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}
