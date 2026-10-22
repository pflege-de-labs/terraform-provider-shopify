resource "shopify_location" "warehouse_hamburg" {
  name                   = "Warehouse Hamburg"
  fulfills_online_orders = true

  address = {
    address1     = "Musterstrasse 1"
    city         = "Hamburg"
    zip          = "20095"
    country_code = "DE"
  }

  active = true

  # Destroy deactivates by default; "delete" also needs allow_data_deletion = true.
  deletion_policy = "deactivate"
}
