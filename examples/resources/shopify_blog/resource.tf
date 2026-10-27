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
