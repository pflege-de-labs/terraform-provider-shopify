# A definition owned by Shopify, which can't be imported into a resource.
data "shopify_metafield_definition" "material" {
  owner_type = "PRODUCT"
  namespace  = "shopify"
  key        = "material"
}

output "material_type" {
  value = data.shopify_metafield_definition.material.type
}
