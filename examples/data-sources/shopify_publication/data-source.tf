# A sales channel publication owned by an app, which this provider doesn't manage.
data "shopify_publication" "online_store" {
  name = "Online Store"
}

output "online_store_publication_id" {
  value = data.shopify_publication.online_store.id
}
