# Report: Phase 2 resources (2026-10-27)

Plan: [docs/design/phase2-resources-plan.md](../design/phase2-resources-plan.md). Executed subagent-driven: one task at a time, a fresh reviewer per task, a final whole-branch review, then one fix wave. Nothing was applied to staging or production; all acceptance tests ran on the dev store only.

## PRs

| PR | Content | State |
|---|---|---|
| #24 | Plan and execution ledger | Merged |
| #25 | Step 7: web presence | Merged |
| #26 | Step 8: shop-level singletons | Merged |
| #27 | Step 9: metafield and metaobject definition data sources | Merged |
| #28 | Step 10: fulfillment service data source | Merged |
| #29 | Step 11: publication data source (catalogs deferred) | Merged |
| step 13 PR (from `feat/phase2-13-content`) | Content types, product media and publishing, metaobject data source, final-review fixes, this report | To be opened |
| docs PR (from `feat/release-docs`, stacked on the step 13 PR) | README, registry index page, guide "importing an existing store", CHANGELOG 0.1.0 | To be opened |

Step 12 has no PR: discovery found no instance of any step 12 type.

## Discovery (2026-10-26)

Read-only `<env>-plan` apps, `cmd/discover` on a temporary branch (never merged), run from a temporary CI job in the provisioning repo (closed, branch deleted). The first four attempts returned "no scope" for 17 types: the app TOMLs lacked `[build] include_config_on_deploy = true`, so CI deploys re-released the dashboard config with the old scopes. Fixed in the provisioning repo and `app/*.toml`; attempt 5 after reinstall: every row `ok`. Raw tables: [docs/design/phase2-discovery/](../design/phase2-discovery/).

Non-zero counts, staging / production, and what became of them:

| Step | Type | Count | Owner | Outcome |
|---|---|---|---|---|
| 7 | web presences | 2 / 3 | merchant | built |
| 8 | shop locales | 1 / 1 | merchant (`de`) | built |
| 8 | shop policies | 1 / 2 | merchant | built |
| 8 | privacy settings, consent policy | 1 / 1, 319 / 319 regions | merchant | built (one task) |
| 8 | checkout and accounts configuration, branding | 1 / 1 each | merchant | built (branding as JSON attribute) |
| 9 | metafield definitions ORDER, SHOP | 0 / 1 each | Checkout Blocks app | data source |
| 10 | fulfillment services | 2 / 2 | Shopify (`manual`), another app | data source |
| 11 | publications | 3 / 5 | channel apps | data source |
| 11 | catalogs | 3 / 5 | channel apps | roadmap (S11-2) |
| 13 | products | 86 / 214 | merchant | built |
| 13 | collections | 14 / 17 | merchant | built |
| 13 | pages, blogs | 8 / 9, 1 / 1 | merchant | built |
| 13 | files | 269 / 432 | merchant | built (URL sources only) |
| 13 | discounts | 0 / 7 | merchant | built for the basic kind only |
| 13 | customer segments | 5 / 5 | Shopify defaults | built (`adopt_existing`) |
| 13 | metaobjects | 0 / ~40 | all `shopify--` standard definitions | data source |
| - | return policy profiles | 1 / 1 | - | roadmap (D6) |

Zero on both stores: custom delivery profiles, carrier services, URL redirects, price lists, selling plan groups, articles and every step 12 type (payment and delivery customizations, validations, cart transforms, fulfillment constraint rules). Not counted: translations (single locale `de`), market localizations, inventory levels.

Decisions taken from the counts (plan section "Decisions needed"): D2 build every in-scope type with a count above 0; D3 Shopify- and app-owned objects stay out of import but list resources still list them; D4 data sources for Shopify-owned objects so configuration can reference them; D5 segments, catalogs and discounts get resources although they looked like Shopify defaults (premise held for segments only); D6 return policy profiles and all zero-count types wait until after the first successful import.

## Types built

Counts at the step 13 head: 19 resources, 19 list resources, 6 data sources (including `shopify_shop` from phase 1). Every resource has identity, import by natural key and a list resource.

