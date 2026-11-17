# Automatic discount: free shipping to Germany and Austria from 50 €, for shipping rates up to 20 €.
resource "shopify_discount_free_shipping" "ab_50" {
  title = "Versandkostenfrei ab 50 €"

  destination            = { country_codes = ["DE", "AT"] }
  maximum_shipping_price = "20.00"
  minimum_requirement    = { subtotal = "50.00" }
  combines_with          = { order_discounts = true }
}

# Code discount: free shipping everywhere, once per customer. Unset attributes stay with the admin.
resource "shopify_discount_free_shipping" "versandfrei" {
  title = "Versandfrei Code"
  code  = "VERSANDFREI"

  destination               = { all = true }
  usage_limit               = 500
  applies_once_per_customer = true
}
