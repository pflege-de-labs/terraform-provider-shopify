resource "shopify_shop_locale" "fr" {
  locale    = "fr"
  published = true
}

# Disabling a locale deletes its translations, so destroy needs allow_data_deletion = true in state.
resource "shopify_shop_locale" "it" {
  locale              = "it"
  allow_data_deletion = true
}

# The primary locale can't be changed or disabled, only adopted. Destroy removes it from state.
resource "shopify_shop_locale" "primary" {
  locale         = "de"
  adopt_existing = true
}

resource "shopify_web_presence" "ch" {
  subfolder_suffix  = "ch"
  default_locale    = shopify_shop_locale.primary.locale
  alternate_locales = [shopify_shop_locale.fr.locale]
}
