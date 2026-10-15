#!/system/bin/sh
set -eu
[ "$(id -u)" = 0 ] || { echo 'Root installer requires a successful root grant.' >&2;exit 1; }
export GOSSHD_DESKTOP_ENABLE=1
export GOSSHD_ANDROID_BACKEND=shell
exec sh "${1:?saved Agent installer required}" "${2:-install}"
