# Phase 2 Resources: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** In progress, D2/D3 decided, D1 pending · **Owner:** Jens Hausherr · **Last updated:** 2026-10-23
**Related:** [[implementation-plan]] (Phase 2 table), [[terraform-provider-resource-model]], [[list-resources-plan]], [bootstrapping](../bootstrapping.md)

**Goal:** Find out, with the wider scopes, which store objects exist on staging and production that the provider can't manage yet, and implement those resource types (resource, identity, list resource, docs, acceptance tests).

**Architecture:** A revived discovery command counts every Phase 2 type with the read-only `<env>-plan` apps in CI. The counts pick and order the resource tasks. Each resource type then follows the existing template: a resource model section first, then one package under `internal/resources/<name>/` with resource, identity and list resource, built and reviewed by subagents, one task at a time.

**Tech Stack:** Go, terraform-plugin-framework v1.19.0, terraform-plugin-testing v1.16.0, genqlient against `internal/client/schema/admin-2026-10.graphql`.

**Spec:** CLAUDE.md (scope, conventions, safety), [[implementation-plan]] Phase 2 (steps 7–13 and their working rules), [[terraform-provider-resource-model]] §2 (identity, import, patterns) and §9a (web presence design), [bootstrapping](../bootstrapping.md) (scope per type).

## Global Constraints

- Admin API version stays **2026-10**. No new Go dependencies.
- Out of scope per CLAUDE.md is not implemented even if discovery finds it: app config, extensions, Functions themselves, webhooks, web pixels, script tags, themes, transactional records (orders, returns, fulfillment orders, gift cards, store credit, payments), store creation, plan, billing, domains, staff, anything needing Shopify approval.
- `id` = GID; identity `{ id = <GID> }`; `terraform import` takes a natural key, never a GID; Read by GID, fallback natural key.
- Every resource: identity, list resource, import from the start. Listed objects must pass the resource's validators and plan as import-only.
- Optional attributes left unset are not managed: optional mutation inputs use `# @genqlient(omitempty: true)`.
- Pattern A for once-per-store objects; Pattern B (`allow_data_deletion`, default `false`) wherever Delete or replace destroys data.
- Volatile types say so in the resource description ("poor fit for Terraform").
- Acceptance tests only against the dev store (`direnv exec .`). Discovery and CI runs on staging/production only with the read-only `<env>-plan` apps. No apply, no mutation outside the dev store.
- Never print tokens; discovery output holds counts, handles and names only, never values.
- Each task: `make fmt lint test` passes and `make generate` leaves no diff.

## Keeping intermediate results

The last run lost its ledger because it was git-ignored inside a worktree. This time:

- Work happens in the worktree `~/src/worktrees/terraform-provider-shopify/phase2` on branch `feat/phase2-resources` (and stacked branches per step, see Task 3).
- The ledger is `docs/design/phase2-progress.md`, **committed**: the controller appends task status, review verdicts, deferred findings and rulings after every task and commits it with the task's commit.
- Implementers commit after every green step (`wip(<pkg>): …` allowed); every commit is pushed right away. Temporary commits are not squashed: the PR is merged with a merge commit, as before.
- The ledger and this plan's "Discovery" section are removed or condensed into a report under `docs/reports/` in the last task.

## Decisions needed before execution

- **D1: dev store scopes.** The dev store token has only the six original scopes (checked 2026-10-23: `write_locations, write_markets, write_metaobject_definitions, write_online_store_navigation, write_products, write_shipping`). Acceptance tests for new types need the full set from `app/shopify.app.toml`: deploy it to the dev app and reinstall on the dev store ([[dev-store-scope-reinstall]]). Without it, only discovery (Phase A) can run.
- **D2: which types to build. Decided 2026-10-23:** every in-scope type with a count > 0 on staging or production, in Phase 2 step order. The rest stays on the roadmap: the wrap-up task marks them in the Phase 2 table of [[implementation-plan]].
- **D3: Shopify-owned and app-owned objects. Decided 2026-10-23:** category definitions, `$app:` definitions and fulfillment services of other apps are excluded from import as before; the list resource still lists them.

## Review Focus

