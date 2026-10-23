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
- D1: open. Dev store token has the six original scopes only.

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
