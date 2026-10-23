# Terraform Provider for Shopify: Implementation Plan

**Status:** Phase 1 done, Phase 2 planned · **Owner:** Jens Hausherr · **Last updated:** 2026-10-23
**Related:** [[shopify-infra-automation-approach]], [[terraform-provider-resource-model]]

This plan follows the implementation order in section 12 of the resource model, with a Step 0 added for template cleanup.

## Working rules

- One feature branch per step, one commit per step. No work on `main`.
- A step is done when `make fmt lint test` passes and `make generate` leaves no diff.
- From Step 1 on, each step begins by checking its ⚠ items against the committed 2026-10 schema. Each item is either confirmed in the resource model doc or left as a `// TODO(verify): ...` comment.

## Step 0: Rename and clean up the template

- Rename the module path in `go.mod` and all imports to `github.com/pflege-de-labs/terraform-provider-shopify`.
- Rename the provider address in `main.go` (currently `registry.terraform.io/hashicorp/scaffolding`) to `registry.terraform.io/pflege-de-labs/shopify`, and `-provider-name scaffolding` in `tools/tools.go` to `shopify`.
- Delete `.copywrite.hcl` and the copywrite `go:generate` line. As configured, `make generate` would add HashiCorp/IBM headers to every new file.
- Remove the example action, ephemeral resource and function now. They are out of scope and nothing replaces them.
- Remove the example data source in Step 1 and the example resource in Step 2, when their replacements land.

## Step 1: `internal/client` and `shopify_shop` data source

1. Commit the 2026-10 Admin API schema file and add genqlient to `make generate`.
2. Write unit tests first, for throttle backoff (`THROTTLED` / `throttleStatus`), for mapping `userErrors` to attribute paths, and for token fetch and refresh before expiry (against an `httptest` server).
3. Build the client: client credentials grant or static token, auth header, pinned API version, a semaphore for `concurrency`, and `max_retries` for throttling and 5xx responses. Semantic errors are not retried.
4. Add the provider schema: `store_domain`, `client_id` + `client_secret` or `access_token` (exactly one mode, all Sensitive), `api_version`, `max_retries`, `concurrency`, with env-var fallbacks.
5. Build the `shopify_shop` data source and its acceptance test. This proves the auth and bootstrap path.

## Step 2: `shopify_metafield_definition`

- Build `internal/patterns` here, not up front: the natural-key import helper (`OWNER_TYPE/namespace/key`), Read by GID with a natural-key fallback, and the `allow_data_deletion` guard (Pattern B).
- genqlient `optional: pointer` marshals nil input fields as `null`, which would clear admin-set values. Mark optional mutation inputs `# @genqlient(omitempty: true)` so unset attributes are omitted.
- ⚠ items resolved in resource model §4.
- Acceptance tests: create, update, import, delete blocked when the flag is false, and the ModifyPlan warning on replace.

## Step 3: `shopify_metaobject_definition`

- Write unit tests first for the field diff. It turns the field list into create, update and delete operations plus reorders, matched by `key`. A changed field `type` becomes delete + create.
- Add a validator that requires field `key`s to be unique.
- Removing a field or changing its type is blocked unless `allow_data_deletion = true`, and shows a plan-time warning.
- ⚠ items resolved in resource model §5.

## Step 4: `shopify_menu` and `shopify_location`

**Menu**
- The adopt helper (Pattern A) lands here.
- The plugin framework has no recursive schemas, so the 3 levels of menu items are written as 3 nested block levels.
- Delete has two behaviours. An adopted menu is removed from state only, with a warning. An ordinary menu is deleted for real.
- Validate `resource_id` against `url` per item type.
- ⚠ items resolved in resource model §9.

**Location**
- Activate, deactivate and edit.
- `deletion_policy = deactivate|delete`; `allow_data_deletion` guards only `delete`.
- Import by name, with numeric `legacyResourceId` as fallback. Names confirmed unique per shop (`TAKEN`), see resource model §7.

## Step 5: `shopify_market`

- Uses the conditions-based `marketCreate` / `marketUpdate` mutations. `conditions.regions` is a set.
- Shopify's duplicate-market error (same region set, both active) is reported clearly and never retried. Overlapping markets are allowed, see resource model §6.
- `web_presence_ids` and `catalog_ids` are plain IDs.

## Step 6: `shopify_delivery_profile` (general profile only)

