# Every store has news; adopting it manages the title and comment policy, and destroy leaves it in place.
resource "shopify_blog" "news" {
  handle         = "news"
  title          = "Ratgeber"
  comment_policy = "MODERATED"
  adopt_existing = true
}

# Articles are not managed here. Renaming the handle changes every article URL, without redirects.
resource "shopify_blog" "magazin" {
  handle          = "magazin"
  title           = "Magazin"
  template_suffix = "magazin"

  # Destroying a blog with articles deletes them; set this and apply before destroying.
  allow_data_deletion = false
}

# Only the listed metafields are managed; set value = "" to delete one.
resource "shopify_blog" "ratgeber" {
  handle = "ratgeber"
  title  = "Ratgeber"
  metafields = {
    "custom.intro" = { type = "single_line_text_field", value = "Tipps und Anleitungen" }
  }
}