| Step | Built | Notable behaviour |
|---|---|---|
| 7 | `shopify_web_presence` | Destroying the primary presence only drops it from state; `adopt_existing` otherwise; suffix validator `^[a-z]{2,}$` |
| 8 | `shopify_shop_locale` | `id` is the locale code; the primary locale has a safety path (`adopt_existing`) |
| 8 | `shopify_shop_policy` | Singleton per type, Pattern A; the API can't delete a policy row |
| 8 | `shopify_privacy_settings`, `shopify_consent_policy` | Singletons, Pattern A; `id` of privacy settings is the shop GID; turning off the cookie banner is one-way; writing US regions materialises all 51 US rows |
| 8 | `shopify_checkout_and_accounts_configuration` | Pattern A; branding is a JSON `branding` attribute of the configuration (S8-2); hand-written JSON plans one harmless update, `jsonencode` avoids it |
| 9 | data sources `shopify_metafield_definition` (any owner type), `shopify_metaobject_definition` | Lookup by key also reaches Shopify- and app-owned definitions |
| 10 | data source `shopify_fulfillment_service` | By handle; `manual` has the GID `gid://shopify/FulfillmentService/manual` |
| 11 | data source `shopify_publication` | By name; keyed on the deprecated `Publication.name` (S11-3) |
| 13 | `shopify_product` | Core fields; options and variants (removal guarded by `allow_data_deletion`, option replace keeps no variant); `publication_ids`; `media_ids`. Linked options read-only (S13-7). Media removal and destroy detach files instead of deleting them (T14d-2, F-1); unmanaged `media_ids` keeps Shopify's behaviour |
| 13 | `shopify_collection` | One membership source (S13-3); publishing; sources with exclusions or made by Shopify are never deleted (S13-9) |
| 13 | `shopify_page` | Volatile; handle is the key |
| 13 | `shopify_blog` | A good fit for Terraform (user decision); Pattern B guard fails closed when the article count is unknown |
| 13 | `shopify_file` | URL sources only; replace guarded; destroy checks "already gone" before the guard |
| 13 | `shopify_discount_basic` | Automatic and code discounts; context (all, segments, markets), items and variant selection read back with paging |
| 13 | `shopify_customer_segment` | Adopts Shopify's defaults with `adopt_existing`; full paged scan for the name lookup (T20-1); deleting a segment expires discounts that target only it |
| 13 | data source `shopify_metaobject` | By type and handle |

Shared code that came out of this phase: `internal/patterns` (JSON type, `DiffIDs`, `KeyDrift`, handle and HTML helpers), `internal/publishing`, `internal/acctest` (`Setup`, `KeepFrontpage`), `client.UserErrorDiagsWith`/`By`, `client.IsNotFound`.

## Types skipped

| Type | Why | Count (staging / production) |
|---|---|---|
| `shopify_catalog` | Discovery found only channel app catalogs (excluded by D3) and no market catalogs, so D5's premise doesn't hold (S11-2). The design (model §9i) is kept | 3 / 5 |
| `shopify_discount_free_shipping`, `shopify_discount_bxgy` | Kinds of production's 7 discounts unknown; check at first import (S13-6) | part of 0 / 7 |
| App discounts | Function handle is scoped to the calling app; not creatable here | - |
| File uploads from local files (staged uploads) | Needed for local sources, videos and 3D models; split off as 13b-3b (S13-6, T18-1) | 269 / 432 files exist |
| Variant media, collection images | Product-level `media_ids` only in 14d (T14d-1) | - |
| Metaobject entries as a resource | All instances are `shopify--` standard definitions; data source only (D4) | 0 / ~40 |
| Articles | Zero instances (blogs built; test-only article operations exist in `blog.graphql`) | 0 / 0 |
| Return policy profiles | D6, readable with current scopes | 1 / 1 |
| Custom delivery profiles, carrier services | Zero instances; also tied to market-driven shipping (see roadmap) | 0 / 0 |
| URL redirects, price lists, selling plan groups | Zero instances | 0 / 0 |
| Step 12: payment and delivery customizations, validations, cart transforms, fulfillment constraint rules | Zero instances; each would need a Function installed | 0 / 0 |
| Translations, market localizations, inventory levels | Not counted; single locale `de` | - |
| `shopify_metafield_definition` for more owner types | Resource takes any owner type; discovery found only app-owned definitions beyond PRODUCT, scope per owner type not verified | ORDER, SHOP 0 / 1 |

All of them stay on the roadmap in [implementation-plan.md](../design/implementation-plan.md).

## Rulings

Each ruling was made by the controller during execution; "costs" is the price if it turns out wrong. P = plan, T = task, S = step, F = final review. Where a ruling was later amended, the amendment is part of its row.

