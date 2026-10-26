# Core fields only; unset attributes (vendor here) keep whatever the admin has.
resource "shopify_product" "bettschutz" {
  handle           = "bettschutzeinlage"
  title            = "Bettschutzeinlage"
  status           = "DRAFT"
  product_type     = "Pflegehilfsmittel"
  tags             = ["pflegehilfsmittel", "waschbar"]
  description_html = file("${path.module}/products/bettschutzeinlage.html")
  seo_title        = "Waschbare Bettschutzeinlage"
}

# Take over a product that already has the handle; destroy only removes it from state.
resource "shopify_product" "existing" {
  handle         = "handschuhe"
  title          = "Einmalhandschuhe"
  adopt_existing = true
}

# Destroy deletes the product with its variants, inventory and media, so it needs the flag in state first.
# Archiving (status = "ARCHIVED") is the safer way to retire a product.
resource "shopify_product" "temporary" {
  handle              = "aktion"
  title               = "Aktion"
  allow_data_deletion = true
}
