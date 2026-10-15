# Embedded in the tokenized installer; platform, mode and enrollment inputs are set by Go.
# Android must be detected before treating its Linux kernel as a Linux host.
if [ "$os" = linux ] && { [ -n "${TERMUX_VERSION:-}" ] || [ -n "${ANDROID_ROOT:-}" ] || [ -x /system/bin/getprop ]; }; then
  os=android
fi
if [ "$os" = android ]; then
  case "$mode" in run|install) ;; *) echo 'usage: sh installer.sh [run|install]' >&2; exit 1 ;; esac
  umask 077
  termux=false
  if [ "$(id -u)" != 0 ] && [ -n "${PREFIX:-}" ] && [ -x "$PREFIX/bin/sh" ]; then
    termux=true
    android_dir="${HOME:?HOME must be set in Termux}/.gosshd"
    android_home="$HOME"
    android_sh="$PREFIX/bin/sh"
    android_shell="$android_sh"
    if [ -x "${SHELL:-}" ]; then android_shell="$SHELL"; elif [ -x "$PREFIX/bin/bash" ]; then android_shell="$PREFIX/bin/bash"; fi
    # uname describes the kernel, not a 32-bit Termux userspace on a 64-bit phone.
    if command -v dpkg >/dev/null 2>&1; then arch="$(dpkg --print-architecture)"; fi
    if [ "$mode" = install ]; then
      command -v sv >/dev/null 2>&1 && command -v service-daemon >/dev/null 2>&1 || {
        echo 'Termux background installation requires: pkg install termux-services; then restart your shell.' >&2; exit 1;
      }
    fi
  else
    [ "$(id -u)" = 0 ] || { echo 'Native Android requires a root shell; use Termux for an unrooted device.' >&2; exit 1; }
    android_dir=/data/local/gosshd
    android_home="$android_dir"
    android_sh=/system/bin/sh
    [ -x "$android_sh" ] || android_sh="$(command -v sh)"
    android_shell="$android_sh"
    if command -v getprop >/dev/null 2>&1; then
      abi="$(getprop ro.product.cpu.abi)"
      if [ -n "$abi" ]; then arch="$abi"; fi
    fi
    if [ "$mode" = install ]; then
      for utility in nohup readlink; do
        command -v "$utility" >/dev/null 2>&1 || { echo "Root Android background installation requires $utility (toybox or BusyBox)." >&2; exit 1; }
      done
    fi
  fi
  if command -v getprop >/dev/null 2>&1; then
    sdk="$(getprop ro.build.version.sdk)"
    case "$sdk" in ''|*[!0-9]*) ;; *) [ "$sdk" -ge 24 ] || { echo 'Android 7.0 / API 24 or newer is required.' >&2; exit 1; } ;; esac
  fi
  case "$arch" in
    aarch64|arm64|arm64-v8a) arch=arm64 ;;
    arm|armv7l|armv7*|armeabi-v7a) arch=armv7 ;;
    *) echo "unsupported Android ABI: $arch (expected armeabi-v7a or arm64-v8a)" >&2; exit 1 ;;
  esac
  download() {
    if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
    elif command -v wget >/dev/null 2>&1; then wget -q "$1" -O "$2"
    else echo 'Install curl or wget before running the Android installer.' >&2; return 1; fi
  }
  mkdir -p "$android_dir"
  chmod 700 "$android_dir"
  android_dir="$(cd "$android_dir" && pwd -P)"
  if [ "$termux" = false ]; then android_home="$android_dir"; fi
  agent_target="$android_dir/gosshd-agent"
  command -v readlink >/dev/null 2>&1 || { echo 'readlink is required to safely stop the installed agent.' >&2; exit 1; }
  tmp="$(mktemp "$android_dir/.gosshd-agent-download.XXXXXX")"
  checksum_tmp=''
  trap 'rm -f "$tmp" "$checksum_tmp" "$tmp.run"' EXIT
  checksum_tmp="$(mktemp "$android_dir/.gosshd-agent-sha.XXXXXX")"
  url="$server/download/agent/android/$arch"
  download "$url" "$tmp"
  download "$url.sha256" "$checksum_tmp"
  expected_sha="$(tr -d '[:space:]' < "$checksum_tmp")"
  case "$expected_sha" in ''|*[!0-9a-fA-F]*) echo 'invalid agent SHA256' >&2; exit 1 ;; esac
  [ "${#expected_sha}" = 64 ] || { echo 'invalid agent SHA256 length' >&2; exit 1; }
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s  %s\n' "$expected_sha" "$tmp" | sha256sum -c -
  elif command -v shasum >/dev/null 2>&1; then
    actual_sha="$(shasum -a 256 "$tmp" | awk '{print $1}')"
    [ "$actual_sha" = "$expected_sha" ] || { echo 'sha256 mismatch' >&2; exit 1; }
  else echo 'A SHA256 checker (sha256sum or shasum) is required.' >&2; exit 1; fi
  chmod 700 "$tmp"
  # Quote every persisted value, including HOME/PREFIX paths containing spaces or quotes.
  quote_sh() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }
  {
    printf '#!%s\nset -eu\n' "$android_sh"
    printf 'export HOME='; quote_sh "$android_home"; printf '\n'
    printf 'export PATH='; quote_sh "$PATH"; printf '\n'
    printf 'exec '; quote_sh "$android_dir/gosshd-agent"
    printf ' --server '; quote_sh "$server"
    printf ' --enrollment-token '; quote_sh "$enrollment_token"
    printf ' --ssh-port '; quote_sh "$ssh_port"
    printf ' --id-file '; quote_sh "$android_dir/agent.json"
    printf ' --shell '; quote_sh "$android_shell"
    printf ' --root '; quote_sh "$android_home"
    printf '\n'
  } > "$tmp.run"
  chmod 700 "$tmp.run"
  if [ "$termux" = true ]; then
    export SVDIR="$PREFIX/var/service"
    service_dir="$SVDIR/gosshd-agent"
    if [ -d "$service_dir" ]; then
      sv -w 10 down "$service_dir" || { echo 'Failed to stop the existing Termux agent service; installation aborted.' >&2; exit 1; }
    fi
  fi
  stop_agent_processes
  mv -f "$tmp" "$agent_target"
  mv -f "$tmp.run" "$android_dir/run.sh"
  rm -f "$checksum_tmp"
  trap - EXIT
  if [ "$mode" = run ]; then exec "$android_dir/run.sh"; fi
  if [ "$termux" = true ]; then
    mkdir -p "$service_dir" "$HOME/.termux/boot"
    {
      printf '#!%s\nexec ' "$android_sh"; quote_sh "$android_dir/run.sh"
      printf ' >> '; quote_sh "$android_dir/agent.log"; printf ' 2>&1\n'
    } > "$service_dir/run"
    chmod 700 "$service_dir/run"
    rm -f "$service_dir/down"
    {
      printf '#!%s\n' "$android_sh"
      printf 'export PREFIX='; quote_sh "$PREFIX"; printf '\n'
      printf 'export PATH='; quote_sh "$PATH"; printf '\n'
      printf 'export SVDIR='; quote_sh "$SVDIR"; printf '\n'
      printf 'command -v termux-wake-lock >/dev/null 2>&1 && termux-wake-lock\n'
      printf 'service-daemon start >/dev/null 2>&1 || true\nsv -w 15 up '; quote_sh "$service_dir"; printf '\n'
    } > "$HOME/.termux/boot/gosshd-agent"
    chmod 700 "$HOME/.termux/boot/gosshd-agent"
    service-daemon start >/dev/null 2>&1 || true
    attempt=0
    until sv -w 15 up "$service_dir"; do
      attempt=$((attempt + 1))
      [ "$attempt" -lt 10 ] || { echo 'Termux service failed to start; restart the shell and check agent.log.' >&2; exit 1; }
      sleep 1
    done
    echo "Termux service installed. Log: $android_dir/agent.log"
    echo 'For reboot startup, install Termux:Boot from the same source as Termux and open it once. Exclude Termux from battery optimization.'
    echo "Stop/start: sv down/up $service_dir"
  else
    {
      printf '#!%s\nset -eu\numask 077\ndir=' "$android_sh"; quote_sh "$android_dir"; printf '\n'
      cat <<'CONTROL'
running() {
  pid="$(cat "$dir/agent.pid" 2>/dev/null || true)"
  case "$pid" in ''|*[!0-9]*|0|1) return 1 ;; esac
  kill -0 "$pid" 2>/dev/null || return 1
  exe="$(readlink "/proc/$pid/exe" 2>/dev/null || true)"
  case "$exe" in "$dir/gosshd-agent"|"$dir/gosshd-agent (deleted)") return 0 ;; *) return 1 ;; esac
}
case "${1:-start}" in
  start)
    if running; then echo "gosshd-agent already running (pid=$pid)"; exit 0; fi
    nohup "$dir/run.sh" </dev/null >>"$dir/agent.log" 2>&1 &
    printf '%s\n' "$!" > "$dir/agent.pid"
    sleep 1
    running || { echo "gosshd-agent failed to start; check $dir/agent.log" >&2; exit 1; }
    ;;
  stop)
    if running; then
      kill "$pid"
      attempt=0
      while running && [ "$attempt" -lt 10 ]; do sleep 1; attempt=$((attempt + 1)); done
      if running; then
        kill -KILL "$pid"
        attempt=0
        while running && [ "$attempt" -lt 5 ]; do sleep 1; attempt=$((attempt + 1)); done
        if running; then echo 'Timed out waiting for the old agent to exit.' >&2; exit 1; fi
      fi
    fi
    rm -f "$dir/agent.pid"
    ;;
  restart) "$0" stop; "$0" start ;;
  status) if running; then echo "gosshd-agent running (pid=$pid)"; else echo 'gosshd-agent stopped'; exit 1; fi ;;
  *) echo "usage: $0 [start|stop|restart|status]" >&2; exit 1 ;;
esac
CONTROL
    } > "$android_dir/control.sh"
    chmod 700 "$android_dir/control.sh"
    "$android_dir/control.sh" start
    if [ -d /data/adb/service.d ]; then
      {
        printf '#!%s\nexec ' "$android_sh"; quote_sh "$android_dir/control.sh"; printf ' start\n'
      } > /data/adb/service.d/gosshd-agent.sh
      chmod 700 /data/adb/service.d/gosshd-agent.sh
      echo 'Root framework boot hook installed: /data/adb/service.d/gosshd-agent.sh'
    else
      echo "No root-framework service.d directory found. Add '$android_dir/control.sh start' to your root manager's boot facility; reboot startup is not configured."
    fi
    echo "Root Android service installed. Log: $android_dir/agent.log"
    echo "Control: $android_dir/control.sh start|stop|restart|status"
  fi
  exit 0
fi
