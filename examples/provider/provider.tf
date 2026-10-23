# Credentials are usually set via SHOPIFY_STORE_DOMAIN, SHOPIFY_CLIENT_ID and SHOPIFY_CLIENT_SECRET.
provider "shopify" {
  store_domain  = "example.myshopify.com"
  client_id     = var.shopify_client_id
  client_secret = var.shopify_client_secret
}
