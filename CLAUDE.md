# terraform-provider-shopify

Custom Terraform provider for **Shopify store objects** that the Admin GraphQL API can manage: configuration and schema first, content too (marked as volatile).
Repo started from HashiCorp's `terraform-provider-scaffolding-framework` template.

## Read first

- `docs/design/shopify-infra-automation-approach.md`: why a hybrid approach, what is in/out of scope, pitfalls.
- `docs/design/terraform-provider-resource-model.md`: resource schemas, import keys, Patterns A/B, implementation order.
- `docs/design/implementation-plan.md`: steps done and planned (Phase 2: remaining resource types).
- `docs/bootstrapping.md`: app setup and the scope each resource type needs.

Read the relevant sections before implementing or changing any resource. If code and design docs disagree, stop and ask; don't silently pick one.

## Scope

Goal (decided 2026-10-23): cover every store object the Admin API can manage, even if a given project won't use it. Volatile types (content, or objects often edited in the admin or by other systems) are implemented too, but their docs say they are a poor fit for Terraform; the choice is the user's.

Implemented: `shopify_shop` data source, `shopify_metafield_definition`, `shopify_metaobject_definition`, `shopify_menu`, `shopify_location`, `shopify_market`, `shopify_delivery_profile` (general profile only). Planned: Phase 2 in `docs/design/implementation-plan.md`, scopes per type in `docs/bootstrapping.md`.

Not covered, do not add:
- App config, extensions, Functions themselves, app webhooks, web pixels, script tags: `shopify.app.toml` + Shopify CLI. Resources may reference a Function by ID.
- Themes: theme tooling (GitHub integration, `shopify theme push`).
- Transactional records: orders, returns, fulfillment orders, gift cards, store credit, payments.
- Store creation, plan, billing, domains, staff; anything whose scope needs Shopify approval (taxes, users).

A new resource type that needs a new scope: add it to the scope table in `docs/bootstrapping.md` and to `app/*.toml`. Granting it on a store needs the user (app redeploy + reinstall).

## Stack

- Go, `terraform-plugin-framework` (not SDKv2), `terraform-plugin-testing` for acceptance tests.
- Typed GraphQL via `genqlient`, generated from the pinned Admin API schema (**2026-10**). API upgrades should surface as compile errors.
- Docs generated with `tfplugindocs`; keep `examples/` and `templates/` in sync with schemas.

## Layout

```
main.go
internal/client/       GraphQL client: auth, version pinning, throttling, retries, userErrors
internal/provider/     provider schema + Configure
internal/resources/<name>/   one package per resource
internal/datasources/shop/
internal/patterns/     adopt, data-deletion guard, natural-key import helpers
examples/  docs/  templates/
```

The template's `example_*` resources, data sources, functions and ephemeral resources in `internal/provider/` are placeholders: remove them as real ones replace them. Provider address and names still need renaming from `hashicorp/scaffolding` to `registry.terraform.io/pflege-de-labs/shopify` (module `github.com/pflege-de-labs/terraform-provider-shopify`).

## Commands

Use the template's `GNUmakefile`:

- `make build`: compile
- `make fmt`: gofmt
- `make lint`: golangci-lint (config in `.golangci.yml`)
- `make generate`: regenerate docs and GraphQL code; commit the output
- `make test`: unit tests (fast, no network)
- `make testacc`: acceptance tests (`TF_ACC=1`), **real API calls**, see safety rules

Before declaring a task done: `make fmt lint test` must pass, and `make generate` must leave no uncommitted diff.

## Conventions

**Identity and import**
- `id` in state = Shopify GID. `terraform import` takes a natural key, never a GID (e.g. `PRODUCT/custom/material`, `<handle>`, `<type>`). Every resource supports import from the start.
- Read looks up by GID first, then falls back to the natural key.
- Every resource has a resource identity (`id` = GID, `MutableIdentity` when Read can re-adopt by natural key) and a list resource for `terraform query`. Listed resources must produce config that passes the resource's own validators and plans as import-only.

**Attributes**
- RequiresReplace only where the API forbids in-place change.
- Server-generated values: Computed + UseStateForUnknown.
- Optional attributes left unset are **not managed**; never overwrite admin edits to them.

**Pattern A: adopt**
- Objects that exist once per store (general delivery profile, default menus): Create = find then update; Delete = remove from state only, with a warning diagnostic.
- Ordinary resources get this via `adopt_existing = true`; without it, Create fails if the natural key already exists.

**Pattern B: data-loss guard**
- Metafield/metaobject definitions and locations have `allow_data_deletion` (default `false`). Delete, or a replace that would delete, errors unless `true`. Add a plan-time warning (ModifyPlan) when a change forces replace or removes a metaobject field.

**Errors and API behaviour**
- Shopify returns `userErrors` inside HTTP 200: always check them, map to attribute paths via `userErrors.field` where possible.
- Throttling is cost-based (leaky bucket): handle `THROTTLED` / `throttleStatus` with wait-and-retry in `internal/client`. Provider-level `concurrency` caps in-flight calls regardless of Terraform parallelism.
- Async operations (bulk ops, some deletions): poll with a timeout from the `timeouts` block.
- Don't retry errors that are semantic (e.g. a country already belongs to another market); report them clearly.

**Testing**
- Pure logic (field diffing, delivery-profile diffing, throttle backoff, userErrors mapping) gets unit tests written **first**.
- Acceptance tests cover create, update, import, and the Pattern A/B behaviour per resource.

## Unverified API assumptions

The resource model marks open points with `⚠`. Don't guess them. Check the 2026-10 schema/docs and either confirm or record the finding in the design doc. If you can't verify, leave a `// TODO(verify): ...` comment and mention it in your summary.

## Safety rules

- Acceptance tests run **only** against the disposable dev store configured via `SHOPIFY_STORE_DOMAIN` / `SHOPIFY_ACCESS_TOKEN`. Never against a staging or production store.
- Never print, log, commit or paste tokens or webhook secrets. Don't read `.env*`, `*.tfstate*` or `*.tfvars` files. Mark token attributes `Sensitive`.
- Don't run `terraform destroy`, or `apply` with `allow_data_deletion = true`, unless asked.
- Don't add dependencies or bump the pinned API version without asking.

## Working style

- Work in the implementation order from section 12 of the resource model; one resource per task, one commit per step.
- For non-trivial changes, propose a short plan before editing.
- Keep changes small and focused; no unrelated refactors.
- Summaries: what changed, what is tested, what is still unverified.
