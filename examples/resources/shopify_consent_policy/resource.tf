# Shopify refuses consent policy writes while its own cookie banner is on.
resource "shopify_privacy_settings" "this" {
  cookie_banner_auto_managed = false
}

resource "shopify_consent_policy" "de" {
  country_code     = "DE"
  consent_required = true
  depends_on       = [shopify_privacy_settings.this]
}

# A region is its own policy.
resource "shopify_consent_policy" "us_ca" {
  country_code               = "US"
  region_code                = "USCA"
  data_sale_opt_out_required = true
  depends_on                 = [shopify_privacy_settings.this]
}

# Destroy only removes policies from state; the rows keep their values.