1. **Singleton objects (Pattern A):** shop policies, privacy settings, checkout branding, checkout and accounts configuration exist once per store. `terraform destroy` must only remove them from state with a warning, never blank them. Owned by each singleton task, acceptance step "destroy leaves the object unchanged" (read it back via the client after `CheckDestroy`).
2. **Admin edits to unmanaged attributes:** a config that sets one attribute must not reset the others. Owned by each task: acceptance step that sets attribute A, changes B through the client, then expects an empty plan (`plancheck.ExpectEmptyPlan()`).
3. **Missing scope:** Shopify returns an empty list or `ACCESS_DENIED`. List resources and the discovery command must report `ACCESS_DENIED` as "no scope", not crash; empty lists stay ambiguous (documented). Owned by Task 1 (`TestClassify`) and each list resource's description.
4. **Generated config round-trip:** `terraform query -generate-config-out` then `plan` shows only imports. Owned by each task's query acceptance test (same pattern as [[list-resources-plan]] Task 4–9 step 5).
5. **App-owned objects:** carrier services, fulfillment services and Function-backed customizations belong to an app; updating one owned by another app fails. The resource must report Shopify's userError clearly, and the list resource marks owner app in `DisplayName`. Owned by the tasks for those types.

---

## Phase A: Discovery

### Task 1: Revive and extend the discovery command

**Files:**
- Restore from unreachable commit `9df4e42` (`feat: add temporary discovery command`, branch `feat/discover`, never merged): `cmd/discover/main.go`, `cmd/discover/main_test.go`, the `go.mod`/`go.sum` lines it changed.
- Branch: `feat/discover-phase2` from `main` (temporary, never merged; pushed so `go run …@<sha>` works).

**Interfaces:**
- Consumes: `client.New(client.Config)`, `(*client.Client).MakeRequest`.
- Produces: `go run github.com/pflege-de-labs/terraform-provider-shopify/cmd/discover@<sha>`, env `SHOPIFY_STORE_DOMAIN`, `SHOPIFY_CLIENT_ID`, `SHOPIFY_CLIENT_SECRET`; Markdown table on stdout: `Type | Filter | Count | Keys | Status`.

- [ ] **Step 1:** `git cherry-pick 9df4e42` onto `feat/discover-phase2`; `go test ./cmd/discover/` passes.
- [ ] **Step 2: Failing test** `TestSpecsCoverPhase2`: `specs()` contains one entry per type name below (assert on the `typ` set).
- [ ] **Step 3: Add specs** (count; keys only where a natural key exists). Root fields checked in the pinned schema:
  - step 7: `webPresences`
  - step 8: `shopLocales` (key: locale), `shop.shopPolicies` (key: type), `privacySettings`, `consentPolicy`, `checkoutProfiles` (key: name), `checkoutBranding` per profile (count of non-null)
  - step 9: `metafieldDefinitions` for all `MetafieldOwnerType` values (already present)
  - step 10: `deliveryProfiles` split general/custom (present), `carrierServices` (key: name), `shop.fulfillmentServices` (key: handle)
  - step 11: `urlRedirects`, `publications` (key: name), `catalogs`, `priceLists` (key: name), `sellingPlanGroups`
  - step 12: `paymentCustomizations`, `deliveryCustomizations`, `validations`, `cartTransforms`, `fulfillmentConstraintRules` (key: title / function id)
  - step 13: `products`, `collections`, `pages`, `blogs`, `articles`, `discountNodes`, `metaobjects` (per definition type), `files`, `segments` (count only for all)
  - also count `returnPolicyProfiles` (scope check only).
- [ ] **Step 4:** Tests pass; `direnv exec . go run ./cmd/discover` on the dev store prints a plausible table (most rows "no scope" until D1).
- [ ] **Step 5: Commit** `feat(discover): cover phase 2 types`, push the branch.

### Task 2: Run discovery on staging and production

**Files:** temporary workflow `.github/workflows/discover.yml` on a draft PR in `pflege-de/shopify-provisioning` (deleted afterwards).

- [ ] **Step 1:** Workflow on `pull_request`, matrix `[staging, production]`, `environment: <env>-plan`, `actions/setup-go`, `go run …/cmd/discover@<sha> >> "$GITHUB_STEP_SUMMARY"`. No AWS role, no Terraform. `actionlint` passes.
- [ ] **Step 2:** Draft PR; both jobs pass; copy both summaries.
- [ ] **Step 3:** Record results as section "Discovery 2026-10-23 (phase 2)" at the end of this file. Rows with "no scope" mean the readonly app wasn't reinstalled with the new scopes: stop and report.
- [ ] **Step 4: Gate.** Apply D2 to the counts; write the resulting ordered task list (Tasks 4…N) into the ledger. Close the draft PR, delete its branch. **Commit** `docs: record phase 2 discovery`.

