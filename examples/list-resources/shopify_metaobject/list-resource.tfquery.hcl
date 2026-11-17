# Without config, every type except app-owned ones is listed.
list "shopify_metaobject" "materials" {
  provider = shopify
  config {
    type = "shopify--material"
  }
}