| # | Ruling | Cost if wrong |
|---|---|---|
| P1 | `feat/phase2-resources` (plan + ledger) is the bottom of the stack; step branches stack on it | One docs-only PR at the bottom |
| P2 | The ledger was committed instead of kept in git-ignored scratch (the last run lost its ledger); briefs and reports stay in scratch | Ledger commits in the history |
| P3 | The temporary discovery CI job (provisioning repo, other GitHub environments) ran by the controller, not an agent | An unreviewed temporary workflow, deleted afterwards |
| T1-1 | Discovery uses `checkoutAndAccountsConfiguration`, not the deprecated `checkoutProfiles`/`checkoutBranding` the plan names | One row's semantics if the replacement doesn't map 1:1 |
| T4-1 | Web presence design (§9a, 2026-10-23) was kept; the implementer only dropped the stale "blocked on scope" note and re-checked the locale item, without a controller stop | A late design change if the locale check contradicts §9a |
| S8-1 | Step 8 design ran as one design-only dispatch for all five types, reviewed by the controller first | One larger re-design if the batch shares a wrong assumption |
| S8-2 | Checkout branding is merged into `shopify_checkout_and_accounts_configuration` as a JSON `branding` attribute; in 2026-10 it is a field of the configuration and has no separate object | A split later if the resource proves too big |
| S8-3 | `shopify_shop_locale.id` is the locale code and `shopify_privacy_settings.id` the shop GID, deviating from "id = GID": neither has a GID of its own | An identity schema change if Shopify adds IDs |
| S8-4 | Privacy settings write steps stayed skipped until the dev store's cookie banner was off; disabling it is one-way through the API, so it was the user's call (approved 2026-10-26, dev store only) | Untested write paths until then |
| T7-1 | The resource's COOKIE_BANNER disable path is covered by fake-API unit tests plus one identical real probe call, not by acceptance: the banner can't be re-enabled | An untested real-API path if the resource's request differs from the probe |
| S9-1 | Step 9 data sources mirror the existing resources' schemas read-only; model section and code in one dispatch, the task review checks the section | Rework if the reviewer rejects the schema shape |
| S11-1 | Step 11 design (catalogs, publication data source) ran as a design-only dispatch, reviewed by the controller | One more dispatch |
| S11-2 | `shopify_catalog` goes on the roadmap with its §9i design kept: all discovered catalogs are channel app catalogs and market catalogs are 0, so D5's "likely defaults" premise fails and D6 applies | A later task if the user wants catalogs now (reversible) |
| S11-3 | `shopify_publication` keys on the deprecated `Publication.name`: publications have no handle and `Catalog.title` differs per store | A migration when Shopify removes the field (surfaces as a compile error) |
| S13-1 | Step 13 design ran in two sequential design-only dispatches: (a) products and collections, (b) the rest | One extra dispatch |
| S13-2 | Products split into 14a core, 14b options and variants, 14c publishing, 14d media; collection is its own task | More task overhead |
| S13-3 | Collections v1 manage exactly one membership source (2026-10 replaced `ruleSet` with sources); exclusions, sub-collections and shared sources stay unmanaged | A schema extension later |
| S13-4 | Discounts are one resource per method (basic, free shipping, BXGY) in a shared package | Three schemas to keep aligned |
| S13-5 | The `timeouts` block and time handling are hand-rolled, no `terraform-plugin-framework-timeouts`/`-timetypes` (CLAUDE.md: no new dependencies unasked; user informed) | A little code the modules would provide |
| S13-6 | Deferred to the roadmap: local file uploads (staged uploads), free-shipping and BXGY discounts until the kinds of production's 7 are known, app discounts | Later tasks if needed |
| T14b-1 | Option rename means a new name at the same position; rename plus reorder is treated as remove plus add and hits the deletion guard | An extra `allow_data_deletion` step for such edits |
| S13-7 | §9k.2 amended: linked options are mapped read-only on Read/import/list; writes that would send options or variants to a linked product are refused before `productSet`; core-field writes work. The original "Read fails" would block importing staging/production | v1 never manages linked options |
| S13-8 | Collection sources are manageable when `shareable = false`, whatever app created them; no app check, because the plan app and the apply app differ | Managing non-shareable sources made by other apps (Shopify may reject; documented) |
| S13-9 | A collection source with exclusions or made by Shopify is never deleted; emptying it is refused with an explanation | Users need an admin step to empty those sources |
| T18-1 | `shopify_file` covers URL sources only; other sources fail in `ValidateConfig` with a roadmap note; VIDEO and MODEL_3D are importable and adoptable but not creatable or content-changeable. Amended: they also can't be renamed (schema `UNSUPPORTED_MEDIA_TYPE_FOR_FILENAME_UPDATE`), the plan error is correct | A second pass over the file resource when staged uploads are built |
| T18-2 | Three file review minors went into fix round 1: untaint hint on Create timeout, list skips filenames that fail the validator, skipped files don't count toward the list `limit` | One bigger fix round |
| T18-3 | The file list skips files without a URL (no derivable filename); §9o previously said failed files are listed. User signed off 2026-10-27 | Such files are invisible to `terraform query` |
| T14d-1 | Product media is product-level `media_ids` only; variant media and collection images go to the roadmap | A design adjustment if the probe surprises |
| T14d-2 | Media removal is non-destructive: removed IDs are detached with `fileUpdate(referencesToRemove)` before `productSet`; no media deletion guard. The product is the single writer of the link | One extra API call per update with removals |
| T19-1 | The "variant read-back unverified" minor went into discount fix round 1 (probe plus acceptance step) | A probe |
| T20-1 | Segment name lookup scans `segments(first: 250)` paged instead of searching; the index lags 2-4 s and would miss a new segment | One extra page per 250 segments per lookup |
| T20-2 | Segment Delete also tolerates a top-level `NOT_FOUND` | Nothing (one line) |
| F-1 | Product Delete and replace detach managed media before `productDelete`, fail closed; extends T14d-2 to destroy. Amended: "managed" is recorded in private state on every write (`media_ids` set in config), not inferred from state, because Optional+Computed state always holds the live list; no record means detach | Imported products keep their files on destroy until first written; files referenced only by literal GIDs from generated config stay in the library |

