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
