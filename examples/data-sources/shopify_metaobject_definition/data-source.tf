# A definition owned by Shopify, which can't be imported into a resource.
data "shopify_metaobject_definition" "material" {
  type = "shopify--material"
}

output "material_fields" {
  value = [for f in data.shopify_metaobject_definition.material.field : f.key]
}