User decisions: blogs are a good fit for Terraform (`bootstrapping.md` corrected, 2026-10-27); the auth-mode documentation follows the code (checked at Configure time, `ConflictsWith` in the schema, no plan-time validation); T18-3 signed off; build every type with a count above 0 (D2); data sources for Shopify-owned objects (D4); the cookie banner may be turned off on the dev store (S8-4).

## Open points and decisions needed

**Parked findings**
- `media_managed` residual: the private record only updates on write. Config going from `media_ids` unset to exactly the live list produces no Update, the record stays "unmanaged", and a later product destroy lets Shopify delete files that config now manages. Narrow (needs that change with no other diff). Fix: a ModifyPlan that forces an update when the record is false and config sets `media_ids`, or Delete detaching whenever state `media_ids` is non-empty. Cost until fixed: possible file loss in that edge case.
- The replace (`gift_card`) and import (no record) detach paths are covered by unit tests only.
- Cookie banner and US regional consent rows can't be reverted through the API (see side effects).

**Fix later (final review triage, one issue per group)**
- Product: `compare_at_price` can't be cleared; ModifyPlan matches state while write uses live; adopt-create removes variants without a plan warning; linked-option refusal only at apply; gift_card replace warning is wrong under `adopt_existing`; one spurious plan after a reverted admin edit of `description_html`; publications N+1 on lists above 10.
- Media: a file attached in the admin between read and `productSet` is detached and deleted if unused elsewhere (document it); raw `FILE_DOES_NOT_EXIST` message; an unknown GenericFile ID fails at apply after the detach; no acceptance step for `adopt_existing` with `media_ids`, no >20 media paging test.
- Publishing: shared `publishing.Apply` glue; stale `live` set on partial failure; misleading message on a nil `Publishable`; ordering untested; `write_publications` stated as fact without a `TODO(verify)`.
- Collection: `match_type` with empty membership and the S13-9 refusal fail at apply, not plan; import without conditions plans an update.
- File: replace warning wrong with `adopt_existing`; no hint when a GID is imported; unknown `content_type` plans a replace; fixed 5 s settle wait in Delete; plan paths without unit tests; `extensionMismatch` stricter than verified.
- Page, blog, segment: `FindPages` fetches 5 before the exact compare (blog uses 25); `removeDuplicate` drops userError messages and is untested; `nameRe` misses U+000B and U+0085; no list-limit acceptance test for segments.
- Discount: `contextFromAPI` returns `[]` for an empty list (generated config would fail `SizeAtLeast(1)`); the >250 items paging query never ran live; several acceptance gaps.
- Other: nil payload checks differ between packages; updates that change only a flag still call the API in product, collection and discount; "already gone" is detected three ways; `seo_*`/`template_suffix` accept `""`, which may read back null (unverified); two-line comments against the house rule; ACCESS_DENIED branches of the data sources lack fake-client tests.
- Steps 7-11, already on main: no test for destroying the primary web presence without `adopt_existing` (Pattern A safety path, first in line); shop locale Delete of an already gone locale fails; partial locale Create leaves an enabled locale without state; consent policy test nits and a missing one-way plan warning; the metafield standard-definition acceptance test always skips on the dev store.

