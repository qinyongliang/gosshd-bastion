#!/usr/bin/env bash
set -euo pipefail
base="$HOME/.cache/gosshd-forward-validation/go1.26.4"
mkdir -p "$base"
if [ ! -x "$base/go/bin/go" ]; then
 curl --fail --location --retry 2 --output "$base/go1.26.4.linux-amd64.tar.gz" "https://go.dev/dl/go1.26.4.linux-amd64.tar.gz"
 printf '%s  %s\n' '1153d3d50e0ac764b447adfe05c2bcf08e889d42a02e0fe0259bd47f6733ad7f' "$base/go1.26.4.linux-amd64.tar.gz" | sha256sum --check
 tar -xzf "$base/go1.26.4.linux-amd64.tar.gz" -C "$base"
fi
"$base/go/bin/go" version
