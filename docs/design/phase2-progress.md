# SDD ledger — plan: docs/design/phase2-resources-plan.md

Temporary execution log, committed so it survives worktree cleanup. Removed in the wrap-up task.

## Pre-flight scan

| Rows | Produces → consumes | Finding |
|---|---|---|
| Task 1 → Task 2 | `cmd/discover` at a pushed SHA on `feat/discover-phase2` → `go run …@<sha>` in the temporary workflow | consistent |
| Task 2 → Task 4…N | discovery counts → ordered type list (D2) | consistent; list written here at the gate |
| Task 3 → Task 4…N | stacked branches → per-type commits | plan and ledger already live on `feat/phase2-resources`; see ruling P1 |
| Task 4…N ↔ each other | `internal/provider/provider.go`, `docs/bootstrapping.md`, resource model doc | sequential tasks, no parallel implementers: no conflict |
| Task 1 (self) | cherry-pick of `9df4e42` touches `go.mod`/`go.sum` (gqlparser indirect → direct, accepted earlier as R8) | consistent |
| Task 2 (self) | temporary workflow in `pflege-de/shopify-provisioning` runs with `<env>-plan` read-only creds | consistent with Global Constraints |
| Task 4…N (self) | acceptance tests need the scope per type on the dev store | blocked on D1 until the dev app is redeployed and reinstalled |
| Wrap-up (self) | removes this ledger, updates roadmap | consistent |

- Ruling P1: `feat/phase2-resources` (plan + ledger) is the bottom of the stack; step branches stack on it — keeps plan and ledger in every branch — costs one docs-only PR at the bottom.
- Ruling P2: the ledger lives here (committed) instead of the skill's git-ignored workspace; briefs, reports and review packages stay in `.superpowers/sdd/phase2-resources-plan/` (scratch) — the user asked that nothing gets lost; last run lost its ledger — costs ledger commits in the history.
- Ruling P3: Task 2 (temporary CI workflow in `shopify-provisioning`) is run by the controller, not a subagent, as last time (R5) — it touches another repo and GitHub environments — costs an unreviewed temp workflow, deleted afterwards.

## Decisions

- D2 (2026-10-23): build in-scope types with count > 0 on staging or production; the rest goes on the roadmap.
- D3 (2026-10-23): Shopify- and app-owned objects stay excluded from import.
- D1 (2026-10-26): resolved. Dev store token holds all 28 scopes of `app/shopify.app.toml`, `expires_in` 86399.
- D4/D5/D6 (2026-10-26): see the plan section "Discovery 2026-10-26 (phase 2)".

