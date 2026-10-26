# Bootstrapping: Shopify App and Credentials

**Last updated:** 2026-10-23 · **Related:** [[shopify-infra-automation-approach]] (Bootstrapping and auth), [[terraform-provider-resource-model]] §1

The provider authenticates as a Dev Dashboard app installed on the target store. This is the manual "step 0" for each store. It can't be automated.

## Prerequisites

- The app and the store must belong to the **same Shopify organization**, or the token request fails with `shop_not_permitted`.
- Create dev stores from the Dev Dashboard's **Dev stores** page. A store created from the Shopify admin is not in the organization.
- ⚠ Not yet tested for production stores. They must also be owned by the same organization.

## 1. Create the app

Once per organization, not per store. In the Dev Dashboard, create a new app without a template.

## 2. Release a version

The app config lives in [`app/shopify.app.toml`](../app/shopify.app.toml) (scopes, app URL, webhooks API version `2026-10`). It is deployed with [Shopify CLI](https://shopify.dev/docs/api/shopify-cli):

```sh
cd app
shopify app config link   # once: pick the app, fills in client_id
shopify app deploy        # releases a new version with the TOML's config
```

`shopify app deploy` overwrites whatever is set in the dashboard, so change scopes in the TOML only. Separate staging and production apps each get their own `shopify.app.<env>.toml` (`--config <env>`).

Scopes:

The provider aims to cover every store object the Admin API lets you manage. Grant only the scopes for the resource types your configuration uses: every extra write scope widens what a leaked secret can change. The reference configs in [`app/`](../app) list the full set.

Scope names from [Shopify's access scope list](https://shopify.dev/docs/api/usage/access-scopes) (checked 2026-10-23). "Fit" says whether the objects are stable configuration (good) or change often through the admin or other systems (volatile): volatile types can be managed, but Terraform will often show drift.

| Scope | Resource types | Status | Fit |
|---|---|---|---|
| none | `shopify_shop` data source | ✅ | |
| `write_products` | `shopify_metafield_definition` (owner types `PRODUCT`, `PRODUCTVARIANT`, `COLLECTION`), catalogs, price lists, selling plan groups; products and collections | ✅ definitions (`PRODUCT`), rest planned | definitions good; products, collections volatile |
| `write_metaobject_definitions` | `shopify_metaobject_definition` | ✅ | good |
| `write_online_store_navigation` | `shopify_menu`, URL redirects | ✅ menus, redirects planned | menus good; redirects volatile |
| `write_locations` | `shopify_location` | ✅ | good |
| `write_markets` | `shopify_market`, web presences | ✅ markets, web presences planned | good |
| `write_shipping` | `shopify_delivery_profile` (general; custom profiles planned), carrier services | ✅ general profile | good; carrier services are app-owned |
| `write_locales` | shop locales; web presence locales (needed to read them) | planned | good |
| `write_legal_policies` | shop policies | planned | good |
| `write_privacy_settings` | privacy settings, cookie banner | planned | good |
| `write_checkout_and_accounts_configurations` | checkout and accounts configuration | planned | good |
| `write_checkout_branding_settings` | checkout branding | planned | good |
| `write_translations` | translations, market localizations | planned | volatile (follows content) |
| `write_publications` | publications, publishing to channels | planned | volatile |
| `write_purchase_options` | selling plan groups (together with `write_products`) | planned | volatile |
| `write_customers` | customer segments; metafield definitions for `CUSTOMER` ⚠ | planned | segments volatile |
| `write_companies` | metafield definitions for `COMPANY`, `COMPANY_LOCATION` ⚠ (B2B stores only; not in the reference configs) | planned | good |
| `write_orders`, `write_draft_orders` | metafield definitions for `ORDER`, `DRAFTORDER` ⚠ | planned | good |
| `write_content` | pages, blogs, articles; their metafield definitions ⚠ | planned | volatile (content) |
| `write_discounts` | discounts; metafield definitions for `DISCOUNT` ⚠ | planned | volatile (content) |
| `write_metaobjects` | metaobject entries | planned | volatile (content) |
| `write_files` | files and media | planned | volatile (content) |
| `write_inventory` | inventory levels | planned | volatile |
| `write_fulfillments` | fulfillment services | planned | app-owned (callback URL) |
| `write_payment_customizations`, `write_delivery_customizations`, `write_validations`, `write_cart_transforms`, `write_fulfillment_constraint_rules` | Function-backed customizations (need a Function from an installed app) | planned | good once the Function exists |

⚠ Which scope a metafield definition needs per owner type is not documented by Shopify. Without it, listing returns an empty result instead of an error (checked on the dev store). Verify each mapping when its owner type is implemented.

Not covered, because another tool owns them or they are records, not configuration:
- Themes (`write_themes`): theme tooling (GitHub integration, `shopify theme push`).
- App webhooks, web pixels, script tags, app proxy, extensions: the app's `shopify.app.toml`.
- Orders, returns, fulfillment orders, gift cards, store credit, payments, marketing events, reports: transactional records.
- Taxes (`read_taxes`/`write_taxes`) and staff (`read_users`): need Shopify approval.

## 3. Install on the store

Repeat for every store Terraform configures. Shopify CLI can't install an app on a store (`shopify app dev` only previews on dev stores), so this stays a manual step: on the app's **Home** page, click **Install app**, select the store, then click **Install**.

## 4. Credentials

From the app's **Settings** page, copy the **Client ID** and **Client secret**.

- Locally: put them in `.envrc` (git-ignored) and run `direnv allow`:
  ```sh
  export SHOPIFY_STORE_DOMAIN=<handle>.myshopify.com   # admin: Settings → Domains
  export SHOPIFY_CLIENT_ID=...
  export SHOPIFY_CLIENT_SECRET=...
  ```
- CI: store them as secrets with the same names.
- Never commit them, print them or paste them anywhere.

## 5. Verify

This prints the granted scopes and the token lifetime, never the token itself:

```sh
curl -s -X POST "https://$SHOPIFY_STORE_DOMAIN/admin/oauth/access_token" \
  --data-urlencode grant_type=client_credentials \
  --data-urlencode "client_id=$SHOPIFY_CLIENT_ID" \
  --data-urlencode "client_secret=$SHOPIFY_CLIENT_SECRET" | jq 'del(.access_token)'
```

Expected: `scope` matches the list in step 2, and `expires_in` is `86399` (24h). The provider fetches and refreshes tokens itself, so no token needs to be stored.

## Changing scopes

1. Change `scopes` in `app/shopify.app.toml` and the table in step 2, then run `shopify app deploy`.
2. **Uninstall the app** in the store admin (**Settings → Apps**) and install it again (step 3).

Releasing alone did **not** update the scopes on a store that already had the app installed, even though the docs suggest it should for apps owned by your organization. Run the step 5 check afterwards.

## Recommended: separate plan and apply apps

A pipeline that plans on pull requests hands the plan job the app's credentials. With write scopes, anyone who can open a PR in the repo could edit the workflow, or add a data source that runs at plan time, and use the write token on the store. No state backend access is needed for that.

To avoid this, create **two apps per store**:

| App | Scopes | Used by |
|---|---|---|
| Plan | the `read_*` counterparts of the write scopes you grant in step 2 | `terraform plan` on PRs and branches |
| Apply | the `write_*` scopes from step 2 | `terraform apply` on the main branch only |

`terraform plan` only reads, so the plan app is enough for it. Keep the apply app's secret where only main-branch jobs can read it, e.g. a GitHub environment whose deployment branches are limited to `main`. Then a PR can leak at most a read-only token. Each app has its own `shopify.app.toml` (`--config <name>`) and is installed on the store separately.

The plan app's config is [`app/shopify.app.readonly.toml`](../app/shopify.app.readonly.toml). Create it as a second app (step 1), then from `app/` run `shopify app config link --config readonly` and `shopify app deploy --config readonly`, and install it on each store (step 3). Inside the plan job, use the same env var names as in step 4. An apply with these credentials fails with `ACCESS_DENIED` and changes nothing.

The read scopes cover the nested fields of markets and the delivery profile: PR plans with the plan app passed on staging and production (2026-10-23).

Running PR plans with the write app is a valid shortcut while there are only trusted contributors, but treat it as a known risk.

## Troubleshooting

| Symptom | Cause |
|---|---|
| `shop_not_permitted` | Store not in the app's organization |
| Non-JSON response or 404 | App not installed, or wrong domain (must be the `.myshopify.com` domain) |
| `invalid_client` | Wrong client ID/secret, or stray quotes or whitespace in `.envrc` |
| `scope` doesn't match the version | Store still has the old installation: reinstall |
| `ACCESS_DENIED` during plan | Plan app lacks a scope: add it to `shopify.app.readonly.toml` and redeploy |

## Sources

- [Get API access tokens for Dev Dashboard apps](https://shopify.dev/docs/apps/build/dev-dashboard/get-api-access-tokens)
- [Create apps using the Dev Dashboard](https://shopify.dev/docs/apps/build/dev-dashboard/create-apps-using-dev-dashboard)
- [App configuration (`shopify.app.toml`)](https://shopify.dev/docs/apps/build/cli-for-apps/app-configuration)
- [Manage access scopes](https://shopify.dev/docs/apps/build/authentication-authorization/manage-access-scopes)
