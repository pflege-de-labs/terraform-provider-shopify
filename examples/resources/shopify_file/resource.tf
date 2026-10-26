# Shopify fetches the file from the URL. Local paths are not supported yet.
resource "shopify_file" "logo" {
  content_type = "IMAGE"
  filename     = "logo.png"
  source       = "https://assets.example.com/logo.png"
  # Fetch again when the file behind the URL changes.
  source_hash = "2026-10-27"
  alt         = "Company logo"

  # Deleting or replacing a file breaks everything that uses it; set this and apply before destroying.
  allow_data_deletion = false
}

resource "shopify_file" "datasheet" {
  content_type = "FILE"
  filename     = "datasheet.pdf"
  source       = "https://assets.example.com/datasheet.pdf"

  timeouts {
    create = "5m"
  }
}

# External videos have no filename; the URL is their key.
resource "shopify_file" "howto" {
  content_type = "EXTERNAL_VIDEO"
  source       = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
}

# Take over a file that exists: without source, only alt is managed.
resource "shopify_file" "existing" {
  content_type   = "IMAGE"
  filename       = "hero.jpg"
  alt            = "Hero image"
  adopt_existing = true
}
