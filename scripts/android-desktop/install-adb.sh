#!/bin/sh
# Same APK and local setup UI as manual installation; never handle credentials.
set -eu
apk='./gosshd-agent-android.apk'
serial=''
adb_bin="${ADB:-adb}"
accessibility=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    --apk|--serial|--adb)
      [ "$#" -ge 2 ] || { echo "Missing value for $1" >&2; exit 2; }
      case "$1" in --apk) apk=$2;; --serial) serial=$2;; --adb) adb_bin=$2;; esac
      shift 2 ;;
    --enable-accessibility) accessibility=true; shift ;;
    *) echo 'Usage: install-adb.sh [--apk file.apk] [--serial device] [--adb path] [--enable-accessibility]' >&2; exit 2 ;;
  esac
done
case "$apk" in /*) ;; *) apk="$PWD/$apk";; esac
[ -f "$apk" ] || { echo 'APK file not found.' >&2; exit 1; }
command -v "$adb_bin" >/dev/null 2>&1 || { echo 'ADB not found; install Android Platform Tools.' >&2; exit 1; }
devices=$("$adb_bin" devices | awk '$2 == "device" { print $1 }')
if [ -n "$serial" ]; then
  printf '%s\n' "$devices" | grep -Fx -- "$serial" >/dev/null || { echo 'Selected device is not connected and authorized.' >&2; exit 1; }
else
  count=$(printf '%s\n' "$devices" | awk 'NF { count++ } END { print count+0 }')
  case "$count" in
    0) echo 'No authorized device. Enable USB debugging, connect the phone and approve its prompt.' >&2; exit 1 ;;
    1) serial=$devices ;;
    *) echo 'Multiple devices found. Select one with --serial.' >&2; exit 1 ;;
  esac
fi
adb_device() { "$adb_bin" -s "$serial" "$@"; }
sdk=$(adb_device shell getprop ro.build.version.sdk | tr -d '\r\n')
case "$sdk" in ''|*[!0-9]*) echo 'Cannot read Android version.' >&2; exit 1;; esac
[ "$sdk" -ge 30 ] || { echo 'This APK requires Android 11 (API 30) or newer.' >&2; exit 1; }
adb_device install -r "$apk"
if [ "$accessibility" = true ]; then
  adb_device shell pm grant com.gosshd.desktop android.permission.WRITE_SECURE_SETTINGS
fi
adb_device shell am start -n com.gosshd.desktop/.ui.MainActivity
printf '%s\n' 'Enter the server HTTPS address and enrollment token in the app, then tap Save and start Agent. ADB can now be disconnected.'
if [ "$accessibility" = true ]; then
  printf '%s\n' 'Optional desktop control: tap Activate with ADB grant inside the app, then enable desktop control.'
fi
