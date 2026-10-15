#!/system/bin/sh
# Local wrapper around a saved owner-scoped Agent installer. No credentials printed.
set -eu
umask 077
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
installer=${1:?usage: install.sh <saved-agent-installer.sh> <companion.apk> [run|install]}
apk=${2:?companion APK required for unrooted branch}
mode=${3:-install}
case "$mode" in run|install) ;; *) echo 'mode must be run or install' >&2;exit 1;;esac
installer=$(CDPATH= cd -- "$(dirname -- "$installer")" && printf '%s/%s' "$PWD" "$(basename -- "$installer")")
apk=$(CDPATH= cd -- "$(dirname -- "$apk")" && printf '%s/%s' "$PWD" "$(basename -- "$apk")")
quote_sh(){ printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\"'\"'/g")"; }
requested=${GOSSHD_ANDROID_INSTALL_MODE:-auto}
case "$requested" in auto|root|accessibility) ;; *) echo 'GOSSHD_ANDROID_INSTALL_MODE must be auto, root or accessibility' >&2;exit 1;;esac
root_available=false
if [ "$(id -u)" = 0 ];then root_available=true
elif [ "$requested" != accessibility ] && command -v su >/dev/null 2>&1 && command -v timeout >/dev/null 2>&1;then
 root_uid=$(timeout 5 su -c 'id -u' 2>/dev/null || true)
 [ "$root_uid" = 0 ] && root_available=true
fi
if [ "$requested" = root ] && [ "$root_available" != true ];then echo 'root requested but su is absent, denied or timed out' >&2;exit 1;fi
if [ "$requested" != accessibility ] && [ "$root_available" = true ];then
 if [ "$(id -u)" = 0 ];then exec sh "$script_dir/root.sh" "$installer" "$mode";fi
 exec su -c "sh $(quote_sh "$script_dir/root.sh") $(quote_sh "$installer") $(quote_sh "$mode")"
fi
exec sh "$script_dir/accessibility.sh" "$installer" "$apk" "$mode"
