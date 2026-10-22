resource "shopify_market" "dach" {
  handle  = "dach"
  name    = "DACH"
  enabled = true

  conditions = {
    regions = ["DE", "AT", "CH"]
  }

  currency_settings = {
    base_currency    = "EUR"
    local_currencies = false
  }
}
