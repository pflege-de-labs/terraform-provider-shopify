# Terraform Provider for Shopify: Resource Model (Draft)

**Status:** Draft for discussion · **Owner:** Jens Hausherr · **Last updated:** 2026-10-22
**Related:** [[shopify-infra-automation-approach]]

This draft covers the first scope from the approach doc: the shop data source, metafield definitions, metaobject definitions, markets, locations, shipping (delivery) profiles and menus. It targets Admin GraphQL API **2026-10** (current stable). Items marked ⚠ need checking against the API reference before implementation.

---

## 1. Provider configuration

```hcl
provider "shopify" {
  store_domain  = "pflege-dev.myshopify.com" # env: SHOPIFY_STORE_DOMAIN

  # Either client credentials (preferred) ...
  client_id     = var.shopify_client_id        # env: SHOPIFY_CLIENT_ID (sensitive)
  client_secret = var.shopify_client_secret    # env: SHOPIFY_CLIENT_SECRET (sensitive)
  # ... or a static token
  # access_token = var.shopify_token           # env: SHOPIFY_ACCESS_TOKEN (sensitive)

  api_version   = "2026-10"                    # pinned default, overridable
  max_retries   = 5                            # throttling / 5xx backoff
  concurrency   = 2                            # max in-flight GraphQL calls
}
```