## Tasks
- Task 1: dispatched (base 2cca99c, branch feat/discover-phase2, worktree ~/src/worktrees/terraform-provider-shopify/discover)
- Task 1: implemented 1285792..0eacac2 (cherry-pick of 9df4e42 + phase 2 specs); review: spec ❌ (deprecated checkout roots), 2 Important, 3 Minor
- Ruling T1-1: discovery uses `checkoutAndAccountsConfiguration`, not the deprecated `checkoutProfiles`/`checkoutBranding` the plan text names — implementation-plan step 8 requires non-deprecated checkout APIs — costs one row's semantics if the replacement doesn't map 1:1.
- Task 1: fix round 1/5 dispatched (findings: deprecated checkout roots, partial counts on error, returnPolicyProfiles keys, consentPolicy label)
- Task 1: fix round 1/5 (5 addressed, 0 open; commits 0eacac2..00eaf32)
- Task 1: minor (deferred): `checkoutBranding` row counts non-null `branding` per configuration; page size 250 untested for `checkoutAndAccountsConfigurations`
- Task 1: complete (commits 2cca99c..00eaf32 on feat/discover-phase2, review clean)
- Task 2: run 1 (pflege-de/shopify-provisioning#13, run 40024477009, discover@00eaf32): raw tables in `docs/design/phase2-discovery/`. STOPPED at step 3: both `<env>-plan` apps still lack the new scopes — "no scope" for shopLocales, shopPolicies, privacySettings, checkoutAndAccountsConfigurations, publications, payment/delivery customizations, validations, cartTransforms, fulfillmentConstraintRules, pages, blogs, articles, discountNodes, files, segments, returnPolicyProfiles. Waiting for reinstall; then re-run the job on #13.
- Task 2 (interim, scopes already held): webPresences 2/3, catalogs 3/5, products 86/220, collections 14/17, fulfillmentServices manual + app-owned; 0 on both: urlRedirects, priceLists, carrierServices, sellingPlanGroups, custom delivery profiles. `metaobjects` 0 per type is untrustworthy (read_metaobjects missing, Shopify returns empty lists).
- Task 2: run 1 attempt 2 (2026-10-26, same run, discover@00eaf32): unchanged. The same 17 types are still "no scope" on both stores; only production products changed (220 → 214). `<env>-plan` SHOPIFY_CLIENT_ID matches the `-readonly` app TOMLs, which request all read scopes (deployed in run 40013074998). So the readonly installs still haven't granted the new scopes. Still STOPPED at step 3.
- Task 2: run 1 attempt 3 (2026-10-26, after provisioning#15 deploy 40303665033): still 17 "no scope" on both stores (real `ACCESS_DENIED`). Added a temporary "Granted scopes" step to discover.yml (91e892c, prints `scope`/`expires_in` only) to see what the readonly installs actually hold. Push pending (SSH key).
- Task 2: cause found (2026-10-26): app TOMLs lacked `[build] include_config_on_deploy = true`, so CI deploys re-released the dashboard config with the old scopes. Fix: pflege-de/shopify-provisioning#16 (and provider #20 for `app/*.toml`). After merge + release (+ install update if needed), re-run discovery.
- Task 2: attempt 4 (2026-10-26, after #16 deploy 40319109119; versions now list all scopes): still 17 "no scope" on both stores, so the installs haven't accepted the new scopes yet. Waiting for the readonly reinstall.
- Task 2: attempt 5 (2026-10-26, after reinstall): all rows ok, no "no scope". Results in plan section "Discovery 2026-10-26 (phase 2)".
- Task 2: gate. Ordered tasks (Phase 2 step order): 4 web presence (7); 5 shop locales, 6 shop policies, 7 privacy settings + consent policy, 8 checkout and accounts configuration, 9 checkout branding (8); 10 catalogs, publications settled in its design (11); 11 products, 12 collections, 13 pages, 14 blogs, 15 files, 16 discounts, 17 customer segments (13). Roadmap: everything else in the plan section. D4 open with default.
- Task 2: complete. #13 closed, branch deleted.
- D1 still blocks Phase B: merge provider #20 first (so `app/shopify.app.toml` has `include_config_on_deploy`), then link + deploy the dev app and reinstall it on the dev store.
- Task list revised for D4–D6 (in Phase 2 step order): 4 web presence (7); 5 shop locales, 6 shop policies, 7 privacy settings + consent policy, 8 checkout and accounts configuration, 9 checkout branding (8); 10 data sources `shopify_metafield_definition` + `shopify_metaobject_definition` (9); 11 data source `shopify_fulfillment_service` (10); 12 catalogs, 13 data source `shopify_publication` (11); 14 products, 15 collections, 16 pages, 17 blogs, 18 files, 19 discounts, 20 customer segments, 21 data source `shopify_metaobject` (13).
- Task 3: branch layout: `feat/phase2-07-web-presence` from `feat/phase2-resources` @ fa5cdc0 (main merged in at fa5cdc0). Plain git stacking; pushes over HTTPS (gh token) while the SSH key is unavailable.
- Task 3: complete
- Ruling T4-1: §9a (web presence) was designed and reviewed on 2026-10-23; the implementer only drops the stale "blocked on scope" note, re-checks the locale ⚠ with `read_locales` now granted, and continues to code without a controller stop — design is unchanged apart from that — costs a late design change if the locale check contradicts §9a.
- Task 4: dispatched (base 40d5910, branch feat/phase2-07-web-presence, model sonnet)
- Task 4: implemented 40d5910..d7a4b93 (DONE_WITH_CONCERNS: list code lands in the step 4 commit because resource.go shares `listPage`; acc tests skip unless TF_ACC; worktree has no `.envrc`, run via `direnv exec <main checkout>`; TODO(verify) delete of a presence still linked to a market). Review dispatched (opus).
- Task 4: review: spec ✅, quality Approved, 0 Critical/Important.
- Task 4: minor (deferred): model §9a line 422 still says the primary-delete userError text was not captured (contradicts line 431)
- Task 4: minor (deferred): no test for destroying the primary presence without `adopt_existing` (state-only removal on SHOP_MUST_HAVE_PRIMARY_DOMAIN_WEB_PRESENCE, resource.go:260) — Pattern A safety path, triage before merge
- Task 4: minor (deferred): Review Focus 2 step uses `PlanOnly` instead of `plancheck.ExpectEmptyPlan()` (resource_test.go:220)
- Task 4: minor (deferred): test helpers: `findBySuffix` nil deref (resource_test.go:208), `publishLocale` ignores userErrors (83-91), first-page-only lookup (96-108)
- Task 4: minor (deferred): primary-domain error drops other userErrors in the same response (resource.go:260-266)
- Task 4: ⚠️ resolved: `make fmt lint test` + `make generate` re-run by controller, clean, no diff. Review Focus 4 list-path generate-config: same identity-import workaround as the earlier list resources (cross-task, known). Suffix validator `^[a-z]{2,}$` vs legacy suffixes on staging/production: check at the first import.
- Task 4: complete (commits 40d5910..d7a4b93, review clean)
- Ruling S8-1: step 8 design (Step 1 of Tasks 5–9) runs as one design-only dispatch (opus) for all five types, reviewed by the controller before any implementer starts — the five are same-shaped shop-level singletons and must share Pattern A semantics — costs one larger re-design if the batch gets a shared assumption wrong.
- Step 8: branch feat/phase2-08-shop-settings from feat/phase2-07-web-presence @ 690ca52; design dispatch running.
- Step 8 design: 334ecf3..d329be9 (§9b–9f), report step8-design-report.md. Controller review: approved with the rulings below.
- Ruling S8-2: checkout branding (Task 9) is merged into Task 8 `shopify_checkout_and_accounts_configuration` with a JSON `branding` attribute (§9e/§9f) — in 2026-10 branding is a field of the configuration and its only writable content, there is no separate object to manage — costs a split later if the task proves too big (fallback in the design report).
- Ruling S8-3: `shopify_shop_locale.id` is the locale code and `shopify_privacy_settings.id` the shop GID, deviating from "id = GID" — `ShopLocale` and `PrivacySettings` have no GID of their own — costs an identity schema change if Shopify adds IDs later.
- Ruling S8-4: Task 7 (privacy settings + consent policy) runs with write steps skipped until the dev store's Shopify cookie banner is off; disabling it is one-way through the API and is the user's call (asked) — costs untested write paths until then.
- Step 8 open: dev store keeps an empty LEGAL_NOTICE policy row (API can't delete policies); branding on the dev store now reads empty containers instead of null.
- Task 5 (shop locale): dispatched (base e25c80a, model sonnet)
- Task 5: implemented e25c80a..e5f504d (DONE_WITH_CONCERNS: 859ca7c alone fails lint, unused `localeRe` used by the next commit; acc tests not run red first; TODO(verify) disabling a locale a web presence uses; `published = false` on primary relies on Shopify's error). Review dispatched (sonnet).
- Task 5: review: spec ✅, quality Approved, 0 Critical/Important.
- Task 5: minor (deferred): primary-locale safety test reaches adopt=false via update, not via import-then-destroy (resource_test.go adoptPrimary)
- Task 5: minor (deferred): partial Create (enable ok, publish fails) leaves an enabled locale without state; retry needs adopt_existing — same in sibling resources
- Task 5: minor (deferred): Delete of a locale already gone fails on "The locale doesn't exist." instead of succeeding
- Task 5: minor (deferred): unchecked nil on mutation payloads (resource.go ShopLocaleEnable/Disable) — same in sibling resources
- Task 5: ⚠️ resolved: controller re-ran `make fmt lint test` + `make generate`: clean, no diff. List-path generate-config: same identity-import workaround (cross-task, known).
- Task 5: complete (commits e25c80a..e5f504d, review clean)
- Task 6 (shop policy): dispatched (base 5851c8d, model sonnet)
- Task 6: implemented 5851c8d..4a48d66 (DONE_WITH_CONCERNS minor: dev store gained an empty SHIPPING_POLICY row the API can't remove; TODO(verify) auto-managed PRIVACY_POLICY write, TOO_BIG path, whitespace-only body). Review dispatched (sonnet).
- Task 6: review: spec ✅, quality Approved, 0 Critical/Important.
- Task 6: minor (deferred): nil deref on `ShopPolicyUpdate`/`.ShopPolicy` payload (resource.go) — same as shoplocale
- Task 6: minor (deferred): no unit test for TOO_BIG → `body` userError mapping (field path assumed `["shopPolicy","body"]`)
- Task 6: minor (deferred): whitespace-only body passes the validator, untested against the API
- Task 6: minor (deferred): `ownPolicy` comment says "restores pre-test body" but writes ""; list acc test doesn't assert empty rows are skipped
- Task 6: ⚠️ resolved: controller re-ran `make fmt lint test` + `make generate`: see ledger commit. Dev store now has empty LEGAL_NOTICE and SHIPPING_POLICY rows (not removable via API).
- Task 6: complete (commits 5851c8d..4a48d66, review clean)
- Task 7 (privacy settings + consent policy): dispatched (base 2e77eb6, model sonnet), write steps skipped per S8-4
- S8-4 update (2026-10-26): user approved turning off Shopify's cookie banner on the dev store. Implementer told: `privacyFeaturesDisable` only with COOKIE_BANNER, dev store only, record before/after; consent writes then run with cleanup; DATA_SALE_OPT_OUT_PAGE and PRIVACY_POLICY stay forbidden.
- Task 7: implemented 2e77eb6..efa6ce9 (DONE_WITH_CONCERNS: COOKIE_BANNER disabled once on the dev store via a client probe, the resource's disable path is covered by fake-API unit tests only; writing US/USCA materialised all 51 US regional rows (irreversible via API); dso/pp untouched; TODO(verify) consentPolicyRegions check, CA materialisation). Review dispatched (sonnet).
- Task 7: review: spec ❌ (1 Important, plan-mandated: regional-write side effect undocumented for users), 6 Minor.
- Ruling T7-1: the resource's COOKIE_BANNER disable path stays covered by fake-API unit tests plus one identical real probe call, not by acceptance — the banner can't be re-enabled through the API, so a real acceptance run is impossible on this dev store — costs an untested real-API path if the resource's request differs from the probe.
- Task 7: minor (deferred): acc-test regex alternation makes the region_code half pointless (consent_policy_test.go:157)
- Task 7: minor (deferred): CheckDestroy checks IS, not the finally destroyed US/USCA; comment misleading (consent_policy_test.go:122)
- Task 7: minor (deferred): AT config uses boolOr(live); a null live value would trigger a write to AT (consent_policy_test.go:58-60)
- Task 7: minor (deferred): regionRe accepts foreign regions (DE+USCA); docs don't say country-level US/CA rows can't be written (logic.go:82)
- Task 7: minor (deferred): multi-line comments against house rule (consent_policy.go:241,282; helpers_test.go:37; consent_policy_test.go:55; privacy_settings_test.go:84)
- Task 7: minor (deferred): optional plan-time warning when a plan turns a privacy feature off (one-way)
- Task 7: fix round 1/5 dispatched (resume implementer; finding: document regional-write side effect)
- Task 7: fix round 1/5 (1 addressed, 0 open — regional-write side effect now in resource, region_code and list descriptions; commits efa6ce9..5a99542). Controller re-ran `make fmt lint test` (0 issues) + `make generate`: clean.
- Task 7: minor (deferred): regional side-effect wording claims all countries; only US verified (CA TODO(verify))
- Task 7: complete (commits 2e77eb6..5a99542, review clean after 1 fix round)
- Task 8 (checkout and accounts configuration + branding JSON): dispatched (base 616af0c, model opus — custom semantic-equality type and JSON projection need judgment)
- Task 8: implemented 616af0c..a90527e (DONE_WITH_CONCERNS: whitespace/key-order of hand-written branding JSON plans one no-op update (semantic equality not applied at plan time; jsonencode avoids it); import into a partial-branding config plans one harmless update; write-only paths unit-tested only; TODO(verify) branding scope, images/fonts read-back, overrides). Review dispatched (opus).
- Task 8: review: spec ✅, quality Approved, 0 Critical/Important (opus).
- Task 8: ⚠️ resolved: controller probed `checkoutAndAccountsConfiguration(id:)` with an unknown GID on the dev store: returns `null`, no error, so `getByID`'s gone detection holds. `make fmt lint test` (0 issues) + `make generate`: clean.
- Task 8: minor (deferred): §9f still says "key order and whitespace never plan" next to the finding that hand-written JSON plans once
- Task 8: minor (deferred): `branding` description "colours compare case-insensitively" true for drift only; editing config hex case plans one update (resource.go:106)
- Task 8: minor (deferred): `follows_published` edge case (dropping `name` alone keeps name fallback) only in §9f, not in the `name` description
- Task 8: minor (deferred): 3-line comment in the GraphQL file before `branding {`
- Task 8: minor (deferred): `prune` in helpers_test.go mirrors `normalise` (independent oracle; add a comment)
- Task 8: minor (deferred): a configuration named `published` can't be imported by name (undocumented)
- Task 8: minor (deferred): transport/throttle errors attached to `branding` attribute (resource.go:307)
- Task 8: minor (deferred): test style `!= true` (brandingtype_test.go:35)
- Task 8: complete (commits 616af0c..a90527e, review clean)
- Task 9: merged into Task 8 (ruling S8-2), nothing separate to do.
- Step 8 complete on feat/phase2-08-shop-settings.
- Ruling S9-1: Task 10 data sources mirror the existing resources' schemas as read-only attributes; the implementer writes the model section and code in one dispatch, and the task review checks the section — a data source has no identity, import, list or Pattern A/B, so there are no open design questions to settle before code — costs a rework if the reviewer rejects the schema shape.
- Step 9: branch feat/phase2-09-definition-data-sources from feat/phase2-08-shop-settings @ 54e56c7. Task 10 brief written by controller (task-10-brief.md; data sources don't fit the resource template).
- Task 10: dispatched (base 54e56c7, model sonnet)
- Task 10: implemented 54e56c7..dd81cf4 (DONE; refactor exports Definition/SetFromAPI from both resource packages, resource acc suites re-run green; no shopify.* metafield defs on dev store → that test skips; TODO(verify) owner-type scopes, $app: type resolution). Review dispatched (sonnet).
- Task 10: review: spec ✅, quality Approved, 0 Critical/Important. Controller re-ran `make fmt lint test` + `make generate`: clean.
- Task 10: minor (deferred): no non-acceptance test of the null-result / ACCESS_DENIED branches (needs a fake client)
- Task 10: minor (deferred): 9g TODO(verify) on `$app:` resolution covers metaobject types only, not metafield namespaces
- Task 10: minor (deferred): metafield standard-definition acc test hardwired to PRODUCT/shopify/material, always skips on the dev store; double PreCheck
- Task 10: minor (deferred): no acceptance step feeding the data source's `field` list into a resource via `dynamic "field"`
- Task 10: complete (commits 54e56c7..dd81cf4, review clean)
- Step 9 complete.
- CORRECTION: Task 10 is NOT complete. The lines above ("Task 10: complete", "Step 9 complete") were committed before the controller read the check output. `make test` without SHOPIFY_* env fails: the shopifyStandard acc tests call `acctest.PreCheck` outside `resource.Test`, so they fail instead of skipping when TF_ACC is unset (breaks CI). Treated as Important; fix round 1/5 dispatched (resume implementer). Branch feat/phase2-10-… deleted locally (never pushed).
- Task 10: fix round 1/5 (2 addressed, 0 open — TF_ACC skip before PreCheck/API in shopifyStandard tests, $app: TODO for metafield namespaces; commits 21bbaa2..a9fb025). Controller re-ran `make fmt lint test` with SHOPIFY_*/TF_ACC unset: exit 0, 0 lint issues; `make generate`: no diff.
- Task 10: minor (deferred): `accSetup` test helper copied into ~7 packages; candidate for internal/acctest
- Task 10: complete (commits 54e56c7..a9fb025, review clean after 1 fix round)
- Step 9 complete (for real).
- Step 10: branch feat/phase2-10-fulfillment-service-data-source from step 9 @ 3f537c1. Task 11 brief written by controller.
- Task 11 (fulfillment service data source): dispatched (base 3f537c1, model sonnet)
- Task 11: implemented 3f537c1..761ceb7 (DONE; schema differs from brief: no permitsSkuSharing, manual id is gid://shopify/FulfillmentService/manual; dev store has only manual; TODO(verify) location scope for the list). Controller re-ran make fmt lint test with SHOPIFY_*/TF_ACC unset: exit 0, 0 issues; make generate: no diff. Review dispatched (sonnet).
- Task 11: review: spec ✅, quality Approved, 0 Critical/Important. ⚠️ app toml scopes: resolved, dev token holds write_fulfillments (D1 check).
- Task 11: minor (deferred): `manual` acc test asserts only Set on booleans/service_name; assert values and that callback_url is null (data_source_test.go:724)
- Task 11: minor (deferred): no unit test of ACCESS_DENIED branch (same as sibling data sources)
- Task 11: complete (commits 3f537c1..761ceb7, review clean)
- Step 10 complete.
- Ruling S11-1: step 11 design (catalogs resource + publication data source) runs as one design-only dispatch (opus), controller reviews before implementation — catalogs have three subtypes (market, app, company location) and link to publications and price lists, which needs real design — costs one more dispatch.
- Step 11: branch feat/phase2-11-catalogs from step 10 @ e9c0053; design dispatch running (opus).
- Step 11 design: dd25a6e..b363080 (§9i shopify_catalog, §9j shopify_publication data source), report step11-design-report.md. Dev store restored (probe catalogs, publications, price lists, market removed).
- Ruling S11-2: `shopify_catalog` (Task 12) goes on the roadmap with its §9i design kept — discovery's catalogs are all channel app catalogs (excluded by D3) and market catalogs are 0 on staging/production, so D5's "likely defaults" premise doesn't hold and D6 applies — costs a later task if the user wants catalogs now (user informed, reversible).
- Ruling S11-3: `shopify_publication` keys on the deprecated `Publication.name` — publications have no handle and `Catalog.title` differs per store; a later API removal surfaces as a compile error — costs a migration when the field goes.
- Task 12: deferred to roadmap (S11-2).
- Task 13 (publication data source): dispatched (base 5ec58c0, model sonnet)
- Task 13: implemented 5ec58c0..61aa4c2 (DONE_WITH_CONCERNS minor: lint quiet on deprecated Publication.name, S11-3 holds; step-2 commit alone leaves a generate diff; §9j TODO(verify)s open; duplicate path unit-tested only). Controller re-ran make fmt lint test (SHOPIFY_*/TF_ACC unset): exit 0, 0 issues; make generate: no diff. Review dispatched (sonnet).
- Task 13: review: spec ✅, quality Approved, 0 Critical/Important.
- Task 13: minor (deferred): dead `&& name != ""` (logic.go:846); no multi-page unit test; duplicate names in `available:`; no LengthAtLeast(1) validator on `name`; provider registration order; acc test pins dev-store `auto_publish = false`
- Task 13: complete (commits 5ec58c0..61aa4c2, review clean)
- Step 11 complete (Task 12 deferred to roadmap, S11-2).
- Ruling S13-1: step 13 design runs in two sequential design-only dispatches (opus), each reviewed by the controller before implementation: (a) products + collections, (b) pages, blogs, files, discounts, customer segments, metaobject data source — products alone are large (variants, options, media), and parallel design dispatches would race on the same model doc — costs one extra dispatch.
- Step 13: branch feat/phase2-13-content from step 11 @ e63ae96; design (a) products + collections dispatched (opus).
- Step 13 design (a): 00e251d..89bbbd7 (§9k product with sub-sections 9k.1–9k.4, §9l collection), report step13a-design-report.md. Controller review: approved. Dev store restored (probe products deleted, frontpage restored).
- Ruling S13-2: products split into sub-tasks 14a core (§9k.1), 14b options + variants (§9k.2), 14c publishing on product and collection (§9k.3, after Task 15), 14d product media (§9k.4, after files Task 18); collection is Task 15 — one task can't hold productSet + variants + publishing + media, and media needs file IDs — costs more task overhead.
- Ruling S13-3: collections v1 manage exactly one membership source (2026-10 replaced ruleSet with sources); exclusions, sub-collections and shared sources stay unmanaged — the API change makes multi-source modelling a guess today — costs a schema extension later.
- Step 13 design (b) (pages, blogs, files, discounts, customer segments, metaobject data source): dispatched (opus).