## Phase B: Resources

### Task 3: Branch layout

- [ ] One branch per Phase 2 step, stacked on the previous one: `feat/phase2-07-web-presence` from `main`, `feat/phase2-08-shop-settings` from that, and so on (`gh stack` if available). One PR per step, opened when its last task passes review. Ledger and plan travel on the bottom branch and are carried up.

### Task 4…N: One task per chosen resource type

Order: Phase 2 step order, then the order types appear in the step's row. Web presence (§9a, design done) is first if D2 picks it.

**Files (per type `<pkg>`):**
- Modify: `docs/design/terraform-provider-resource-model.md` (new section before §10)
- Create: `internal/client/queries/<pkg>.graphql`, `internal/resources/<pkg>/resource.go`, `list.go`, `resource_test.go`, pure-logic file + `_test.go` where diffing or mapping exists
- Modify: `internal/provider/provider.go` (`Resources`, `ListResources`), `docs/bootstrapping.md` (status ✅), `examples/resources/<type>/`, `examples/list-resources/<type>/`, `templates/` if the type has a template
- Generated: `internal/client/generated.go`, `docs/`

**Interfaces:**
- Consumes: `patterns.IDIdentitySchema`, `patterns.Paginate`, `patterns.SplitImportKey`, `patterns.AllowDataDeletionAttribute`, `patterns.GuardDataDeletion`, `patterns.UseStateForUnknownIncludingNull`, `patterns.Description`, `client.UserErrorDiags`; existing resources as reference (`menu` for Pattern A with handle key, `metaobjectdefinition` for Pattern B and nested diffing, `location` for name keys).
- Produces: resource type `shopify_<type>`, list resource of the same name, identity `{ id }`.

- [ ] **Step 1: Design.** Resource model section: schema (attribute, type, required/optional/computed, RequiresReplace), import key, Pattern A/B, fit (good/volatile), scope, every ⚠ checked against `admin-2026-10.graphql` or left as `TODO(verify)`. Controller reviews the section before code. **Commit** `docs(model): design shopify_<type>`.
- [ ] **Step 2: Unit tests first** for pure logic (diffing, input mapping, import-key parsing). Fail, implement, pass. **Commit.**
- [ ] **Step 3: Failing acceptance test** `TestAcc<Type>_basic`: create, update, import by natural key (`ImportStateVerify`), identity (`statecheck.ExpectIdentity` GID regex `^gid://shopify/<Kind>/`), identity import step, Review Focus 2 step; plus Pattern A or B steps.
- [ ] **Step 4: Implement** resource (CRUD, ImportState with natural key and identity paths, userErrors via `client.UserErrorDiags`). Acceptance test passes on the dev store. **Commit** `feat(<pkg>): add shopify_<type>`.
- [ ] **Step 5: List resource** and `TestAccList<Type>` (`SkipBelow 1.14.0`, `ExpectLengthAtLeast`, `ExpectIdentity`, generate-config round-trip). **Commit** `feat(<pkg>): add list resource`.
- [ ] **Step 6: Docs.** Examples, description (volatile note), `make generate`; `make fmt lint test` passes; no diff after generate. **Commit** `docs(<pkg>): …`.
- [ ] **Step 7: Review** by a fresh reviewer subagent (spec compliance against the model section and Global Constraints, then code quality). Fix, re-review until clean. Ledger update, commit, push.

Type-specific notes the design step must settle:
- `shopify_metafield_definition` for more owner types (step 9): extends the existing resource; per owner type find the read/write scope on the dev store and record it in bootstrapping's ⚠ note.
- Custom delivery profiles (step 10): extend `shopify_delivery_profile` with a `default`-less mode or a new type; decide in the model, reuse `diff.go`.
- Function-backed types (step 12): `function_id` is a plain string; the acceptance test skips when the dev store has no Function installed (`t.Skip` with a reason).
- Content types (step 13): only those D2 picks; each gets its own design and may split into more tasks.

### Task N+1: Wrap-up

- [ ] Final whole-branch review by a fresh reviewer on the most capable model.
- [ ] Report in `docs/reports/2026-10-<dd>-phase2-resources.md`: PRs, types built, types skipped and why, rulings, decisions needed. Remove the ledger, update CLAUDE.md "Implemented" and the Phase 2 table in [[implementation-plan]]: built types ✅, the rest marked as roadmap with their discovery counts. **Commit**, push, open the remaining PRs. No merge, no apply on staging or production.
