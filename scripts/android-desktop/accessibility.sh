#!/system/bin/sh
set -eu
umask 077
installer=${1:?saved Agent installer required}
apk=${2:?companion APK required}
mode=${3:-install}
[ "$(id -u)" != 0 ] || { echo 'Run auxiliary installation as the ordinary Termux user.' >&2;exit 1; }
[ -n "${PREFIX:-}" ] && [ -x "$PREFIX/bin/sh" ] || { echo 'Unrooted Agent installation requires Termux. ADB/Shizuku can activate the APK, but do not install a Termux Agent for you.' >&2;exit 1; }
[ -f "$apk" ] || { echo 'Companion APK not found.' >&2;exit 1; }
if command -v termux-open >/dev/null 2>&1;then termux-open --view "$apk";else echo "Install the verified companion APK locally: $apk";fi
printf '%s\n' 'In GOSSHD Desktop: enable accessibility manually or with ADB/Shizuku, enable local control, then copy its pairing token.'
mkdir -p "$HOME/.gosshd"
chmod 700 "$HOME/.gosshd"
token_file="$HOME/.gosshd/desktop-pairing.token"
if [ ! -s "$token_file" ];then
 [ -t 0 ] || { echo 'Pair locally first and save the token to ~/.gosshd/desktop-pairing.token (chmod 600), then rerun.' >&2;exit 1; }
 printf 'Pairing token (hidden): ' >&2
 hidden=false
 if command -v stty >/dev/null 2>&1;then stty -echo;hidden=true;trap 'stty echo' EXIT INT TERM;fi
 IFS= read -r pairing_token
 if [ "$hidden" = true ];then stty echo;trap - EXIT INT TERM;fi
 printf '\n' >&2
 case "$pairing_token" in *[!A-Za-z0-9_-]*|'') echo 'Invalid pairing token.' >&2;exit 1;;esac
 [ "${#pairing_token}" = 43 ] || { echo 'Invalid pairing token length.' >&2;exit 1; }
 printf '%s\n' "$pairing_token" > "$token_file";unset pairing_token
fi
pairing_token=$(cat "$token_file")
case "$pairing_token" in *[!A-Za-z0-9_-]*|'') echo 'Invalid saved pairing token.' >&2;exit 1;;esac
[ "${#pairing_token}" = 43 ] || { echo 'Invalid saved pairing token length.' >&2;exit 1; }
unset pairing_token
chmod 600 "$token_file"
export GOSSHD_DESKTOP_ENABLE=1
export GOSSHD_ANDROID_BACKEND=accessibility
export GOSSHD_ANDROID_BRIDGE_TOKEN_FILE="$token_file"
exec sh "$installer" "$mode"
