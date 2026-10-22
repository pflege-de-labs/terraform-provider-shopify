# Take over the store's default main menu; destroy only removes it from state.
resource "shopify_menu" "main" {
  handle         = "main-menu"
  title          = "Main menu"
  adopt_existing = true

  item {
    title       = "Care products"
    type        = "COLLECTION"
    resource_id = "gid://shopify/Collection/123"

    item {
      title = "Bed protection"
      type  = "HTTP"
      url   = "/collections/bed-protection"
    }
  }

  item {
    title       = "Guide"
    type        = "PAGE"
    resource_id = "gid://shopify/Page/456"
  }
}
