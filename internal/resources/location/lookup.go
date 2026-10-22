package location

import (
	"strconv"
	"strings"
)

// lookupFor maps an import key to a GID (numeric legacyResourceId) or a name search query.
func lookupFor(key string) (gid, query string) {
	if _, err := strconv.ParseUint(key, 10, 64); err == nil {
		return "gid://shopify/Location/" + key, ""
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(key)
	return "", `name:"` + escaped + `"`
}
