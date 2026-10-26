# A fulfillment service owned by Shopify or another app, which this provider doesn't manage.
data "shopify_fulfillment_service" "manual" {
  handle = "manual"
}

output "manual_location_id" {
  value = data.shopify_fulfillment_service.manual.location_id
}
