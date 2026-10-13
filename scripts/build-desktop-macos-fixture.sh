#!/bin/sh
set -eu
# Owned GUI fixture only; this app never requests desktop-control permissions.
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
app=${1:-"$root/build/desktop/Fixture.app"}
mkdir -p "$app/Contents/MacOS"
xcrun swiftc -O -framework AppKit "$root/internal/desktop/native/macos/Fixture.swift" -o "$app/Contents/MacOS/Fixture"
cat > "$app/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.gosshd.desktop-fixture</string><key>CFBundleName</key><string>GOSSHD desktop test fixture</string><key>CFBundleExecutable</key><string>Fixture</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>
PLIST
codesign --force --sign - "$app"
printf '%s\n' "$app"
