# The store's general profile; Terraform owns its zones and rates.
resource "shopify_delivery_profile" "general" {
  location_group {
    location_ids = [shopify_location.warehouse_hamburg.id]

    zone {
      name      = "Germany"
      countries = ["DE"]

      method {
        name     = "Standard"
        price    = "4.95"
        currency = "EUR"
      }

      # Same name as above: rates are matched by name plus condition.
      method {
        name      = "Standard"
        price     = "0.00"
        currency  = "EUR"
        condition = { field = "TOTAL_PRICE", operator = "GREATER_THAN_OR_EQUAL_TO", value = "49.00" }
      }
    }
  }
}