**Authentication.** Dev Dashboard apps get Admin API tokens through the client credentials grant, and those tokens expire after 24 hours. A static token alone would need refreshing before every CI run.
- `client_id` + `client_secret`: Configure POSTs `grant_type=client_credentials` to `https://<store_domain>/admin/oauth/access_token`. The client keeps the token in memory only, never in state, and fetches a new one shortly before `expires_in` runs out, so long applies keep working.
- `access_token`: static token, used as is. For tokens obtained elsewhere.
- Exactly one of the two modes must be set (env vars count). Config validation only rejects `access_token` next to `client_id`/`client_secret` (`ConflictsWith`); an exactly-one check there would reject credentials from env vars. Missing or half-set credentials surface in Configure (`client.New`), when the provider connects. Both credentials and the token are `Sensitive`.
- Only works when the app and the store belong to the same Shopify organization; otherwise Shopify returns `shop_not_permitted`. Dev stores must be created from the Dev Dashboard for this reason.
- Scopes come from the released app version, not from the token request. Missing-scope errors from the API are reported clearly, naming the scope where the API gives it. Checked 2026-10-23: `ACCESS_DENIED` says only "Access denied for orders field." plus a docs link, no scope name; the message is passed through as is.
- Source: [Get API access tokens for Dev Dashboard apps](https://shopify.dev/docs/apps/build/dev-dashboard/get-api-access-tokens)

- One provider instance is one store. Use provider aliases for multiple stores.
- `concurrency` caps parallel requests no matter how many resources Terraform runs in parallel. This keeps us inside the cost-based leaky bucket.

---

## 2. Cross-cutting conventions

### IDs and import
- `id` in state = Shopify GID (`gid://shopify/MetafieldDefinition/123`).
- `terraform import` uses a **natural key**, never a GID, so imports work the same way on every store:

| Resource | Import key |
|---|---|
| `shopify_metafield_definition` | `<OWNER_TYPE>/<namespace>/<key>`, e.g. `PRODUCT/custom/material` |
| `shopify_metaobject_definition` | `<type>`, e.g. `care_level` |
| `shopify_market` | `<handle>` |
| `shopify_location` | `<name>` (no handle exists; unique among active locations, see §7). A numeric `legacyResourceId` is accepted as fallback |
| `shopify_delivery_profile` | `<name>`, or `default` for the general profile |
| `shopify_menu` | `<handle>` |
| `shopify_shop_locale` | `<locale>`, e.g. `fr` (no GID exists; `id` is the locale code) |
| `shopify_shop_policy` | `<TYPE>`, e.g. `TERMS_OF_SERVICE` |
| `shopify_privacy_settings` | `default` (singleton; `id` is the shop GID) |
| `shopify_consent_policy` | `<COUNTRY>` or `<COUNTRY>/<REGION>`, e.g. `DE`, `US/USCA` |
| `shopify_checkout_and_accounts_configuration` | `published`, or the configuration `<name>` |
| `shopify_catalog` | `<title>` (unique across all catalogs; market catalogs only, see §9i) |
| `shopify_product` | `<handle>` (unique per shop, see §9k) |
| `shopify_collection` | `<handle>` (unique per shop, see §9l) |
| `shopify_page` | `<handle>` (unique per shop, see §9m) |
| `shopify_blog` | `<handle>` (unique per shop; Shopify suffixes a taken handle, see §9n) |
| `shopify_file` | `<filename>`, e.g. `logo.png` (unique per shop); an external video by its URL, see §9o |
| `shopify_discount_basic`, `shopify_discount_free_shipping`, `shopify_discount_bxgy` | `code/<code>` for code discounts (codes are unique per shop), `automatic/<title>` for automatic ones (titles are unique among them), see §9p |
| `shopify_customer_segment` | `<name>` (unique per shop; Shopify suffixes a taken name, see §9q) |

- Read looks up by GID first. If the GID is gone, it falls back to the natural key, so a manual recreate in the admin doesn't break state.

### Resource identity and list resources
- Every resource also has Terraform resource identity: a single attribute `id` (the GID). `import { to = ...  identity = { id = "gid://..." } }` needs Terraform >= 1.12; the string `terraform import` keeps taking the natural key.
- Identity import looks the object up by GID with its typed by-ID query. Only `shopify_metafield_definition` uses `node(id:)`, bound to `json.RawMessage` because genqlient can't generate the `Node` interface.
- Identity is the GID and follows natural-key re-adoption: Read falls back to the natural key when the GID is gone, so all resources with that fallback set `MutableIdentity` and the new GID becomes the identity.
- Each resource has a list resource (`list "shopify_<name>" ...` in a `.tfquery.hcl` file). `terraform query -generate-config-out=<file>` turns the results into resource and `import` blocks, which bootstraps an existing store. Needs Terraform >= 1.14.
- tfplugindocs renders list resources to `docs/list-resources/` from `examples/list-resources/<type>/list-resource.tfquery.hcl`.
- `access.admin` on metafield/metaobject definitions is null when Shopify reports an output-only value (`PRIVATE`, `PUBLIC_READ`, `PUBLIC_READ_WRITE`), so generated config doesn't carry it.

### Attribute behaviour
- **RequiresReplace** only where the API forbids an in-place change (e.g. metafield `owner_type`/`namespace`/`key`).
- **Computed + UseStateForUnknown** for server-generated values (GIDs, timestamps).
- Optional attributes left unset are **not managed**. The provider doesn't overwrite admin edits to them. This reduces drift noise.

### Pattern A: "adopt" for objects that exist once per store
Some objects already exist on every store and can't be created or deleted (the shop, the general delivery profile, default menus such as `main-menu`/`footer`: the schema marks them `isDefault`, "can't be deleted").
- **Create** = find by natural key, then update to the desired state.
- **Delete** = remove from state only, plus a warning diagnostic.
- Normal resources get the same behaviour through an `adopt_existing = true` flag. Without it, Create fails if the natural key already exists, which prevents silently taking over objects someone else manages.

### Pattern B: guarding against data loss
Resources whose deletion destroys data (metafield and metaobject definitions, locations) get:
- `allow_data_deletion = false` (default). Delete, or a replace that would delete, **fails with an error** unless it is set to `true`.
- A docs recommendation to also use `lifecycle { prevent_destroy = true }` in production.
- At plan time, a ModifyPlan warning when a change forces a replace.

### Error handling
- GraphQL `userErrors` (returned inside HTTP 200) → Terraform diagnostics, mapped to the attribute path from `userErrors.field` where possible.
- Throttling (`THROTTLED` / `extensions.cost.throttleStatus`) → wait until enough points are restored, then retry.
- Async operations (e.g. none in phase 1: `metaobjectDefinitionDelete` returns only `deletedId`, no job) → poll the returned job until done, with a timeout set via the `timeouts` block.

---

## 3. Data source: `shopify_shop`

Read-only, for wiring and assertions.

| Attribute | Type | Notes |
|---|---|---|
| `id` | string | Shop GID |
| `name` | string | |
| `myshopify_domain` | string | |
| `primary_domain` | string | |
| `currency_code` | string | Shop base currency |
| `plan_name` | string | Useful to gate Plus-only config |
| `enabled_presentment_currencies` | list(string) | |
| `timezone` | string | IANA |

---

## 4. Resource: `shopify_metafield_definition`

```hcl
resource "shopify_metafield_definition" "care_level" {
  owner_type  = "PRODUCT"
  namespace   = "custom"
  key         = "care_level"
  name        = "Pflegegrad"
  description = "Relevant care level for the product"
  type        = "number_integer"

  validations = {
    min = "1"
    max = "5"
  }

  access = {
    admin      = "MERCHANT_READ_WRITE"
    storefront = "PUBLIC_READ"
  }

  pin                         = true
  use_as_collection_condition = true

  allow_data_deletion = false
  lifecycle { prevent_destroy = true }
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `owner_type` | string | Required, RequiresReplace |
| `namespace` | string | Required, RequiresReplace |
| `key` | string | Required, RequiresReplace |
| `type` | string | Required, RequiresReplace. Confirmed: `MetafieldDefinitionUpdateInput` has no `type` field |
| `name`, `description` | string | Updatable |
| `validations` | map(string) | Updatable. Values are strings, as in the API. JSON-shaped validations (e.g. `metaobject_definition_id`) are passed as JSON strings. Update replaces the whole list (verified by acceptance test), so the provider always sends it in full |
| `access` | object | `admin` (`MERCHANT_READ`, `MERCHANT_READ_WRITE`), `storefront` (`PUBLIC_READ`, `NONE`), `customer_account` (`READ_WRITE`, `READ`, `NONE`). Reads can return admin values that aren't valid input (e.g. `PUBLIC_READ_WRITE`), so only configured fields are sent |
| `pin` | bool | `pin` field on create/update input; read from `pinnedPosition` |
| `use_as_collection_condition` | bool | `capabilities.smartCollectionCondition.enabled`. Delete is blocked by Shopify while in use as a collection rule |
| `allow_data_deletion` | bool | Pattern B. When true, delete calls `metafieldDefinitionDelete` with `deleteAllAssociatedMetafields: true` (confirmed; the values are deleted asynchronously) |

Mutations: `metafieldDefinitionCreate`, `metafieldDefinitionUpdate`, `metafieldDefinitionDelete`.

**Read** uses `metafieldDefinition(identifier:)` with the natural key only. The query has no `id` argument, so GID-first lookup isn't possible here (identity import uses `node(id:)` bound to `json.RawMessage`, since genqlient can't generate the `Node` interface). Owner type, namespace and key can't change, so this finds a definition recreated in the admin just like GID-first with fallback would.

**List:** `owner_type` (Required). An owner type the app can't read returns an empty list, not `ACCESS_DENIED`.

---

## 5. Resource: `shopify_metaobject_definition`

```hcl
resource "shopify_metaobject_definition" "faq" {
  type             = "faq_entry"
  name             = "FAQ entry"
  display_name_key = "question"

  access = {
    admin      = "MERCHANT_READ_WRITE"
    storefront = "PUBLIC_READ"
  }

  capabilities = {
    publishable  = true
    translatable = true
  }

  field {
    key      = "question"
    name     = "Question"
    type     = "single_line_text_field"
    required = true
  }

  field {
    key  = "answer"
    name = "Answer"
    type = "rich_text_field"
  }

  allow_data_deletion = false
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `type` | string | Required, RequiresReplace, import key |
| `name`, `description`, `display_name_key` | string | Updatable |
| `access` | object | `admin`, `storefront` |
| `capabilities` | object | `publishable`, `translatable`, `renderable` (bools). `online_store` needs a `urlHandle` and is deferred |
| `field` | ordered list of blocks | `key`, `name`, `type`, `description`, `required`, `validations`. Fully managed: the list in config is the field list |
| `allow_data_deletion` | bool | Pattern B. Confirmed: `metaobjectDefinitionDelete` removes the definition at once and its entries, metafield definitions and metafields asynchronously. For field removal or type change, the planned value counts (it's an update) |

**Field diffing.** Update translates the block list into the update mutation's per-field `create` / `update` / `delete` operations (`MetaobjectFieldDefinitionOperationInput`, confirmed), matched by `key`:
- A new key → `create`.
- A changed name, description, required flag or validations → `update`.
- A removed key, or a changed field `type` (delete + create) → **destroys the data in that field**. Blocked unless `allow_data_deletion = true`, with a plan-time warning.
- A changed order → reorder (field order matters in the admin UI). Every kept field is sent as an update in config order with `resetFieldOrder: true`.
- Shopify rejects a delete and a create of the same key in one call ("duplicates other inputs"), so deletes are sent in a first call, the rest in a second. Not atomic: if the second call fails, the next apply recreates the missing fields.

**Read** uses `metaobjectDefinition(id:)`, falling back to `metaobjectDefinitionByType`.

Entries (metaobjects) are content and stay **out of scope**.

**List:** no config.

---

## 6. Resource: `shopify_market`

```hcl
resource "shopify_market" "dach" {
  handle  = "dach"
  name    = "DACH"
  enabled = true

  conditions = {
    regions = ["DE", "AT", "CH"]
  }

  currency_settings = {
    base_currency   = "EUR"
    local_currencies = false
  }

  web_presence_ids = ["gid://shopify/MarketWebPresence/1"] # optional, plain IDs for now
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `handle` | string | Required, import key. Updatable. A taken handle fails with `TAKEN` |
| `name` | string | Required |
| `enabled` | bool | Maps to `status` (`ACTIVE` / `DRAFT`); the API's `enabled` field is deprecated |
| `conditions.regions` | set(string) | Country codes. B2B (`company_location_ids`) and POS (`location_ids`) conditions come later; they are dev preview |
| `currency_settings` | object | `base_currency`, `local_currencies` |
| `catalog_ids`, `web_presence_ids` | set(string) | Links only, as plain IDs; unset = unmanaged. Web presences themselves: `shopify_web_presence` (section 9a). Catalogs: `shopify_catalog` (section 9i) owns the catalog-market relation through its `market_ids`; leave `catalog_ids` unset on markets whose catalogs Terraform manages |

Notes:
- Uses the **2025+ Markets model** (conditions-based `marketCreate` / `marketUpdate`), not the legacy region mutations.
- **Overlap is allowed** (checked on the dev store): a country can be in several markets. Only two active markets with exactly the same region set conflict (`DUPLICATE_REGION_MARKET` "There is an existing region market with the same regions", plus `DUPLICATE_UNIQUE_MARKET`). Draft markets don't conflict. So moving countries between markets needs no update order; the error is reported as is and not retried.
- Regions, web presences and catalogs are updated with add/delete lists, diffed between state and plan. `markets(query:)` can't filter by handle, so lookups by handle page through all markets.
- Follow-up resource: `shopify_web_presence`, see section 9a (blocked on scope).

**List:** no config. Non-region markets are skipped with a warning.

---

## 7. Resource: `shopify_location`

```hcl
resource "shopify_location" "warehouse_hamburg" {
  name                   = "Lager Hamburg"
  fulfills_online_orders = true

  address = {
    address1     = "Musterstraße 1"
    city         = "Hamburg"
    zip          = "20095"
    country_code = "DE"
  }

  active              = true
  deletion_policy     = "deactivate" # or "delete"
  allow_data_deletion = false        # only checked when deletion_policy = "delete"
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `name` | string | Required, import key (numeric ID as fallback) |
| `address` | object | Updatable via `locationEdit` |
| `fulfills_online_orders` | bool | |
| `active` | bool | `locationActivate` / `locationDeactivate` |
| `deletion_policy` | string | `deactivate` (default) or `delete`. Delete requires deactivating first. Shopify refuses deletion with `LOCATION_HAS_INVENTORY` / `LOCATION_HAS_PENDING_ORDERS` (from the error enum; not exercised) |
| `allow_data_deletion` | bool | Pattern B. Guards only `deletion_policy = "delete"`; deactivation is never blocked |

Mutations: `locationAdd`, `locationEdit`, `locationActivate`, `locationDeactivate`, `locationDelete`. Activate and deactivate require an `@idempotent(key:)` directive since 2026-04; the provider sends a random key per call, which retries reuse.

**Lookup:** Read uses `location(id:)`, falling back to `locations(query: "name:\"…\"", includeInactive: true)` with an exact-name match. Import accepts the name or the numeric `legacyResourceId`. The search lags a few seconds behind writes. Names are unique per shop: `locationAdd` returns `TAKEN` ("You already have a location with this name"), confirmed against an active location. A deactivated location does **not** block the name (checked on the dev store 2026-10-23), so after a `deactivate` destroy and re-create two locations share a name; name lookup then fails as ambiguous and import needs the numeric ID.

Watch out: the location limit depends on the plan, and deactivating a location that holds inventory needs a destination for that inventory: without `destinationLocationId`, `locationDeactivate` fails with `HAS_ACTIVE_INVENTORY_ERROR` (schema). The provider passes no destination, so the error is reported and nothing moves. That is a reason to default to `deactivate` and treat deletion as rare.

**List:** `include_inactive` (Optional, default `false`); `includeLegacy` is always false.

---

## 8. Resource: `shopify_delivery_profile`

This is the most complex resource in scope: profiles → location groups → zones → method definitions (rates and conditions).

```hcl
resource "shopify_delivery_profile" "general" {
  # Pattern A, implicit: always the store's general profile (no flag in phase 1)

  location_group {
    location_ids = [shopify_location.warehouse_hamburg.id]

    zone {
      name      = "Deutschland"
      countries = ["DE"]

      method {
        name   = "Standardversand"
        price  = "4.95"
        currency = "EUR"
      }

      method {
        name     = "Kostenloser Versand"
        price    = "0.00"
        currency = "EUR"
        condition = {
          field    = "TOTAL_PRICE"
          operator = "GREATER_THAN_OR_EQUAL_TO"
          value    = "49.00"
        }
      }
    }
  }
}
```

Phase 1 implements the general profile only:
- Adopted implicitly (no flag): create takes it over, destroy only removes it from state. Import key `default`.
- Flat rates with optional price or weight conditions. Carrier-calculated rates are read past and never touched. Product-to-profile assignment stays out of scope.
- **Diff against the live tree.** Update fetches the current profile and diffs config against it, so no nested IDs live in state. Location groups match by position, zones by name, rates by **name plus condition**: real profiles repeat rate names (a flat "Standard" and a free "Standard" over €55 in the same zone), so name alone isn't unique. Validators require unique zone names per group and unique name + condition per zone.
- The update input has create/update lists per level and profile-level delete lists by ID (confirmed). Flat rates update in place via the rate definition ID. Conditional rates come back with synthetic IDs (`DeliveryRateDefinition/0?source=RateRangeCondition…`), so any change to one is a delete + create.
- Amounts come back reformatted (`"55.00"` → `"55.0"`); they are compared numerically and state keeps the configured string.
- A rate with both a minimum and a maximum comes back as two conditions sharing one base ID (`…DeliveryCondition/<n>?operator=…`). The provider models one condition per rate, so it fails on such rates rather than drop a bound. Follow-up: model `condition` as `{ field, min, max, unit }`.
- Zone countries are sent only when the set changes, with all provinces included. Province subsets are not modeled: changing a zone's countries widens partial-province selections in that zone. `*` stands for the rest of the world.
- The full tree can exceed Shopify's single-query cost limit (1000). The provider reads ≤ 50 locations per group, ≤ 25 zones per group and ≤ 25 rates per zone, and fails on larger profiles instead of managing part of one.

Mutations: `deliveryProfileUpdate` only (create/remove are for custom profiles, out of scope).

**List:** no config. Lists the general profile only.

---

## 9. Resource: `shopify_menu`

```hcl
resource "shopify_menu" "main" {
  handle         = "main-menu"
  title          = "Hauptmenü"
  adopt_existing = true

  item {
    title = "Pflegehilfsmittel"
    type  = "COLLECTION"
    resource_id = "gid://shopify/Collection/123"

    item {
      title = "Bettschutzeinlagen"
      type  = "HTTP"
      url   = "/collections/bettschutz"
    }
  }

  item {
    title = "Ratgeber"
    type  = "PAGE"
    resource_id = "gid://shopify/Page/456"
  }
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `handle` | string | Required, import key. Updatable, except for default menus |
| `title` | string | Required |
| `item` | nested blocks (≤ 3 levels) | `title`, `type`, `resource_id` or `url`, `tags` |
| `is_default` | bool | Computed. Default menus are never deleted, only removed from state |
| `adopt_existing` | bool | Pattern A. With `true`, destroy only removes the menu from state |

Notes:
- **Terraform owns the whole tree.** Update sends the complete item list without item IDs, which replaces all items (confirmed by acceptance test). Any item a merchant adds in the admin will be removed on the next apply. If merchants need to edit a menu, keep that menu out of Terraform.
- Menu items point at collections and pages, which are content. Referencing them by GID makes configs specific to one store. Option: allow `resource_handle` plus `type`, and resolve it to a GID at apply time.
- Validation: `url` is required for HTTP and not allowed for other types (Shopify derives it, so it is only stored for HTTP items). `resource_id` is required for COLLECTION, PRODUCT, PAGE, BLOG, ARTICLE, METAOBJECT, SHOP_POLICY and CUSTOMER_ACCOUNT_PAGE. Checked on the dev store (2026-10-23): without it `menuCreate` returns "shop_policy not found" / "customer_account_page not found"; CATALOG needs none (Shopify sets `/collections/all`). Full enum: FRONTPAGE, COLLECTION, COLLECTIONS, PRODUCT, CATALOG, PAGE, BLOG, ARTICLE, SEARCH, SHOP_POLICY, HTTP, METAOBJECT, CUSTOMER_ACCOUNT_PAGE.
- **Lookup by handle:** `menus(query:)` can't filter by handle, so the provider pages through all menus. The list lags a few seconds behind writes (search index), so a menu created moments ago may not be found yet by import or adopt. Shopify suffixes a taken handle (`-1`) instead of failing, so Create also checks the returned handle, removes the duplicate and fails.

**List:** no config. Default menus are included.

---

## 9a. Resource: `shopify_web_presence`

```hcl
resource "shopify_web_presence" "ch" {
  subfolder_suffix  = "ch"         # or domain_id, exactly one
  default_locale    = "de"
  alternate_locales = ["fr", "it"] # optional, unset = unmanaged
}

resource "shopify_market" "dach" {
  # ...
  web_presence_ids = [shopify_web_presence.ch.id]
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `id` | string | `MarketWebPresence` GID. Computed, UseStateForUnknown |
| `subfolder_suffix` | string | Optional; exactly one of this or `domain_id`. Validated as 2 or more lowercase ASCII letters (Shopify rejects digits and hyphens, and lowercases the value, which would otherwise show as a perpetual diff). Updatable in place (confirmed on the dev store). Switching between a subfolder and a domain forces replace, because `WebPresenceUpdateInput.subfolderSuffix` "must be null if subfolder suffix is not already defined" |
| `domain_id` | string | Optional. **RequiresReplace**: `WebPresenceUpdateInput` has no `domainId`. It only references an existing `Domain` GID; domains are never created or managed (out of scope) |
| `default_locale` | string | Required. Updatable |
| `alternate_locales` | set(string) | Optional + Computed, UseStateForUnknown; unset = unmanaged |
| `root_urls` | map(string) | Computed, locale → URL. Informational |
| `adopt_existing` | bool | Pattern A, see below |

Import key: the subfolder suffix for subfolder presences, the domain host (e.g. `shop.example.ch`) for domain presences. A host contains dots and a suffix is ASCII letters only, so they can't be confused. Import pages through `webPresences` (the query has no `query:` filter), matches the key and sets the GID. Read looks up by GID first, then falls back to `subfolder_suffix` or `domain_id`.

Notes:
- Uses `webPresenceCreate` / `webPresenceUpdate` / `webPresenceDelete` and the top-level `webPresences` query. The `marketWebPresence*` mutations are deprecated and not used. Linking to markets stays on `shopify_market.web_presence_ids`.
- **Pattern A:** the shop's primary-domain presence can't be removed: the error code `SHOP_MUST_HAVE_PRIMARY_DOMAIN_WEB_PRESENCE` exists, and on the dev store a delete of it left it in place (the userError text is in the notes below). Adopting it (`adopt_existing = true`) updates it in place, and destroy only removes it from state with a warning. Every store has one such presence (dev: the myshopify domain, locale `en`, no markets). `CANNOT_DELETE_PRIMARY_MARKET_WEB_PRESENCE` is deprecated ("No longer used").
- **No Pattern B:** deleting a presence removes routing/SEO configuration, not stored data.
- Errors are reported as returned and not retried: `CANNOT_HAVE_SUBFOLDER_AND_DOMAIN`, `REQUIRES_DOMAIN_OR_SUBFOLDER`, `SUBFOLDER_SUFFIX_MUST_BE_AT_LEAST_2_LETTERS`, `SUBFOLDER_SUFFIX_CANNOT_BE_SCRIPT_CODE`, `DISABLED_LANGUAGE`, `REGION_SPECIFIC_LANGUAGE`, `DOMAIN_NOT_FOUND`, `CANNOT_ADD_CUSTOMER_DOMAIN`.
- No enum-typed attributes, so there's no output-only enum mapping.
- **Checked on the dev store (2026-10-26, `read_locales`/`write_locales` granted):**
  - `defaultLocale { locale }` and `alternateLocales { locale }` are readable. The ⚠ locale points are confirmed, the schema stays as designed.
  - A locale can be a default or alternate locale only if it is enabled **and published** on the shop: an enabled but unpublished one fails with `DISABLED_LANGUAGE` (alternate) or `UNPUBLISHED_LANGUAGE` (default), and a locale that isn't enabled fails with `DISABLED_LANGUAGE` or, on update, sometimes a bare "Internal error". Enabling and publishing locales is not part of this resource (planned as the shop locale type).
  - **Omit, don't null:** an explicit `null` for `subfolderSuffix` in `webPresenceUpdate` fails with `REQUIRES_DOMAIN_OR_SUBFOLDER` on a subfolder presence and with `CANNOT_HAVE_SUBFOLDER_AND_DOMAIN` on the primary-domain presence; `defaultLocale: null` fails with `CANNOT_SET_DEFAULT_LOCALE_TO_NULL`. All optional inputs are omitted when unset, and `subfolderSuffix` is only sent when it changes. `alternateLocales: []` clears the list, so an empty set is sent as `[]` while an unset one is omitted.
  - **Create is idempotent for subfolders:** `webPresenceCreate` with a suffix that already exists returns the existing presence instead of an error, so Create checks existence itself (needed for `adopt_existing` semantics). A taken domain fails with `TAKEN`.
  - The primary-domain presence cannot be deleted: `SHOP_MUST_HAVE_PRIMARY_DOMAIN_WEB_PRESENCE`, "The shop must have a web presence that uses the primary domain." Delete treats this code as "remove from state only" even without `adopt_existing`. Adopting looks the presence up by `domain_id` (the list returns it).
  - Suffix rules: letters only (`SUBFOLDER_SUFFIX_MUST_CONTAIN_ONLY_LETTERS`), at least 2 (`SUBFOLDER_SUFFIX_MUST_BE_AT_LEAST_2_LETTERS`).
  - `webPresences` has no `webPresence(id:)` root field, so Read by GID uses `node(id:)`. The list is not search-indexed and showed new presences immediately.
  - Unverified: deleting a presence that is still linked to a market (`TODO(verify)` in the resource).
- **Scopes observed on the dev store (2026-10-23):**
  - `read_markets`: `webPresences { id subfolderSuffix domain { id host } rootUrls { locale url } markets }` works.
  - `write_markets`: `webPresenceCreate`, `webPresenceUpdate` (subfolder suffix change) and `webPresenceDelete` all work.
  - `read_locales` or `read_markets_home`: needed for `defaultLocale` and `alternateLocales` (type `ShopLocale`) and for top-level `shopLocales`. With `read_markets` only, these return "Access denied … Required access: `read_locales` access scope or `read_markets_home` access scope". `rootUrls[].locale` lists the locales but doesn't mark the default, so it can't stand in for them.

---

## 9b. Resource: `shopify_shop_locale`

```hcl
resource "shopify_shop_locale" "fr" {
  locale    = "fr"
  published = true # optional, unset = unmanaged
}

resource "shopify_web_presence" "ch" {
  subfolder_suffix  = "ch"
  default_locale    = "de"
  alternate_locales = [shopify_shop_locale.fr.locale]
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `id` | string | Computed, UseStateForUnknown. The locale code: `ShopLocale` is not a `Node` and has no GID |
| `locale` | string | Required, **RequiresReplace** (no rename; the key of every mutation). ISO code, case-sensitive: `pt-BR` is valid, `pt-br` fails with "Locale is invalid". Not validated locally, Shopify's error is clear |
| `published` | bool | Optional + Computed, UseStateForUnknown; unset = unmanaged. A new locale starts unpublished |
| `name` | string | Computed, UseStateForUnknown. English display name (`French`) |
| `primary` | bool | Computed, UseStateForUnknown. True for the shop's primary locale |
| `adopt_existing` | bool | Pattern A, see below |
| `allow_data_deletion` | bool | Pattern B, see below |

Import key: the locale code (`fr`, `pt-BR`). Identity `{ id = "fr" }`; the acceptance test checks identity with a locale regex instead of the GID regex. Read lists `shopLocales` (all enabled locales, `published: false` is the default and returns published and unpublished ones) and matches by code. There is no GID, so there is no GID-first lookup and no `MutableIdentity`.

Granularity: one resource per locale. Each locale is enabled, published and disabled on its own, so locales have independent lifecycles.

Notes:
- Uses `shopLocales`, `shopLocaleEnable`, `shopLocaleUpdate` (only `published`) and `shopLocaleDisable`.
- **Web presences are not managed here.** `ShopLocaleInput.marketWebPresenceIds` and `shopLocaleEnable(marketWebPresenceIds:)` overlap with `shopify_web_presence.default_locale`/`alternate_locales`. That resource owns the relation; this one never sends `marketWebPresenceIds`, so the two can't fight. A locale must be enabled and published here before a web presence can use it (§9a).
- **Create:** look the locale up first. If it exists, adopt it with `adopt_existing = true`, otherwise fail (as the API would: "Locale has already been taken"). Then `shopLocaleEnable(locale)` and, if `published` is set and differs from the result, `shopLocaleUpdate`.
- **Update:** only `published` can change; `shopLocaleUpdate` is called only when it does.
- **Primary locale (Pattern A, always):** the API can't change or disable it. `ShopLocaleInput` has no primary field, and `shopLocaleUpdate` on it fails with "The primary locale of your store can't be changed through this endpoint." It can only be adopted (`adopt_existing = true`). Its destroy only removes it from state with a warning, regardless of flags; `shopLocaleDisable` is never called for it. Changing the primary locale is an admin task; whether `webPresenceUpdate(defaultLocale:)` on the primary-domain presence does it is `TODO(verify)` and not part of this resource.
- **Adopted non-primary locale:** with `adopt_existing = true`, destroy only removes it from state with a warning.
- **Pattern B:** `shopLocaleDisable` "deletes all translations of this locale" (schema). Destroying or replacing a non-adopted, non-primary locale fails unless `allow_data_deletion = true` is in state; ModifyPlan warns when `locale` changes.
- Unverified: what `shopLocaleDisable` does to a locale that a web presence still uses (`TODO(verify)`). Terraform's dependency order removes it from `shopify_web_presence` first when the config references `shopify_shop_locale.<x>.locale`.
- Errors come as plain `UserError` (no code), `field = ["locale"]`, mapped to `locale`: "Locale has already been taken", "Locale is invalid", "The locale doesn't exist.", "The primary locale of your store can't be changed through this endpoint." None is retried.
- **Checked on the dev store (2026-10-26):** enable `fr` → unpublished, `marketWebPresences = []`; enabling again fails as above; `xx` and `pt-br` are invalid, `pt-BR` works; publish, an empty `ShopLocaleInput` (no-op) and unpublish work; unpublishing `en` (primary) fails with the message above; update and disable of a locale that isn't enabled fail with "The locale doesn't exist."; disable works. Not tried: disabling the primary locale.
- **Fit:** good. Locales change rarely and are configuration.
- **Verified by acceptance tests (2026-10-26):** create, in-place `published` change, import by code and by identity, unset `published` stays out of the plan, adopt of a non-primary locale (destroy leaves it enabled, guard not applied), adopt of the primary (destroy leaves it enabled with and without `adopt_existing`), guard on destroy and replace. `ModifyPlan` skips the data-loss warning for adopted and primary locales, because destroy only drops them from state. The primary locale's `published` is read from the API as `true`; setting it to `false` in config surfaces Shopify's error. List results omit nothing: every enabled locale, one request, no pagination.
- **Scopes:** `read_locales` / `write_locales` (reads and all four operations above work with `write_locales`).
- **Review Focus 2:** set `published` only in one step and leave it out in another, change it through the client in between: the config without `published` plans empty.

**List:** no config. Lists every enabled locale, the primary included. `DisplayName`: `fr (French)`, primary as `en (English, primary)`. Results carry `locale` and `published`, which pass validation and plan as import-only.

---

## 9c. Resource: `shopify_shop_policy`

```hcl
resource "shopify_shop_policy" "terms" {
  type           = "TERMS_OF_SERVICE"
  body           = file("${path.module}/policies/agb.html")
  adopt_existing = true # needed when the policy already has text
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `id` | string | `ShopPolicy` GID. Computed, UseStateForUnknown |
| `type` | string | Required, **RequiresReplace** (the key; `ShopPolicyInput` selects the policy by type). One of `REFUND_POLICY`, `SHIPPING_POLICY`, `PRIVACY_POLICY`, `TERMS_OF_SERVICE`, `TERMS_OF_SALE`, `LEGAL_NOTICE`, `SUBSCRIPTION_POLICY`, `CONTACT_INFORMATION` |
| `body` | string | Required, at least 1 character. HTML, stored and returned verbatim (no normalisation needed) |
| `title` | string | Computed, UseStateForUnknown. Set by Shopify, translated (`Legal notice`) |
| `url` | string | Computed, UseStateForUnknown. Public URL of the policy page |
| `adopt_existing` | bool | Pattern A, see below |

Import key: the type (`TERMS_OF_SERVICE`). Read queries `shop { shopPolicies { id type title body url } }` (one query, at most eight rows), matches the GID first and falls back to the type, so `MutableIdentity` is set. A policy with an empty body counts as absent.

Granularity: one resource per policy type. The API addresses policies one type at a time, and they are written and owned separately (legal notice vs. shipping policy).

Notes:
- Uses `shopPolicyUpdate` only; there is no create or delete mutation. `shopPolicyUpdate` creates the policy row when the type has none yet (checked).
- **Pattern A:** a policy exists once per type and can't be deleted.
  - Create looks the type up. Missing or empty body: write it. Non-empty body: fail unless `adopt_existing = true`, because the text is usually written by legal or the merchant and must not be overwritten silently.
  - Destroy only removes it from state, with a warning, whatever `adopt_existing` says. It never sends `body: ""`: an empty body doesn't delete the row (checked), it just blanks the policy (Review Focus 1). Replace (a `type` change) therefore leaves the old policy as it is.
- **No Pattern B:** nothing is deleted.
- **`PRIVACY_POLICY` and Shopify's auto-managed privacy policy.** `privacySettings.privacyPolicy.autoManaged` is `true` on the dev store, which means Shopify generates that text. `TODO(verify)`: whether `shopPolicyUpdate` on `PRIVACY_POLICY` is rejected, flips `autoManaged`, or gets regenerated over. Not tried, because the generated text and the auto-managed flag can't be restored through the API. Docs: turn auto-management off first (`privacy_policy_auto_managed = false` on `shopify_privacy_settings`, §9d) and reference it with `depends_on`.
- Translations of policy bodies (`HasPublishedTranslations`) are not managed (translations are a separate roadmap type).
- Errors: `ShopPolicyUserError`, only code `TOO_BIG` (body too large), mapped to `body`. Not retried.
- genqlient: `body` has the `HTML` scalar, so `internal/client/genqlient.yaml` needs `HTML: type: string`, like `URL`.
- **Checked on the dev store (2026-10-26):** the store has one policy, `PRIVACY_POLICY`, Shopify-generated. `shopPolicyUpdate` for `LEGAL_NOTICE` created the row (new GID) and returned the body byte for byte, including `&amp;`, `<br>` and a `<script>` tag; the same body again didn't touch `updatedAt`; plain text is accepted as is. `body: ""` keeps the row (same GID, empty body, still listed in `shopPolicies`), so it can't be removed again. `node(id:)` resolves a `ShopPolicy` GID.
- **Fit: volatile.** Policies are legal texts, often edited in the admin by people outside the infrastructure team, and Shopify generates the privacy policy. The resource description says so ("poor fit for Terraform unless the texts are maintained in the repository").
- **Verified by acceptance tests (2026-10-26):** create, in-place `body` change (`&amp;`, `<br>`, `<b>` come back verbatim), import by type and by identity, identity import with generated config, adopt (create fails without `adopt_existing`, the refused create leaves the text, adopt overwrites), destroy leaves the text for adopted and imported policies, another type's edit plans empty, an admin edit of the body plans an update, an emptied row plans a create (empty body counts as absent). Tests write only `LEGAL_NOTICE` and `SHIPPING_POLICY`; `SHIPPING_POLICY` gained an empty row that can't be removed.
- **Scopes:** `read_legal_policies` / `write_legal_policies` (checked with `write_legal_policies`).
- **Review Focus 1:** acceptance step reads the policy back through the client after destroy and expects the body unchanged. **Review Focus 2:** `body` is the only writable attribute, so the step doesn't apply; the test instead edits another type's policy through the client and expects an empty plan. It uses the dev store's existing empty `LEGAL_NOTICE` row, because each new type adds a row that can't be removed.

**List:** no config. Lists every policy with a non-empty body; empty rows are skipped, since they fail the `body` validator and mean "no policy". `DisplayName`: `TERMS_OF_SERVICE (Terms of service)`, from `type` and `title`. Results carry `type` and `body`.

---

## 9d. Resources: `shopify_privacy_settings` and `shopify_consent_policy`

One task, one package (`privacysettings`), two resource types: consent policies can only be written once Shopify's cookie banner is off, which is a privacy setting.

```hcl
resource "shopify_privacy_settings" "this" {
  cookie_banner_auto_managed = false # we use our own consent banner (CMP)
  # data_sale_opt_out_page_auto_managed, privacy_policy_auto_managed: unset = unmanaged
}

resource "shopify_consent_policy" "de" {
  country_code     = "DE"
  consent_required = true
  depends_on       = [shopify_privacy_settings.this]
}

resource "shopify_consent_policy" "us_ca" {
  country_code               = "US"
  region_code                = "USCA"
  data_sale_opt_out_required = true
  depends_on                 = [shopify_privacy_settings.this]
}
```

### `shopify_privacy_settings`

| Attribute | Type | Behaviour |
|---|---|---|
| `id` | string | Computed, UseStateForUnknown. The shop GID (`gid://shopify/Shop/<n>`): `PrivacySettings` has no ID of its own |
| `cookie_banner_auto_managed` | bool | Optional + Computed, UseStateForUnknown; unset = unmanaged. Can only be switched off, see below |
| `data_sale_opt_out_page_auto_managed` | bool | Same |
| `privacy_policy_auto_managed` | bool | Same |
| `cookie_banner_enabled` | bool | Computed, no `UseStateForUnknown`: it flips with the banner, so a plan that changes the banner shows it as known after apply. `banner.enabled`, informational |

Import key: `default` (the store's only privacy settings, as for `shopify_delivery_profile`). Read: `privacySettings { banner { autoManaged enabled } dataSaleOptOutPage { autoManaged } privacyPolicy { autoManaged } }` plus `shop { id }`.

Granularity: one aggregate resource. `privacySettings` is a singleton and `privacyFeaturesDisable` takes the feature list in one call.

Notes:
- **The API can only switch features off.** The Mutation type has `privacyFeaturesDisable(featuresToDisable: [COOKIE_BANNER | DATA_SALE_OPT_OUT_PAGE | PRIVACY_POLICY])` and nothing that enables them (checked: no other mutation mentions privacy, banner or cookies). So:
  - `false` in config and `true` live: Create/Update call `privacyFeaturesDisable` with every such feature in one call.
  - `true` in config and `false` live: error, "can only be turned back on in the Shopify admin". ModifyPlan raises it at plan time when state is known; Create (adopting) raises it at apply time.
  - Someone re-enabling a feature in the admin shows as drift, and the next apply turns it off again.
- **Why a resource and not a data source:** it is the declared, enforced precondition for `shopify_consent_policy` (a shop with its own consent banner wants Shopify's banner to stay off), and it corrects admin drift in the one direction the API allows. If the controller prefers, the read side alone works as a data source; then consent policies document the admin step instead.
- **Checked on the dev store (2026-10-26):** `privacyFeaturesDisable([COOKIE_BANNER])` flips both `banner.autoManaged` and `banner.enabled` (`true`/`true` before, `false`/`false` after); `dataSaleOptOutPage` and `privacyPolicy` stayed `autoManaged: true`. The mutation returned `featuresDisabled: [COOKIE_BANNER]` and no userErrors. The other two features were never disabled. Read re-reads after the disable, because `cookie_banner_enabled` changes with it (see the attribute table).
- **Pattern A (implicit):** singleton. Create = read, then disable what config asks; destroy only removes it from state with a warning and never touches a feature.
- **No Pattern B.**
- Errors: `PrivacyFeaturesDisableUserError`, only code `FAILED`. Reported, not retried.
- **Fit:** good. Set once, rarely changed.
- **Scopes:** `read_privacy_settings` / `write_privacy_settings` (reads checked).

### `shopify_consent_policy`

| Attribute | Type | Behaviour |
|---|---|---|
| `id` | string | `ConsentPolicy` GID. Computed; a custom `UseStateForUnknown` keeps it unless it is an `SD-` ID, because the first write gives a system default a numeric ID. Shopify's defaults have synthetic IDs `gid://shopify/ConsentPolicy/SD-<code>`; a written policy has a numeric ID |
| `country_code` | string | Required, **RequiresReplace** (part of the key). `PrivacyCountryCode`, two uppercase letters |
| `region_code` | string | Optional, **RequiresReplace** (part of the key). Country code plus subdivision, e.g. `USCA`, `CAQC`; only US and CA regions exist. Unset = the country-level policy |
| `consent_required` | bool | Optional + Computed, UseStateForUnknown; unset = unmanaged |
| `data_sale_opt_out_required` | bool | Optional + Computed, UseStateForUnknown; unset = unmanaged |

Import key: `<COUNTRY>` or `<COUNTRY>/<REGION>` (`DE`, `US/USCA`), parsed with `patterns.SplitImportKey`. Read: `consentPolicy(id:)` when the GID is numeric, otherwise or when it returns nothing, `consentPolicy(countryCode:, regionCode:)`. `SD-` IDs don't resolve through `consentPolicy(id:)` (returns `[]`, checked), and writing a default turns it into a numeric row, so the GID changes and `MutableIdentity` is set. `consentPolicy(countryCode: X)` without `regionCode` returns only the country-level row (checked for `CA`); Read still filters `regionCode == null` to be safe.

Granularity: one resource per country or region. Each of the 319 rows is written independently, and a shop changes a handful; an aggregate resource would make every config carry all 319 rows.

Notes:
- `consentPolicyUpdate(consentPolicies: [...])` upserts by country and region; there is no delete. Create and Update send one element.
- **Precondition:** every write fails while Shopify's cookie banner is on: `SHOPIFY_COOKIE_BANNER_NOT_DISABLED`, "Shopify's cookie banner must be disabled.", `field = null` (checked on the dev store, also for a no-op write). The diagnostic says to set `cookie_banner_auto_managed = false` on `shopify_privacy_settings` and add `depends_on`.
- **Pattern A (implicit):** a policy exists for every supported country and region (Shopify's default if none was written), so Create always adopts: it upserts the configured values. No `adopt_existing` flag: the values are two booleans, nothing a merchant authored. Destroy only removes it from state with a warning; the row keeps its values (it does not fall back to Shopify's default, and there is no API to make it).
- **No Pattern B.**
- **Checked on the dev store (2026-10-26, banner off):**
  - An input with only one of the two booleans leaves the other unchanged (`DE`: sent `dataSaleOptOutRequired` only, `consentRequired` stayed `true`). "Unset = unmanaged" holds with omitted fields.
  - Writing an `SD-` row returns a numeric ID (`SD-USCA` became `gid://shopify/ConsentPolicy/1613157335371`), with the unsent boolean at its default. The row never goes back to `SD-`, so the `id` plan modifier keeps the ID only when it is numeric (a system default gets a new ID on its first write).
  - **Writing one regional row materializes every regional row of that country.** After the first write to `US/USCA`, all 51 US regional rows (50 states and DC) had numeric IDs with the default values (`false`/`false`); the country-level `US` row stayed `SD-US`. The list resource therefore emits all of them once any US region is written, and a first write to a region can't be undone. Not checked for `CA` (not written).
  - `consentPolicyUpdate` returns only the rows it changed, so the resource always re-reads the row after a write. Create skips the write when every configured value already matches, which also makes adopting a row work while Shopify's banner is on.
  - `userErrors.field` is `[]`, so errors are mapped by code. `US` without a region: `REGION_CODE_REQUIRED`; `US` + `CAAB`: `REGION_CODE_MUST_MATCH_COUNTRY_CODE`; `DE` + `USCA`: an error with `code: null`, "Region code is not supported. Setting policies for regions is only supported for countries: CA, US" (reported unmapped).
  - `TODO(verify)`: whether `consentPolicyRegions` (317 entries) must be checked locally or `UNSUPORTED_CONSENT_POLICY` is enough (it is enough until shown otherwise; no unsupported country was written, because the write could succeed).
- Errors: `COUNTRY_CODE_REQUIRED`, `REGION_CODE_REQUIRED`, `REGION_CODE_MUST_MATCH_COUNTRY_CODE` (mapped to `region_code`), `SHOPIFY_COOKIE_BANNER_NOT_DISABLED`, `UNSUPORTED_CONSENT_POLICY` (Shopify's spelling). None is retried.
- **Checked on the dev store (2026-10-26):** 319 policies, 3 of them non-default (`DE` consent required, `IS`, `LI`), all others `SD-` with both flags `false`. The 3 non-default rows exist while the banner is auto-managed and nothing was written, so Shopify creates non-`SD-` rows too: "non-default" means "not a system default row", not "written by the merchant". `consentPolicy(countryCode: DE)` returns only the numeric row, not an `SD-DE` one.
- **Fit:** good when the shop runs its own consent banner (CMP); not manageable while Shopify's banner is on. Description says so.
- **Scopes:** `read_privacy_settings` / `write_privacy_settings` (reads checked; writes blocked by the precondition, not by scope).

### Testing (both)
- The dev store's cookie banner was turned off once, with the user's approval (`privacyFeaturesDisable([COOKIE_BANNER])`); it can't be turned back on through the API, so no acceptance test can run the disable again. The disable path is covered by a unit test against a fake API (`apply_test.go`), and by that one real call.
- Write steps run only when the banner is already off (`requireBannerOff`), otherwise `t.Skip("Shopify's cookie banner is on; ...")`. Tests never disable `DATA_SALE_OPT_OUT_PAGE` or `PRIVACY_POLICY`; configs set only `cookie_banner_auto_managed`.
- Always run: read, import (`default`, key, identity, generated config), the `SD-` default row adopted without a write, `DE` adopted with its live values, the list resources.
- Banner off: create and update of `IS` and `US/USCA`, the plan-time and create-time "can't be turned back on" errors, `REGION_CODE_MUST_MATCH_COUNTRY_CODE`, Review Focus 2. Written rows are restored in cleanup; a row written from `SD-` stays a written row.
- Review Focus 1: after destroy, read back through the client and expect unchanged values. Review Focus 2 (consent policy): set `consent_required` only, change `data_sale_opt_out_required` through the client, expect an empty plan.
- After the 2026-10-26 runs the dev store's non-default rows are `DE`, `IS`, `LI` and the 51 US regional rows that the first write to `US/USCA` materialized; all of them have the values they had before (`false`/`false`, `DE` consent required).

**List (`shopify_privacy_settings`):** no config. One result, `DisplayName` `Privacy settings`, with the three `*_auto_managed` values.

**List (`shopify_consent_policy`):** no config. Lists only rows whose ID has no `SD-` prefix (the schema documents the prefix as "system default"), so Shopify's 316 untouched defaults stay out; on a fresh dev store that leaves `DE`, `IS`, `LI` (plus every row of a country whose region was written, see above). `DisplayName`: `DE` or `US/USCA`, the import key. Results carry the key and both booleans; importing them plans as import-only even while the banner is on (no write).

---

## 9e. Resource: `shopify_checkout_and_accounts_configuration`

```hcl
resource "shopify_checkout_and_accounts_configuration" "live" {
  # name unset: the published configuration
  branding = jsonencode({
    designTokens = {
      colors       = { palette = { color1 = "#1A4D8F" } }
      cornerRadius = { base = 6 }
    }
    components = { checkbox = { cornerRadius = "LARGE" } }
  })
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `id` | string | `CheckoutAndAccountsConfiguration` GID. Computed, UseStateForUnknown |
| `name` | string | Optional + Computed, UseStateForUnknown (before RequiresReplace, so an unset name doesn't plan a replace). Selects the configuration by name; unset = the published one. **RequiresReplace**: the API can't rename, and a different name means a different configuration (replace is harmless, destroy only drops state) |
| `is_published` | bool | Computed, UseStateForUnknown |
| `branding` | string (JSON) | Optional; unset = branding unmanaged. Model and drift handling in §9f |

Import key: `published` for the published configuration, otherwise its `name`. Names come from the shop name (dev store: `terraform-provider-test configuration`), so `published` is the key that works on every store. Read: `checkoutAndAccountsConfiguration(id:)` first; if it is gone, `checkoutAndAccountsConfigurations(query: "is_published:true")` when `name` is unset, otherwise page through the list and match `name` (no name filter exists). `MutableIdentity` is set. A name that matches more than one configuration is an error that lists the GIDs. After a different configuration is published in the admin, the resource keeps following its GID (now a draft); `is_published = false` shows it, and the docs say to re-import.

Granularity: one resource per configuration, holding its branding. Checkout branding is not a resource of its own (decision in §9f).

Notes:
- Uses `checkoutAndAccountsConfiguration(s)` and `checkoutAndAccountsConfigurationUpdate(id:, configuration:)`. The deprecated `checkoutProfiles`, `checkoutBranding` and `checkoutBrandingUpsert` are not used.
- **What is writable:** `CheckoutAndAccountsConfigurationInput` has only `branding` and `overrides`. There is no create, delete, rename or publish mutation; publishing goes through rollouts (`RolloutCheckoutAndAccountsConfigurationPublishChange`), which are out of scope here.
- **Overrides are not managed in this task.** `CheckoutAndAccountsConfigurationOverrideInput` is `{ id, branding }`: it says nothing about what an override applies to, so a new override can't be targeted. The dev store has none, and discovery didn't count them on staging/production. `TODO(verify)`: what overrides target and how they are created; until then they are left alone (never sent).
- **Pattern A (implicit, no `adopt_existing` flag):** configurations are created in the checkout editor and can't be deleted through the API. Create = find (published or by name), then update branding. Destroy only removes the resource from state with a warning; it never sends `branding: null` or resets anything (Review Focus 1).
- **No Pattern B.**
- Errors: `CheckoutAndAccountsConfigurationUserError` has `code` (`INVALID`, `BLANK`, `MISSING`, `NOT_FOUND`, `INTERNAL_SERVER_ERROR`, `TRANSIENT`), `reason` (e.g. `branding_invalid_hex_color`) and `field` (`["configuration","branding",…]`). They map to `branding`, and the message includes the JSON path from `field` and the `reason`. Unknown keys or wrong types in the JSON fail as GraphQL variable errors before execution and are reported as such. Nothing is retried; `TRANSIENT` asks the user to re-apply.
- **Checked on the dev store (2026-10-26):** one configuration, published, no overrides; branding only has three surface background colours (`#FFFFFF`). Update semantics in §9f.
- **Fit: volatile.** Branding is usually edited in the admin's checkout editor. The description says so.
- **Scopes:** `read_checkout_and_accounts_configurations` / `write_checkout_and_accounts_configurations`. `TODO(verify)`: whether branding also needs `*_checkout_branding_settings`; both are already in `app/*.toml` and granted on the dev store, so the dev store can't tell them apart.
- **Review Focus 2:** branding sets path A; path B is changed through the client; expect an empty plan.

**List:** no config. Lists every configuration, published or not. `DisplayName`: the name, with ` (published)` appended for the published one. Results carry `name` and `branding` (live branding as described in §9f), which plan as import-only.

---

## 9f. Checkout branding: the `branding` attribute of §9e

Checkout branding is **not a resource of its own**. In 2026-10 the non-deprecated path is `CheckoutAndAccountsConfiguration.branding`, written with `checkoutAndAccountsConfigurationUpdate(configuration: { branding })`; `checkoutBranding(checkoutProfileId:)` and `checkoutBrandingUpsert` are deprecated in favour of it. Branding is the configuration's only writable content (§9e), so a separate `shopify_checkout_branding` would be the same object under two resources, both writing through the same mutation. Discovery's "1 per configuration" is this field.

**Modelling: one JSON string, in the API's camelCase, following `CheckoutAndAccountsConfigurationBrandingInput`.**
- A typed schema doesn't fit one task: the input tree has 871 leaves over 66 input types and 25 enums, and every generated config would carry all of them.
- A curated typed subset (design tokens only) would miss `components` and `surfaces`, which is where the dev store's only values are.
- JSON keeps the two states the API distinguishes: a missing key is not managed, an explicit `null` resets that value to Shopify's default (both checked). Optional typed attributes can't tell "unset" from "reset". Keys match Shopify's docs one to one; users write `jsonencode({...})`.

**Update semantics (checked on the dev store, 2026-10-26):**
- Deep merge: setting `designTokens.cornerRadius.base` and then only `components.checkbox.cornerRadius` kept both.
- `null` resets: `base: null` cleared that leaf; `checkbox: null` cleared the whole subtree.
- Atomic: an invalid value anywhere rejects the whole update (`INVALID`, reason `branding_invalid_hex_color`, for `red` and `#abc`), and nothing is written.
- Hex colours come back uppercased (`#abcdef` → `#ABCDEF`).
- An unknown key fails GraphQL variable validation (top-level `errors`).
- After a reset, the read returns empty containers (`designTokens.cornerRadius: { small: null, base: null, large: null }`) where it returned `null` before.

**Write:** the configured JSON is sent as is, only when it differs from state (`# @genqlient(for: "CheckoutAndAccountsConfigurationInput.branding", bind: "encoding/json.RawMessage")`, as in `web_presence.graphql`). Removing a key from config stops managing it; the live value stays, and the docs say to set it to `null` first to reset it. `branding` unset sends nothing.

**Read and drift (pure logic, unit tests first):**
1. Fetch the live branding with a selection over the whole output type, bound to `json.RawMessage`. The selection is written once from the schema (about 1,250 lines; requested cost 846 for `first: 10`, actual 29).
2. Normalise it: drop `null` values and objects left empty.
3. With prior state: build the result from the paths in the state's JSON only. A write-only path keeps the state value; any other path takes the live value (`null` if absent). If every leaf is equal, state keeps its string unchanged; otherwise the projected object is written and the drift shows. Paths not in state are ignored, so admin edits elsewhere don't plan (Review Focus 2).
4. Without prior state (import, list): the normalised live branding.

Leaf equality: numbers numerically, `#RRGGBB` strings case-insensitively, everything else exactly. The attribute uses a custom string type with semantic equality (`basetypes.StringValuableWithSemanticEquals`, part of the framework, no new dependency), so key order and whitespace never show as drift against state; a reformatted hand-written string still plans one update (below). ValidateConfig only checks that the value is a JSON object; Shopify validates keys and values.

**Write-only paths.** Comparing the output and input trees: 846 leaf paths match, and three groups differ. v1 treats them as write-only: never drift-checked, never emitted on import or list. The docs list them.
- **Images** (8 paths: `components.favicon`, `components.header.logo.image`, `surfaces.{checkout,customerAccounts,signIn}.components.header.logo.image`, `surfaces.checkout.components.header.cartLink.image`, `surfaces.checkout.components.orderSummary.backgroundImage`, `surfaces.signIn.components.main.backgroundImage`). The input takes `{ mediaImageId }`; the output is a union over `Image`, whose `id` isn't a `MediaImage` GID. `TODO(verify)`: whether the media image can be resolved from the read value.
- **Fonts** (`designTokens.typography.{primary,secondary}`). The input takes `shopifyFontGroup { baseFontHandle boldFontHandle loadingStrategy }` or `customFontGroup { base { weight genericFileId } bold { … } loadingStrategy }`; the output is a union with `name`, `sources` and `weight`, but no handles. `TODO(verify)`: whether the handle can be derived from name and weight.
- **`components.merchandiseThumbnail.aspectRatio`**: input only.

**Implementation findings (2026-10-26):**
- The read selection leaves out the image and font unions: they are write-only, so reading them buys nothing. A schema walk confirms 846 matching leaf paths, 10 output-only union paths (8 images, 2 fonts) and 25 input-only paths.
- Semantic equality runs when the framework reads and applies, not when it plans: Terraform core requires the planned value of a non-computed attribute to equal the config. With `jsonencode()` the config text is canonical (sorted keys, compact), so this never shows. A hand-written JSON string that is reformatted without changing values plans one update, which writes the same values.
- Read's fallback when the GID is gone: the private-state flag `follows_published` is set when `name` was unset in config (Create, Update) or the import key was `published`; then Read looks up the published configuration, otherwise the name in state. An import by identity falls back by name. The flag is only rewritten when Create or Update runs, so dropping `name` from config with no other change keeps the fallback by name.
- Import with an empty live branding (only nulls and empty containers) sets `branding` to null, so generated config leaves it out.
- Acceptance tests confirmed on the dev store: deep merge, `null` reset, uppercased hex, and an invalid colour rejecting the whole update (`branding_invalid_hex_color` at `designTokens.colors.palette.color1`, nothing written).

**Scope of the task:** one task, together with §9e. The pure logic (normalise, project, compare, the write-only list) is a small file with table tests. If it turns out too large, split §9e into "configuration without branding" (lookup, import, identity, list) and "branding", in that order.

**Dev store:** the experiments above were undone by sending `null` for every leaf set. The three original leaves are unchanged. Left behind: three empty containers in the read and new `editedAt`/`updatedAt` timestamps.

---

## 9g. Data sources for Shopify-owned definitions

Decision D4 (2026-10-26). Shopify's standard definitions (`shopify.*` metafield definitions, `shopify--*` metaobject definitions) and app-owned ones (`checkoutblocks.*` on ORDER and SHOP, `$app:…` types) are excluded from import (D3): Terraform must not manage them. Configuration still needs to read them, for example to point a metaobject reference validation at a standard definition. Lookup by natural key also works for merchant-owned definitions.

```hcl
data "shopify_metafield_definition" "material" {
  owner_type = "PRODUCT"
  namespace  = "shopify"
  key        = "material"
}

data "shopify_metaobject_definition" "material" {
  type = "shopify--material"
}
```

**Arguments (Required):** `shopify_metafield_definition`: `owner_type` (any `MetafieldOwnerType`, not only `PRODUCT`), `namespace`, `key`. `shopify_metaobject_definition`: `type`, exactly as the API returns it (`shopify--material`, `app--123--foo`, or a merchant type). `metaobjectDefinitionByType(type: "$app:foo")` resolves to the caller's prefix (checked on the dev store, 2026-10-27, §9r). `TODO(verify)`: the same for a metafield `namespace` of `$app:…`.

**Attributes (all Computed):** every other attribute of the resource of the same name, with the same names, types and nested shapes, so a value can be passed from data source to resource. Left out: the provider-only `adopt_existing`, `allow_data_deletion` and `timeouts`. `id` is the GID. The `field` list of a metaobject definition is a computed list of objects (data sources can't have computed blocks), with the resource block's attributes (`key`, `type`, `name`, `description`, `required`, `validations`); to build a resource from it, use `dynamic "field"`. As on the resource, empty values read back as `""` (top-level strings), `null` (field description and validations), and Shopify-set access values the API won't accept as input (`admin = PUBLIC_READ_WRITE`) as `null`.

**Lookup and code sharing:** no new GraphQL. `metafieldDefinition(identifier:)` and `metaobjectDefinitionByType(type:)` are the resources' own natural-key queries, and the data sources use the resources' API-to-state mapping (`SetFromAPI` on an exported `Definition` struct that the resource model embeds), so the two can't drift apart. There is no fallback by GID and nothing is written to state beyond the read.

**Errors:**
- Not found: error, never an empty result, naming the key (`no metafield definition PRODUCT/shopify/material`, `no metaobject definition of type shopify--material`).
- Missing scope: Shopify's `ACCESS_DENIED` is reported with the owner type and the scope hint. The dev store (2026-10-26) returns `null` instead of an error for an owner type the app can't read (§ scope ⚠ in `docs/bootstrapping.md`), so the not-found error for a metafield definition also says the app may lack the read scope for that owner type. `TODO(verify)`: which scope each owner type needs.
- Nothing is retried (throttling is handled in the client).

**Scopes:** same as the resources: `read_products` (or `write_products`) for `PRODUCT`/`PRODUCTVARIANT`/`COLLECTION`, `read_metaobject_definitions` for metaobject definitions; other owner types per the ⚠ note.

**Fit:** good. These are read-only references. Definitions owned by Shopify or an app can change with a new Shopify release or app version, and a data source reads the current state on every plan.

**Tests:** acceptance: (a) a definition created by the resource in the same config reads back through the data source with every mirrored attribute equal; (b) a Shopify-standard definition, if the dev store has one; (c) not found. Unit: the not-found message.

---

## 9h. Data source: `shopify_fulfillment_service`

Decision D4 (2026-10-26). Discovery found two fulfillment services on staging and production: `manual` (Shopify's built-in) and `test_fulfillmentservice_creation` (owned by another app). Both are excluded from import (D3). The data source makes them referenceable, for example by a configuration that needs the service's location or ID. There is no resource: this provider doesn't manage fulfillment services.

```hcl
data "shopify_fulfillment_service" "manual" {
  handle = "manual"
}
```

**Argument (Required):** `handle`, the service's human-readable unique identifier (`manual`).

**Attributes (all Computed, from `FulfillmentService` in the 2026-10 schema):**

| Attribute | API field | Why |
|---|---|---|
| `id` | `id` | GID; `manual`'s is `gid://shopify/FulfillmentService/manual`, not numeric (checked on the dev store) |
| `service_name` | `serviceName` | name merchants see |
| `type` | `type` | `MANUAL`, `THIRD_PARTY` or `GIFT_CARD`; tells built-in from app services |
| `location_id` | `location.id` | the service's location GID, the usual reason to look it up; null if the API returns none |
| `callback_url` | `callbackUrl` | where Shopify calls the service; null for `manual` |
| `inventory_management` | `inventoryManagement` | whether the service reports inventory |
| `tracking_support` | `trackingSupport` | whether the service supplies tracking numbers |
| `requires_shipping_method` | `requiresShippingMethod` | whether products must be physically shipped |

Left out: `fulfillmentOrdersOptIn` (deprecated, always `true`), `createdAt` (not a useful reference). `permitsSkuSharing` no longer exists in 2026-10, so it isn't offered.

**Lookup:** `shop { fulfillmentServices { ... } }` (not deprecated; there is no root field and no `query:` filter) lists every installed service, and the data source picks the one whose `handle` matches exactly. The list is small, so no pagination. There is no fallback by GID and nothing is written beyond the read.

**Errors:**
- Not found: error naming the handle and listing the handles that exist (`no fulfillment service with handle "x"; available handles: manual, ...`).
- Missing scope (`read_fulfillments`): Shopify's `ACCESS_DENIED` is reported with the scope name.
- Nothing is retried (throttling is handled in the client).

**Scopes:** `read_fulfillments` (or `write_fulfillments`). `TODO(verify)`: whether the list also needs a location scope to return `location`.

**Fit:** good. Read-only reference; services installed by apps come and go, and the data source reads the current state on every plan.

**Tests:** acceptance: read `manual` and check its attributes (the dev store has only `manual`, so a third-party service is covered by unit tests only); not found. Unit: match by handle, the not-found message, mapping from the API result.

---

## 9i. Resource: `shopify_catalog`

Decision D5 assumed the catalogs found by discovery (staging 3, production 5) are Shopify defaults to adopt. **They are not:** on the dev store all three catalogs are `AppCatalog`s, one per sales channel publication (`Channel Catalog <publication id> for Online Store`, `… for Shop`, `… for Point of Sale`), and on staging and production the catalog count equals the publication count, every one of them a channel app. App catalogs can't be created, changed or deleted through the API (`CANNOT_CREATE_APP_CATALOG`, `CANNOT_MODIFY_APP_CATALOG`, `CANNOT_DELETE_APP_CATALOG`) and are excluded by D3. The type a merchant manages, the market catalog, has no instances on either store. There are no defaults to adopt.

```hcl
resource "shopify_catalog" "de_b2c" {
  title      = "DE B2C"
  market_ids = [shopify_market.dach.id]
  status     = "ACTIVE" # optional, unset = unmanaged (created as ACTIVE)

  # optional, unset = unmanaged; plain IDs, see below
  # publication_id = "gid://shopify/Publication/123"
  # price_list_id  = "gid://shopify/PriceList/456"
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `id` | string | `MarketCatalog` GID (`gid://shopify/MarketCatalog/<n>`). Computed, UseStateForUnknown |
| `title` | string | Required, import key. Updatable. Unique across **all** catalogs, app catalogs included: a taken title fails with `TAKEN`, `field = ["input","title"]` (checked, also against an app catalog's title) |
| `market_ids` | set(string) | Required, may be empty. `Market` GIDs. Sent in full as `context.marketIds` (replace semantics, see below). The catalog owns this relation |
| `status` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. `ACTIVE`, `DRAFT` or `ARCHIVED`. `CatalogCreateInput.status` is required, so Create sends `ACTIVE` when unset |
| `publication_id` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. The publication that limits which products the catalog shows; null when product availability follows the sales channel |
| `price_list_id` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. The catalog's price list |
| `adopt_existing` | bool | Pattern A, see below |
| `allow_data_deletion` | bool | Pattern B, see below |

Import key: the title. Titles are unique per shop, so a lookup never finds two. Import looks it up with `catalogs(query: "title:\"<title>\"")`, without a `type` filter, and matches the title exactly (the search is a text search; `type: MARKET` would turn an app catalog's title into a bare "not found"). A match that is not a `MarketCatalog` fails with "not a market catalog" and its type. Read: `catalog(id:)` first; it returns the `Catalog` interface, and anything other than a `MarketCatalog` is an error. If the GID is gone, the same title lookup as import; `MutableIdentity` is set.

Granularity: one resource per catalog.

**Scope of the type: market catalogs only.**
- `AppCatalog`: owned by the channel apps; excluded (D3). The publications they hold are readable through `shopify_publication` (§9j).
- `CompanyLocationCatalog`: B2B. Needs the `companies` scopes, which aren't in `app/*.toml` (`docs/bootstrapping.md`), and B2B is plan-gated (`COMPANY_LOCATION_CATALOG_STATUS_PLAN`, `UNPERMITTED_ENTITLEMENTS_DIRECT_CATALOG_ASSIGNMENT`). Roadmap. `CatalogContextInput` takes exactly one of `marketIds` and `companyLocationIds` (`MUST_PROVIDE_EXACTLY_ONE_CONTEXT_TYPE`), so a later `company_location_ids` attribute, exactly one of the two, extends this resource without a new type.

**Markets: one owner.** `CatalogCreateInput.context` is non-null, so a catalog names its markets from the start, unlike web presences (§9a), where the market owns the link. `shopify_catalog.market_ids` owns the relation. `shopify_market.catalog_ids` writes the same relation (`marketUpdate(catalogsToAdd/catalogsToDelete)`; checked: removing a catalog there removes the market from the catalog). It stays as it is (Optional + Computed, unset = unmanaged), and the docs of both resources say: set `market_ids` on the catalog and leave `catalog_ids` unset on its markets; setting both makes each apply undo the other.

Notes:
- Uses `catalog(id:)`, `catalogs(type: MARKET, query:)`, `catalogCreate`, `catalogUpdate`, `catalogDelete`. `catalogContextUpdate` (add/remove lists) is not needed: `catalogUpdate(input: { context: { marketIds } })` replaces the whole market set (checked: `[de]` → `[tf-probe]` gave `[tf-probe]`; `[]` cleared it), so Update sends the planned set when it differs from state.
- **Update** sends only changed fields: `title`, `status`, `context`, and `publicationId` / `priceListId` when set in config and different from state. Removing `publication_id` or `price_list_id` from config stops managing it; the link stays. An explicit `null` input detaches (checked for both), but the provider never sends it, since unset can't be told from "detach".
- **What a market catalog holds, verified on the dev store (2026-10-26):**
  - Several catalogs per market work, and one catalog can have several markets (two region markets; `CANNOT_ADD_MORE_THAN_ONE_MARKET` not hit). A catalog with no markets is valid: `catalogCreate` with `marketIds: []` and removing the last market both succeed.
  - `status: DRAFT` on a market catalog works (`MARKET_CATALOG_STATUS`, "must have an active status", not hit). `ARCHIVED` not tried.
  - Deleting a market removes it from the catalog's markets; the catalog stays. The catalog then plans an update of `market_ids` (drift), which Terraform's dependency order avoids when `market_ids` references `shopify_market.<x>.id`.
  - A publication or price list belongs to at most one catalog: assigning one that another catalog holds fails with `TAKEN` (`field = ["input","publicationId"]` / `["input","priceListId"]`), and nothing is moved. Attaching a free one (`publicationCreate` / `priceListCreate` without `catalogId`) through `catalogUpdate` works. `PRICE_LIST_NOT_ALLOWED_FOR_PRIMARY_MARKET` was not hit for a catalog in the primary market `de`.
  - A market catalog's publication has an empty `name` and no channels, and the root `publications` query leaves it out unless `catalogType: MARKET` is passed.
- **`publication_id` and `price_list_id` are plain IDs.** No resource creates catalog publications or price lists yet (price lists: 0 on both stores, roadmap; publications: §9j is a read-only data source for channel publications). Until then the IDs come from outside Terraform. The product selection inside a publication and the prices inside a price list are content and stay out of this resource.
- **Pattern A: `adopt_existing` only, no implicit adoption.** No market catalog exists by default. With `adopt_existing = true`, Create looks the title up (same lookup as import; a non-market catalog is an error) and updates the found catalog to the config; destroy only removes it from state with a warning. Without it, Create fails on a taken title (`TAKEN` from the API, mapped to `title`).
- **Pattern B: deleting a catalog deletes its publication and price list.** `catalogDelete(deleteDependentResources: false)` (the default) still deleted both, whether they were created for the catalog (`publicationCreate(catalogId:)`, `priceListCreate(catalogId:)`) or created free and attached later: afterwards `publication(id:)` and `priceList(id:)` returned `null` (checked twice, 2026-10-26). The publication holds the product selection, the price list holds prices; both are data. So Delete reads the catalog first, and if it has a publication or a price list, fails unless `allow_data_deletion = true`, naming both IDs. A catalog with neither is deleted without the guard (it only holds the title, status and market links). Nothing forces a replace (no RequiresReplace attribute), so there is no plan-time replace warning. `deleteDependentResources` is not sent.
- `CATALOG_NOT_FOUND` on delete ("Catalog does not exist", checked) means already gone: removed from state without error.
- Errors (`CatalogUserError`, `field` relative to `input`): `TAKEN` (`title`, `publication_id` or `price_list_id`, by `field`), `MARKET_NOT_FOUND` (`market_ids`), `PUBLICATION_NOT_FOUND` (`publication_id`), `PRICE_LIST_NOT_FOUND` and `MARKET_AND_PRICE_LIST_CURRENCY_MISMATCH` and `PRICE_LIST_NOT_ALLOWED_FOR_PRIMARY_MARKET` (`price_list_id`), `MUST_PROVIDE_EXACTLY_ONE_CONTEXT_TYPE` (`market_ids`; can't happen, the provider always sends `marketIds`), `UNPERMITTED_ENTITLEMENTS_MARKET_CATALOGS` and `CONTEXT_CATALOG_LIMIT_REACHED` (plan limits, unmapped), `CANNOT_MODIFY_MARKET_CATALOG` / `CANNOT_DELETE_MARKET_CATALOG` (not hit; `TODO(verify)`: which market catalogs they protect). None is retried, `CATALOG_CONTEXT_LOCKED` and `PRICE_LIST_LOCKED` ("try again later") included: the message asks the user to re-apply.
- genqlient: `catalog(id:)` and the payloads return the `Catalog` interface with three implementers; select `__typename` and `... on MarketCatalog { markets(first: 250) { nodes { id } } }`. Unlike `Node`, genqlient generates this interface.
- **Fit:** good. A catalog is configuration (which markets see which product set and prices). The publication contents and price lists it points to are volatile and not managed here.
- **Scopes:** `TODO(verify)`: Shopify's docs list `write_products` for the catalog mutations; reading `markets` on a catalog needs `read_markets`; attaching a publication may need `write_publications`. All three are granted on the dev store, so it can't tell them apart.
- **Review Focus 1:** after destroy of an adopted catalog, read it back through the client and expect it unchanged. **Review Focus 2:** set `status` only in one step, leave it out in the next, change it through the client in between: the config without `status` plans empty.
- **Testing:** the dev store has one market (`de`, primary). Tests create a draft market for the multi-market and replace steps and delete it in cleanup. The Pattern B step attaches a free price list (`priceListCreate` without `catalogId`, through the client), checks that destroy fails without `allow_data_deletion` and that the price list is gone after destroy with it (acceptance tests may set it on the dev store).

**List:** no config. `catalogs(type: MARKET)`, paged; app and company location catalogs are never listed. `DisplayName`: `<title> (<market handles, comma-separated>)`, `<title> (no markets)` when empty. Results carry `title`, `market_ids`, `status`, and `publication_id` / `price_list_id` when not null, which pass validation and plan as import-only.

---

## 9j. Data source: `shopify_publication`

Decision D4 (2026-10-26). Discovery found 3 publications on staging (Online Store, Point of Sale, Shop) and 5 on production (Online Store, Point of Sale, Konstantins Headless Store, Shopify GraphiQL App, Pflege.De Shop Headless 04). Each belongs to a sales channel app and its app catalog (§9i), so none is managed here (D3). The data source makes them referenceable, for example as a publication ID for a future product or collection resource that publishes to channels. There is no resource.

```hcl
data "shopify_publication" "online_store" {
  name = "Online Store"
}
```

**Argument (Required):** `name`, matched exactly (case-sensitive).

**Attributes (all Computed, from `Publication` in the 2026-10 schema):**

| Attribute | API field | Why |
|---|---|---|
| `id` | `id` | `Publication` GID, what publishing mutations take |
| `auto_publish` | `autoPublish` | whether new products are published to it automatically |
| `catalog_id` | `catalog.id` | the catalog the publication belongs to (an `AppCatalog` GID for channels); null if none |

Left out: `supportsFuturePublishing`, `operation`, the product and collection connections (content), and the deprecated `app` (use the catalog).

**Name, not handle.** Publications have no handle. `Publication.name` is deprecated in 2026-10 ("Use `Catalog.title` instead"), but `Catalog.title` of a channel's app catalog is `Channel Catalog <publication id> for Online Store`, which differs per store and can't be a key. The channel behind a publication has a handle (`online_store`, `pos`, `shop-72` on the dev store), but the `shop-72` suffix suggests it is not the same on every store. So the key stays the name, read from the deprecated field: it returns the channel name on the dev store, and genqlient copies descriptions, not deprecation reasons, into generated code, so lint stays quiet (confirmed at implementation: `make lint` reports no issues). If a later API version removes the field, the generated code stops compiling, which is the intended signal. `TODO(verify)`: whether every channel publication has a matching `channels.nodes[].name` (true for all three on the dev store), which would be the non-deprecated fallback.

**Lookup:** root `publications(first: 250)` without `catalogType`, paged (`patterns.Paginate`). On the dev store it returns the channel publications only; a market catalog's publication (empty `name`) shows up only with `catalogType: MARKET` (checked, §9i). Publications with an empty name are skipped. No fallback by GID and nothing is written beyond the read.

**Duplicate names:** the API doesn't stop two channels from having the same publication name (none found: the five production names are distinct). More than one match is an error that lists the GIDs and catalog IDs of the matches, never a pick. Not found: error naming the name and listing the names that exist (`no publication named "Onlne Store"; available: Online Store, Point of Sale, Shop`), as `shopify_fulfillment_service` does.

**Errors:** missing scope (`read_publications`): Shopify's `ACCESS_DENIED`, reported with the scope name. Nothing is retried (throttling is handled in the client).

**Scopes:** `read_publications` (or `write_publications`). `TODO(verify)`: checked only with both `read_publications` and `read_products` granted.

**Fit:** good. Read-only reference; channels come and go with app installs, and the data source reads the current state on every plan.

**Tests:** acceptance: read `Online Store` and check `id` (`^gid://shopify/Publication/`), `catalog_id` (`^gid://shopify/AppCatalog/`) and `auto_publish`; not found. On the dev store all three publications (Online Store, Shop, Point of Sale) have `auto_publish = false`, so the test asserts `false` for the Online Store. Unit: exact match, empty names skipped, the duplicate and not-found messages, mapping.

---

## 9k. Resource: `shopify_product`

Discovery: 86 products on staging, 214 on production, all merchant-owned (D2: build). Products are content: merchants and other systems (PIM, ERP, apps) edit them in the admin all the time. The resource exists so a store can be bootstrapped or a small catalogue kept in a repository; the description says it is a poor fit for Terraform otherwise.

```hcl
resource "shopify_product" "bettschutz" {
  handle            = "bettschutzeinlage"
  title             = "Bettschutzeinlage"
  status            = "ACTIVE"                 # optional, unset = unmanaged (created as ACTIVE)
  vendor            = "pflege.de"              # optional, unset = unmanaged (Shopify sets the shop name)
  product_type      = "Pflegehilfsmittel"
  tags              = ["pflegehilfsmittel", "waschbar"]
  description_html  = file("${path.module}/products/bettschutzeinlage.html")
  seo_title         = "Waschbare Bettschutzeinlage"
  publication_ids   = [data.shopify_publication.online_store.id] # §9k.3

  options = [
    { name = "Größe", values = ["S", "M"] },
  ]

  variants = [
    { option_values = { "Größe" = "S" }, price = "19.90", sku = "BSE-S" },
    { option_values = { "Größe" = "M" }, price = "24.90", sku = "BSE-M", compare_at_price = "29.90" },
  ]

  allow_data_deletion = false
}
```

**Granularity: one resource per product, holding its options, variants, publications and (later) media.** Every product has at least one variant, Shopify creates and deletes variants together with option values, and `productSet` writes options and variants of one product in a single synchronous call. Separate variant resources would split one `productSet` call over many resources, with no natural key of their own (a variant is identified by its option values within its product) and constant ordering trouble. Publishing is a set of IDs on the product (§9k.3), not a resource, so only one resource ever writes a product's publications. Media is a list of file IDs on the product (§9k.4).

The work is split into four tasks (implementation order): §9k.1 core, §9k.2 options and variants, §9l collection (next section), §9k.3 publishing for both products and collections, §9k.4 media (after the files type, step 13 part b).

### 9k.1 Product core

| Attribute | Type | Behaviour |
|---|---|---|
| `id` | string | `Product` GID. Computed, UseStateForUnknown |
| `handle` | string | Required, import key. Updatable in place. Validated: no upper-case letters and no whitespace, since Shopify normalises them (`TF-Design Probe_3` became `tf-design-probe_3`, checked), which would otherwise plan forever |
| `title` | string | Required |
| `status` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. `ACTIVE`, `DRAFT`, `ARCHIVED`, `UNLISTED` (2026-10 has all four). Create without it gives `ACTIVE` (checked) |
| `description_html` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. See "HTML normalisation" |
| `vendor` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. Shopify sets the shop name when it is missing (checked) |
| `product_type` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. Free text; Shopify returns `""` when unset |
| `category_id` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. `TaxonomyCategory` GID (`gid://shopify/TaxonomyCategory/aa-1`) |
| `tags` | set(string) | Optional + Computed, UseStateForUnknown; unset = unmanaged. Shopify trims, de-duplicates case-insensitively and sorts (`["Zeta"," spaced ","zeta"]` read back as `["spaced","Zeta"]`, checked); a validator rejects leading or trailing whitespace and case-insensitive duplicates so the read matches the config |
| `seo_title`, `seo_description` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. Null when Shopify uses the defaults |
| `template_suffix` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged |
| `gift_card` | bool | Optional + Computed, UseStateForUnknown, **RequiresReplace** (`GIFT_CARD_ATTRIBUTE_CANNOT_BE_CHANGED`). Gift card *products* are catalogue entries; issued gift cards stay out of scope |
| `adopt_existing` | bool | Pattern A, see below |
| `allow_data_deletion` | bool | Pattern B, see below |

Import key: the handle (`bettschutzeinlage`). Handles are unique per shop (`productSet` and `productCreate` both fail with `HANDLE_NOT_UNIQUE` / "already in use" on a taken handle, checked; no `-1` suffix as with menus). Read: `product(id:)` first; if it is gone, `productByIdentifier(identifier: { handle })` (not search-indexed, unlike `products(query:)`); `MutableIdentity` is set. `productByHandle` is deprecated and not used.

**Writes go through `productSet` (synchronous), never `productCreate`/`productUpdate`.** One mutation covers the core fields, options and variants, so §9k.2 extends the same call instead of adding a second write path.
- Create: `productSet(input)` without `identifier`; a taken handle fails with `HANDLE_NOT_UNIQUE`, mapped to `handle`. With `adopt_existing = true`, Create looks the handle up first and, if found, writes the config with `identifier: { id }`.
- Update: `productSet(identifier: { id }, input)`. `ProductSetInput` has no `id`; the identifier alone selects the product, and a new `handle` in `input` renames it in place (checked). With `identifier: { handle }`, `input.handle` is required (`MISSING_FIELD_REQUIRED`), so the provider never uses the handle identifier.
- **Omit, don't empty:** keys left out of `ProductSetInput` are unchanged, list fields included: omitting `metafields`, `collections`, `productOptions` and `variants` kept all of them (checked). `tags`, `metafields` and `collections` given as a list are replaced as a whole. The provider sends only attributes that are set in config, and never sends `collections` (§9l owns membership), `metafields` (metafield values are a separate roadmap type), `files` unless `media_ids` is set (§9k.4), `requiresSellingPlan`, `claimOwnership` or `combinedListingRole`.
- `redirectNewHandle` is not sent: a handle change creates no URL redirect (checked: none was created). URL redirects are a roadmap type.
- Large products: `synchronous: true` may time out for products with many variants; Shopify's answer is `synchronous: false` plus `productOperation` polling. The schema names no threshold ("may be desirable depending on the input complexity/size, and should be used if you are experiencing timeouts"; default limit 2,048 variants per product). Checked on the dev store (2026-10-27, throwaway product `tf-acc-probe-large-…`, deleted again): a synchronous `productSet` creating a product with 3 options (12 × 12 × 14 values) and 2,016 variants returned after 21 s, and re-sending all 2,016 variants with a new price took 16 s, both without userErrors, about a third of the client's 60 s HTTP timeout; `productDelete` took 1 s. So a product near the variant limit doesn't need the async mode on its own; v1 stays synchronous only. A timeout under production load can't be ruled out from the dev store; it would surface as a transport error, which the client does not retry.

**HTML normalisation.** Shopify repairs `descriptionHtml`: `<p>Hi<br>there<b>bold</p>` came back as `<p>Hi<br>there<b>bold</b></p>` (checked); well-formed HTML, `<script>` tags and trailing whitespace come back verbatim. To avoid a permanent diff for HTML Shopify rewrites, Create and Update store the value Shopify returned in private state (`description_html_written`). Read keeps the configured value in state while the live value equals that stored value, and takes the live value otherwise (an admin edit then shows as drift). Pure logic, unit-tested first.

**Pattern A: `adopt_existing` only.** Products have no store defaults. With `adopt_existing = true`, Create updates the product with that handle (above), and destroy only removes it from state with a warning.

**Pattern B: deleting a product destroys data.** `productDelete` removes the product's variants and their inventory items with all stock levels, media not used by another product, option values, publications, metafield values and translations (schema description). Checked on the dev store: after `productDelete`, the product's only `MediaImage` was gone from `files`, and the variant's `InventoryItem` returned `null` a few seconds later. Completed orders and refunds keep their copy of the product data; draft orders lose the reference (schema). So Delete, or a replace that would delete (only `gift_card` forces one), fails unless `allow_data_deletion = true`; ModifyPlan warns on a planned replace. With `media_ids` managed, Delete detaches the media first and they survive (§9k.4). The docs suggest `status = "ARCHIVED"` instead of destroy, and `lifecycle { prevent_destroy = true }`. A missing product on delete (`Product does not exist`, plain `UserError`, `field = ["id"]`, checked) means already gone: removed from state without error.

Notes:
- **Not managed in v1** (left out of `ProductSetInput`, so admin values stay): metafield values (roadmap type; `metafields` would replace all of them), translations (roadmap), inventory quantities (`inventoryQuantities`; inventory levels are a roadmap type), collections (§9l), selling plans (`requiresSellingPlan`, `productJoinSellingPlanGroups`; selling plan groups are on the roadmap), combined listings and bundles (`combinedListingRole`, `productBundle*`), `claimOwnership`, `giftCardTemplateSuffix`. Deprecated fields (`bodyHtml`, `productCategory`, `customProductType`, `productChangeStatus`, `productPublish`, `productCreateMedia`, `productByHandle`) are not used.
- The dev store put two of three new `DRAFT` probe products into its `frontpage` collection without being asked (an `ACTIVE` one stayed out). `TODO(verify)`: the cause. The product never reads or writes collections, so it doesn't drift; a `shopify_collection` for `frontpage` would (§9l).
- **genqlient:** `Money` needs a string binding in `internal/client/genqlient.yaml` (like `Decimal`); `HTML` and `Decimal` already have one.
- Errors: `ProductSetUserError` (`code`, `field` relative to `input`): `HANDLE_NOT_UNIQUE` (`handle`), `INVALID_PRODUCT`, `INVALID_INPUT`, `PRODUCT_DOES_NOT_EXIST` (Read handles the gone case before writing), `PRODUCT_SUSPENDED`, `GIFT_CARDS_NOT_ACTIVATED` / `GIFT_CARD_ATTRIBUTE_CANNOT_BE_CHANGED` (`gift_card`), `CAPABILITY_VIOLATION`, `GENERIC_ERROR`, plus the option and variant codes in §9k.2. `productDelete`: plain `UserError`. None is retried.
- **Fit: volatile.** The description says so: products are usually owned by merchants or a PIM; managing them here means every admin edit to a managed attribute is reverted on the next apply. Unset attributes stay unmanaged to keep that small.
- **Scopes:** `read_products` / `write_products` (`docs/bootstrapping.md` already lists products). `TODO(verify)`: whether `read_inventory` is needed for the `inventoryItem` fields read in §9k.2; every scope is granted on the dev store, so it can't tell.
- **Review Focus 1:** acceptance step reads the product back through the client after an adopted product's destroy and expects it unchanged. **Review Focus 2:** set `vendor` in one step, leave it out in the next, change it through the client in between: the config without `vendor` plans empty.
- **Testing:** handles `tf-acc-product-<random>`; destroy with `allow_data_deletion = true` (allowed on the dev store). Unit tests first: the handle and tag validators, the description-HTML keep-or-take rule, input mapping (only set attributes are sent).
- **Implemented (§9k.1, verified on the dev store):** the input is built in one function (`buildInput`) that §9k.2 extends; every `ProductSetInput` key is `omitempty` in the query, and `tags` is raw JSON so `[]` clears while unset is omitted. A configured description is kept in state while the live value equals the one Shopify returned at the last write (private state key `description_html_written`); an admin edit shows as drift and the next apply restores it. Delete looks the product up first instead of matching the `Product does not exist` message. `adopt_existing = true` makes destroy state-only for that resource, also when the product was created by it. An import test config must leave out `allow_data_deletion = true`, which an import can't know. `seo` is sent as one `SEOInput` with only the configured sibling set: leaving `description` out kept an admin-set SEO description, and setting only `description` through the API kept the title (checked).

**List:** no config. `products(first: 50)`, paged (`patterns.Paginate`); requested cost with options, variants (first 250) and `barcodes(first: 20)` per variant (§9k.2) was 1,064 for a page of 100, refused against the 1,000 limit, and 828 for 50 (checked). With `resourcePublicationsV2(first: 10)` per product (§9k.3) a page of 50 costs 898 (checked on the dev store, 2026-10-26), so pages stay at 50; 100 would cost about twice that. With `media(first: N) { nodes { id } }` (§9k.4) it costs 940 for N = 10, 947 for 20 and 961 for 50 (checked, 2026-10-27); N = 20 is used. `DisplayName`: `<title> (<handle>)`. Results carry `handle`, `title` and every Optional + Computed attribute that is not null, so generated config plans as import-only; with §9k.2 they also carry `options` and `variants` (generated config for 214 products is long, one block per variant). Gift card products are listed (`gift_card = true`).

### 9k.2 Options and variants

```hcl
  options = [
    { name = "Größe", values = ["S", "M", "L"] },
    { name = "Farbe", values = ["Weiß"] },
  ]

  variants = [
    {
      option_values     = { "Größe" = "S", "Farbe" = "Weiß" }
      price             = "19.90"
      sku               = "BSE-S-W"
      barcodes          = [{ value = "4012345678901", type = "EAN" }]
      inventory_policy  = "DENY"
      taxable           = true
      tracked           = true
      requires_shipping = true
      weight            = { value = 0.4, unit = "KILOGRAMS" }
    },
    # ...
  ]
```

| Attribute | Type | Behaviour |
|---|---|---|
| `options` | list(object) | Optional + Computed, UseStateForUnknown; unset = unmanaged together with `variants`. Up to 3 options (`OPTIONS_OVER_LIMIT`), order = position. `name` (string, Required), `values` (list(string), Required, order = position) |
| `variants` | list(object) | Optional + Computed, UseStateForUnknown. Required when `options` is set. Order = position |
| `variants[].option_values` | map(string) | Required. Option name → value; keys must equal the option names, values must be in that option's `values`, combinations unique (ValidateConfig). Empty for a product without options |
| `variants[].id`, `variants[].inventory_item_id` | string | Computed. Carried forward by ModifyPlan from the matched prior variant (below), unknown for new variants |
| `variants[].price`, `compare_at_price` | string | Optional + Computed; unset = unmanaged. Decimal strings; compared numerically (`"10"` reads back as `"10.00"`, checked), the configured spelling stays in state when equal |
| `variants[].sku` | string | Optional + Computed; unset = unmanaged. Not an identity: SKUs can be empty or repeated |
| `variants[].barcodes` | list(object) | Optional + Computed; unset = unmanaged. `value`, `type` (optional, `BarcodeType`). 2026-10 replaces the deprecated single `barcode` |
| `variants[].inventory_policy` | string | Optional + Computed; `DENY` / `CONTINUE` |
| `variants[].taxable` | bool | Optional + Computed |
| `variants[].tracked`, `requires_shipping` | bool | Optional + Computed. Written through `inventoryItem` |
| `variants[].weight` | object | Optional + Computed. `value` (number), `unit` (`WeightUnit`). Written through `inventoryItem.measurement.weight`; matters for weight-based shipping rates (§8) |

**Unset `options` and `variants`:** neither is sent; Shopify keeps the existing ones (checked). A new product then has Shopify's default: one option `Title` with value `Default Title` and one variant. Read maps a product with `hasOnlyDefaultVariant = true` to `options = []` and one variant with empty `option_values`; writing that variant sends `productOptions: [{ name: "Title", values: [{ name: "Default Title" }] }]` and `optionValues: [{ optionName: "Title", name: "Default Title" }]`, which updated the default variant in place (checked, same variant GID). `productSet` refuses `variants` without `productOptions` (`PRODUCT_OPTIONS_INPUT_MISSING`, checked), so both are always sent together.

**Variant identity is the option-value combination, not the SKU.** `productSet` matches variants without `id` by their option values: re-sending the same variants with a changed price kept every variant and inventory item GID (checked), and adding an option kept the existing variant (`S` → `S / Red`, same GID). But renaming an option without its `id` re-created every variant (new variant and inventory item GIDs, SKU lost; checked), and with the option, option-value and variant IDs the same rename kept all GIDs (checked). So Update always sends IDs:
1. Read the live product (options with `optionValues { id name }`, variants with `id` and `selectedOptions`).
2. Match planned options to prior-state options by name, then leftovers by position: same position and a new name is a rename. Within each matched option, match values the same way (name first, then position among the leftovers). Inserting `XS` before `S` therefore adds a value instead of shifting names.
3. Map each prior variant's option values through those renames; a planned variant whose combination equals a mapped prior combination keeps that variant's `id`.
4. Send `productOptions` with `id`s for matched options and values, and `variants` with `id`s for matched variants. Everything unmatched is created; prior variants without a match are deleted.

The same matching runs in ModifyPlan to carry `id` and `inventory_item_id` (and the unset Optional + Computed variant fields) forward, so the plan shows which variants survive. Pure logic in `match.go`, unit-tested first. `ProductOption.values` lists only values that have variants; Read uses `optionValues` (a value without variants is allowed, `hasVariants = false`, checked).

**Pattern B for variants.** Removing a variant, or an option value or option whose variants go with it, deletes those variants' inventory items and stock (checked: a removed variant's inventory item returned `null`). Like a removed metaobject field (§5), such an update fails unless `allow_data_deletion = true` (the planned value counts), and ModifyPlan warns, naming the variants by option values. Renames detected by the matching above don't count as removals.

Notes:
- Writes: options and variants go in the same `productSet` call as §9k.1. `productVariantsBulk*` and `productOption*` are not used: `productSet` covers creation, update, deletion and ordering in one synchronous call, and splitting writes across mutations would make a partly applied change possible.
- Within a variant, unset fields are omitted and keep their value (checked: a variant sent without `price` and `sku` kept both).
- Variant order = list order, sent as `position`; option and value order likewise. Reordering in the admin plans a change back.
- **Not managed in v1:** inventory quantities (`inventoryQuantities`; inventory levels roadmap), cost, country/province of origin and HS codes, `unitPriceMeasurement` / `showUnitPrice`, `taxCode` (its read field is deprecated without replacement), `requiresComponents`, variant `metafields`, variant media (`file`; roadmap, §9k.4), linked options (`linkedMetafield`). Ruling S13-7 (2026-10-26) replaced "Read fails on a linked option" so that staging and production imports aren't blocked: Read, import and list map a linked option's names and values read-only. The list leaves `options` and `variants` null for such a product, so generated config keeps them unmanaged. A write that sends options or variants to a product with a linked option (update, or create with `adopt_existing`) is refused before `productSet`, naming the option and its metafield, since writing it without `linkedMetafield` would unlink it. Writes of core fields only work as usual. `TODO(verify)`: how many staging/production products use linked options; the acceptance path is untested because the dev store has none.
- Read pages `variants(first: 250)` per product (up to 2,048 variants per product).
- Errors (`ProductSetUserError`, `field` relative to `input`): `OPTIONS_OVER_LIMIT`, `OPTION_VALUES_OVER_LIMIT`, `OPTION_VALUES_MISSING`, `DUPLICATED_OPTION_NAME`, `DUPLICATED_OPTION_VALUE`, `OPTION_DOES_NOT_EXIST`, `OPTION_VALUE_DOES_NOT_EXIST` (`options`), `VARIANTS_OVER_LIMIT`, `INVALID_VARIANT`, `PRODUCT_VARIANT_DOES_NOT_EXIST`, `DUPLICATED_VALUE`, `INPUT_MISMATCH` (`variants`), the barcode codes (`INCOMPATIBLE_BARCODE_FIELDS`, `BLANK_BARCODE_VALUE`, `TOO_MANY_BARCODES`, `BARCODE_VALUE_TOO_LONG`, `DUPLICATE_BARCODE_VALUE`, `INVALID_BARCODE_TYPE`, `INVALID_BARCODE_SEPARATORS`, `INVALID_BARCODE_LENGTH`, `INVALID_BARCODE_PREFIX`, `INVALID_BARCODE_CHARACTERS`, `BARCODE_CHECKSUM_VALIDATION_FAILED`) mapped to the variant's `barcodes`. `field` paths carry list indexes (`BLANK_BARCODE_VALUE` came back as `["input","variants","0","barcodes"]`, checked), the index of the sent list, which follows config order, so they map to `variants[i]`. None is retried.
- **Review Focus 2:** leave `sku` out of one variant, change it through the client: the plan stays empty.
- **Testing:** acceptance steps for add variant, rename option and value (variant `id`s unchanged), remove variant with and without `allow_data_deletion`, default-variant product price change. Unit tests first for matching, validation and price comparison.
- **Implemented (§9k.2, verified on the dev store):**
  - "Position" for leftovers means the same index in the list: a new name takes the place of an unmatched old name at the same index (rename), otherwise it is new. A rename that also moves (`[S, M]` → `[Medium, S]`) is therefore a removal plus an addition, guarded like any removal; rename and reorder go in separate applies. This is the safer reading: a false rename would silently move a variant's stock to another value.
  - A variant is kept when its combination, mapped through renames, equals a planned one on the options both sides share. This covers adding an option, which keeps variants, and removing one: when that collapses several variants onto one combination, the first keeps its IDs and the rest count as removed. When an option is dropped and another one added in the same apply (no rename at the same index), the shared options can't tell variants apart, so no variant is kept: all count as removed (guard and warning), and all planned ones are new. Without this, stock would move between unrelated variants.
  - `options` and `variants` are set together (ValidateConfig); `options = []` with one variant with `option_values = {}` is the only spelling of a product without options, which is what Read produces.
  - Update sends option and value IDs plus variant IDs; variants reference options by (new) name, which resolved renames (checked). Removing an option with the variant IDs kept every variant, and so did default variant → options and options → default variant (the first variant becomes the default one) (checked).
  - A barcode sent without `type` reads back with `type = null`, so `type` is Optional only; `barcodes = []` clears them (checked; sent as raw JSON, since an `omitempty` list would drop `[]`). A weight of 250 `GRAMS` reads back as 250 `GRAMS` (checked).
  - A configured `sku = ""` stays in state when Shopify returns null. `compare_at_price` can't be cleared: unset means unmanaged and the input can't send null.
  - The variant-removal guard uses the planned `allow_data_deletion`, so one apply can set it and remove a variant; product destroy still needs it in state.

### 9k.3 Publishing (products and collections)

```hcl
resource "shopify_product" "bettschutz" {
  # ...
  publication_ids = [data.shopify_publication.online_store.id]
}

resource "shopify_collection" "pflegehilfsmittel" {
  # ...
  publication_ids = [data.shopify_publication.online_store.id]
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `publication_ids` | set(string) | Optional + Computed, UseStateForUnknown; unset = unmanaged. `Publication` GIDs of sales channels, usually from `data.shopify_publication` (§9j) |

**A set on the resource, not a separate resource.** A `shopify_product_publication` would write the same relation as this attribute; with only the attribute there is one writer per product or collection. The same attribute and the same small helper (`publishablePublish` / `publishableUnpublish`, `resourcePublicationsV2`) serve both types, so one task adds it to both after §9l.

- Read: `resourcePublicationsV2(first: 10, onlyPublished: false) { nodes { publication { id } } }`. With `onlyPublished: false` a draft product shows its publications too (`isPublished = false` until it is `ACTIVE`; checked: publishing a `DRAFT` product listed both publications as unpublished, and `ACTIVE` flipped Online Store to published). Membership, not `isPublished`, is what the attribute holds, so a status change doesn't plan a publication change. The first page is 10 because 50 per product would blow the list query's cost limit (§9k.1); a resource with more publications is paged by `GetProductPublications` / `GetCollectionPublications`.
- Update: publish the added IDs with `publishablePublish(id, input: [{ publicationId }])`, unpublish the removed ones with `publishableUnpublish` (both checked; publishing an already-published ID is a no-op). `publishDate` (scheduled publishing) is not managed. A new product is published nowhere (checked) and a new collection neither (schema), so Create calls `publishablePublish` after the create when the attribute is set.
- **Channel publications only.** `resourcePublicationsV2` returns `APP` catalog publications unless `catalogType` is passed, and for collections only `APP`. A market catalog's publication ID (§9i) would publish but never read back, planning forever; the docs say to use channel publications, and Update fails with a clear error when Shopify accepts an ID that the follow-up read doesn't return. `TODO(verify)`: whether market-catalog product publishing is wanted; then a `catalogType: MARKET` read can be added.
- Channels with `autoPublish = true` publish new products themselves (all three dev-store channels are `false`; staging and production unknown). With `publication_ids` set, the next plan removes such an automatic publication; unset, it stays unmanaged.
- Errors: plain `UserError`, mapped to `publication_ids` (e.g. an unknown publication ID). Products sold only by subscription can be published to online stores only (schema), which surfaces as Shopify's error.
- **Scopes:** `write_publications` (already in `docs/bootstrapping.md`, "publishing to channels"). `TODO(verify)`: whether `write_products` alone suffices for product publishing; all scopes are granted on the dev store.
- **Fit: volatile**, like products: merchants publish and unpublish in the admin.
- **Testing:** acceptance step adds and removes the Online Store publication for a product and a collection; Review Focus 2: publish through the client while the config leaves `publication_ids` unset, expect an empty plan. The list results include `publication_ids` when not empty.

**Implemented (§9k.3, verified on the dev store, 2026-10-26):**
- One shared package, `internal/publishing`, holds the attribute schema, the diff, the paging and the `publishablePublish` / `publishableUnpublish` calls (`Sync`); `shopify_product` and `shopify_collection` each add only the paging query and a call. Both build the desired set from config, so unset is never sent.
- Publish runs before unpublish, so a rejected ID unpublishes nothing. After the calls, `Sync` compares the publications the mutation returned with the configured ones; a configured ID Shopify accepted but doesn't list (a market catalog's) is an attribute error on `publication_ids` that names it, and state then holds the real publications. An extra one (an `autoPublish` channel racing the apply) is not an error: the result is the planned set and the next refresh shows the drift.
- Published to a draft product and to a collection: both read back with `onlyPublished: false`; Online Store, Shop and Point of Sale all accept products and collections. An unknown publication ID (`gid://shopify/Publication/1`) fails with a `UserError` mapped to `publication_ids`.
- A failed publish after a create leaves the product or collection in state, so Terraform marks it tainted; the error carries a note to run `terraform untaint` and fix `publication_ids` first. For a product, a replace would hit the `allow_data_deletion` guard.
- The `PublicationsOfPublishable` fragment on the `Publishable` interface is spread into `ProductFields` and `CollectionFields`. genqlient can't generate `node(id:)` queries (`interface Node had non-object implementation Catalog`), so paging has one query per type. `DateTime` needed a string binding in `genqlient.yaml` (`PublicationInput.publishDate`, never sent).
- List results leave `publication_ids` null when a product or collection is published nowhere, so generated config doesn't claim to manage it; Read gives an empty set.
- `TODO(verify)`: market-catalog publishing (the dev store has none, so the "accepted but not listed" error is unit-tested only), and whether `write_products` alone suffices (all scopes are granted).

### 9k.4 Media (after the files type)

`media_ids` (list(string), Optional + Computed, UseStateForUnknown, unset = unmanaged, order = position): file GIDs attached to the product, sent as `productSet(input: { files: [{ id }] })`. Product media are files: an image added through `productSet(files: [{ originalSource }])` showed up in `files` with the same `MediaImage` GID (checked). So a product references files from the planned `shopify_file` type (step 13 part b) instead of uploading its own, and this sub-task waits for it. `files` given as a list replaces all media (list semantics of `productSet`); omitted, it keeps them.

Open for that task: whether detaching a file through `productSet` deletes it (deleting the product deleted its only image, checked; Pattern B of `shopify_file` must account for it), variant media (`ProductVariantSetInput.file`), and collection images (`CollectionCreateInput.image` takes an `ImageInput` URL, not a file ID; `TODO(verify)`). Deprecated `productCreateMedia`, `productUpdateMedia`, `productDeleteMedia` are not used.

**Implemented (§9k.4, verified on the dev store, 2026-10-27):** rulings T14d-1 (product-level `media_ids` only) and T14d-2 (removal must not delete files).
- **Probe 1 (throwaway products `tf-acc-probe-<n>` and files from the 1×1 PNG URL, all deleted again):** `productSet(files: [{id: A}, {id: B}])` attached both, `status = READY` in the mutation response (no polling needed for files that are already `READY`); `[B, A]` reordered and kept both GIDs. **Setting `files` to `[A]` deleted B:** `node(id: B)` returned `null` at once and `files(query: "filename:…")` found nothing; `files: []` deleted A the same way. A file that another product still had (B on P1 and P2, then detached from P1) stayed `READY`. An `ExternalVideo` attached and was deleted on detach like an image. `productDelete` deleted a file only that product used and kept one another product had (refines §9k.1). A `GenericFile` GID fails the whole request with a top-level GraphQL error (`productSet Invalid id: gid://shopify/GenericFile/…`), not a userError; an unknown `MediaImage` GID gives `INVALID_INPUT`, `field = ["input","files"]`, "Media ids [1] do not exist". `fileUpdate(files: [{ id: A, referencesToRemove: [P1] }])` detached A and A stayed `READY`.
- **Probe 2 (T14d-2):** P1 `[a, b, c, v]` (v an `ExternalVideo`), P2 `[b]`. One `fileUpdate` with `{id: b, referencesToRemove: [P1]}` and `{id: v, …}` detached both (P1 then `[a, c]`); `productSet(files: [c, a])` reordered; 5 s later b and v were `READY` and P2 still had b. Detaching a and c, then `productSet(files: [])`: both stayed `READY`. Detaching a file that is not attached: no error. Unknown file: `FILE_DOES_NOT_EXIST`, `field = ["files"]`, "File id [\"gid://shopify/MediaImage/1\"] does not exist."; unknown product: `REFERENCE_TARGET_DOES_NOT_EXIST`, `field = ["files"]`. A batch with one valid and one unknown file failed as a whole (the valid file stayed attached). The fields carry no index, so the provider maps every error to `media_ids` with Shopify's message, which names the file.
- **Removal is non-destructive:** when `media_ids` is set, Update reads the live product; IDs it has that the config lacks (removed from config, or attached in the admin) are detached with one `fileUpdate(referencesToRemove: [<product>])` call before `productSet(files:)` sends the configured list, so `productSet` never removes a file. A failed detach stops before `productSet`. Create with `adopt_existing` does the same against the adopted product. No `allow_data_deletion` guard and no plan warning for media; `allow_data_deletion` still guards product destroy and variant removal. The product is still the only writer of the link (§9o).
- **File and ID removed together:** if `shopify_file` is destroyed first, the file is gone from the live product and nothing is detached; if the product update runs first, it detaches the file, then `shopify_file` deletes it. Either way only `shopify_file` deletes it, under its own `allow_data_deletion` (acceptance step with the product's flag `false`). A file deleted between the product's read and the detach would fail the detach with `FILE_DOES_NOT_EXIST`; a re-apply succeeds (not reproduced).
- **Media types:** `Product.media` is the `Media` interface (`MediaImage`, `Video`, `Model3d`, `ExternalVideo`, all of which implement `File`), read through a flattened `MediaID` fragment. `FileSetInput.id` accepted `MediaImage` and `ExternalVideo` GIDs; a list validator allows those four types and unique values only, so a `GenericFile` fails at plan. `Video` and `Model3d` GIDs work too (checked on the dev store, 2026-10-27, with a 2 s MP4 and a one-triangle GLB uploaded by a throwaway probe, see §9o): `productSet(files: [{id: video}, {id: model}])` attached both (`mediaContentType` `VIDEO` / `MODEL_3D`, `status = READY` in the response), `[model, video]` reordered them, and one `fileUpdate(referencesToRemove: [<product>])` detached both, which stayed `READY`. A file in `FAILED` state is refused with `INVALID_INPUT`, `field = ["input","files","<i>","id"]`, "File with id … is in the FAILED state and cannot be associated with a product."
- **Input:** `ProductSetInput.files` is raw JSON like `tags`, so `[]` is sent and clears (after the detach) while unset is omitted.
- **Paging and cost:** `ProductFields` carries `media(first: 20)`; `GetProductMedia` pages the rest (`first: 250`) in `completeProduct`, like variants and publications. List page of 50: 898 without media, 940 / 947 / 961 with `first: 10 / 20 / 50` (limit 1,000). Only a single first page was exercised on the dev store; the follow-up paging is unit-tested with a fake page source.
- **List:** results carry `media_ids` when not empty (null otherwise), so generated config holds file GIDs and is tied to the store, like a manual collection's `product_ids`.
- **Product destroy** (ruling T14d-2 extended to destroy): Delete, and the delete half of a replace, detaches all the live product's media with the same `fileUpdate(referencesToRemove:)` call before `productDelete`, so a file `shopify_file` guards survives (acceptance step: file with `allow_data_deletion = false`, product destroyed, file still there). A failed detach stops before `productDelete`. Delete gets no config, so the last write records in private state (`media_managed`) whether `media_ids` was set; `false` keeps Shopify's behaviour (probe 1: the files no other product uses are deleted). No record (an imported product not written since) detaches, which keeps files rather than deleting them. The product and `shopify_file` docs say so.
- **Roadmap:** variant media (`ProductVariantSetInput.file`) and collection images (`CollectionInput.image` takes an `ImageInput`, not a file GID; `TODO(verify)` whether a file URL works there).

**Dev store (2026-10-26):** probes created three products (`tf-design-probe-1`, `-2`, `-3`) and published, unpublished and re-statused them; all three were deleted, along with the one image added to `-2`. Two probes landed in `frontpage`, which gained an empty `Untitled Source`; the source was removed again, so `frontpage` is back to `sources: []`, 0 products, `MOST_RELEVANT`. End state equals the start: 0 products, 0 files, 1 collection.

---

## 9l. Resource: `shopify_collection`

Discovery: 14 collections on staging, 17 on production, merchant-owned (D2: build). Collections are closer to configuration than products (they back navigation and menus, §9), but their membership and texts are edited in the admin, so the type is volatile too.

```hcl
# Rule-based ("smart"): products match conditions
resource "shopify_collection" "pflegehilfsmittel" {
  handle     = "pflegehilfsmittel"
  title      = "Pflegehilfsmittel"
  sort_order = "BEST_SELLING"   # optional, unset = unmanaged (created as MOST_RELEVANT)
  match_type = "ANY"
  conditions = jsonencode([
    { productTag = { relation = "TAGGED_WITH", values = ["pflegehilfsmittel"], matchType = "ANY" } },
    { variantPrice = { relation = "GREATER_THAN", value = { amount = "5", currencyCode = "EUR" } } },
  ])
  publication_ids = [data.shopify_publication.online_store.id] # §9k.3
}

# Manual: an explicit product list
resource "shopify_collection" "empfehlungen" {
  handle      = "empfehlungen"
  title       = "Unsere Empfehlungen"
  product_ids = [shopify_product.bettschutz.id]
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `id` | string | `Collection` GID. Computed, UseStateForUnknown |
| `handle` | string | Required, import key. Updatable in place. Same validator as `shopify_product.handle` (`TODO(verify)`: that collections normalise handles like products) |
| `title` | string | Required |
| `description_html` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. Same keep-or-take rule as products (§9k.1); `TODO(verify)`: that collections repair HTML the same way |
| `sort_order` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. `CollectionSortOrder`: `ALPHA_ASC`, `ALPHA_DESC`, `BEST_SELLING`, `MOST_RELEVANT`, `CREATED`, `CREATED_DESC`, `MANUAL`, `PRICE_ASC`, `PRICE_DESC`. Created as `MOST_RELEVANT` when unset (checked) |
| `seo_title`, `seo_description` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged |
| `template_suffix` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged |
| `match_type` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. `ANY` or `ALL`: how `conditions` combine. Null for a collection without a source; kept after its conditions are removed (checked) |
| `conditions` | string (JSON) | Optional; unset = unmanaged. A JSON list of `CollectionSourceInclusionConditionInput` objects in the API's camelCase, one key each. See "Conditions" |
| `product_ids` | set(string) | Optional + Computed, UseStateForUnknown; unset = unmanaged. Products included by hand (the source's `selections`), whatever the conditions say |
| `source_id` | string | Computed, UseStateForUnknown. The `CollectionConditionsSource` GID that holds `match_type`, `conditions` and `product_ids` |
| `adopt_existing` | bool | Pattern A, see below |

**Manual and smart collections are one model in 2026-10.** `Collection.ruleSet` is deprecated ("Use `sources` instead"), `collectionCreate` and `collectionUpdate` take only `collection: CollectionCreateInput` / `CollectionUpdateInput` (the legacy `input: CollectionInput` with `ruleSet` and `products` is gone), and `collectionAddProducts`/`collectionRemoveProducts` are deprecated in favour of `collectionUpdate(... inclusion.selectionsToAdd/selectionsToRemove)`. Membership lives in **sources**: a `CollectionConditionsSource` has an inclusion with `conditions` (the old rules) and `selections` (the old manual product list), both at once if wanted, plus exclusions. There are also sub-collection sources and shareable app-owned sources.
- Checked on the dev store (2026-10-26): collections created with the legacy API (2026-07, `ruleSet` and `products`) read back in 2026-10 as exactly one non-shareable `CollectionConditionsSource` titled `Untitled Source`, target `PRODUCTS`, app `null`. A manual one holds its products as `selections` with `matchType = null`; a smart one holds its rules as `conditions`: `TAG EQUALS a` became `productTag { relation: TAGGED_WITH, values: ["a"], matchType: ALL }`, `VARIANT_PRICE GREATER_THAN 5` became `variantPrice { relation: GREATER_THAN, value: { amount: "5.0", currencyCode: "EUR" } }`, and `appliedDisjunctively: false` became `matchType: ALL`. Staging and production collections were made before 2026-10, so they are expected to look the same (`TODO(verify)` at the first import).
- A collection with no sources is valid: `collectionCreate` without `sources` gives `sources: []`, and deleting the last source with `sourcesToDelete` works (both checked).

**v1 manages one conditions source per collection**, the shape every legacy collection has. `match_type`, `conditions` and `product_ids` are that source's inclusion; `product_ids` are product-level selections (no `variantIds`, target `PRODUCTS`). Read decides per collection:
- No source: the three attributes read as null / empty; `source_id` null.
- Exactly one non-shareable `CollectionConditionsSource`, whichever app created it (S13-8: sources created through 2026-10 carry the creating app, and plans run with a read-only app, so app ownership is not checked), target `PRODUCTS`, no variant-level selections and no `Unknown` condition: managed as above.
- Anything else (several sources, a sub-collection source, a shareable source, `VARIANTS` target, variant selections, a condition Shopify reports as `CollectionSourceInclusionConditionUnknown`): membership is unsupported. If config sets any of the three attributes, Read and plan fail with an error naming the source types; otherwise they stay null and import and list warn that membership is not managed.
- Not managed in v1: exclusions (conditions and products), sub-collection and shareable sources (`collectionConditionsSource*` mutations), variant selections, the product order of `MANUAL` collections (`collectionReorderProducts`), collection image (with §9k.4), metafield values, translations. All roadmap.

**Conditions: a JSON attribute, like checkout branding (§9f).** 18 inclusion condition kinds, each with its own relation enum and value shape (strings, `MoneyInput`, `WeightInput`, booleans, metafield definition IDs, taxonomy categories with `includeDescendants`, metaobject IDs). A typed block would need one nested block per kind or a flattened block with kind-specific fields; JSON in the input's own shape keeps Shopify's docs usable one to one, and users write `jsonencode([...])`.
- Write: sent as is (`# @genqlient(for: "CollectionCreateSourceInclusionInput.conditions", bind: "encoding/json.RawMessage")` and the same for `CollectionUpdateSourceInclusionInput.conditionsToCreate`). Shopify validates kinds and values. ValidateConfig only checks that the value is a JSON list of objects with exactly one key each.
- Read: map each output condition back to its input shape (pure logic, unit tests first): `__typename` `CollectionSourceInclusionCondition<Kind>` → key `<kind>` in lowerCamelCase (`ProductTag` → `productTag`, `MetafieldInt` → `metafieldInt`); `definition { id }` → `definitionId`; `value: Metaobject` / `values: [Metaobject]` → IDs; `values: [{ category { id } includeDescendants }]` → `[{ categoryId, includeDescendants }]`; `MoneyV2 { amount currencyCode }` and `Weight { value unit }` keep their keys; `id` is dropped. `MetafieldInteger` is deprecated in favour of `MetafieldInt` and not requested.
- Equality: a custom string type with semantic equality, order-sensitive for the list (Read returned conditions in creation order, checked), numbers compared numerically (`"5"` reads back as `"5.0"`, checked), everything else exactly. Import and list use the mapped live list.
- Update: when `conditions` or `match_type` change, one `collectionUpdate` call deletes every live condition and creates the planned list: `sourcesToUpdate: [{ condition: { id: source_id, inclusion: { matchType, conditionsToDelete: [<live ids>], conditionsToCreate: [<planned>] } } }]`. Checked: one call replaced a condition, changed `matchType` and swapped a selection at once, with no userErrors. `conditionsToUpdate` (per-condition patch by ID) is not used, so condition IDs are never stored.

**`product_ids`:** Update sends the difference as `selectionsToAdd` / `selectionsToRemove` in the same `sourcesToUpdate` call (checked). Read pages `selections(first: 250)`. With no source yet (an empty collection), the first write creates one with `sourcesToCreate: [{ source: { title: "Untitled Source", inclusion } }]`, the title Shopify gives migrated collections. Create sends the same in `sources`. Products deleted elsewhere drop out of the selections, which shows as drift; config that references `shopify_product.<x>.id` avoids it through dependency order.
- The collection owns membership. `shopify_product` never sends `collections` / `collectionsToJoin` / `collectionsToLeave` (§9k.1), so the two can't fight.
- Membership by conditions is computed asynchronously: `productsCount` was 0 right after the create and correct after the next call (checked), and `CollectionUpdatePayload` has a `job` (it was `null` in every probe). The provider never reads the matched products, only `conditions` and `selections`, which the payload returns at once (checked), so it doesn't wait for the job. `TODO(verify)`: whether selections on large collections ever come back only after the job.

Import key: the handle. Read: `collection(id:)` first; if it is gone, `collectionByIdentifier(identifier: { handle })`; `MutableIdentity` is set. `collectionByHandle` is deprecated and not used. A taken handle fails on create ("Handle has already been taken", plain `UserError` with `field = ["collection"]`, checked; no `-1` suffix), mapped to `handle` by message since `field` names only the input.

**Pattern A: `adopt_existing` only.** Every store has `frontpage` ("Home page", created with the shop; on the dev store one empty Shopify-made source (app `null`), `MOST_RELEVANT`). It is an ordinary collection as far as the schema shows (no `isDefault`), so it needs `adopt_existing = true` like any taken handle: Create looks the handle up and updates it, destroy only removes it from state with a warning. `TODO(verify)`: whether `frontpage` can be deleted; not tried, since it couldn't be restored with its GID. On the dev store, Shopify put two of three new draft products into `frontpage` by itself (§9k.1), so a `frontpage` resource with `product_ids` set would see drift.

**No Pattern B.** `collectionDelete` removes the collection, its sources, publications, translations and metafield values; the products stay (schema). Nothing outside the collection is destroyed, and a recreate from config restores it with a new GID. Menu items that point at it (`shopify_menu`, type `COLLECTION`) lose their target (`TODO(verify)`: whether Shopify removes such items or leaves them dangling); dependency order handles it when the menu references `shopify_collection.<x>.id`. The docs recommend `lifecycle { prevent_destroy = true }` for collections that back navigation. "Collection does not exist" on delete (`field = ["id"]`, checked) means already gone: removed from state without error.

Notes:
- Uses `collection(id:)`, `collectionByIdentifier`, `collections(first:)`, `collectionCreate(collection:)`, `collectionUpdate(collection:)`, `collectionDelete(input:)`. `collectionPublish`/`collectionUnpublish`, `collectionAddProducts(V2)`, `collectionRemoveProducts` and `ruleSet` are deprecated and not used; publishing is §9k.3.
- `redirectNewHandle` is not sent (no URL redirects; roadmap type).
- Conditions referencing metafield definitions or metaobjects carry store-specific GIDs; users reference `shopify_metafield_definition.<x>.id` or the data sources (§9g).
- Errors: plain `UserError` from all three mutations, mapped by `field` where it names an attribute (`["collection","title"]` → `title`), otherwise to the resource with Shopify's message. Condition errors are reported as returned (no codes exist to map). None is retried.
- **Fit: volatile.** The description says so; the rules of a smart collection are the part that suits Terraform best.
- **Scopes:** `read_products` / `write_products` (collections belong to the products scope, `docs/bootstrapping.md`). `TODO(verify)`: confirmed only with every scope granted.
- **Review Focus 1:** after destroy of an adopted collection, read it back through the client and expect it unchanged. **Review Focus 2:** set `sort_order` in one step, leave it out in the next, change it through the client in between: the config without `sort_order` plans empty; likewise `product_ids` unset while the client adds a product.
- **Testing:** handles `tf-acc-collection-<random>`; one product made by the test (`shopify_product`, so §9k.1 lands first). Steps: manual (`product_ids` add and remove), smart (`conditions` change, `match_type` change), a collection that starts empty and gains a source, import by handle and identity. Unit tests first: condition output → input mapping for every kind in the schema, semantic equality, the source support check, selection diff.

**List:** no config. `collections(first: 100)`, paged; requested cost with sources and `selections(first: 250)` was 380 for 100 collections, and 434 with `resourcePublicationsV2(first: 10)` per collection (§9k.3; checked on the dev store, 2026-10-26), so pages stay at 100. `DisplayName`: `<title> (<handle>)`. Results carry `handle`, `title`, the non-null Optional + Computed attributes, and `match_type` / `conditions` / `product_ids` when the source is supported; generated config then holds product GIDs, which tie it to the store (the docs say to replace them with references). App-owned collections aren't distinguishable in the schema; all collections are listed.

**Dev store (2026-10-26):** probes created four collections (`tf-probe-legacy-smart` and `tf-probe-legacy-manual` with API 2026-07, `tf-probe-new` and `tf-probe-empty` with 2026-10) and deleted all four. `frontpage` gained an empty source twice through the product probes; both were removed with `sourcesToDelete`, but by the time of task 15 it had one empty Shopify-made source again (see below), 0 products, `MOST_RELEVANT`.

**Implementation findings (task 15, dev store, 2026-10-26):**
- A source created through the 2026-10 `sources` input carries the creating app (`app: { id: "gid://shopify/App/…" }`, the provider's write app), while 2026-07-migrated sources and `frontpage`'s read `app: null`. Resolved by S13-8: the app is not checked. `TODO(verify)`: whether Shopify rejects writes to a source another app created; such a rejection surfaces as a userError.
- A conditions source can't be empty: create without conditions and selections fails with "A condition based source must have at least one product selection or condition" (`field = ["collection","sources","0","source","inclusion"]`), and so does an update deleting the last condition without adding a selection. So when the planned conditions and products are both empty, the provider deletes the source (`sourcesToDelete`), and creates none. An empty source Shopify made itself (`frontpage`, or one whose products were all deleted) is left alone.
- `matchType` is required on create when conditions are given ("Match type is required when conditions are provided"); ValidateConfig requires `match_type` with a non-empty `conditions`. `matchType` persists after all conditions are removed (an update deleting the condition while adding a selection kept `ALL`, and `matchType: ANY` could be set on a source without conditions), so `match_type` is read as returned, not forced to null.
- Handles are normalised like products (`TF-Probe Upper A` → `tf-probe-upper-a`) and HTML is repaired like products (`<p>Hi<br>there<b>bold</p>` → `…<b>bold</b></p>`): both `TODO(verify)` above are confirmed, and the product validator and keep-or-take rule are shared (`internal/patterns`).
- Deleting a product leaves the collection's source in place with no selections (and `matchType` unchanged).
- S13-9: the provider deletes a source only when it would otherwise become empty, never one with exclusions (they are read only to check this; remove them in the admin first) or one Shopify made (app `null`, can't be recreated); both are refused with an error. Exclusion-only sources exist (checked on the dev store, 2026-10-27): `collectionCreate` with a source that has only `exclusion` (`productTag TAGGED_WITH tfprobe`) succeeded, read back with `inclusion { conditions: [], matchType: null, selections: [] }`, and matched no product (`productsCount` 0 with an untagged and a tagged product on the store, polled for 12 s); `collectionUpdate` deleting the last inclusion condition of a source that also had an exclusion was accepted (no userError, `job: null`), kept the exclusion and `matchType: ALL`, and the collection went from 1 product to 0. So such a source includes nothing: Read treats it as empty (no conditions, no products), and a config with both empty leaves it alone. The S13-9 refusal stays: it is stricter than Shopify needs (emptying the inclusion would keep the exclusions), but it never loses them.
- Output conditions can't be selected with plain field names: every kind has its own `relation` enum and differently typed `value`/`values`, which GraphQL's field-merging rules reject. The query aliases them as `<field>__<Kind>` and the mapping strips the suffix.
- `frontpage` on the dev store had one empty Shopify source (`CollectionConditionsSource/52052885835`, app `null`, `matchType` null) before this task's tests, not `sources: []`; the tests left it like that.

---

## 9m. Resource: `shopify_page`

Discovery: 8 pages on staging, 9 on production, merchant-owned (D2: build). Online store pages are content (imprint, about us, landing pages), edited in the admin by people outside the infrastructure team, so the type is volatile.

```hcl
resource "shopify_page" "impressum" {
  handle          = "impressum"
  title           = "Impressum"
  body            = file("${path.module}/pages/impressum.html") # optional, unset = unmanaged
  published       = true                                       # optional, unset = unmanaged (created as true)
  template_suffix = "legal"                                    # optional, unset = unmanaged
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `id` | string | `Page` GID. Computed, UseStateForUnknown |
| `handle` | string | Required, import key. Updatable in place. Same validator as `shopify_product.handle` (§9k.1): Shopify normalises pages the same way (`TF Probe_1` became `tf-probe_1`, checked) |
| `title` | string | Required. `title: ""` fails with `BLANK` (checked) |
| `body` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. HTML. Same keep-or-take rule as `shopify_product.description_html` (§9k.1): Shopify repairs page bodies the same way (`<p>Hi<br>there<b>bold</p>` came back as `<p>Hi<br>there<b>bold</b></p>`, trailing whitespace kept, checked) |
| `published` | bool | Optional + Computed, UseStateForUnknown; unset = unmanaged. `isPublished`. Create without it gives `true` (checked) |
| `template_suffix` | string | Optional + Computed, UseStateForUnknownIncludingNull; unset = unmanaged. `""` and `null` both mean the default template, but they read back as given (`""` stays `""`, never set reads `null`; checked), so no normalisation is needed |
| `adopt_existing` | bool | Pattern A, see below |

Import key: the handle (`impressum`). Handles are unique per shop: a taken handle fails with `TAKEN`, `field = ["page","handle"]`, on create and on update (checked; no `-1` suffix, unlike blogs and menus). Read: `page(id:)` first. If it is gone, `pages(first: 5, query: "handle:<handle>")` and an exact handle match; there is no `pageByHandle` or `pageByIdentifier` root field, so the fallback is search-indexed (a new page was found right away in the probe; see the lag measurement under Implemented). `MutableIdentity` is set.

**Publishing: `published` only, no `publishDate`.** `Page` is not `Publishable`: there is no `publication_ids` as on products (§9k.3), and pages are visible on the online store only. `isPublished` is the switch. `publishDate` (scheduled publishing) is not managed and never sent: it is an admin workflow, and it fights with `isPublished` (`isPublished: true` with a future `publishDate` fails with `INVALID_PUBLISH_DATE`, checked). A scheduled page reads as `published = false` (checked: a future `publishDate` gave `isPublished = false` and `publishedAt` = that date), so a config with `published = true` publishes it at the next apply. `publishedAt` is not exposed.

**Pattern A: `adopt_existing` only.** Every store has a `contact` page (`templateSuffix = "contact"`, created with the shop; on the dev store the only page). The schema doesn't mark it (no `isDefault`), so it is handled like `frontpage` (§9l): `adopt_existing = true` updates it in place, destroy only removes it from state with a warning. Without the flag, Create fails on the taken handle. `TODO(verify)`: whether `contact` can be deleted; not tried, it couldn't be restored with its GID.

**No Pattern B.** `pageDelete` removes the page, its metafield values and translations. The body and settings come from config, so a recreate restores the page with a new GID; unmanaged metafield values and translations are lost, as for collections (§9l). Menu items of type `PAGE` lose their target (`TODO(verify)`, same open point as collections). `NOT_FOUND` on delete ("Page does not exist", checked) means already gone: removed from state without error.

Notes:
- Uses `page(id:)`, `pages(first:, query:)`, `pageCreate(page:)`, `pageUpdate(id:, page:)`, `pageDelete(id:)`.
- Update sends only changed attributes. Never sent: `metafields` (metafield values are a roadmap type), `publishDate`, `redirectNewHandle` (a handle change creates no redirect; URL redirects are a roadmap type). Renaming the handle in place works (checked).
- genqlient: `body` has the `HTML` scalar, already bound to `string`.
- Errors (`PageCreateUserError` / `PageUpdateUserError`, `field` relative to `page`): `TAKEN` (`handle`), `BLANK` (`title`), `TOO_LONG`, `TOO_SHORT`, `TOO_BIG` (`body`), `INVALID`, `INVALID_PUBLISH_DATE` (can't happen, `publishDate` is never sent), `INVALID_VALUE` / `INVALID_TYPE` (metafields, never sent), `NOT_FOUND`. `pageDelete`: `NOT_FOUND`. None is retried.
- **Fit: volatile.** The description says so: page texts are usually maintained in the admin; with `body` unset, Terraform can own the handle, title and template while editors own the text.
- **Scopes:** `read_content` / `write_content` (`docs/bootstrapping.md`). `TODO(verify)`: checked only with every scope granted.
- **Review Focus 1:** after destroy of the adopted page, read it back through the client and expect it unchanged. **Review Focus 2:** set `template_suffix` in one step, leave it out in the next, change it through the client in between: the config without it plans empty; likewise `body` unset while the client edits the body.
- **Testing:** handles `tf-acc-page-<random>`. Steps: create, update `title`/`body`/`published`, handle rename, import by handle and by identity, adopt (`adopt_existing` on a page made through the client). Unit tests first: the handle validator (shared with products), the body keep-or-take rule (shared helper with §9k.1).

**List:** no config. `pages(first: 250)`, paged; requested cost with all attributes was low (149 for pages, blogs and segments together at 250 each, checked). `DisplayName`: `<title> (<handle>)`. Results carry `handle`, `title`, `body`, `published` and `template_suffix` when not null; they pass the validators (Shopify-normalised handles) and plan as import-only.

**Dev store (2026-10-26):** one probe page (`tf-probe_1`, renamed to `tf-probe-renamed`) was created, updated (publish toggles, template suffix, a future `publishDate`) and deleted. End state equals the start: only `contact`.

**Implemented (§9m, verified on the dev store, 2026-10-26):**
- Everything above held in the acceptance tests: create without `published` gives `true`; the body repair is no diff (`<p>Hi<br>there<b>bold</p>` stays configured, `...</b></p>` lives); `template_suffix` `""` reads back as `""`; a handle rename keeps the GID; import by handle, by identity and from generated config; the list shows `body`, `published` and `template_suffix` (null stays null).
- `template_suffix` uses `UseStateForUnknownIncludingNull`, not plain `UseStateForUnknown`: a never-set suffix reads `null`, and plain keeps the plan unknown on every run.
- The handle fallback sends `pages(first: 5, query: "handle:\"<handle>\"")` (quoted) and compares handles exactly; the quoted form found new pages immediately. **Index lag (dev store, 2026-10-27):** five pages created through `pageCreate` and looked up with that query immediately after the mutation returned were each found by the first query (0.42–1.25 s after the create returned, round trip included), so no lag was measurable, unlike segments (§9q, 2 to 4 s) and files (§9o). A lag under load would only degrade the message: the lookup misses, `pageCreate` fails with `TAKEN` on `handle`, and nothing is created twice.
- Update sends only attributes that are configured and differ from state; a plan that changes only `adopt_existing` makes no API call. Adopt does the same against the live page, so a page that already matches is not written at all: adopting `contact` with its live title left it byte-identical (`TestAccPage_adoptContact`, Review Focus 1).
- Review Focus 2 passed: `template_suffix`, `published` and `body` changed through the client while unset in config plan empty, and state takes the live values.
- `pageDelete` is not preceded by a lookup: `NOT_FOUND` is recognised by its `code` and tolerated (`goneOnDelete`, unit-tested only: the refresh before destroy already drops a page deleted outside, which the acceptance test covers). A page deleted outside Terraform is planned for recreation.
- Not verified, still `TODO(verify)`: whether `contact` can be deleted (not tried), menu items of type `PAGE` losing their target. Index lag under production load can't be measured on the dev store; the path is safe either way (above).
- End state of the dev store equals the start: only `contact`.

---

## 9n. Resource: `shopify_blog`

Discovery: 1 blog on staging and on production (D2: build); articles 0 / 0 (roadmap, D6). A blog is a container: handle, title, comment policy and template. Its articles are content and stay out of this resource.

```hcl
resource "shopify_blog" "news" {
  handle          = "news"
  title           = "Ratgeber"
  comment_policy  = "MODERATED" # optional, unset = unmanaged (created as CLOSED)
  template_suffix = "ratgeber"  # optional, unset = unmanaged
  adopt_existing  = true        # every store has "news"

  allow_data_deletion = false
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `id` | string | `Blog` GID. Computed, UseStateForUnknown |
| `handle` | string | Required, import key. Updatable in place. Same validator as `shopify_product.handle` (`TF Probe_Blog` became `tf-probe_blog`, checked) |
| `title` | string | Required |
| `comment_policy` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. `CommentPolicy`: `AUTO_PUBLISHED`, `CLOSED`, `MODERATED`. Create without it gives `CLOSED` (checked); in-place update checked |
| `template_suffix` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. `""` reads back as `""`, never set as `null` (checked), as for pages |
| `adopt_existing` | bool | Pattern A, see below |
| `allow_data_deletion` | bool | Pattern B, see below |

Import key: the handle (`news`). Read: `blog(id:)` first; if it is gone, `blogs(first: 25, query: "handle:\"<handle>\"")` with an exact handle match (no `blogByHandle` root field, so search-indexed; lag measured under Implemented). `MutableIdentity` is set.

**Shopify suffixes a taken handle instead of failing**, on create and on update: `blogCreate` with `handle: "news"` returned a new blog `news-1`, and `blogUpdate` to `news` renamed the blog to `news-1` (both checked, no userError). So, as for menus (§9):
- Create looks the handle up first (fails unless `adopt_existing`), then checks the returned handle; a suffixed handle (another writer won the race) means the new blog is deleted again and Create fails, naming the handle.
- Update with a new handle looks it up first and fails if it is taken. If Shopify still returns a different handle, the update fails with an error naming both, and state takes the returned handle, so the next plan shows the drift.

**Pattern A: `adopt_existing` only.** Every store has a `news` blog ("News", `CLOSED`, created with the shop; the dev store's only blog, and very likely the one blog on staging and production). The schema doesn't mark it as a default, so it is handled like `frontpage` (§9l) and `contact` (§9m): `adopt_existing = true` updates it in place, and destroy only removes it from state with a warning. `TODO(verify)`: whether `news` can be deleted; not tried.

**Pattern B: deleting a blog deletes its articles.** `blogDelete` on a blog with one article removed the article too (`article(id:)` returned `null` afterwards, checked); the schema only says it "removes the blog container". So Delete reads `articlesCount` first and, if it is above 0 or missing (`null`, fail closed: the blog may hold articles), fails unless `allow_data_deletion = true`, naming the count when known. An empty blog is deleted without the guard (it holds only config). No attribute forces a replace, so there is no plan-time replace warning. `NOT_FOUND` on delete ("Blog does not exist", checked) means already gone: removed from state without error.

Notes:
- Uses `blog(id:)`, `blogs(first:, query:)`, `blogCreate(blog:)`, `blogUpdate(id:, blog:)`, `blogDelete(id:)`. Update sends only changed attributes.
- Never sent: `metafields`, `redirectNewHandle`, `redirectArticles` (both default `false`; URL redirects are a roadmap type). Renaming a blog therefore changes its article URLs without redirects; the docs say so.
- Not exposed: `feed` (legacy FeedBurner settings, `null` on the dev store), `tags` (derived from articles), `articlesCount` (only used by the Pattern B check).
- Errors (`BlogCreateUserError` / `BlogUpdateUserError`, `field` relative to `blog`): `BLANK` (`title`), `INVALID`, `TOO_LONG`, `INCLUSION` (`comment_policy`), `INVALID_VALUE` / `INVALID_TYPE` (metafields, never sent), `NOT_FOUND`. `blogDelete`: `NOT_FOUND`. There is no `TAKEN`: see the handle check above. None is retried.
- **Fit: good.** A blog's settings are stable configuration; articles, the content, are not managed (roadmap). The description says so, and that destroying a blog with articles needs `allow_data_deletion`.
- **Scopes:** `read_content` / `write_content`. `TODO(verify)`: checked only with every scope granted.
- **Review Focus 1:** after destroy of the adopted `news` blog, read it back through the client and expect it unchanged. **Review Focus 2:** set `comment_policy` in one step, leave it out in the next, change it through the client in between: the config without it plans empty.
- **Testing:** handles `tf-acc-blog-<random>`. Steps: create, update, handle rename, import by handle and identity, adopt `news` (destroy leaves it), a taken handle fails without creating `-1`. Pattern B: create an article through the client (`articleCreate`), destroy fails without `allow_data_deletion`, succeeds with it, and the article is gone. Unit tests first: the suffixed-handle check.

**List:** no config. `blogs(first: 250)`, paged. `DisplayName`: `<title> (<handle>)`. Results carry `handle`, `title`, `comment_policy` and `template_suffix` when not null, and plan as import-only.

**Dev store (2026-10-26):** probes created blogs `tf-probe_blog` and `news-1` (the suffix probe) and one article; `news-1` was deleted, the probe blog was renamed (`news-1`, then `tf-probe-blog-2`) and deleted with its article. End state equals the start: only `news`, 0 articles.

**Implemented (§9n, verified on the dev store, 2026-10-26):**
- Everything above held in the acceptance tests: create without `comment_policy` gives `CLOSED`; `template_suffix` `""` reads back as `""` and a never-set one as `null` (so it uses `UseStateForUnknownIncludingNull`, as for pages); a handle rename keeps the GID; import by handle, by identity and from generated config; the list shows `comment_policy` and `template_suffix`. No attribute deviates from the table.
- Review Focus 1: adopting `news` and destroying leaves it byte-identical (id, handle, title, comment policy, suffix). Review Focus 2: `comment_policy` set, then left out and changed through the client: the config without it plans empty and state takes the client's value.
- Suffixed handles: a taken handle on create is refused by the lookup before `blogCreate`, on update before `blogUpdate`; neither test left a `-1` blog. The post-create check (delete the suffixed copy, then fail) and the post-update check (state takes the returned handle) only run when Shopify wins a race, so they are not reached by the tests; `handleDrift` is unit-tested.
- Pattern B: `articlesCount` reflected an article created seconds earlier, so the guard fires on the first destroy (`holds 1 articles`, plus the `allow_data_deletion` hint); with `allow_data_deletion = true` blog and article are gone. An empty blog is deleted without the flag (every other test's destroy).
- `NOT_FOUND` on delete is handled by `goneOnDelete` (unit-tested), not by an acceptance test: Terraform refreshes before destroy, so Read already drops a blog deleted outside. The `deletedOutside` test covers that Read path and the recreate.
- The handle fallback sends `blogs(first: 25, query: "handle:\"<handle>\"")` and compares handles exactly; the quoted form found new blogs immediately. **Index lag (dev store, 2026-10-27):** five blogs created through `blogCreate` were each found by the first lookup sent immediately after the mutation returned (0.42–0.43 s after it returned, round trip included): no measurable lag. Under load a missed lookup would let `blogCreate` suffix the handle; the post-create check then deletes the copy and fails, so nothing is left behind. `TODO(verify)` on deleting `news` stays untried.
- The test-only `articleCreate` / `article(id:)` operations live in `blog.graphql`; the provider never calls them.

---

## 9o. Resource: `shopify_file`

Discovery: 269 files on staging, 432 on production, merchant-owned (D2: build). Files are the store's media library: product images, theme and page assets, downloads. They are content, uploaded and replaced in the admin all the time, so the type is volatile. Product media (§9k.4) references files of this type by ID.

```hcl
resource "shopify_file" "logo" {
  filename     = "logo.png"
  content_type = "IMAGE"
  source       = "${path.module}/assets/logo.png" # local file (staged upload) or https URL
  source_hash  = filesha256("${path.module}/assets/logo.png")
  alt          = "pflege.de Logo"                 # optional, unset = unmanaged

  allow_data_deletion = false
}

resource "shopify_file" "howto" {
  content_type = "EXTERNAL_VIDEO"
  source       = "https://www.youtube.com/watch?v=..."
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `id` | string | File GID; the kind follows the content type: `MediaImage`, `GenericFile`, `Video`, `ExternalVideo`, `Model3d`. Computed, UseStateForUnknown |
| `content_type` | string | Required, **RequiresReplace** (a file can't change kind). `FileContentType`: `IMAGE`, `FILE`, `VIDEO`, `EXTERNAL_VIDEO`, `MODEL_3D`. Read maps `__typename` back. Required because the duplicate check needs it (below) |
| `filename` | string | Optional in the schema; ValidateConfig requires it for every type except `EXTERNAL_VIDEO`, where it must be unset (external videos have no filename). Import key. Updatable in place for `IMAGE` and `FILE` (`fileUpdate`, same GID, checked); `VIDEO` and `MODEL_3D` can't be renamed (`UNSUPPORTED_MEDIA_TYPE_FOR_FILENAME_UPDATE`, schema), which is a plan error. The CDN URL changes with it (`…/files/tf-probe.png` → `…/files/tf-probe-renamed.png`, checked), so ModifyPlan warns that links to the old URL in pages and themes break. A different **extension forces replace**: `fileUpdate` rejects it with `INVALID_FILENAME_EXTENSION` (checked). Validated: ASCII letters, digits, `.`, `_`, `-`, and an extension, since Shopify rewrites anything else (`TF Probe ä.png` became `TF_Probe_a.png`, checked) |
| `source` | string | Optional, but Create fails without it (ModifyPlan, on create only). A local file path (uploaded through a staged upload) or an `http(s)://` URL (Shopify fetches it). See "Sources". Not readable for hosted files: Read keeps the state value, import leaves it null (unmanaged). For `EXTERNAL_VIDEO` it is the video URL and is read back (`originUrl`) |
| `source_hash` | string | Optional. Not interpreted; any change re-uploads the content, like a `source` change. Meant for `filesha256(<path>)`, since a changed file under the same path or URL is otherwise invisible to Terraform (the `aws_s3_object.source_hash` pattern) |
| `alt` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. `""` clears it (checked) |
| `url` | string | Computed. The public URL: `image.url` (`MediaImage`), `url` (`GenericFile`), `sources[0].url` (`Video`: Shopify's SD 480p MP4 rendition; `Model3d`: the uploaded file; both checked, see Implemented), `originUrl` (`ExternalVideo`). `originalSource.url` is never requested: it is a signed storage URL that expires after 300 s and differs on every read (checked for `MediaImage`), so it would plan drift forever. UseStateForUnknown, except ModifyPlan marks it unknown when `filename`, `source` or `source_hash` change (a content update changes the `?v=` query, checked) |
| `adopt_existing` | bool | Pattern A, see below |
| `allow_data_deletion` | bool | Pattern B, see below |
| `timeouts` | block | `create`, `update` (durations, default `10m`). How long Create and Update wait for processing. Hand-rolled block (two strings parsed with `time.ParseDuration`): the `terraform-plugin-framework-timeouts` module would be a new dependency |

**Sources: staged uploads for local files, `originalSource` URLs otherwise.** The brief asked for `originalSource` URLs only if they cover every file type. They don't: `FileCreateInput.originalSource` takes "an external URL … for images, generic files, or external videos. Videos and 3D models require a staged upload URL" (schema). Staged uploads cover every hosted type (`StagedUploadTargetGenerateUploadResource` has `IMAGE`, `FILE`, `VIDEO`, `MODEL_3D`), and a file kept in the repository is the usual Terraform workflow (`file()`, `filesha256()`), so they are the main path. A URL source stays because external videos (YouTube, Vimeo) have nothing to upload, and for images and generic files it is the same call minus the upload. The provider tells the two apart by the `http://` / `https://` prefix.
- **Local path:** `stagedUploadsCreate(input: [{ resource: <content_type>, filename, mimeType, fileSize, httpMethod: PUT }])` → PUT the bytes to the returned `url` with the returned `parameters` as headers → `fileCreate(originalSource: <resourceUrl>)`. `mimeType` from the extension (`mime.TypeByExtension`); `fileSize` is required for videos and 3D models. Plain `net/http`, no new dependency. `TODO(verify)` on the dev store at implementation: the PUT flow, and whether `fileUpdate(originalSource: <resourceUrl>)` replaces the content as a URL does. Not probed here: no upload outside the brief's tiny public image.
- **URL:** sent as `originalSource` as is. ValidateConfig rejects a URL for `VIDEO` and `MODEL_3D`, and a local path for `EXTERNAL_VIDEO`, with the schema's reason.
- A file at a URL that changes later is not noticed unless `source_hash` changes (the URL itself isn't fetched at plan time).

**Create, processing and the filename key.**
- Create sends `filename`, `contentType`, `alt` (when set) and `duplicateResolutionMode: RAISE_ERROR`. The default, `APPEND_UUID`, would silently create `logo_<uuid>.png` and break the key. A taken filename then fails at once with `FILENAME_ALREADY_EXISTS`, `field = ["files","0","filename"]` (checked), mapped to `filename`. `RAISE_ERROR` without `contentType` fails with `INVALID_DUPLICATE_MODE_FOR_TYPE` ("not supported for 'MISSING' media type", checked), which is why `content_type` is required. External videos get neither `filename` nor `duplicateResolutionMode` (`TODO(verify)`: whether `RAISE_ERROR` is rejected for them).
- **Processing is asynchronous.** `fileCreate` returns `fileStatus: UPLOADED` with `image: null`; the 1×1 PNG was `READY` with its URL about a second later (checked). Create and Update (after a content change) poll `node(id:)` every 2 s until `READY` or `FAILED`, within the `timeouts` value. `FAILED`: the file is deleted again (`fileDelete`, otherwise the next apply hits `FILENAME_ALREADY_EXISTS`) and Create fails with each `fileErrors` entry (`code`, `message`, `details`). Timeout: the file stays, state gets its ID (so the next plan sees it), and Create fails saying it is still processing.
- `adopt_existing = true`: Create looks the filename up (below) and, if found, updates it to the config (`alt`, and the content when `source` is set) instead of creating.

Import key: the filename (`logo.png`); for an external video, its URL (contains `://`, so it can't be a filename). Read: `node(id:)` first, bound to `json.RawMessage` with `... on MediaImage`, `... on GenericFile`, `... on Video`, `... on Model3d`, `... on ExternalVideo` fragments, like web presences (§9a); there is no `file(id:)` root field. If it is gone: `files(first: 10, query: "filename:<filename>")`, then an exact match on the derived filename. `MutableIdentity` is set.
- `MediaImage` and `GenericFile` have no `filename` field; it is the last path segment of `image.url` / `url` without the query (`…/files/tf-probe.png?v=…` → `tf-probe.png`, checked for `MediaImage`; `TODO(verify)` for `GenericFile`). `Video` and `Model3d` have `filename`.
- The search is token-based (`filename:tf-probe` and `filename:tf-probe.png` both found `tf-probe.png`) and search-indexed: right after `fileCreate` it returned nothing, a few seconds later the file (checked). Import and adopt of a file made moments ago can therefore miss it; the error says so.
- External videos: `files(query: "media_type:EXTERNAL_VIDEO")`, paged, exact `originUrl` match (`TODO(verify)` the filter value).
- **Duplicates:** with `RAISE_ERROR` Shopify keeps filenames unique, and `APPEND_UUID` uploads (the admin's default) get unique names too. More than one exact match (possible for external videos with the same URL; `TODO(verify)`: whether case-only differences count as distinct filenames, and whether production has duplicates) is an error listing the GIDs, never a pick.

**Update:** `fileUpdate(files: [{ id, … }])` with only the changed fields: `alt`, `filename` (images and generic files only), and `originalSource` when `source` or `source_hash` changed. Replacing the content works in place for `IMAGE` and `FILE` only ("the source from which to update a media image or generic file", schema): same GID, same URL path, new `?v=` (checked for an image from a URL). For `VIDEO`, `MODEL_3D` and `EXTERNAL_VIDEO` a content change forces replace. Adding `source` to an imported file (state `null` → a value) counts as a content change: it re-uploads in place for `IMAGE` and `FILE` and is a guarded replace for the other kinds; the docs say so. Never sent by `shopify_file`: `referencesToAdd` / `referencesToRemove` (only product IDs; the product owns that link, §9k.4, one writer: `shopify_product` sends `referencesToRemove` itself to detach media without deleting them) and `previewImageSource` (roadmap).

**Pattern A: `adopt_existing` only.** Shopify creates no default files (dev store: 0 files).

**Pattern B: deleting a file destroys data and breaks references.** `fileDelete` is permanent; Shopify removes the file from every product that uses it, and links from themes, pages and blog posts break (schema). A recreate uploads the content again, but under a new GID and URL. So Delete, and every replace (`content_type`, a new extension, a content change of a video, 3D model or external video), fail unless `allow_data_deletion = true`, with no exception for unused files; ModifyPlan warns on a planned replace. `FILE_DOES_NOT_EXIST` on delete (checked) means already gone: removed from state without error.

**Product media (§9k.4).** `id` is the file's own GID (`gid://shopify/MediaImage/…`), which `FileSetInput.id` of `productSet(files:)` takes, so `media_ids = [shopify_file.logo.id]` works without a conversion. Deleting a product deletes media that no other product uses (§9k.1, checked), files of this type included, unless the product manages `media_ids`: then its destroy detaches them first (§9k.4). A deleted file plans a create. Removing a file from `productSet(files:)` would delete it unless another product uses it (checked), so `shopify_product` detaches removed media with `fileUpdate(referencesToRemove:)` first and the file stays (§9k.4, ruling T14d-2). Destroying `shopify_file` detaches it from every product. The resource description says that detaching keeps the file and that only a product with `media_ids` unset deletes it on destroy. Delete looks the file up before the `allow_data_deletion` guard, so destroying a file already gone (with its product, or in the admin) never fails on the guard.

Notes:
- Uses `node(id:)`, `files(first:, after:, query:)`, `fileCreate`, `fileUpdate`, `fileDelete`, `stagedUploadsCreate`. Deprecated `stagedUploadTargetGenerate(s)` are not used.
- Errors (`FilesUserError`, `field` like `["files","0","filename"]`): `FILENAME_ALREADY_EXISTS`, `INVALID_FILENAME`, `INVALID_FILENAME_EXTENSION`, `UNSUPPORTED_MEDIA_TYPE_FOR_FILENAME_UPDATE` (`filename`); `INVALID_IMAGE_SOURCE_URL`, `MISMATCHED_FILENAME_AND_ORIGINAL_SOURCE`, `UNACCEPTABLE_ASSET`, `UNACCEPTABLE_TRIAL_ASSET`, `UNACCEPTABLE_UNVERIFIED_TRIAL_ASSET` (`source`); `ALT_VALUE_LIMIT_EXCEEDED` (`alt`); `INVALID_DUPLICATE_MODE_FOR_TYPE`, `NON_IMAGE_MEDIA_PER_SHOP_LIMIT_EXCEEDED`, `FILE_LOCKED`, `NON_READY_STATE`, `MEDIA_CANNOT_BE_MODIFIED`, `INVALID` (resource level); `FILE_DOES_NOT_EXIST` (gone). Processing errors come later as `FileError` codes (`IMAGE_DOWNLOAD_FAILURE`, `GENERIC_FILE_DOWNLOAD_FAILURE`, `UNSUPPORTED_IMAGE_FILE_TYPE`, `FILE_STORAGE_LIMIT_EXCEEDED`, the `VIDEO_*` and `MODEL3D_*` codes, …), reported with `details`. None is retried; `FILE_LOCKED` / `NON_READY_STATE` ask the user to re-apply.
- **Fit: volatile.** The description says so: most stores upload files in the admin or through product media; managing a few assets kept in the repository (logos, downloads) fits, mirroring the whole library doesn't.
- **Scopes:** `read_files` / `write_files` (`docs/bootstrapping.md`). `TODO(verify)`: whether `files` also works with `read_themes` or `read_products` alone (Shopify's docs list alternatives); checked only with every scope granted.
- **Review Focus 1:** after destroy of an adopted file, read it back through the client and expect it unchanged. **Review Focus 2:** set `alt` in one step, leave it out in the next, change it through the client in between: the config without `alt` plans empty.
- **Testing:** filenames `tf-acc-file-<random>.png`. Steps: create from a URL (the 1×1 PNG at `upload.wikimedia.org`) and from a local `testdata/` PNG (13b-3b), alt change, rename (URL changes, GID stays), content change through `source_hash` (GID and URL path stay), extension change plans a replace and fails without `allow_data_deletion`, import by filename and identity, adopt, Pattern B. Unit tests first: the filename validator, filename-from-URL, import-key parsing (filename vs URL), source routing and the type/source checks, the poll loop (READY, FAILED with cleanup, timeout).

**List:** optional config `query`, passed to `files(query:)` in Shopify's search syntax (for example `media_type:IMAGE` or `filename:logo`), so a store with hundreds of files can be imported in parts. `files(first: 250)`, paged; requested cost 24 per page with every field the resource reads (checked), so production's 432 files take two calls. `DisplayName`: `<filename> (<content_type>)`, `<url> (EXTERNAL_VIDEO)` for external videos. Results carry `filename`, `content_type`, `alt` when not null, and `source` for external videos only; hosted files come without `source` (unmanaged), which passes validation and plans as import-only. Files that failed processing are listed too.

**Dev store (2026-10-26):** one probe image (`tf-probe.png` from the 1×1 PNG URL) was created, renamed twice, given a new alt, re-sourced from the same URL, and deleted; a duplicate create was refused. End state equals the start: 0 files.

**Task split:** 13b-3a the resource with URL sources (everything above except the local-path upload); 13b-3b staged uploads of local files. 13b-3a alone is usable for images, generic files and external videos; videos and 3D models need 13b-3b.


**Implemented (§9o 13b-3a, verified on the dev store, 2026-10-27):** URL sources only; staged uploads of local files (13b-3b) are roadmap.
- **Scope of 13b-3a:** `source` takes `http://` / `https://` only (ValidateConfig: a path fails with a roadmap note). `VIDEO` and `MODEL_3D` can't have `source` (staged upload needed), so they can't be created or get new content; they can be imported, adopted, listed, read and given `alt`. ModifyPlan fails a create without `source` and without `adopt_existing`, with the same roadmap note for them; Terraform plans the create half of a replace again with no prior state, so a replace that couldn't recreate the file fails at plan too, before anything is deleted. No `net/http` upload code, no `stagedUploadsCreate` query, no `testdata/` step.
- **Rename of VIDEO / MODEL_3D (ruling T18-1):** `fileUpdate` accepts a filename only for images and generic files (`UNSUPPORTED_MEDIA_TYPE_FOR_FILENAME_UPDATE`, schema), so a changed `filename` on a `VIDEO` / `MODEL_3D` is a plan error, not an in-place rename. Not run against a real video (the dev store has none).
- **Deviation, Update failure:** only Create deletes a `FAILED` file (`fileDelete`, then one error per `fileErrors` entry). A failed content update keeps the file, reports the errors, and leaves `source` / `source_hash` at their old state values so the next plan tries again. A failed processing on the dev store (404 source) reported `UNKNOWN: Media processing failed (… 404 …)`, not `IMAGE_DOWNLOAD_FAILURE`; the file was deleted and the library stayed empty.
- **Timeout:** with `create = "1ns"`, state keeps the file's ID, the apply fails saying it is still processing, and the next plan replaces the tainted resource; the error tells the user to run `terraform untaint <address>` first. Replace and destroy need `allow_data_deletion = true` in state.
- **Replace logic** is one function (`replaceReasons`) used by ModifyPlan for `RequiresReplace` and the warning: `content_type`, a different extension (also case-only: `fileUpdate` refuses `.png` to `.PNG`, checked), and a new `source` / `source_hash` for every type but `IMAGE` and `FILE`. Shopify refuses a filename whose extension differs from the source URL's (`MISMATCHED_FILENAME_AND_ORIGINAL_SOURCE`, checked with `.jpg` for a PNG source: "extension must match original source"), and refuses it only after a replace has deleted the old file. ValidateConfig therefore compares the two extensions (case-insensitively; a URL path without an extension is not checked) for every create and update, so a replace to a refused extension fails at plan, before anything is deleted. Changing the extension means changing `source` too; the acceptance test replaces with a case-only change (`.png` to `.PNG`), the one extension change a single PNG URL allows.
- **A deleted name stays taken for a moment.** Create then gets `<name>_<uuid>.png` although `duplicateResolutionMode: RAISE_ERROR` was sent, and no error: a replace right after the delete did it every time, and a 3 s wait avoided it. A file deleted seconds after its create, before the search index had it, kept the name even through 5 s, so the timeout test waits for the index first. Delete therefore waits until `node(id:)` is null, then 5 s; Create compares the READY filename with the planned one, deletes the stray file and fails with "Shopify renamed the file … apply again". Names that differ only in case count as taken in the same way (`Tf-Acc.png` then `tf-acc.png` gave a suffixed name); the case Shopify was given is kept, so uppercase is allowed and the key compare stays exact.
- **External videos:** `duplicateResolutionMode: RAISE_ERROR` is rejected for them ("not supported for 'EXTERNAL_VIDEO' media type"), so it is not sent; Create looks the URL up first and fails with "File already exists" unless `adopt_existing`. Shopify rewrites the URL (`watch?v=ID` becomes `https://youtu.be/ID`), so `source` keeps the configured form while the live URL equals what Shopify returned at the last write (private state, the `KeepOrTake` rule of the HTML attributes), and `url` is the rewritten one. The create-time duplicate check, adopt and import compare `originUrl` exactly, so they need the rewritten form (the example, the import line and the `source` description say so; two resources with the `watch?v=` form would still create two videos); Read's fallback uses `url`, which has it. A `source` that is unknown at plan time plans a replace for these types, because it can't be told from a change. `media_type:EXTERNAL_VIDEO` is a valid filter (checked).
- `alt` unset reads `""`, never `null`, and a cleared one reads back `""`; plain `UseStateForUnknown` as in the table. `GenericFile` filenames come from the URL like images (checked: an image replaced by a `FILE` of the same name). A content update through `source_hash` keeps the GID and the path and changes `?v=`; a rename keeps the GID and changes the URL (checked).
- **Search:** `files(query: "filename:\"<name>\"")` finds an exact filename, but only a few seconds after the create (the tests wait for it before an adopt, a list or a replace of an unindexed file). Files that failed or are still processing have no URL, so no filename: Read keeps the state's, and the list skips them, and also files whose filename the validator rejects (no extension, spaces, non-ASCII), so every listed config passes validation; such files can't be imported by filename. Skipped files don't count toward the list limit (the filter runs before pagination counts).
- Findings that held as written: one `fileCreate` takes the 1×1 PNG from `upload.wikimedia.org`; the exact URL is `https://upload.wikimedia.org/wikipedia/commons/c/ca/1x1.png` (the spec named only the host); `FILENAME_ALREADY_EXISTS` for a taken name (`Failed to create file`); adopt then destroy leaves the file with Terraform's `alt` and its original URL (Review Focus 1); `alt` unset plans empty after a change through the client (Review Focus 2); import by filename, by identity and from generated config; `query` narrows the list.
- **`sources[0].url` of videos and 3D models (checked on the dev store, 2026-10-27,** with files uploaded by a throwaway probe and deleted again): a 320×240 2 s MP4 was `READY` after 11 s with one source, `SD-480p-0.9Mbps` MP4 at 640×480 (`cdn.shopify.com/videos/c/vp/<id>/<id>.SD-480p-0.9Mbps-<n>.mp4`); a 1920×1080 3 s MP4 was `READY` after 14 s with four sources in this order: SD 480p MP4, HD 1080p MP4, HD 720p MP4, HLS `m3u8`. All were present at `READY` and unchanged over 6 reads in 75 s, and an unauthenticated GET of `sources[0].url` returned 200 `video/mp4`. So a video's `url` is the 480p rendition, not the original, and it is public and stable. A GLB model was `READY` after 11 s with two sources, the uploaded `.glb` first (`cdn.shopify.com/3d/models/o/<hash>/<filename>.glb`, `model/gltf-binary`, 400 bytes as uploaded) and a generated `.usdz`; both GETs returned 200 with those types; unchanged over 4 reads. `publicURL` stays as it is; the `url` description says what a video's URL is. Video and Model3d `filename` came back as uploaded.
- **Staged uploads of videos and 3D models (13b-3b, same probe):** `stagedUploadsCreate(httpMethod: PUT)` for `VIDEO` and `MODEL_3D` returns a Google Cloud Storage URL with the parameters `GoogleAccessId`, `key`, `policy`, `signature`; a PUT with them as headers failed (`VIDEO`: 400 `MissingSecurityHeader`, `MODEL_3D`: 403 `AccessDenied`), and `fileCreate` then gave `FAILED` (`VIDEO_VALIDATION_ERROR` / `MODEL3D_VALIDATION_ERROR`). With `httpMethod: POST`, a `multipart/form-data` POST with the parameters as form fields and the bytes as the last field `file` returned 204, and both files processed to `READY`. 13b-3b therefore needs the POST form flow for these two types, not the PUT flow described under Sources.
- Still unverified: whether `files` also works with `read_themes` or `read_products` alone (all scopes were granted).
- The timeouts block is `patterns.TimeoutsBlock` / `patterns.TimeoutOf` (durations by `time.ParseDuration`, default `10m`), reusable by the discounts. Dev store end state: 0 files, as at the start.
---

## 9p. Resources: `shopify_discount_basic`, `shopify_discount_free_shipping`, `shopify_discount_bxgy`

Discovery: `discountNodes` 0 on staging, 7 on production. D5 assumed Shopify defaults. **They are not:** Shopify creates no discounts with a shop (the dev store, created 2026-10-22, has none, and staging has none), so production's 7 were made by the merchant or an app. Their kinds are unknown (discovery counted nodes only; `TODO(verify)` at the first import). Discounts are promotions, created and ended in the admin by marketing, so the types are volatile.

```hcl
resource "shopify_discount_basic" "newsletter" {
  title = "Newsletter 10 %"
  code  = "WILLKOMMEN10" # unset = automatic discount

  starts_at = "2026-11-01T00:00:00+01:00" # optional, unset = unmanaged (created as now)
  ends_at   = "2026-12-31T23:59:59+01:00" # optional, unset = unmanaged
  context   = { customer_segment_ids = [shopify_customer_segment.subscribers.id] }

  customer_gets = {
    value = { percentage = 0.1 }
    items = { collection_ids = [shopify_collection.pflegehilfsmittel.id] }
  }
  minimum_requirement       = { subtotal = "30.00" }
  combines_with             = { shipping_discounts = true }
  usage_limit               = 1000
  applies_once_per_customer = true
}

resource "shopify_discount_free_shipping" "ab_50" {
  title               = "Versandkostenfrei ab 50 €"
  minimum_requirement = { subtotal = "50.00" }
  destination         = { country_codes = ["DE", "AT"] }
}
```

**Granularity: one resource per discount method (basic, free shipping, buy X get Y), each covering code and automatic discounts.** The API has eight node types (`Discount` union): {code, automatic} × {basic, BXGY, free shipping, app}, each with its own create and update mutation. Neither axis can change in place: a basic discount can't become BXGY, and a code discount can't become automatic. The method decides the shape of the core inputs (`customerGets` vs `customerBuys` + `customerGets` vs `destination` + `maximumShippingPrice`); code vs automatic only adds `code`, `usageLimit` and `appliesOncePerCustomer`. So:
- One resource per method keeps each schema to the attributes that apply, with no "exactly one of three blocks" switch and no per-kind cross-validation of a dozen optional attributes. Each method is one implementation task.
- Code vs automatic is the optional `code` attribute: set = code discount, unset = automatic. Switching between set and unset forces replace (RequiresReplaceIf on nullness); changing the code's value is in place.
- The three share one package (`internal/resources/discount`, as `privacysettings` hosts two resources) with the common attributes, code handling, context, item selection and time comparison.

**App discounts are not designed.** `DiscountCodeAppInput` and `DiscountAutomaticAppInput` take `functionHandle` only (2026-10 has no `functionId` input), and function handles are "scoped to your current app ID. Only finds functions within your app" (schema, on the delivery and payment customization inputs; `TODO(verify)` for discounts, where the description says only "the handle of the function providing the discount"). This provider's app ships no Functions (CLAUDE.md), so it can't create app discounts, and those of other apps belong to the Function's app (D3, Review Focus 5). The three list resources skip them, so generated config never contains one.

### Common attributes (all three)

| Attribute | Type | Behaviour |
|---|---|---|
| `id` | string | `DiscountCodeNode` or `DiscountAutomaticNode` GID (checked). Computed, UseStateForUnknown |
| `title` | string | Required. Unique among automatic discounts (`TAKEN`, "Title must be unique for automatic discount.", checked); code discounts may share titles with each other and with an automatic one (checked) |
| `code` | string | Optional; unset = automatic discount. **RequiresReplace** when it changes between set and unset. Unique per shop, case-insensitively (`tfprobe` after `TfProbe` failed with `TAKEN`, checked); Shopify keeps the case as given. Renaming in place keeps the redeem code's GID (checked) |
| `starts_at` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. RFC 3339. Required by the API on create for both kinds (`BLANK`, checked), so Create sends the current time when unset |
| `ends_at` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged (no end date on create) |
| `status` | string | Computed: `ACTIVE`, `SCHEDULED`, `EXPIRED`, from the dates; kept from state only in plans without a real change (see Implemented). Activate / deactivate mutations are not used: deactivating means setting `ends_at` |
| `context` | object | Optional + Computed, UseStateForUnknown; unset = unmanaged. Who can use it, exactly one of: `all` (bool, must be `true`), `customer_segment_ids` (set(string)), `market_ids` (set(string)). Code discounts require a context on create (`BLANK`, checked), so Create sends `all` when unset |
| `combines_with` | object | Optional + Computed, UseStateForUnknown; unset = unmanaged. `product_discounts`, `order_discounts`, `shipping_discounts` (bool). All `false` when created without it (checked) |
| `usage_limit` | number | Optional + Computed, UseStateForUnknown; unset = unmanaged. Code discounts only (ValidateConfig). Total uses; `null` = unlimited |
| `applies_once_per_customer` | bool | Optional + Computed, UseStateForUnknown; unset = unmanaged. Code discounts only |
| `adopt_existing` | bool | Pattern A, see below |

**Times:** Shopify returns UTC (`2026-10-26T12:00:00+01:00` read back as `2026-10-26T11:00:00Z`, checked), so `starts_at` and `ends_at` use a custom string type whose semantic equality compares the instant; the configured spelling stays in state. (`terraform-plugin-framework-timetypes` would be a new dependency.) An end date or usage limit set in the admin can't be removed from config (unset = unmanaged); the docs say to set a far end date or a high limit instead.

**Context:** `DiscountContextInput` writes segments and markets as `add` / `remove` lists. Update reads the live context, sends the difference, or switches kind (`{ all: ALL }` after segments replaced them, checked). Segments ↔ markets switch the same way (checked on the dev store, 2026-10-27): `context: { markets: { add: [<market>] } }` alone on a discount with a segment context read back as `DiscountMarkets` with only that market, and `customerSegments: { add }` alone switched it back, no userErrors, unlike items, which need the old kind removed (`TestAccDiscountBasic_code`). Individual customers (`DiscountCustomers`) are not offered: customers are records, not configuration. A live context of individual customers or `DiscountContextUnknown` is unsupported: if config sets `context`, plan fails naming the context type; otherwise it stays null and import and list warn. Deleting a discount's only segment **expires the discount** and resets its context to all buyers (checked: status `ACTIVE` → `EXPIRED`, context `DiscountCustomerSegments` → `DiscountBuyerSelectionAll`), so the docs say to reference `shopify_customer_segment.<x>.id` (dependency order then updates the discount first).

**Codes: one per discount.** `code` is the discount's code. Bulk codes (`discountRedeemCodeBulkAdd`, async jobs, often thousands of personal codes) are not managed (roadmap). A discount with more than one code (`codesCount > 1`) can still be imported by any of its codes: Read keeps the state `code` while `codeDiscountNodeByCode(code)` returns this discount, and otherwise takes the first code. Update refuses to change `code` on such a discount before calling Shopify, which refuses it too (checked on the dev store, 2026-10-27): after `discountRedeemCodeBulkAdd` added two codes to a one-code discount (the bulk creation was done and `codesCount` 3 within 0.5 s), `discountCodeBasicUpdate(code:)` failed with "Cannot update the code of a bulk discount." (`field = ["id"]`, no `code`) for a new code and also for the discount's own first or third code, and the codes stayed as they were. An update without `code` (title, usage limit) on that discount succeeded, so its other attributes stay manageable. The list's `DisplayName` shows the extra count.

Import key: `code/<code>` for a code discount (`code/WILLKOMMEN10`), `automatic/<title>` for an automatic one (`automatic/Versandkostenfrei ab 50 €`), split at the first `/` (`patterns.SplitImportKey`), so codes and titles may contain `/`. Read: `discountNode(id:)` first (not deprecated; `codeDiscountNode` and `automaticDiscountNode` are). If it is gone: `codeDiscountNodeByCode(code:)` for code discounts (direct, case-insensitive: `TFPROBE` found `TfProbe`, checked), `discountNodes(query: "method:automatic AND title:\"<title>\"")` with an exact title match for automatic ones (search-indexed; the probe found a new discount at once). `MutableIdentity` is set. A match of another method fails with "is a <kind> discount; use shopify_discount_<kind>"; an app discount with "belongs to an app".

**Pattern A: `adopt_existing` only.** No default discounts exist (above). With `adopt_existing = true`, Create looks the key up and updates the found discount; destroy only removes it from state with a warning. Without it, Create fails on a taken code or automatic title (`TAKEN` from the API, mapped to `code` / `title`).

**No Pattern B.** Deleting a discount removes its configuration; orders keep their discount applications (schema), and the config recreates it. But a recreate restarts usage counting (`asyncUsageCount`), so `usage_limit` and `applies_once_per_customer` start from zero and customers can use a once-per-customer code again. ModifyPlan therefore warns on a planned replace (only `code` set ↔ unset forces one). A missing discount on delete (`INVALID`, "Automatic discount does not exist.", checked; the code message is `TODO(verify)`) means already gone: removed from state without error.

**Not managed in v1:** `tags` (discount tags), `recurringCycleLimit` and the subscription flags (`appliesOnSubscription`, `appliesOnOneTimePurchase`, `isSubscription`; selling plans are a roadmap type), `productDiscountsWithTagsOnSameCartLine`, metafields, bulk codes, shareable URLs. `discountClasses` follows from the items (all items → `ORDER`, collections → `PRODUCT`, checked) and isn't exposed.

### `shopify_discount_basic`

| Attribute | Type | Behaviour |
|---|---|---|
| `customer_gets` | object | Required. `value`: exactly one of `percentage` (number, 0 < x ≤ 1) or `amount` (object: `amount` decimal string, `applies_on_each_item` bool). `items`: exactly one of `all` (bool, `true`), `product_ids` and/or `variant_ids` (set(string)), `collection_ids` (set(string)) |
| `minimum_requirement` | object | Optional + Computed, UseStateForUnknown; unset = unmanaged. At most one of `subtotal` (decimal string) and `quantity` (number); `{}` = no minimum. Every write sends both kinds, the unused one as `null` (see Implemented) |

Mutations: `discountCodeBasicCreate` / `Update`, `discountAutomaticBasicCreate` / `Update`, `discountCodeDelete`, `discountAutomaticDelete`.
- **Items** are written as add / remove lists (`DiscountProductsInput.productsToAdd/Remove`, `productVariantsToAdd/Remove`, `DiscountCollectionsInput.add/remove`), unlike the full lists Read returns. Update reads the live items and sends the difference; switching kind sends the new kind only (`{ all: true }` replaced a collection selection, checked). `collections: { add: [] }` changed nothing (checked). Read pages `products`, `productVariants`, `collections` (first 250).
- **Minimum requirement:** `minimumRequirement: null` is ignored (the minimum stayed, checked); `{ subtotal: { greaterThanOrEqualToSubtotal: null } }` removes it (checked), so `{}` in config sends that. Subtotals read back as `MoneyV2` (`"50"` → `"50.0"`, checked) and compare numerically, as product prices do (§9k.2).
- Value: `DiscountCustomerGetsValueInput.percentage` / `discountAmount`; `discountOnQuantity` is BXGY only.

### `shopify_discount_free_shipping`

| Attribute | Type | Behaviour |
|---|---|---|
| `destination` | object | Optional + Computed, UseStateForUnknown; unset = unmanaged. Exactly one of `all` (bool, `true`) or `country_codes` (set(string), ISO codes). Countries are written as `add` / `remove`, like items. `includeRestOfWorld` is deprecated and not used |
| `maximum_shipping_price` | string | Optional + Computed, UseStateForUnknown; unset = unmanaged. Decimal; only shipping rates up to it are discounted |
| `minimum_requirement` | object | As for basic |

Mutations: `discountCodeFreeShippingCreate` / `Update`, `discountAutomaticFreeShippingCreate` / `Update`. `TODO(verify)`: the create defaults (destination when unset), and whether automatic free-shipping titles share the uniqueness rule.

### `shopify_discount_bxgy`

| Attribute | Type | Behaviour |
|---|---|---|
| `customer_buys` | object | Required. `items` (as for basic; `TODO(verify)` whether `all` is accepted) and exactly one of `quantity` (number) or `amount` (decimal string, purchase amount) |
| `customer_gets` | object | Required. `items` (as above), `quantity` (number of discounted items) and exactly one of `percentage` or `amount`: `DiscountOnQuantityInput { quantity, effect }` |
| `uses_per_order_limit` | number | Optional + Computed, UseStateForUnknown; unset = unmanaged |

Mutations: `discountCodeBxgyCreate` / `Update`, `discountAutomaticBxgyCreate` / `Update`.

### All three

- Errors (`DiscountUserError`, `field` relative to the input, e.g. `["automaticBasicDiscount","title"]`): `TAKEN` (`title` or `code`), `BLANK`, `INVALID`, `TOO_LONG`, `TOO_SHORT`, `END_DATE_BEFORE_START_DATE` (`ends_at`), `VALUE_OUTSIDE_RANGE`, `GREATER_THAN` / `LESS_THAN` and their variants, `MINIMUM_SUBTOTAL_AND_QUANTITY_RANGE_BOTH_PRESENT` (`minimum_requirement`), `EXCEEDED_MAX`, `ACTIVE_PERIOD_OVERLAP`, `CONFLICT`, `DUPLICATE`, `IMPLICIT_DUPLICATE`, `INCLUSION`, `INVALID_COMBINES_WITH_FOR_DISCOUNT_CLASS` (`combines_with`), `APPLIES_ON_NOTHING`, `INTERNAL_ERROR`; `extraInfo` is appended to the message. The tag, subscription and function codes can't occur (never sent). None is retried.
- **Fit: volatile.** The descriptions say so: promotions are usually run from the admin; long-running rules (free shipping thresholds, a newsletter code) fit Terraform.
- **Scopes:** `read_discounts` / `write_discounts` (`docs/bootstrapping.md`). `TODO(verify)`: whether reading segment and market contexts also needs `read_customers` / `read_markets`; checked only with every scope granted.
- **Review Focus 1:** after destroy of an adopted discount, read it back through the client and expect it unchanged. **Review Focus 2:** set `ends_at` in one step, leave it out in the next, change it through the client in between: the config without it plans empty; likewise `combines_with`.
- **Testing:** codes `TFACC<random>`, titles `tf-acc-discount-<random>`. Per resource: code and automatic create, update (dates, value, items switch, minimum set and cleared), code rename, code ↔ automatic plans a replace, import by both key forms and identity, adopt, a taken automatic title. Unit tests first: the time equality, import-key parsing, item and context diffs (add / remove / kind switch), the minimum mapping (`{}` → clear), mapping the `Discount` union to state.

**List:** one per resource, no config. `discountNodes(first: 50)`, paged; requested cost with the basic fragments (items and markets at first 250) was 359 for 50 (checked). Each list keeps its method and skips other methods and app discounts. `DisplayName`: `<title> (code <CODE>)`, `<title> (code <CODE>, +<n> codes)`, or `<title> (automatic)`. Results carry the attributes above; generated config then holds product, collection, segment and market GIDs, which tie it to the store (the docs say to replace them with references).

**Dev store (2026-10-26):** probes created five basic discounts (three automatic, two code; plus refused duplicates), updated items, context, minimum and code, created two probe segments for the context check and deleted them (which expired the discount using them), and deleted everything. End state equals the start: 0 discounts, the 5 default segments.

**Implemented (§9p 13b-4a, verified on the dev store, 2026-10-27):** ruling S13-6: only `shopify_discount_basic` and its list; free shipping and BXGY wait for production's kinds.
- **Package `internal/resources/discount`:** `common.go` holds what every method shares (common attributes and model, the input of the common fields, the state mapping, context, Pattern A lookups, delete, import key, `userErrors` mapping, ModifyPlan); `items.go` the item selection, value and minimum requirement, which BXGY and free shipping reuse; `types.go` the time and decimal types. Both reuse `patterns.JSONType` (a string with semantic equality) with an instant or a numeric comparison (S13-5); no new type code. `basic.go` and `basic_list.go` add the two basic mutations and the list. The mutations return only the ID; every write reads the discount back by GID, so Read, Create, Update and list share one mapping. `patterns` gained `DecimalEqual` (moved from `product`), `DiffIDs`, `PaginateCounting` (now also used by `market`'s list) and the including-null modifier for Int64, Bool and Object.
- **Item kind switch:** collections → products failed with "Cannot entitle collections in combination with product variants or products" (checked), so a switch between collections and products removes the old kind in the same call (then accepted, checked). `{ all: true }` replaces either. An update whose items are unchanged sends `customerGets` without `items` (accepted, checked). Selecting a product doesn't list its variants on read (`variant_ids` stays null, checked), and selecting one variant of a two-variant product reads back as that variant only, without its product (`product_ids` stays null, plans empty, checked). Read pages `products`, `productVariants` and `collections` past the first 250 (`GetDiscountItemsPage`, every list continued from its end cursor, so a finished one returns an empty page); the merge is unit-tested, the paging query was not reached on the dev store (no discount with more than 250 items).
- **Minimum:** subtotal → quantity failed with "Minimum subtotal cannot be defined when minimum quantity is." and "Minimum quantity cannot be defined when minimum subtotal is." (checked), so every write sends both kinds with the unused one `null`; `{}` sends both `null`, which removed a quantity minimum too (checked). `"30"` read back as `"30.0"` without a diff.
- **Errors (checked):** a taken code gives "Code must be unique. Please try a different code." on `code`; a taken automatic title "Title must be unique for automatic discount." on `title`. `applies_on_each_item = true` with `items = { all = true }` is refused: "'applies on each item' must be 'false' when there are no items specified in customer get" (documented, not validated).
- **Plans:** Terraform proposes the non-computed nested attributes of an unset Optional + Computed object (`context`, `minimum_requirement`) from config, i.e. null, so the framework sees a change and marks computed values unknown although the object's own modifier restores it. The resource-level ModifyPlan (after the attribute modifiers) therefore sets `status` to its state value and keeps it only when the plan then equals the state: plans without a real change stay empty, and any real update shows `status` as known after apply, so a discount that starts or ends between plan and apply can't fail the apply. Nested defaults (`combines_with` flags, `applies_on_each_item`) exist only in the plan, so writes take `customer_gets` and a configured `combines_with` from the plan.
- **Times:** `starts_at = "2026-10-01T12:00:00+02:00"` stays as written while Shopify holds `2026-10-01T10:00:00Z` (no diff, checked). Import can only return UTC, and a plan must take the configured value, so importing into a config that spells a time with an offset plans an in-place update of `starts_at` (harmless); generated config uses UTC and plans import-only (checked).
- **Deviations:** the import key is split with `strings.Cut`, not `patterns.SplitImportKey` (which wants an exact number of parts), so `automatic/<title>` works for titles with `/` (checked with `…/50`). Markets are read at first 250 and not paged: more fails Read with a clear error and the list skips such a discount with a warning (shortcut in code). When the read-back after a successful create fails, Create still writes the ID (and identity) to state, so Terraform taints the discount instead of orphaning it (not reachable in the tests).
- **Covered by acceptance tests:** code and automatic create; update of value, items (all → collections → products), context (all → segment → market → segment → all, with a default segment and the dev store's market), minimum (subtotal → quantity → `{}`), dates, limits and code rename in place (same GID); code → automatic replace; import by `code/…`, `automatic/…` and identity, generated config import-only; adopt and Review Focus 1; Review Focus 2 for `ends_at` and `combines_with` on both kinds; a taken code and a taken automatic title; the list with both kinds. **Unit tests only:** unsupported live contexts (individual customers, `DiscountContextUnknown`), discounts with several codes, other methods and app discounts in the list. `TODO(verify)` in code: the scopes for segment and market contexts.
- **Dev store:** the tests leave 0 discounts, 0 products and `frontpage` with `sources: []`; the draft test product adds an empty `Untitled Source` to `frontpage`, which the test's cleanup removes (one left by a first run was removed by hand through the client).

**Task split (implementation order):** 13b-4a `shopify_discount_basic` with the shared package (largest); 13b-4b `shopify_discount_free_shipping`; 13b-4c `shopify_discount_bxgy`. 13b-4a needs `shopify_customer_segment` (§9q) only for its tests' segment context, which can use a default segment instead.

---

## 9q. Resource: `shopify_customer_segment`

Discovery: 5 segments on staging and on production. D5 assumed Shopify defaults, and **that holds:** the dev store has the same five, all created in the same second as the shop (`creationDate` 2026-10-22T19:29:11Z, shop `createdAt` 19:29:07Z, checked): "Customers who haven't purchased" (`number_of_orders = 0`), "Customers who have purchased more than once" (`number_of_orders > 1`), "Abandoned checkouts in the last 30 days" (`abandoned_checkout_date >= -30d`), "Email subscribers" (`email_subscription_status = 'SUBSCRIBED'`), "Customers who have purchased at least once" (`number_of_orders >= 1`). A segment is a saved customer query (ShopifyQL segment syntax); discounts target segments (§9p `context.customer_segment_ids`), and marketing edits them in the admin.

```hcl
resource "shopify_customer_segment" "subscribers" {
  name  = "Newsletter-Abonnenten DE"
  query = "email_subscription_status = 'SUBSCRIBED' AND customer_countries CONTAINS 'DE'"
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `id` | string | `Segment` GID. Computed, UseStateForUnknown |
| `name` | string | Required, import key. Updatable in place |
| `query` | string | Required. Stored and returned verbatim (`number_of_orders=0   AND   …` read back unchanged, checked), so no normalisation. Shopify validates it on write (`number_of_orderz = 0` failed with "Query Line 1 Column 0: 'number_of_orderz' filter cannot be found.", `field = ["query"]`, checked) |
| `adopt_existing` | bool | Pattern A, see below |

Import key: the name (`Email subscribers`). Read: `segment(id:)` first; if it is gone, `segments(first: 10, query: "name:\"<name>\"")` and an exact name match (search-indexed; a renamed segment was found at once, checked). `MutableIdentity` is set.

**Names are unique, but Shopify suffixes a taken name instead of failing**, on create and on update: `segmentCreate(name: "Email subscribers")` created `Email subscribers (2)`, and renaming another segment to it gave `Email subscribers (3)` (both checked, no userError). So, as for blogs (§9n) and menus (§9):
- Create looks the name up first (fails unless `adopt_existing`) and checks the returned name; a suffixed name means the new segment is deleted again and Create fails, naming the name.
- Update with a new name looks it up first and fails if it is taken; if Shopify still returns a different name, the update fails naming both, and state takes the returned name.
- `TODO(verify)`: whether names differing only in case count as taken.

**Pattern A: `adopt_existing` only.** The five defaults are created with the shop, but they are ordinary segments as far as the API shows (no `isDefault`, same mutations), so they are handled like `frontpage` (§9l): `adopt_existing = true` updates the segment with that name, and destroy only removes it from state with a warning. Taking over a default by name works on every store, since the names are the same everywhere (in the shop's admin language; `TODO(verify)` that a German-language shop's defaults also have these English names: staging and production were not read by name). `TODO(verify)`: whether the defaults can be deleted; not tried, since a recreate gets a new GID and the defaults may be referenced by marketing automations.

**No Pattern B.** A segment holds a query, not data: customers stay, and a recreate from config restores the same membership. But **deleting a segment expires the discounts that target only that segment** and resets their context to all buyers (checked, §9p); marketing automations and campaigns that use it may break (`TODO(verify)`). The docs say to reference segments as `shopify_customer_segment.<x>.id` from discounts, so dependency order changes the discount first. Nothing forces a replace (`name` and `query` are both updatable), so there is no replace warning. "Segment does not exist" on delete (plain `UserError`, `field = ["id"]`, checked) means already gone: removed from state without error.

Notes:
- Uses `segment(id:)`, `segments(first:, query:)`, `segmentCreate(name:, query:)`, `segmentUpdate(id:, name:, query:)`, `segmentDelete(id:)`. Update sends only the changed argument. `segmentMigrations` is deprecated and not used; `segmentFilters` / `segmentFilterSuggestions` (query building help) are not needed.
- Errors: plain `UserError` (no codes) from all three mutations, mapped by `field` (`["query"]` → `query`, `["name"]` → `name`), otherwise to the resource. None is retried.
- **Fit: volatile.** The description says so: segments are usually built and changed by marketing in the admin; the ones discounts or automations depend on fit Terraform.
- **Scopes:** `read_customers` / `write_customers` (`docs/bootstrapping.md`: "customer segments"). `TODO(verify)`: checked only with every scope granted.
- **Review Focus 1:** after destroy of an adopted default segment, read it back through the client and expect it unchanged. **Review Focus 2:** `name` and `query` are both Required, so the step doesn't apply; the test instead edits another segment through the client and expects an empty plan (as §9c does).
- **Testing:** names `tf-acc-segment-<random>`. Steps: create, update `query` and `name`, import by name and identity, adopt a default (`Email subscribers`, `query` set to its current value so nothing changes; destroy leaves it), a taken name fails without leaving `… (2)` behind, an invalid query reports Shopify's message on `query`. Unit tests first: the suffixed-name check, userError field mapping.

**List:** no config. `segments(first: 250)`, paged. `DisplayName`: the name. Results carry `name` and `query`, and plan as import-only. All segments are listed, the defaults included (they are merchant-editable, unlike D3's Shopify-owned definitions).

**Dev store (2026-10-26):** probes created five segments (`Email subscribers (2)`, `TF Probe Segment` renamed to `Email subscribers (3)` and then `tf probe segment`, and `TF Probe Seg` three times for the discount check) and deleted all of them. End state equals the start: the five defaults, unchanged.

**Implemented (§9q, verified on the dev store, 2026-10-27):**
- Everything above held in the acceptance tests: create, in-place `query` update and rename (same GID), import by name, by identity and from generated config, `adopt_existing` on a client-made segment (destroy leaves it with Terraform's query), the list. `number_of_orderz = 0` fails on create and on update with Shopify's message ("'number_of_orderz' filter cannot be found"), a failed update leaves state and the live segment as they were. Empty `name` and `query` are rejected by validators and by Shopify ("Name can't be blank", "Query can't be blank", `field = ["name"]` / `["query"]`, checked), so every listed segment passes them.
- **Deviation: the name lookup scans `segments(first: 250)` instead of searching.** The search index lags 2 to 4 seconds behind writes (a new segment was not found by `name:"..."` for about 4 s, while the unfiltered list showed it at once), so a lookup by search would miss a segment created just before and Create would produce the suffixed copy. The scan is exact, paged (one call up to 250 segments) and stops at the first match. The ID read of a missing segment is not `null` but a GraphQL error with `extensions.code = NOT_FOUND` ("Segment does not exist"); `client.IsNotFound` maps it to "gone", so Read falls back to the name.
- `TODO(verify)` resolved: names differing only in case are **not** taken (`TF-ACC-...` next to `tf-acc-...` is created unsuffixed, `caseOnlyDiffers`), so the match is case-sensitive. Leading and trailing whitespace of a name is trimmed by Shopify (`" x "` came back as `"x"`), so a validator rejects it at plan time (`nameRe`, unit-tested; a plan-only acceptance step covers it); without it Create would see the trimmed copy as a name clash and delete it.
- `segmentDelete` of an already deleted segment returns a plain userError (`field = ["id"]`, "Segment does not exist"), not a top-level error like `segment(id:)` (checked by deleting a throwaway segment twice); Delete also tolerates a `NOT_FOUND` error, untested since Shopify does not send one here.
- Review Focus 1: adopting `Email subscribers` with its live query and destroying leaves it byte-identical (id, name, query). Review Focus 2: another segment edited through the client plans empty; an edit of the managed segment's query plans an update.
- Suffixed names: a taken name on create is refused by the lookup before `segmentCreate`, on rename before `segmentUpdate`; no `(2)` copy was left. The post-create check (delete the copy, then fail) and the post-update check (state takes the returned name) only run when Shopify wins a race, so no acceptance test reaches them; `patterns.KeyDrift` (shared with `shopify_blog`) is unit-tested.
- "Segment does not exist" on delete is handled by `goneOnDelete` (unit-tested, matched by message since the error has no code), not by an acceptance test: Terraform refreshes before destroy, so Read already drops a segment deleted outside (`deletedOutside` covers that and the recreate). The discount-expiry effect of a delete is not exercised by this resource's tests (§9p's probe covers it); the docs state it and no plan-time warning exists, as specified.
- Not tested: the five defaults' names on a German-language shop, deleting a default, the scopes with fewer than all granted (`TODO(verify)` stays in the spec).

---

## 9r. Data source: `shopify_metaobject`

Decision D4 (2026-10-26). Discovery found no metaobjects on staging and about 40 on production, all in Shopify's standard definitions (`shopify--*`). Metaobject entries are content; managing them is a roadmap type (`write_metaobjects`). Configuration still needs to point at single entries, for example a metafield value or a collection condition that references a metaobject (§9l), so the data source reads one entry by type and handle. Lookup by key works for merchant-owned types too.

```hcl
data "shopify_metaobject" "material_baumwolle" {
  type   = "shopify--material"
  handle = "baumwolle"
}

# data.shopify_metaobject.material_baumwolle.id
# data.shopify_metaobject.material_baumwolle.fields["label"]
```

**Arguments (Required):** `type`, exactly as the API returns it (`shopify--material`, or a merchant type; usually `data.shopify_metaobject_definition.<x>.type` or `shopify_metaobject_definition.<x>.type`), and `handle`. `$app:foo` resolves to the caller's app prefix (checked on the dev store, 2026-10-27): `metaobjectDefinitionCreate(type: "$app:tfacc_probe")` stored `app--<app id>--tfacc_probe`, and `metaobjectByHandle` and `metaobjectDefinitionByType` found the entry and the definition with either spelling. The data source echoes the configured `type`, so `$app:<name>` works as is (`TestAccMetaobjectDataSource_appType`).

**Attributes (all Computed, from `Metaobject` in the 2026-10 schema):**

| Attribute | Type | API field | Why |
|---|---|---|---|
| `id` | string | `id` | `Metaobject` GID, what references take |
| `display_name` | string | `displayName` | the entry's label (the display-name field's value, or Shopify's fallback) |
| `fields` | map(string) | `fields { key value }` | field key → `value`, as Shopify serialises metafield values: plain text as is, references as GIDs, booleans and numbers as strings, list types as a JSON array string (use `jsondecode()`). Fields without a value are **null entries**, not left out (the dev store's entries have many: `value_string`, `references`, …), so `fields["x"]` works for every field of the definition without `lookup()` |
| `status` | string | `capabilities.publishable.status` | `ACTIVE` or `DRAFT` when the definition has the publishable capability; null otherwise (null for the dev store's entries, checked) |
| `updated_at` | string | `updatedAt` | RFC 3339 |

**Map, not a list of objects.** §9g gives metaobject *definitions* a computed `field` list so it can be fed back into the resource with `dynamic "field"`. Entries have no resource yet (roadmap), and the use is a value lookup by key, which a map makes direct. A field's type is not exposed: it comes from the definition (`data.shopify_metaobject_definition.<x>.field`). Left out: `fields[].jsonValue` and `values` (the same data as JSON), `references`, `thumbnailField`, `createdBy*`, `referencedBy`, `capabilities.onlineStore` (template suffix, only with the online store capability).

**Lookup:** `metaobjectByHandle(handle: { type, handle })`, one direct call (checked: `{ type: "shopify--knowledge-base-fact", handle: "delivery_countries" }` returned the entry). There is no fallback by GID and nothing is written beyond the read.

**Errors:**
- Not found: error, never an empty result, naming both (`no metaobject shopify--material/baumwolle`). Shopify returns `null` both for an unknown handle and for an unknown type (checked), so the message also says the type may not exist or the app may lack access to it (an app-owned type without merchant read access is expected to look the same; `TODO(verify)`).
- Missing scope (`read_metaobjects`): Shopify's `ACCESS_DENIED`, reported with the scope name. Nothing is retried.

**Scopes:** `read_metaobjects` (or `write_metaobjects`). `TODO(verify)`: checked only with every scope granted; whether `shopify--*` standard entries need anything else.

**Fit:** good. Read-only reference; entries change in the admin, and the data source reads the current state on every plan.

**Tests:** acceptance: (a) an entry created through the client (`metaobjectCreate`) in a definition made by `shopify_metaobject_definition` in the same config, every attribute checked, a field without a value as a null entry, then the entry deleted; (b) the dev store's `shopify--knowledge-base-fact` / `delivery_countries` entry, skipped when it is gone (Shopify created it, not the tests); (c) not found. Unit: mapping (null values, the publishable status), the not-found message.

**Dev store (2026-10-26):** read-only; one standard definition with entries, `shopify--knowledge-base-fact` (2 entries, `delivery_countries` and `shipping_policy`, 18 fields each, most without a value).

**Implemented (§9r, verified on the dev store, 2026-10-27):** the lookup, mapping and errors are as specified; `type` and `handle` are echoed from the configuration. Acceptance tests cover the three cases of the Tests paragraph: (a) creates the entry through the client between two steps (the definition first, then the entry and the data source), checks every attribute and the null `summary` field through an output (`fields["summary"] == null`), and deletes the entry in a last step before the definition goes; (b) reads `shopify--knowledge-base-fact` / `delivery_countries` and compares `id`, `display_name` and the field count with a direct API read, with `status` unset; (c) not found, matching the `no metaobject <type>/<handle>` message. Unit tests cover the mapping (null values, the publishable status) and the message text. Still `TODO(verify)`: the access-denied path, which no test reaches because the dev store has every scope. The queries bind GraphQL `JSON` to `json.RawMessage` in `genqlient.yaml`, only because `MetaobjectCreateInput.values` needs a binding to compile; it is never sent. `CreateMetaobject` / `DeleteMetaobject` exist for the tests only.

---

## 10. Not covered

Since 2026-10-23 the provider aims to cover every store object the Admin API can manage; remaining types are planned in [[implementation-plan]] Phase 2. Not covered:

| Item | Reason / where it lives |
|---|---|
| App config, extensions, Functions, app webhooks, web pixels, script tags | `shopify.app.toml` + `shopify app deploy` (resources may reference a Function by ID) |
| Theme | GitHub integration / `shopify theme push` |
| Orders, returns, fulfillment orders, gift cards, store credit, payments | Transactional records, not configuration |
| Store creation, plan, billing, domains, staff, taxes | Manual runbook; taxes and staff scopes need Shopify approval |

---

## 11. Go project layout (proposal)

```
terraform-provider-shopify/
├── main.go
├── internal/
│   ├── client/          # GraphQL client: auth, version pinning, throttling, retries, userErrors
│   ├── provider/        # provider schema + configure
│   ├── resources/
│   │   ├── metafielddefinition/
│   │   ├── metaobjectdefinition/
│   │   ├── market/
│   │   ├── location/
│   │   ├── deliveryprofile/
│   │   └── menu/
│   ├── datasources/shop/
│   └── patterns/        # adopt, data-deletion guard, natural-key import helpers
├── examples/
├── docs/                # generated with tfplugindocs
└── .github/workflows/   # lint, unit, acceptance tests against a dev store
```

- Framework: `terraform-plugin-framework`, with `terraform-plugin-testing` for acceptance tests.
- GraphQL: generate typed queries from the 2026-10 schema (e.g. `genqlient`) so API upgrades show up as compile errors.
- Acceptance tests run against a disposable dev store created with `shopify store create dev`.

---

## 12. Implementation order

1. Client package (auth, throttling, userErrors) + `shopify_shop` data source → proves the auth and bootstrap path.
2. `shopify_metafield_definition` → simplest resource with real value; exercises import and Pattern B.
3. `shopify_metaobject_definition` → field diffing.
4. `shopify_menu`, `shopify_location`.
5. `shopify_market`.
6. `shopify_delivery_profile` (general profile only).
7. Further types: [[implementation-plan]] Phase 2.

## Open questions

- Which resources do we let merchants own (→ `ignore_changes` or not managed at all)? Menus are the main candidate.
- Do we need multi-store support in one Terraform root module, or one state per store?
- Menu items: reference by GID, or resolve by handle?
- ~~Where does the Admin API token come from in CI?~~ Answered: client credentials grant from CI secrets `SHOPIFY_CLIENT_ID` / `SHOPIFY_CLIENT_SECRET` (see §1).
