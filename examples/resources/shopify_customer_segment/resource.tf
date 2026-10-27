# Newsletter subscribers in Germany; a discount can target the segment by ID.
resource "shopify_customer_segment" "subscribers" {
  name  = "Newsletter-Abonnenten DE"
  query = "email_subscription_status = 'SUBSCRIBED' AND customer_countries CONTAINS 'DE'"
}

# Every store starts with this segment; adopting it manages its query, and destroy leaves it in place.
resource "shopify_customer_segment" "email_subscribers" {
  name           = "Email subscribers"
  query          = "email_subscription_status = 'SUBSCRIBED'"
  adopt_existing = true
}
