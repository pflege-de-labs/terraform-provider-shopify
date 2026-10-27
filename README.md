# Terraform Provider for Shopify

Manage the configuration of a Shopify store with Terraform, through the Admin GraphQL API: metafield and metaobject definitions, markets, locations, delivery profiles, menus, locales, policies, privacy and checkout settings. Content such as products, collections, pages and files is supported too, but is marked as volatile in the docs because it is edited in the admin all the time.

Registry: `pflege-de-labs/shopify`. Documentation: [Terraform Registry](https://registry.terraform.io/providers/pflege-de-labs/shopify/latest/docs) (generated into [`docs/`](docs)).

## Approach

Each part of a Shopify setup is handled by the tool that matches how Shopify manages it. This provider covers store configuration and schema. App configuration, extensions and Functions (`shopify.app.toml` and the Shopify CLI), themes (theme tooling), and transactional records such as orders, payments and gift cards are out of scope. See [`CLAUDE.md`](CLAUDE.md) ("Scope") and [`docs/design/shopify-infra-automation-approach.md`](docs/design/shopify-infra-automation-approach.md).

## Requirements

- Terraform 1.14 or later for `terraform query` (list resources); identity-based `import` blocks need 1.12 or later.
- A Shopify Dev Dashboard app installed on the store, with the Admin API scopes for the resource types you use. See [`docs/bootstrapping.md`](docs/bootstrapping.md) for the setup and the scope table.
- To build from source: Go 1.25 or later (see `go.mod`).

## Usage

```terraform
terraform {
  required_providers {
    shopify = {
      source = "pflege-de-labs/shopify"
    }
  }
}

provider "shopify" {
  store_domain  = "example.myshopify.com"
  client_id     = var.shopify_client_id
  client_secret = var.shopify_client_secret
}
```

Authenticate with `client_id` and `client_secret` (client credentials grant, preferred: the provider fetches and renews the token itself) or with a static `access_token`. Each setting can also come from an environment variable:

| Argument | Environment variable |
|---|---|
| `store_domain` | `SHOPIFY_STORE_DOMAIN` |
| `client_id` | `SHOPIFY_CLIENT_ID` |
| `client_secret` | `SHOPIFY_CLIENT_SECRET` |
| `access_token` | `SHOPIFY_ACCESS_TOKEN` |

`store_domain` must be the `<handle>.myshopify.com` domain. The provider index page lists the other arguments (`api_version`, `max_retries`, `concurrency`) and the behaviour every user should know: import keys, unmanaged attributes, `adopt_existing`, `allow_data_deletion`, volatile types and throttling.

## Importing an existing store

Every resource has a list resource, so `terraform query -generate-config-out=generated.tf` can write the configuration and `import` blocks for what a store already has. See the guide [Importing an existing store](docs/guides/importing-an-existing-store.md).

## Development

```shell
make build      # go build
make install    # build and install the provider binary into $GOPATH/bin
make fmt        # gofmt
make lint       # golangci-lint
make test       # unit tests
make generate   # regenerate docs (tfplugindocs) and GraphQL code (genqlient); commit the output
make testacc    # acceptance tests (TF_ACC=1), real API calls
```

Before a change is done, `make fmt lint test` must pass and `make generate` must leave no uncommitted diff. Docs are generated from the schemas, `examples/` and `templates/`; edit those, not `docs/`. The GraphQL client is generated from the pinned Admin API schema (2026-10).

**Acceptance tests make real API calls and must run only against a disposable dev store**, configured with `SHOPIFY_STORE_DOMAIN` and `SHOPIFY_ACCESS_TOKEN`. Never run them against a staging or production store. Never print, log or commit tokens or secrets.

Design notes live in [`docs/design/`](docs/design); [`docs/devcontainer.md`](docs/devcontainer.md) describes the dev container.

## License

[MPL-2.0](LICENSE)
