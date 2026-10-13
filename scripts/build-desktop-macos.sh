#!/bin/sh
set -eu
# Run on a Mac with Xcode Command Line Tools. Artifacts stay outside source.
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
out=${1:-"$root/build/desktop/macos"}
app="$out/GOSSHD Desktop Helper.app"
identity=${DESKTOP_SIGN_IDENTITY:--}
keychain=${DESKTOP_SIGN_KEYCHAIN:-}
skip_self_test=${DESKTOP_SKIP_SELF_TEST:-0}
case "$skip_self_test" in 0|1) ;; *) echo "DESKTOP_SKIP_SELF_TEST must be 0 or 1" >&2; exit 1;; esac
if [ -n "$keychain" ]; then
  if [ "$identity" = "-" ]; then
    echo "DESKTOP_SIGN_KEYCHAIN requires a certificate DESKTOP_SIGN_IDENTITY" >&2
    exit 1
  fi
  if [ ! -f "$keychain" ]; then
    echo "DESKTOP_SIGN_KEYCHAIN must name an existing keychain file" >&2
    exit 1
  fi
fi
mkdir -p "$app/Contents/MacOS"
arch=${DESKTOP_ARCH:-$(uname -m)}
case "$arch" in arm64|x86_64) ;; *) echo "unsupported desktop arch" >&2; exit 1;; esac
xcrun swiftc -O -target "$arch-apple-macosx12.3" -framework AppKit -framework ApplicationServices -framework ScreenCaptureKit -framework CoreImage -framework CoreMedia "$root/internal/desktop/native/macos/DesktopHelper.swift" -o "$app/Contents/MacOS/gosshd-desktop-helper"
cat > "$app/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.gosshd.desktop-helper</string><key>CFBundleName</key><string>GOSSHD Desktop Helper</string><key>CFBundleExecutable</key><string>gosshd-desktop-helper</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleVersion</key><string>1</string><key>LSMinimumSystemVersion</key><string>12.3</string><key>LSUIElement</key><true/></dict></plist>
PLIST
# Reuse the same certificate and bundle identifier across updates. Missing or
# unusable configured identities must fail; never fall back to ad-hoc signing.
# The default ad-hoc identity is for development, not stable distribution.
if [ -n "$keychain" ]; then
  codesign --force --sign "$identity" --keychain "$keychain" "$app"
else
  codesign --force --sign "$identity" "$app"
fi
if [ "$skip_self_test" = 1 ]; then
  echo "Native helper self-test skipped by DESKTOP_SKIP_SELF_TEST=1."
elif [ "$arch" = "$(uname -m)" ]; then
  "$app/Contents/MacOS/gosshd-desktop-helper" --self-test
else
  echo "Cross-architecture helper built; runtime tests require a matching Mac."
fi
printf '%s\n' "$app"
