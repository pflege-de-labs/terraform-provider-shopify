# One block per owner type; an empty result warns, since it can mean a missing read scope.
list "shopify_metafield_definition" "product" {
  provider = shopify

  config {
    owner_type = "PRODUCT"
  }
}

list "shopify_metafield_definition" "customer" {
  provider = shopify

  config {
    owner_type = "CUSTOMER"
  }
}

list "shopify_metafield_definition" "order" {
  provider = shopify

  config {
    owner_type = "ORDER"
  }
}
