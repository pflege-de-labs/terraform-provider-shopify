# Path and target are given as Shopify stores them: lower case, no trailing slash, special characters percent-encoded.
resource "shopify_url_redirect" "old_page" {
  path   = "/old-page"
  target = "/pages/new-page"
}

# The target may be an absolute URL.
resource "shopify_url_redirect" "campaign" {
  path   = "/summer"
  target = "https://example.com/collections/summer?ref=redirect"
}

# Takes over a redirect that already exists; destroy leaves it in place.
resource "shopify_url_redirect" "existing" {
  path           = "/legacy"
  target         = "/collections/all"
  adopt_existing = true
}
