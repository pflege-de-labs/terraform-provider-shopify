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
