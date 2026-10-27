data "shopify_publication" "online_store" {
  name = "Online Store"
}

# Smart collection: products match the conditions, written in the API's own input shape.
resource "shopify_collection" "pflegehilfsmittel" {
  handle     = "pflegehilfsmittel"
  title      = "Pflegehilfsmittel"
  sort_order = "BEST_SELLING"
  match_type = "ANY"
  conditions = jsonencode([
    { productTag = { relation = "TAGGED_WITH", values = ["pflegehilfsmittel"], matchType = "ANY" } },
    { variantPrice = { relation = "GREATER_THAN", value = { amount = "5", currencyCode = "EUR" } } },
  ])

  # Removing an ID unpublishes the collection from that channel. Unset leaves the publications to the admin.
  publication_ids = [data.shopify_publication.online_store.id]

  # Collections behind navigation menus shouldn't be destroyed by accident.
  lifecycle {
    prevent_destroy = true
  }
}

# Manual collection: products picked by hand, referenced so their GIDs never sit in the configuration.
resource "shopify_collection" "empfehlungen" {
  handle      = "empfehlungen"
  title       = "Unsere Empfehlungen"
  product_ids = [shopify_product.bettschutz.id]
}

# Every store has frontpage; adopting it manages the title, and destroy leaves it in place.
resource "shopify_collection" "frontpage" {
  handle         = "frontpage"
  title          = "Startseite"
  adopt_existing = true
}
