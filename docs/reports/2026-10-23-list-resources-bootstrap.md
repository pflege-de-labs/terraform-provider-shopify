# Report: list resources and import bootstrap (2026-10-23)

Plan: [docs/design/list-resources-plan.md](../design/list-resources-plan.md) (on branch `feat/list-resources`).
Executed subagent-driven while the owner was away. Nothing was merged or applied.

## PRs

| PR | Content | State |
|---|---|---|
| [pflege-de-labs/terraform-provider-shopify#16](https://github.com/pflege-de-labs/terraform-provider-shopify/pull/16) | Resource identity (GID) and list resources for all 6 types, shared helpers in `internal/patterns`, fixes, docs | Unit and dev-store acceptance tests pass; final review clean after fixes |
| [pflege-de/shopify-provisioning#10](https://github.com/pflege-de/shopify-provisioning/pull/10) | `terraform-query.yml`, `<env>/discover.tfquery.hcl`, generated config with `import` blocks | PR plan: staging 11 to import, production 12 to import, 0 to add/change/destroy |

Merge order:
1. [shopify-provisioning#6](https://github.com/pflege-de/shopify-provisioning/pull/6) (shop data source, production in the plan matrix).
2. Provider #16.
3. In #10, point `SHOPIFY_PROVIDER_REF` at #16's merge commit (currently #16's head `705e5bc`), then merge #10. This imports staging only.
4. Production imports apply once `production` is added to the apply matrix. That needs an explicit go-ahead: production is the live shop.

## Discovery results

Read-only apps, `cmd/discover` on branch `feat/discover` (temporary, never merged).

| Type | Staging | Production | Outcome |
|---|---|---|---|
| Metafield definitions, PRODUCT | 5 | 25 | 5 imported per store; 20 Shopify category definitions on production excluded |
| Metafield definitions, SHOP | 0 | 1 | Excluded: owned by the Checkout Blocks app |
| Metaobject definitions | 0 | 26 | All Shopify category definitions (`shopify--*`): excluded |
| Menus | 3 | 4 | Imported |
| Locations | 1 | 1 | Imported |
| Markets | 1 | 1 | Imported |
| Delivery profile (general) | 1 | 1 | Imported |
| Web presences | 2 | 3 | In scope, not supported yet: blocked on scope (decision 1) |
| Fulfillment services | 1 | 2 | Out of scope: Shopify built-in or app-owned |
| Shop locales, shop policies, segments, return policy profiles | ? | ? | Not countable: read-only app lacks the scopes |

## Decisions needed

1. **Web presences:** reading their locales needs `read_locales` (or `read_markets_home`). Adding it means redeploying the apps and reinstalling on the dev, staging and production stores. The design is in resource model §9a; the resource gets built once the scope is granted.
2. **Shopify category definitions:** 46 on production (26 `shopify--*` metaobject definitions, 20 `shopify.*` metafield definitions) are not imported. Shopify creates them when a product category is set. Should they be managed?
3. **Shop locales and policies:** phase-3 candidates in the docs. Counting them needs `read_locales` and `read_legal_policies`.
4. **`go.mod`:** `vektah/gqlparser/v2` moved from indirect to direct. Same version, no new module, but CLAUDE.md asks for confirmation of dependency changes.
5. **Unscoped metafield owner types:** listing returns an empty list instead of an error when the app can't read an owner type. Documented, but definitions can be missed silently.
6. **Parallel location creation:** two `locationAdd` calls at once can fail with "try again later". The provider doesn't retry it (pre-existing). Options: a bounded retry, or documenting `-parallelism=1`.

## Rulings made during execution

| # | Ruling | Cost if wrong |
|---|---|---|
| R1 | Discovery command lives only on branch `feat/discover`, never merged | Branch can be deleted |
| R2 | List-resource examples at `examples/list-resources/<type>/` | None: matches the tfplugindocs path |
| R3 | One provider branch and PR (#16) instead of one per resource | Bigger review |
| R4 | Import stops at an open PR; no merge, no apply | One more manual step |
| R5 | Temporary discovery CI job set up by the controller, not an agent | Unreviewed temp workflow, already deleted |
| R6 | `access.admin` of metafield/metaobject definitions is null when Shopify reports a value that can't be set through the API (`PRIVATE`, `PUBLIC_READ`, `PUBLIC_READ_WRITE`) | That setting isn't managed for older definitions |
| R7 | Missing-scope error path kept, though Shopify returns an empty list (decision 5) | Definitions can be missed silently |
| R8 | `gqlparser` indirect → direct accepted (decision 4) | — |
| R9 | One shared pagination helper instead of a copy per resource | — |
| R10 | Plan modifier keeps a null `access.admin` stable, so it doesn't show "(known after apply)" on every plan. Not reproducible on the dev store; precautionary | One extra helper |
| R11 | Parallel location retry left for later (decision 6) | Multi-location applies may flake |
| R12 | Web presence work before the docs task | None |
| R13 | Web presence resource parked; only the design section committed | One doc section to revise |
| R14 | `MutableIdentity` on the 5 resources that re-adopt by natural key after an out-of-band recreate (CLAUDE.md requires that fallback) | Identity isn't a stable handle across out-of-band recreation |
| R15 | #10 built on top of #6 | #6 must be merged first |
| R16 | Shopify category definitions excluded from import (decision 2) | They stay unmanaged, as today |

## Other notes

- While designing web presences, an agent sent a delete for the dev store's primary web presence by mistake. Shopify refused it; nothing changed.
- `terraform query` needs Terraform ≥ 1.14; identity-based import needs ≥ 1.12. The provider's CI matrix includes 1.13, so query tests skip there.
- Deferred minor review findings and the full execution log are in the ledger: `~/src/worktrees/terraform-provider-shopify/list-resources/.superpowers/sdd/list-resources-plan/progress.md` (git-ignored, kept until the PRs are merged).
- Worktrees still present: `~/src/worktrees/terraform-provider-shopify/list-resources`, `~/src/worktrees/terraform-provider-shopify/discover`, `~/src/shopify-provisioning-import`, `~/src/shopify-provisioning-shop-data`.
