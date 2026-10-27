list "shopify_file" "images" {
  provider = shopify

  config {
    query = "media_type:IMAGE"
  }
}
