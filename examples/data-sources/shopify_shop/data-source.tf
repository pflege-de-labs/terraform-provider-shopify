data "shopify_shop" "this" {}

output "currency" {
  value = data.shopify_shop.this.currency_code
}
