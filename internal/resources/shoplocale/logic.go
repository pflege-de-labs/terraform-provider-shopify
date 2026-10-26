package shoplocale

import (
	"fmt"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// findLocale matches the code exactly: Shopify's codes are case-sensitive (`pt-BR`, not `pt-br`).
func findLocale(ls []client.ShopLocaleFields, code string) *client.ShopLocaleFields {
	for i := range ls {
		if ls[i].Locale == code {
			return &ls[i]
		}
	}
	return nil
}

// needsPublish is false for an unset (unmanaged) want, so admin changes are never overwritten.
func needsPublish(want *bool, current bool) bool {
	return want != nil && *want != current
}

// keepReason says why destroy must leave the locale in Shopify; empty means it may be disabled.
func keepReason(primary, adopted bool) string {
	switch {
	case primary:
		return "the primary locale can't be disabled"
	case adopted:
		return "it was adopted with adopt_existing"
	}
	return ""
}

func displayName(l *client.ShopLocaleFields) string {
	if l.Primary {
		return fmt.Sprintf("%s (%s, primary)", l.Locale, l.Name)
	}
	return fmt.Sprintf("%s (%s)", l.Locale, l.Name)
}
