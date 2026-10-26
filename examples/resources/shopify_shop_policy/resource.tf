# Keep the text in the repository; edits made in the admin show up as drift.
resource "shopify_shop_policy" "terms" {
  type = "TERMS_OF_SERVICE"
  body = file("${path.module}/policies/agb.html")
}

# A type that already has text must be adopted, or the apply fails instead of overwriting it.
resource "shopify_shop_policy" "legal_notice" {
  type           = "LEGAL_NOTICE"
  body           = file("${path.module}/policies/impressum.html")
  adopt_existing = true
}

# Destroy only removes policies from state; the API can't delete them.
