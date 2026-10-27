resource "shopify_customer_segment" "subscribers" {
  name  = "Newsletter-Abonnenten DE"
  query = "email_subscription_status = 'SUBSCRIBED' AND customer_countries CONTAINS 'DE'"
}

resource "shopify_collection" "pflegehilfsmittel" {
  handle = "pflegehilfsmittel"
  title  = "Pflegehilfsmittel"
}

# Code discount: 10 % off one collection for newsletter subscribers, from 30 € on.
resource "shopify_discount_basic" "newsletter" {
  title = "Newsletter 10 %"
  code  = "WILLKOMMEN10"

  # Compared by instant: Shopify returns UTC, the spelling here stays.
  starts_at = "2026-11-01T00:00:00+01:00"
  ends_at   = "2026-12-31T23:59:59+01:00"

  # Reference segments, so deleting one updates the discount first (a discount whose only segment is deleted expires).
  context = { customer_segment_ids = [shopify_customer_segment.subscribers.id] }

  customer_gets = {
    value = { percentage = 0.1 }
    items = { collection_ids = [shopify_collection.pflegehilfsmittel.id] }
  }
  minimum_requirement       = { subtotal = "30.00" }
  combines_with             = { shipping_discounts = true }
  usage_limit               = 1000
  applies_once_per_customer = true
}

# Automatic discount: 5 € off every order. Unset attributes stay with the admin.
resource "shopify_discount_basic" "five_off" {
  title = "5 € Rabatt"

  customer_gets = {
    value = { amount = { amount = "5.00" } }
    items = { all = true }
  }
}
