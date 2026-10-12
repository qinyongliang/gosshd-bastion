#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "$0")" && pwd)"
repo="$(cd -- "$script_dir/../.." && pwd)"
export PATH="$HOME/.cache/gosshd-forward-validation/go1.26.4/go/bin:$PATH"
# Reuse already verified module sources if present; never write go.mod/go.sum.
if [ -d /mnt/d/vfox/golang/v-1.26.4/golang-1.26.4/packages/pkg/mod ]; then
 export GOMODCACHE=/mnt/d/vfox/golang/v-1.26.4/golang-1.26.4/packages/pkg/mod
fi
export GOFLAGS=-mod=readonly
out="$HOME/.cache/gosshd-forward-validation/bin"
mkdir -p "$out"
cd "$repo"
CGO_ENABLED=0 go build -p 2 -o "$out/server" ./cmd/gosshd-server
CGO_ENABLED=0 go build -p 2 -o "$out/agent" ./cmd/gosshd-agent
CGO_ENABLED=0 go build -p 2 -o "$out/backend" ./scripts/forward-load/backend
python3 -m py_compile "$script_dir/runner.py"
exec bash "$script_dir/network.sh" "$out/server" "$out/agent" "$@" --backend-binary "$out/backend"
