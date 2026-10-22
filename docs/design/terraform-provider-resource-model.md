# Terraform Provider for Shopify: Resource Model (Draft)

**Status:** Draft for discussion · **Owner:** Jens Hausherr · **Last updated:** 2026-10-21
**Related:** [[shopify-infra-automation-approach]]

This draft covers the first scope from the approach doc: the shop data source, metafield definitions, metaobject definitions, markets, locations, shipping (delivery) profiles and menus. It targets Admin GraphQL API **2026-10** (current stable). Items marked ⚠ need checking against the API reference before implementation.

---

## 1. Provider configuration

```hcl
provider "shopify" {
  store_domain = "pflege-dev.myshopify.com" # env: SHOPIFY_STORE_DOMAIN
  access_token = var.shopify_token          # env: SHOPIFY_ACCESS_TOKEN (sensitive)
  api_version  = "2026-10"                  # pinned default, overridable
  max_retries  = 5                          # throttling / 5xx backoff
  concurrency  = 2                          # max in-flight GraphQL calls
}
```

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
| `shopify_location` | `<name>` (no handle exists; falls back to numeric ID) |
| `shopify_delivery_profile` | `<name>`, or `default` for the general profile |
| `shopify_menu` | `<handle>` |

- Read looks up by GID first. If the GID is gone, it falls back to the natural key, so a manual recreate in the admin doesn't break state.

### Attribute behaviour
- **RequiresReplace** only where the API forbids an in-place change (e.g. metafield `owner_type`/`namespace`/`key`).
- **Computed + UseStateForUnknown** for server-generated values (GIDs, timestamps).
- Optional attributes left unset are **not managed**. The provider doesn't overwrite admin edits to them. This reduces drift noise.

### Pattern A: "adopt" for objects that exist once per store
Some objects already exist on every store and can't be created or deleted (the shop, the general delivery profile, default menus such as `main-menu`/`footer` ⚠).
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
- Async operations (e.g. metaobject definition deletion ⚠) → poll the returned job until done, with a timeout set via the `timeouts` block.

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
| `type` | string | Required, RequiresReplace ⚠ (check whether some type migrations are allowed in place) |
| `name`, `description` | string | Updatable |
| `validations` | map(string) | Updatable. Values are strings, as in the API. JSON-shaped validations (e.g. `metaobject_definition_id`) are passed as JSON strings |
| `access` | object | `admin`, `storefront`, `customer_account` ⚠ |
| `pin` | bool | Maps to the pin/unpin mutations |
| `use_as_collection_condition` | bool | Delete is blocked by Shopify while in use as a collection rule |
| `allow_data_deletion` | bool | Pattern B. When true, delete calls `metafieldDefinitionDelete` with `deleteAllAssociatedMetafields: true` ⚠ |

Mutations: `metafieldDefinitionCreate`, `metafieldDefinitionUpdate`, `metafieldDefinitionDelete`, `metafieldDefinitionPin`/`Unpin`.

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
| `capabilities` | object | `publishable`, `translatable`, `renderable`, `online_store` ⚠ |
| `field` | ordered list of blocks | `key`, `name`, `type`, `description`, `required`, `validations` |
| `allow_data_deletion` | bool | Pattern B. Deleting the definition removes all its entries ⚠ |

**Field diffing.** Update translates the block list into the update mutation's per-field `create` / `update` / `delete` operations ⚠, matched by `key`:
- A new key → `create`.
- A changed name, description, required flag or validations → `update`.
- A removed key, or a changed field `type` (delete + create) → **destroys the data in that field**. Blocked unless `allow_data_deletion = true`, with a plan-time warning.
- A changed order → reorder (field order matters in the admin UI).

