#!/usr/bin/env bash
set -euo pipefail
version="${1:-dev}"
[[ "$version" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]] || { echo "invalid version: $version" >&2; exit 1; }
ndk="${ANDROID_NDK_HOME:-${ANDROID_NDK_ROOT:-}}"
[ -n "$ndk" ] || { echo 'Set ANDROID_NDK_HOME to an Android NDK installation (r27c or newer).' >&2; exit 1; }
case "$(uname -s)" in
  Linux) host_tag=linux-x86_64 ;;
  Darwin) host_tag=darwin-x86_64 ;;
  *) echo 'Use build-agent-android.ps1 on Windows.' >&2; exit 1 ;;
esac
clang="$ndk/toolchains/llvm/prebuilt/$host_tag/bin/clang"
[ -x "$clang" ] || { echo "Android NDK compiler not found: $clang" >&2; exit 1; }
cd "$(dirname "$0")/.."
mkdir -p dist
for arch in armv7 arm64; do
  if [ "$arch" = armv7 ]; then
    goarch=arm; goarm=7; target=armv7a-linux-androideabi24
  else
    goarch=arm64; goarm=''; target=aarch64-linux-android24
  fi
  output="build/agent/$version/android/$arch/gosshd-agent"
  mkdir -p "$(dirname "$output")"
  echo "building agent android/$arch (API 24+)"
  # anet's restricted Android linknames require a scoped workaround.
  env GOOS=android GOARCH="$goarch" GOARM="$goarm" CGO_ENABLED=1 \
    CC="\"$clang\" --target=$target" \
    go build -buildmode=pie -trimpath \
      -ldflags="-s -w -checklinkname=0 -extldflags=-Wl,-z,max-page-size=16384 -X main.version=$version" \
      -o "$output" ./cmd/gosshd-agent
  cp "$output" "dist/gosshd-agent-$version-android-$arch"
done
