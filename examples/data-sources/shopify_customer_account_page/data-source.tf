# Shopify creates the customer account pages; the GIDs differ per shop.
data "shopify_customer_account_page" "orders" {
  handle = "orders"
}

output "orders_page_id" {
  value = data.shopify_customer_account_page.orders.id
}
