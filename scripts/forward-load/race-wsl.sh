#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "$0")" && pwd)"
cd "$script_dir/../.."
export PATH="$HOME/.cache/gosshd-forward-validation/go1.26.4/go/bin:$PATH"
if [ -d /mnt/d/vfox/golang/v-1.26.4/golang-1.26.4/packages/pkg/mod ]; then
 export GOMODCACHE=/mnt/d/vfox/golang/v-1.26.4/golang-1.26.4/packages/pkg/mod
fi
export CGO_ENABLED=1 GOFLAGS=-mod=readonly
exec go test -p 2 -race ./internal/forward ./internal/tunnel ./internal/agent ./internal/server -run 'HTTP|Forward|Edge|StableConn|WebRTC|Telemetry' -count=1 -timeout 300s
