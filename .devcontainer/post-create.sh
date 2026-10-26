#!/usr/bin/env bash
set -euo pipefail
ws=$(git rev-parse --show-toplevel)
sudo chown vscode: ~/.cache/agent-token ~/.local/share/mise ~/go
mise trust "$ws/mise.toml"
mise install
