# Only the listed keys are managed; other shop metafields keep their values.
# Destroy removes the resource from state and keeps the values in Shopify.
resource "shopify_shop_metafields" "this" {
  metafields = {
    "custom.tagline" = { type = "single_line_text_field", value = "Made in Germany" }
  }
}
