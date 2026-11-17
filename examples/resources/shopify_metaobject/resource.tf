# Volatile: entries are content. Only the keys in fields are managed.
resource "shopify_metaobject" "baumwolle" {
  type   = shopify_standard_metaobject_definition.material.type
  handle = "baumwolle"
  fields = {
    label = "Baumwolle"
    # List and JSON types take a JSON string.
    synonyms = jsonencode(["Cotton", "BW"])
  }
  status = "ACTIVE"
}

# A reference takes the GID of another object; refer to its resource or data source, never a literal GID.
resource "shopify_metaobject" "shirt_material" {
  type   = shopify_metaobject_definition.product_detail.type
  handle = "shirt"
  fields = {
    material = shopify_metaobject.baumwolle.id
  }
}

# Destroy needs allow_data_deletion = true in state first, since it breaks every reference to the entry.
