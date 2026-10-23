resource "shopify_metafield_definition" "care_level" {
  owner_type  = "PRODUCT"
  namespace   = "custom"
  key         = "care_level"
  name        = "Care level"
  description = "Relevant care level for the product"
  type        = "number_integer"

  validations = {
    min = "1"
    max = "5"
  }

  access = {
    storefront = "PUBLIC_READ"
  }

  pin = true

  # Destroying or replacing deletes all stored values; opt in explicitly when needed.
  allow_data_deletion = false

  lifecycle {
    prevent_destroy = true
  }
}
