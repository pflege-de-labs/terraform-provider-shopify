## 0.1.3 (Unreleased)

FEATURES:

* **New Resource:** `shopify_standard_metaobject_definition`
* **New Resource:** `shopify_metaobject`
* **New Resource:** `shopify_shop_metafields` (the shop's own metafield values; one per store)
* **New List Resource:** `shopify_shop_metafields`
* **New Resource:** `shopify_discount_free_shipping` (destination, maximum shipping price, minimum requirement, subscription flags; code and automatic)
* **New Resource:** `shopify_discount_bxgy` (buy X get Y: `customer_buys` and `customer_gets` with quantity or amount; code and automatic)
* **New List Resource:** `shopify_standard_metaobject_definition`
* **New List Resource:** `shopify_metaobject`
* **New List Resource:** `shopify_discount_free_shipping`
* **New List Resource:** `shopify_discount_bxgy`
* **New Resource:** `shopify_url_redirect` (path to target; canonical input only, `adopt_existing`; volatile)
* **New List Resource:** `shopify_url_redirect` (optional `query` filter, to copy a store's redirects with `terraform query -generate-config-out`)
* **New Data Source:** `shopify_product` (lookup by variant SKU, e.g. for `product_ids` of products Terraform doesn't manage)

ENHANCEMENTS:

* resource/shopify_collection, list/shopify_collection: new `image` attribute (`src`, `alt_text`, computed `url`). The configured `src` stays in state while the collection still has the image of the last apply; import and the list use the CDN URL, so generated config copies the image to another store.
* resource/shopify_collection, list/shopify_collection: the same optional `metafields` attribute as on products.
* resource/shopify_product, list/shopify_product: new optional `metafields` attribute, a map of `namespace.key` to `{ type, value }`. Only the listed keys are managed, `value = ""` deletes one, and import and the list take every key outside app namespaces. Reference values must come from resource references, since their GIDs differ per store.
* resource/shopify_page, resource/shopify_blog, list/shopify_page, list/shopify_blog: new optional `metafields` attribute with the same semantics as on `shopify_product`.
* resource/shopify_product, list/shopify_product: new optional `metafields` attribute on each entry of `variants`, with the same rules as the product's. Not read or written for a product with a linked option, which can't have variants managed yet.
* resource/shopify_metafield_definition: owner types `CUSTOMER`, `ORDER`, `DRAFTORDER`, `DISCOUNT`, `COMPANY` and `COMPANY_LOCATION` are covered by acceptance tests and documented with their scopes. The reference app configs add `read_companies` / `write_companies` (B2B stores only, optional).
* resource/shopify_discount_basic, list/shopify_discount_basic: a discount of another method is now refused with "use shopify_discount_<method>" instead of "not supported yet", and the list skips it with the same detail.
* list/shopify_metaobject_definition: skips Shopify's standard definitions (`shopify--…` types), which `shopify_standard_metaobject_definition` lists, so generated config no longer holds ordinary definitions for them.
* list/shopify_metafield_definition: an empty result comes with a warning, since Shopify returns one instead of an error when the app lacks the owner type's read scope.

BUG FIXES:

* resource/shopify_discount_basic, list/shopify_discount_basic: a discount whose item selection is empty (e.g. its products were deleted) reads as `product_ids = []` or `collection_ids = []` instead of failing Read, import and the list. Shopify rejects creating such a discount and updating it unless the same change adds items; the error now says so.

## 0.1.2 (Unreleased)

FEATURES:

* **New Data Source:** `shopify_customer_account_page`

BUG FIXES:

* resource/shopify_delivery_profile: `location_ids` may reference a location created in the same apply (an unknown element no longer fails validation).

## 0.1.1 (October 29, 2026)

BUG FIXES:

* resource/shopify_discount_basic: an import no longer plans an in-place update when `customer_gets.value.percentage` has no exact float64 form (e.g. `0.1`). Only `status` changed, to `(known after apply)`.

## 0.1.0 (October 28, 2026)

FEATURES:

* **New Resource:** `shopify_metafield_definition`
* **New Resource:** `shopify_metaobject_definition`
* **New Resource:** `shopify_menu`
* **New Resource:** `shopify_location`
* **New Resource:** `shopify_market`
* **New Resource:** `shopify_delivery_profile`
* **New Resource:** `shopify_shop_locale`
* **New Resource:** `shopify_privacy_settings`
* **New Resource:** `shopify_consent_policy`
* **New Resource:** `shopify_shop_policy`
* **New Resource:** `shopify_product`
* **New Resource:** `shopify_collection`
* **New Resource:** `shopify_blog`
* **New Resource:** `shopify_page`
* **New Resource:** `shopify_file`
* **New Resource:** `shopify_web_presence`
* **New Resource:** `shopify_checkout_and_accounts_configuration`
* **New Resource:** `shopify_discount_basic`
* **New Resource:** `shopify_customer_segment`
* **New Data Source:** `shopify_fulfillment_service`
* **New Data Source:** `shopify_publication`
* **New Data Source:** `shopify_metafield_definition`
* **New Data Source:** `shopify_metaobject_definition`
* **New Data Source:** `shopify_metaobject`
* **New Data Source:** `shopify_shop`
* **New List Resource:** `shopify_metafield_definition`
* **New List Resource:** `shopify_metaobject_definition`
* **New List Resource:** `shopify_menu`
* **New List Resource:** `shopify_location`
* **New List Resource:** `shopify_delivery_profile`
* **New List Resource:** `shopify_market`
* **New List Resource:** `shopify_shop_locale`
* **New List Resource:** `shopify_privacy_settings`
* **New List Resource:** `shopify_consent_policy`
* **New List Resource:** `shopify_shop_policy`
* **New List Resource:** `shopify_product`
* **New List Resource:** `shopify_collection`
* **New List Resource:** `shopify_blog`
* **New List Resource:** `shopify_page`
* **New List Resource:** `shopify_file`
* **New List Resource:** `shopify_web_presence`
* **New List Resource:** `shopify_checkout_and_accounts_configuration`
* **New List Resource:** `shopify_discount_basic`
* **New List Resource:** `shopify_customer_segment`
