# All sales channel publications, e.g. to map exported publication_ids to names.
data "shopify_publications" "all" {}

output "publication_names_by_id" {
  value = { for p in data.shopify_publications.all.publications : p.id => p.name }
}
