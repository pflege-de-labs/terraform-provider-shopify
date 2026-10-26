# Turn off Shopify's cookie banner because the shop runs its own consent banner.
# This is one-way through the API; turning it back on happens in the Shopify admin.
# Unset attributes are not managed.
resource "shopify_privacy_settings" "this" {
  cookie_banner_auto_managed = false
}
