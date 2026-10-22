# Shopify Infra Automation: Approach & Pitfalls

**Status:** Proposal, not yet decided · **Owner:** Jens Hausherr · **Last updated:** 2026-10-22

## Summary

No official Shopify Terraform provider exists, and Shopify only partly fits the Terraform model. The proposal is a **hybrid**: each part of the shop is handled by the tool that matches how Shopify manages it. The Shopify CLI handles the app and theme. A focused custom Terraform provider handles store configuration and schema. Separate jobs handle catalog and content.

## The parts of a Shopify setup

| Part | How to automate it | Notes |
|---|---|---|
| **The store itself** | Dev stores: `shopify store create dev` (CLI 4.8+, released Sept 2026). Production stores: manual. | Plan, billing, payments, domains and staff stay manual or are only partly covered by the API. |
| **App config** (scopes, webhooks, URLs, extensions, Functions) | `shopify.app.toml` in git, deployed with `shopify app deploy` and an automation token | Already declarative and fits gitops. Don't rebuild it in Terraform. |
| **Store "schema"** (metafield and metaobject definitions, markets, locations, shipping, menus, policies, pages) | Admin GraphQL API | Where a custom Terraform provider earns its keep. |
| **Theme** | Shopify GitHub integration, or `shopify theme push` | Merchants editing in the theme editor write back to the JSON templates and settings. |
| **Catalog and content** (products, collections, discounts) | A product data system, a sync job or bulk operations | Keep it out of Terraform state. |

## Options considered

1. **Terraform for everything.** One custom provider for all of the above. It looks tidy, but it re-implements what the CLI already does. Settings with no API leave gaps, and putting the catalog in state doesn't scale.
2. **CLI only.** App config and theme from git, plus scripts for store setup. Simple, but the scripts can't show a plan, can't detect drift and don't know what's already there. This ends up as a weaker version of Terraform.
3. **Hybrid (recommended).**
   - The CLI handles the app and theme.
   - A focused Terraform provider (Go, terraform-plugin-framework) handles store configuration and schema: metafield definitions, metaobject definitions, markets, shipping profiles, locations, menus, maybe webhook subscriptions that the app doesn't own.
   - Separate jobs handle content.
   - Terraform can still run the CLI steps if we want one pipeline.
4. **Reconciler or operator.** A controller that keeps the shop's settings in line with git (Kubernetes CRDs, or Crossplane on top of the provider). Only pays off with many stores.

## Pitfalls

### Bootstrapping and auth

- Chicken-and-egg: the provider needs an Admin API token. That needs an installed app, which needs a store, which can't be created through the API for production. Plan for a manual "step 0" for each store.
- New apps are created in the Dev Dashboard, not the old Partner Dashboard. For apps our organization owns, installed on stores in the same organization, the client credentials grant issues a token without user interaction. Tokens expire after 24 hours, so the provider fetches them itself from the client ID and secret ([Get API access tokens for Dev Dashboard apps](https://shopify.dev/docs/apps/build/dev-dashboard/get-api-access-tokens)).
- Scopes covering protected customer data need Shopify's approval and can't be fully automated.

### Drift and ownership

- Merchants and business teams will change things in the Shopify admin, so `terraform plan` will show drift constantly. Decide for each resource who owns it: Terraform or the admin. Use `ignore_changes` where the admin should win.
- Theme editor changes won't match git unless both sides are synced (the GitHub integration does this).

### Building the provider

- Some objects exist exactly once per store (shop settings, checkout and branding settings). These need "adopt and update" behaviour: create means take over the existing one, delete does nothing.
- Shopify uses GIDs as IDs. Use handles or keys as natural identifiers and support `terraform import` from the start.
- Errors come back as `userErrors` inside an HTTP 200 response, so they must be checked explicitly.
- Some operations finish later: media processing, bulk operations, theme processing.
- The GraphQL rate limit is cost-based (a leaky bucket). Terraform runs operations in parallel by default and will get throttled, so add backoff and retries. Plus stores get higher limits.
- API versions come out quarterly and are supported for about 12 months. Pin a version in the provider and budget for regular upgrades.

### Destructive changes

- Deleting a metafield definition can delete all the data stored in it. Use `prevent_destroy` and require an explicit flag in the provider before deleting data.
- `shopify app deploy --allow-deletes` can remove extensions and the store data that goes with them. In CI, run only `--allow-updates` and keep deletes for manual runs.
- An app deploy is all or nothing, and a release reaches every store with the app installed at once. Use separate staging and production apps, each with its own `shopify.app.toml`.

### Environments

- Stores have no built-in staging. We need separate stores, and dev stores don't match production: payments run in test mode and Plus features are limited unless we use a Plus sandbox or expansion store.
- Dev stores are capped at 250 per organization.
- Terraform state will hold API tokens and webhook secrets, so encrypt the backend.

## Suggested first scope for the provider

- Shop data source
- Metafield definitions
- Metaobject definitions
- Markets
- Locations
- Shipping profiles
- Navigation menus

Put a manual runbook in the same repo for the settings the API doesn't cover.

## Open questions

- Do we go with the hybrid approach? (Decision pending.)
- ~~Can a token be issued without user interaction for apps our organization owns (Dev Dashboard)?~~ Yes, via the client credentials grant, if app and store are in the same organization.
- For each resource, who owns it: Terraform or the Shopify admin?
- Which stores and environments do we need (dev, staging, Plus sandbox)?

## Possible next steps

- Sketch the provider's resource model: resource schemas, import keys, how to handle the one-per-store objects.
- Scaffold the Go project (terraform-plugin-framework).

## Sources

- [Create and delete dev stores in Shopify CLI (changelog)](https://shopify.dev/changelog/create-and-delete-dev-stores-in-shopify-cli.md)
- [Deploy in a CI/CD pipeline](https://shopify.dev/apps/launch/deployment/deploy-in-ci-cd-pipeline)
- [Manage app config files](https://shopify.dev/docs/apps/build/cli-for-apps/manage-app-config-files)
- [App configuration](https://shopify.dev/docs/apps/build/cli-for-apps/app-configuration)
- [Community: shopify.app.toml across environments in CI/CD](https://community.shopify.dev/t/managing-shopify-app-toml-and-env-across-multiple-environments-with-jenkins-ci-cd-and-github/32378)
- [Community: API access for creating transfer stores](https://community.shopify.dev/t/api-access-for-create-transfer-store-and-transfer-a-store-to-a-client/35746)
