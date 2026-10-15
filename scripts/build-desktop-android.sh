#!/usr/bin/env bash
set -euo pipefail
version="${1:-dev}"
[[ "$version" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]] || { echo 'invalid version' >&2; exit 1; }
ndk="${ANDROID_NDK_HOME:-${ANDROID_NDK_ROOT:-}}"
[ -n "$ndk" ] || { echo 'Set ANDROID_NDK_HOME to an NDK installation (r27c or newer).' >&2; exit 1; }
case "$(uname -s)" in
  Linux) host_tag=linux-x86_64 ;;
  Darwin) host_tag=darwin-x86_64 ;;
  *) echo 'Use build-desktop-android.ps1 on Windows.' >&2; exit 1 ;;
esac
clang="$ndk/toolchains/llvm/prebuilt/$host_tag/bin/clang"
[ -x "$clang" ] || { echo 'Android NDK compiler not found.' >&2; exit 1; }
cd "$(dirname "$0")/.."
for arch in armv7 arm64; do
  if [ "$arch" = armv7 ]; then
    goarch=arm; goarm=7; target=armv7a-linux-androideabi24
  else
    goarch=arm64; goarm=''; target=aarch64-linux-android24
  fi
  dir="build/desktop/android/$version/$arch"
  mkdir -p "$dir"
  echo "building independent desktop MCP android/$arch (API 24+)"
  env GOOS=android GOARCH="$goarch" GOARM="$goarm" CGO_ENABLED=1 \
    CC="\"$clang\" --target=$target" \
    go build -buildmode=pie -trimpath \
      -ldflags="-s -w -extldflags=-Wl,-z,max-page-size=16384 -X main.version=$version" \
      -o "$dir/gosshd-desktop-mcp" ./cmd/gosshd-desktop-mcp
  cp docs/desktop-mcp.md docs/android-desktop-mcp.md "$dir/"
done
