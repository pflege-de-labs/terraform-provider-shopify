# An entry of a Shopify standard type, read by type and handle.
data "shopify_metaobject" "material" {
  type   = "shopify--material"
  handle = "baumwolle"
}

output "material_id" {
  value = data.shopify_metaobject.material.id
}

output "material_label" {
  value = data.shopify_metaobject.material.fields["label"]
}
