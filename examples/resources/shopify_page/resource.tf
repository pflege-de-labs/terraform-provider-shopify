# Terraform owns handle, title and template; the text stays with the editors because body is unset.
resource "shopify_page" "ueber_uns" {
  handle          = "ueber-uns"
  title           = "Über uns"
  template_suffix = "about"
}

# Page texts are a poor fit for Terraform: every admin edit to body is reverted on the next apply.
resource "shopify_page" "impressum" {
  handle    = "impressum"
  title     = "Impressum"
  body      = file("${path.module}/pages/impressum.html")
  published = true
}

# Every store has contact; adopting it manages the title, and destroy leaves it in place.
resource "shopify_page" "contact" {
  handle         = "contact"
  title          = "Kontakt"
  adopt_existing = true
}
