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
| `primary` | bool | Computed. True for the shop's primary locale |
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
- **Scopes:** `read_locales` / `write_locales` (reads and all four operations above work with `write_locales`).
- **Review Focus 2:** set `published` only in one step and leave it out in another, change it through the client in between: the config without `published` plans empty.

**List:** no config. Lists every enabled locale, the primary included. `DisplayName`: `fr (French)`, primary as `en (English, primary)`. Results carry `locale` and `published`, which pass validation and plan as import-only.

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