- Write unit tests first for the diff: the create, update and delete lists at each level, matched by zone name and by method name plus condition (method names repeat in real profiles). Add validators that require those keys to be unique. See resource model §8.
- Pattern A, implicit (no flag): always adopts the general profile. Delete only removes it from state.

## Dependencies needing approval

- `github.com/Khan/genqlient`: the generator in `tools/go.mod` and the runtime in the main `go.mod`.
- `terraform-plugin-framework-timeouts`, for the `timeouts` block on async operations.
- `terraform-plugin-framework-validators`, for enum, conflicts-with and uniqueness checks.

## Decisions (2026-10-22)

1. **Location deletion:** both. `deletion_policy = deactivate|delete` (default `deactivate`); `allow_data_deletion` guards only `delete`.
2. **Location import key:** `<name>`, with the numeric `legacyResourceId` accepted as fallback. Why:
   - The GID is `gid://shopify/Location/<legacyResourceId>`, so both IDs change together when a location is recreated. The numeric ID is no more stable than the GID.
   - Location has no `handle`. Names are unique per shop: confirmed on the dev store (`TAKEN`), but only among active locations: a deactivated one does not block the name.
   - Names are portable across stores; numeric IDs are not, so they are the fallback only (e.g. for duplicate or unknown names).
3. **Market links:** `web_presence_ids` and `catalog_ids` take plain IDs. `shopify_market_web_presence` is deferred.
4. **Provider address:** `registry.terraform.io/pflege-de-labs/shopify`, Go module `github.com/pflege-de-labs/terraform-provider-shopify`.
5. **No `adopt_default` flag:** phase 1 supports only the general delivery profile, so adopting it is implicit. `adopt_existing` (natural-key takeover) is a different concern and stays. Revisit when custom profiles are added.
6. **Authentication:** client credentials grant (`client_id` / `client_secret`) is the main mode, because Dev Dashboard tokens expire after 24h. A static `access_token` stays as alternative. See resource model §1.
7. **Schema source:** `internal/client/schema/admin-2026-10.graphql`, introspected from Shopify's public proxy (`https://shopify.dev/admin-graphql-direct-proxy/<version>`) with `schema/fetch-schema.js`. No store token needed. `make generate` only runs genqlient on the committed file; refetch only when bumping the version. Dev store: see [bootstrapping](../bootstrapping.md).

## Phase 2: every manageable resource type (decided 2026-10-23)

Goal: the provider covers every store object the Admin API can manage. Volatile types are implemented too; their docs mark them as a poor fit. What stays uncovered is listed in CLAUDE.md (Scope).

Working rules, in addition to the ones above:
- Each step starts with a section in the resource model (schema, import key, Pattern A/B, fit, ⚠ checked against the 2026-10 schema).
- Every resource gets identity and a list resource from the start ([[list-resources-plan]] Tasks 4–9 are the template).
- A step needing a new scope waits until the user has granted it on the dev store (`docs/bootstrapping.md`); record the scope in its table and in `app/*.toml`.
- The docs of a volatile type say so in the resource description.

| Step | Resource types | Scopes (write) | Fit | Notes |
|---|---|---|---|---|
| 7 | `shopify_web_presence` | `markets`, `locales` | good | design in resource model §9a |
| 8 | Shop-level singletons: shop locales, shop policies, privacy settings, checkout and accounts configuration, checkout branding | `locales`, `legal_policies`, `privacy_settings`, `checkout_and_accounts_configurations`, `checkout_branding_settings` | good | Pattern A; use non-deprecated checkout APIs |
| 9 | `shopify_metafield_definition` for all owner types | per owner type ⚠ | good | find the scope per owner type; unscoped types list as empty |
| 10 | Custom delivery profiles, carrier services, fulfillment services | `shipping`, `fulfillments` | good; carrier/fulfillment services app-owned | extends `shopify_delivery_profile` or a new type, decide in the model |
| 11 | URL redirects, publications, catalogs, price lists, selling plan groups | `online_store_navigation`, `publications`, `products` | volatile | |
| 12 | Payment and delivery customizations, validations, cart transforms, fulfillment constraint rules | matching `*_customizations`, `validations`, `cart_transforms`, `fulfillment_constraint_rules` | good | reference a Function by ID; the Function ships with an app |
| 13 | Products, collections, pages, blogs, articles, discounts, metaobject entries, files, translations, market localizations, customer segments, inventory levels | `products`, `content`, `discounts`, `metaobjects`, `files`, `translations`, `customers`, `inventory` | volatile | content; large schemas, split per type when planned in detail |
