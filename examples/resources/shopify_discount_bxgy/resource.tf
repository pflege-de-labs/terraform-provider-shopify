resource "shopify_collection" "pflegehilfsmittel" {
  handle = "pflegehilfsmittel"
  title  = "Pflegehilfsmittel"
}

# Automatic discount: buy 2 items of the collection, get 1 of them free (at most once per order).
resource "shopify_discount_bxgy" "zwei_fuer_eins" {
  title                = "2 kaufen, 1 gratis"
  uses_per_order_limit = 1

  customer_buys = {
    quantity = 2
    items    = { collection_ids = [shopify_collection.pflegehilfsmittel.id] }
  }
  customer_gets = {
    quantity   = 1
    percentage = 1
    items      = { collection_ids = [shopify_collection.pflegehilfsmittel.id] }
  }
}

# Code discount: spend 30 € on the collection, get 2 of its items 5 € off each. Unset attributes stay with the admin.
resource "shopify_discount_bxgy" "bonus" {
  title = "Bonus"
  code  = "BONUS5"

  customer_buys = {
    amount = "30.00"
    items  = { collection_ids = [shopify_collection.pflegehilfsmittel.id] }
  }
  customer_gets = {
    quantity = 2
    amount   = "5.00"
    items    = { collection_ids = [shopify_collection.pflegehilfsmittel.id] }
  }
}
