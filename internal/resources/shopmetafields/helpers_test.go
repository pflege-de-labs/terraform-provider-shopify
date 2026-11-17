package shopmetafields_test

import "regexp"

var (
	shopGIDRe    = regexp.MustCompile(`^gid://shopify/Shop/\d+$`)
	invalidKeyRe = regexp.MustCompile(`import it with "default"`)
)
