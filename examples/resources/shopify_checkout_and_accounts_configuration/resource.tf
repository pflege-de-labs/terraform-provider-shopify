# Name unset: the published configuration. Only the keys written here are managed.
resource "shopify_checkout_and_accounts_configuration" "live" {
  branding = jsonencode({
    designTokens = {
      colors       = { palette = { color1 = "#1A4D8F" } }
      cornerRadius = { base = 6 }
    }
    components = {
      checkbox = { cornerRadius = "LARGE" }
      # null resets this value to Shopify's default.
      primaryButton = null
    }
  })
}

# Destroy only removes the configuration from state; the branding stays.
