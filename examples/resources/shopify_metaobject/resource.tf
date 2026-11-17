# Volatile: entries are content. Only the keys in fields are managed.
resource "shopify_metaobject" "shipping_faq" {
  # Refer to the definition, so Terraform creates the type first.
  type   = shopify_metaobject_definition.faq.type
  handle = "versandkosten"
  fields = {
    question = "Was kostet der Versand?"
    # Rich text, list and JSON types take a JSON string.
    answer = jsonencode({
      type     = "root"
      children = [{ type = "paragraph", children = [{ type = "text", value = "Ab 55 € versandkostenfrei." }] }]
    })
  }
  status = "ACTIVE"
}

# Destroy needs allow_data_deletion = true in state first, since it breaks every reference to the entry.
