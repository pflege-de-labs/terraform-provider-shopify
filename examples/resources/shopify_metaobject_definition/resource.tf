resource "shopify_metaobject_definition" "faq" {
  type             = "faq_entry"
  name             = "FAQ entry"
  display_name_key = "question"

  access = {
    storefront = "PUBLIC_READ"
  }

  capabilities = {
    publishable  = true
    translatable = true
  }

  field {
    key      = "question"
    name     = "Question"
    type     = "single_line_text_field"
    required = true
  }

  field {
    key  = "answer"
    name = "Answer"
    type = "rich_text_field"
  }

  # Removing fields, changing a field type or destroying deletes entry data; opt in explicitly when needed.
  allow_data_deletion = false
}
