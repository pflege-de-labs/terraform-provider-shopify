# Large catalogues produce long generated config; limit the list when bootstrapping.
list "shopify_product" "all" {
  provider = shopify
  limit    = 50
}
