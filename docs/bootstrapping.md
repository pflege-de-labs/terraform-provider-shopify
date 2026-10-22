# Bootstrapping: Shopify App and Credentials

**Last updated:** 2026-10-22 · **Related:** [[shopify-infra-automation-approach]] (Bootstrapping and auth), [[terraform-provider-resource-model]] §1

The provider authenticates as a Dev Dashboard app installed on the target store. This is the manual "step 0" for each store. It can't be automated.

## Prerequisites

- The app and the store must belong to the **same Shopify organization**, or the token request fails with `shop_not_permitted`.
- Create dev stores from the Dev Dashboard's **Dev stores** page. A store created from the Shopify admin is not in the organization.
- ⚠ Not yet tested for production stores. They must also be owned by the same organization.

## 1. Create the app

In the Dev Dashboard, create a new app. Don't base it on a template or create it with Shopify CLI. A CLI-managed app takes its scopes from `shopify.app.toml`, and any `shopify app deploy` overwrites what is set in the dashboard.

## 2. Release a version

Under **Versions**:

| Field | Value |
|---|---|
| App URL | `https://shopify.dev/apps/default-app-home` (the provider has no UI) |
| Webhooks API version | `2026-10`, the pinned Admin API version |
| Scopes | see below |

Then click **Release**. Saving without releasing changes nothing.

Scopes (copy as is):

```
write_locations,write_markets,write_metaobject_definitions,write_online_store_navigation,write_products,write_shipping
```

| Scope | Needed for |
|---|---|
| none | `shopify_shop` data source |
| `write_products` | `shopify_metafield_definition` with owner type `PRODUCT` (other owner types may need more ⚠) |
| `write_metaobject_definitions` | `shopify_metaobject_definition` |
| `write_online_store_navigation` | `shopify_menu` |
| `write_locations` | `shopify_location` |
| `write_markets` | `shopify_market` |
| `write_shipping` | `shopify_delivery_profile` |

Grant nothing else. In particular, don't grant theme, page or content scopes: the provider doesn't manage them, and a leaked secret would allow changes to the live theme.

## 3. Install on the store

On the app's **Home** page, click **Install app**, select the store, then click **Install**.

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

1. Release a new version with the changed scope list.
2. **Uninstall the app** in the store admin (**Settings → Apps**) and install it again (step 3).

Releasing alone did **not** update the scopes on a store that already had the app installed, even though the docs suggest it should for apps owned by your organization. Run the step 5 check afterwards.

## Troubleshooting

| Symptom | Cause |
|---|---|
| `shop_not_permitted` | Store not in the app's organization |
| Non-JSON response or 404 | App not installed, or wrong domain (must be the `.myshopify.com` domain) |
| `invalid_client` | Wrong client ID/secret, or stray quotes or whitespace in `.envrc` |
| `scope` doesn't match the version | Store still has the old installation: reinstall |

## Sources

- [Get API access tokens for Dev Dashboard apps](https://shopify.dev/docs/apps/build/dev-dashboard/get-api-access-tokens)
- [Create apps using the Dev Dashboard](https://shopify.dev/docs/apps/build/dev-dashboard/create-apps-using-dev-dashboard)
- [Manage access scopes](https://shopify.dev/docs/apps/build/authentication-authorization/manage-access-scopes)
