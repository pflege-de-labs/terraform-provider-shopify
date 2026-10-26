# Devcontainer

[`.devcontainer/`](../.devcontainer) runs Claude Code as an agent on this repository. GitHub access goes only through short-lived agent tokens from [token-broker](https://github.com/pflege-de/token-broker) (its `docs/containers.md` explains the mechanism). Everything committed or pushed in the container goes out as `pflege-de-agent[bot]`, your own commits from the Source Control view included: commit as yourself from the host.

Requirement: the agent app `pflege-de-agent` is installed on this repository. Until then the broker refuses every token, and `git push` and `gh` fail in the container.

## Start

1. Open a clone, not a git worktree: a worktree's `.git` points outside the mount.
2. With Podman instead of Docker, set `dev.containers.dockerPath` to `podman` in VS Code, or pass `--docker-path podman` to the devcontainer CLI.
3. For acceptance tests, export the dev store's `SHOPIFY_STORE_DOMAIN`, `SHOPIFY_CLIENT_ID` and `SHOPIFY_CLIENT_SECRET` in the shell that starts VS Code or the CLI (e.g. `direnv exec . code .`). They are passed into the container; nothing else from your environment is. Never export staging or production credentials there.
4. On first attach the terminal shows a device code: log in at <https://github.com/login/device>. The login lasts 8 hours; later, run `agent-token --login` in a terminal (or `! agent-token --login` in Claude Code).

`CLAUDE_CODE_OAUTH_TOKEN` is passed from the host if set; otherwise run `claude` and log in.

## What's inside

- Toolchain from [`mise.toml`](../mise.toml) (Go, golangci-lint, Terraform), installed on create into a volume.
- `agent-token`, the git credential helper and the `gh` wrapper from token-broker, pinned in [`.devcontainer/agent-bin/`](../.devcontainer/agent-bin).
- A Claude Code SessionStart hook that reports a missing login.
- Volumes per devcontainer for the login cache, mise and the Go module cache, so rebuilds keep them.

## Limits

- Your SSH agent is still forwarded by VS Code. GitHub stays on HTTPS through `insteadOf`, but the socket is there. For full isolation use token-broker's `agent-run` or the devcontainer CLI.
- Commands without one target repo (`gh api orgs/...`) get no token and run unauthenticated.