**Open `TODO(verify)` in step 13 code (13)**
1. `product/variants.go:146`: how many staging/production products have linked options (Read, list and refusal are unit-tested only).
2. `product/media.go:24`: Video and Model3d GIDs as `productSet` files (only MediaImage and ExternalVideo were attached).
3. `product/resource.go:68`: whether any large product needs async `productSet` with `productOperation` polling.
4. `collection/logic.go:149`: whether a source with exclusions but no inclusion can exist.
5. `collection/resource.go:57`: whether selections of large collections come back only after the `collectionUpdate` job.
6. `collection/resource.go:437`: whether Shopify rejects writes to a source created by another app.
7. `page/resource.go:125`: search index lag after page creation (lookup would miss it, `pageCreate` reports `TAKEN`).
8. `blog/resource.go:119`: the same for blogs (the post-create check still catches it).
9. `file/logic.go:82`: that `sources[0].url` of a Video or Model3d is the public URL.
10. `discount/common.go:156`: whether reading segment and market contexts needs `read_customers` / `read_markets`.
11. `discount/common.go:187`: whether switching between segments and markets needs the old kind removed.
12. `discount/common.go:412`: which of several codes `DiscountCodeBasicInput.code` replaces.
13. `datasources/metaobject/data_source.go:87`: whether `$app:foo` types resolve to the caller's app prefix.

**Pre-import checks (first staging, then production)**
- Linked options: do the products use category-metafield linked options? They import read-only (S13-7) and can't be changed through the provider.
- Production's 7 discounts: which kinds? Basic ones are covered; free-shipping and BXGY ones need the deferred resources (S13-6).
- Web presence suffixes: the validator `^[a-z]{2,}$` may reject legacy suffixes on staging or production.
- Cookie banner: turning it off through `shopify_privacy_settings` is one-way; decide per store before applying.
- Also check collection sources, file extensions (`.jpeg` vs `.jpg` are stricter than verified) and the `shopify_file` list skipping files without a URL.

**Decisions and work still open**
- Flaky parallel `make testacc`: the web presence and shop locale acceptance tests both toggle locale `fr` and fail when packages run in parallel (they pass with `-p 1`). Pre-existing; fix after merge (serialise or use different locales).
- Market-driven shipping (roadmap): the store admin warns that some apps are incompatible with market-driven shipping, and Shopify deprecates the merchant-owned delivery profile APIs for it (opt-in from 2026-10-01, on for all shops 2027-07-01). Find which app triggers the warning and decide how `shopify_delivery_profile` moves to the Markets shipping APIs. Details in the implementation plan roadmap.
- Whether to build the roadmap types now (catalogs, free-shipping and BXGY discounts, staged file uploads, variant media, collection images) or wait for the first import (D6).
- Merge order: step 13 PR first, then the docs PR (stacked on it).

## Dev-store side effects

The dev store is disposable; these are changes that tests could not undo:
- An empty `LEGAL_NOTICE` and an empty `SHIPPING_POLICY` row: the API can't delete policies.
- Shopify's cookie banner is off (user-approved, one-way).
- Writing US consent regions materialised all 51 US regional rows (irreversible via the API); the data-sale opt-out page and privacy policy features were never touched.
- Checkout branding now reads empty containers instead of `null`.
- Acceptance runs used to leave an empty "Untitled Source" on the `frontpage` collection (Shopify adds one for draft products); `acctest.KeepFrontpage` now removes it, earlier leftovers were removed by hand.
- Probes (catalogs, publications, price lists, a market, products, frontpage edits) were removed or restored after each design step.
- The dev store token holds all 28 scopes of `app/shopify.app.toml` (D1 resolved 2026-10-26).

Staging and production: read-only discovery only, no mutation. The temporary discovery PR and branch in the provisioning repo are closed and deleted.