Entries (metaobjects) are content and stay **out of scope**.

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

  web_presence_ids = [shopify_market_web_presence.de.id] # optional
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `handle` | string | Required, import key |
| `name` | string | Required |
| `enabled` | bool | |
| `conditions.regions` | set(string) | Country codes. B2B (`company_location_ids`) and POS (`location_ids`) conditions come later; they are dev preview |
| `currency_settings` | object | `base_currency`, `local_currencies` |
| `catalog_ids`, `web_presence_ids` | set(string) | Links only. Catalogs and price lists are content and stay out of scope |

Notes:
- Uses the **2025+ Markets model** (conditions-based `marketCreate` / `marketUpdate`), not the legacy region mutations.
- A country can belong to only one region market ⚠. Moving a country between two Terraform-managed markets needs a careful update order (remove from A, then add to B). The provider should report the Shopify error clearly rather than retry.
- Possible follow-up resource: `shopify_market_web_presence` (domain or subfolder suffix, default and alternate locales).

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

  active          = true
  deletion_policy = "deactivate" # or "delete"
}
```

| Attribute | Type | Behaviour |
|---|---|---|
| `name` | string | Required, import key |
| `address` | object | Updatable via `locationEdit` |
| `fulfills_online_orders` | bool | |
| `active` | bool | `locationActivate` / `locationDeactivate` |
| `deletion_policy` | string | `deactivate` (default) or `delete`. Delete requires deactivating first. Shopify may refuse deletion for locations with inventory or history ⚠ |

Mutations: `locationAdd`, `locationEdit`, `locationActivate`, `locationDeactivate`, `locationDelete`.

Watch out: the location limit depends on the plan, and deactivating a location that holds inventory needs a destination for that inventory ⚠. That is a reason to default to `deactivate` and treat deletion as rare.

---

## 8. Resource: `shopify_delivery_profile`

This is the most complex resource in scope: profiles → location groups → zones → method definitions (rates and conditions).

```hcl
resource "shopify_delivery_profile" "general" {
  adopt_default = true # Pattern A: the store's general profile

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

Recommendation: **phase 2.**
- Start with the general profile only (`adopt_default = true`), with flat rates and price- or weight-based conditions.
- Product-to-profile assignment is catalog work and stays out of scope; the general profile covers everything by default.
- Update is complicated: the update input uses separate lists for create, update and delete at each level ⚠. Matching is by zone name and method name. Write good unit tests for the diff logic before shipping.

Mutations: `deliveryProfileCreate`, `deliveryProfileUpdate`, `deliveryProfileRemove`.

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
| `handle` | string | Required, import key |
| `title` | string | Required |
| `item` | nested blocks (≤ 3 levels) | `title`, `type`, `resource_id` or `url`, `tags` |
| `adopt_existing` | bool | Pattern A for default menus |

Notes:
- **Terraform owns the whole tree.** Update sends the complete item list, which replaces the menu ⚠. Any item a merchant adds in the admin will be removed on the next apply. If merchants need to edit a menu, keep that menu out of Terraform.
- Menu items point at collections and pages, which are content. Referencing them by GID makes configs specific to one store. Option: allow `resource_handle` plus `type`, and resolve it to a GID at apply time.
- Validation: `resource_id` is required for COLLECTION, PRODUCT and PAGE; `url` for HTTP ⚠ (check the full type enum).

---

## 10. Out of scope (for now)

| Item | Reason / where it lives |
|---|---|
| App config, extensions, Functions, app webhooks | `shopify.app.toml` + `shopify app deploy` |
| Theme | GitHub integration / `shopify theme push` |
| Products, collections, pages, metaobject entries, discounts | Content: product data system or sync jobs |
| Shop settings, checkout and branding, policies | Phase 3 candidates. Need Pattern A and API coverage checks |
| Store creation, plan, billing, payments, domains, staff | Manual runbook |

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

## Open questions

- Which resources do we let merchants own (→ `ignore_changes` or not managed at all)? Menus are the main candidate.
- Do we need multi-store support in one Terraform root module, or one state per store?
- Menu items: reference by GID, or resolve by handle?
- Where does the Admin API token come from in CI (see the bootstrapping section of the approach doc)?
