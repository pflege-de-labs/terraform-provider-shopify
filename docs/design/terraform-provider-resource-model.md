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
- Exactly one of the two modes must be set (plan-time validation; env vars count). Both credentials and the token are `Sensitive`.
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
| `catalog_ids`, `web_presence_ids` | set(string) | Links only, as plain IDs. Catalogs and price lists are content and stay out of scope. Web presences themselves: `shopify_web_presence` (section 9a) |

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
- **Pattern A:** the shop's primary-domain presence can't be removed: the error code `SHOP_MUST_HAVE_PRIMARY_DOMAIN_WEB_PRESENCE` exists, and on the dev store a delete of it left it in place; the userError text was not captured. Adopting it (`adopt_existing = true`) updates it in place, and destroy only removes it from state with a warning. Every store has one such presence (dev: the myshopify domain, locale `en`, no markets). `CANNOT_DELETE_PRIMARY_MARKET_WEB_PRESENCE` is deprecated ("No longer used").
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

Leaf equality: numbers numerically, `#RRGGBB` strings case-insensitively, everything else exactly. The attribute uses a custom string type with semantic equality (`basetypes.StringValuableWithSemanticEquals`, part of the framework, no new dependency), so key order and whitespace never plan. ValidateConfig only checks that the value is a JSON object; Shopify validates keys and values.

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

**Arguments (Required):** `shopify_metafield_definition`: `owner_type` (any `MetafieldOwnerType`, not only `PRODUCT`), `namespace`, `key`. `shopify_metaobject_definition`: `type`, exactly as the API returns it (`shopify--material`, `app--123--foo`, or a merchant type). `TODO(verify)`: whether the API resolves `$app:foo` to the caller's prefix on lookup; the same is unverified for a metafield `namespace` of `$app:…`.

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
