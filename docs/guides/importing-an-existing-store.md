---
page_title: "Importing an existing store"
subcategory: ""
description: |-
  Generate configuration for what a store already has with terraform query, review it, and import it with a plan that changes nothing in Shopify.
---

# Importing an existing store

Most stores have configuration long before Terraform comes along. Every resource of this provider has a matching **list resource**, so `terraform query` can discover what the store holds and write the configuration and `import` blocks for it. You then review that configuration and apply it: the first plan only imports.

You need Terraform 1.14 or later (`terraform query`), a configured provider (see the [provider page](https://registry.terraform.io/providers/pflege-de-labs/shopify/latest/docs)) and an app that can read the types you list. The read-only app described in [Bootstrapping](https://github.com/pflege-de-labs/terraform-provider-shopify/blob/main/docs/bootstrapping.md#recommended-separate-plan-and-apply-apps) is enough for the query and the plan; the write app is only needed for the apply.

## 1. Write a query file

Create a file ending in `.tfquery.hcl` next to your configuration, with one `list` block per resource type you want to discover. The block name after the type is yours.

```terraform
list "shopify_menu" "all" {
  provider = shopify
}

list "shopify_metafield_definition" "product" {
  provider = shopify

  config {
    owner_type = "PRODUCT"
  }
}

# Large catalogues produce long generated config; limit the list when bootstrapping.
list "shopify_product" "first" {
  provider = shopify
  limit    = 50
}
```

Each list resource page shows its own example and arguments. Two are not plain "list everything":

- `shopify_metafield_definition` needs an `owner_type` and lists one owner type per block. If the app has no read scope for an owner type, Shopify returns an empty list rather than an error, so an empty result can mean a missing scope. See the scope table in [Bootstrapping](https://github.com/pflege-de-labs/terraform-provider-shopify/blob/main/docs/bootstrapping.md#2-release-a-version).
- `shopify_file` takes an optional `query` to narrow the files.

Some lists leave things out on purpose, for example `shopify_consent_policy` omits Shopify's untouched defaults and `shopify_shop_policy` skips policies without text. Read the "Lists …" sentence on the list resource's page.

## 2. Generate the configuration

```shell
terraform init
terraform query -generate-config-out=generated.tf
```

`generated.tf` holds a `resource` block and an `import` block for every object found. The `import` blocks use the object's identity (its Shopify GID), which nobody has to type. Move the blocks into your configuration, grouped by type, before you go on.

## 3. Review the generated configuration

The generated configuration describes what the store has now, so it is a starting point, not a finished design. Check at least:

- **Product references in collections.** A collection that holds products by hand has their GIDs in `product_ids`, which tie the configuration to this store. Replace them with references to `shopify_product` resources (or leave `product_ids` out so membership stays unmanaged).
- **Publication IDs.** `publication_ids` on products and collections are GIDs of sales channel publications. Publications belong to sales channel apps and are not managed by the provider; look them up with the `shopify_publication` data source (`data.shopify_publication.<name>.id`) so the configuration works on other stores too.
- **Other IDs.** Any other GID in the output (for example the target of a menu item) is specific to this store; replace it with a reference where the target is managed by Terraform too.
- **Volatile types.** Products, collections, pages, files, shop policies and the checkout and accounts configuration are edited in the admin all the time, and after the import every admin edit to a managed attribute shows up as drift and is reverted by the next apply. Drop the ones you do not want Terraform to own (delete the `resource` and `import` blocks), or delete attributes from the resource so they are not managed: an attribute you leave out of a resource is not managed. Generated page configuration carries each page's `body`, and generated product configuration carries a block per variant.
- **Objects Shopify or an app owns.** Metafield and metaobject definitions in the `shopify` namespace (`shopify--…` types for metaobjects) or created by an app cannot be imported into the resources. Remove them from the generated file. If something else in your configuration needs one, read it with the `shopify_metafield_definition` or `shopify_metaobject_definition` data source. Other Shopify-owned objects have no resource at all and are data sources too: `shopify_fulfillment_service`, `shopify_publication` and `shopify_shop`.
- **Objects that exist once per store.** The general delivery profile, the default menus, the primary locale and the privacy settings appear in the lists. Importing them is correct. Generated configuration does not set `adopt_existing`, and destroying them later only removes them from state, with a warning.
- **Data deletion guard.** Generated configuration does not set `allow_data_deletion`, which defaults to `false`, so a later destroy or replacement of metafield definitions, metaobject definitions, locations and similar types is blocked until you decide otherwise.

## 4. Plan: imports only

```shell
terraform plan
```

The plan must show only objects that will be imported: `N to import, 0 to add, 0 to change, 0 to destroy`. An update means the configuration does not match what the API returns, for example because of an edit in step 3 or an attribute that does not survive the round trip; adjust the configuration until the plan is clean, and report it as a bug if the generated configuration itself plans an update.

Importing only reads from Shopify. When the plan is clean, run `terraform apply`. Afterwards you can delete the `import` blocks.

## Importing a single object

For one object, `terraform import <address> <natural key>` works as well. The key is never a GID: a handle, a type or a composite such as `PRODUCT/custom/material`. Each resource page has an "Import" section with the key.
