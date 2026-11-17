# Enables Shopify's "Material" template; an already-enabled type is adopted.
resource "shopify_standard_metaobject_definition" "material" {
  type = "shopify--material"
}

# Name, fields and access come from the template; read them with the data source.
data "shopify_metaobject_definition" "material" {
  type = shopify_standard_metaobject_definition.material.type
}

# Destroy only removes the definition from state; its entries stay.
