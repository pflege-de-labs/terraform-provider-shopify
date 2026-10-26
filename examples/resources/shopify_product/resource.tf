# Unset attributes (vendor here, sku on size M) keep whatever the admin has.
resource "shopify_product" "bettschutz" {
  handle           = "bettschutzeinlage"
  title            = "Bettschutzeinlage"
  status           = "DRAFT"
  product_type     = "Pflegehilfsmittel"
  tags             = ["pflegehilfsmittel", "waschbar"]
  description_html = file("${path.module}/products/bettschutzeinlage.html")
  seo_title        = "Waschbare Bettschutzeinlage"

  options = [
    { name = "Größe", values = ["S", "M"] },
  ]

  # Variants are identified by their option values. Removing one deletes its stock and needs allow_data_deletion = true.
  variants = [
    {
      option_values    = { "Größe" = "S" }
      price            = "19.90"
      sku              = "BSE-S"
      barcodes         = [{ value = "4006381333931", type = "EAN" }]
      inventory_policy = "DENY"
      tracked          = true
      weight           = { value = 0.4, unit = "KILOGRAMS" }
    },
    { option_values = { "Größe" = "M" }, price = "24.90", compare_at_price = "29.90" },
  ]
}

# A product without options has one default variant.
resource "shopify_product" "versandkosten" {
  handle   = "versandkosten"
  title    = "Versandkostenpauschale"
  options  = []
  variants = [{ option_values = {}, price = "4.95", requires_shipping = false }]
}

# Take over a product that already has the handle; destroy only removes it from state.
resource "shopify_product" "existing" {
  handle         = "handschuhe"
  title          = "Einmalhandschuhe"
  adopt_existing = true
}

# Destroy deletes the product with its variants, inventory and media, so it needs the flag in state first.
# Archiving (status = "ARCHIVED") is the safer way to retire a product; in production also add
# lifecycle { prevent_destroy = true }.
resource "shopify_product" "temporary" {
  handle              = "aktion"
  title               = "Aktion"
  allow_data_deletion = true
}
