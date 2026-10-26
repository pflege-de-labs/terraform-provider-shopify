# Locales must already be enabled and published on the shop.
resource "shopify_web_presence" "ch" {
  subfolder_suffix  = "ch"
  default_locale    = "de"
  alternate_locales = ["fr", "it"]
}

# The shop's primary-domain presence can't be created or deleted, only adopted.
resource "shopify_web_presence" "primary" {
  domain_id      = "gid://shopify/Domain/1234567890"
  default_locale = "de"
  adopt_existing = true
}

resource "shopify_market" "ch" {
  handle           = "ch"
  name             = "Switzerland"
  enabled          = true
  web_presence_ids = [shopify_web_presence.ch.id]

  conditions = {
    regions = ["CH"]
  }

  currency_settings = {
    base_currency    = "CHF"
    local_currencies = false
  }
}
