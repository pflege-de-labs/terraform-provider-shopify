# Product GIDs differ per shop; the SKU finds the same product in each.
data "shopify_product" "bettschutz" {
  sku = "BS-100"
}

resource "shopify_collection" "bestselling" {
  title       = "Bestselling"
  handle      = "bestselling"
  product_ids = [data.shopify_product.bettschutz.id]
}
