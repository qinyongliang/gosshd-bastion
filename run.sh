#!/usr/bin/env sh
set -eu

repo="${GOSSHD_REPO:-qinyongliang/gosshd-bastion}"
version="${GOSSHD_VERSION:-}"
proxy="${GOSSHD_PROXY_URL:-https://gh-proxy.com/}"

if [ -z "$version" ]; then
  latest_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${repo}/releases/latest")"
  version="${latest_url##*/}"
fi

if [ -z "$version" ] || [ "$version" = "latest" ]; then
  echo "unable to resolve latest gosshd release version" >&2
  exit 1
fi

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux|darwin|freebsd|openbsd|netbsd) ;;
  *) echo "unsupported os: $os" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  i386|i686|386) arch="386" ;;
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  armv6l|armv6*) arch="armv6" ;;
  armv7l|armv7*) arch="armv7" ;;
  riscv64) arch="riscv64" ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

platform="${os}-${arch}"
asset="gosshd-${version}-${platform}.tar.gz"
url="https://github.com/${repo}/releases/download/${version}/${asset}"
tmp_root="${GOSSHD_TMPDIR:-${TMPDIR:-/tmp}}/gosshd"
archive="${tmp_root}/${asset}"
extract_dir="${tmp_root}/server-${version}-${platform}-$$"

mkdir -p "$tmp_root"

download() {
  src="$1"
  dst="$2"
  enforce_speed="$3"
  if [ "$enforce_speed" = "yes" ]; then
    curl -fL --connect-timeout 20 --retry 2 --speed-limit 102400 --speed-time 5 "$src" -o "$dst"
  else
    curl -fL --connect-timeout 20 --retry 2 "$src" -o "$dst"
  fi
}

echo "downloading ${url}"
if ! download "$url" "$archive" yes; then
  proxy_url="${proxy%/}/${url}"
  echo "direct download failed or slow; retrying ${proxy_url}" >&2
  download "$proxy_url" "$archive" no
fi

mkdir -p "$extract_dir"
tar -xzf "$archive" -C "$extract_dir"

server="${extract_dir}/gosshd-${platform}/gosshd-server"
if [ ! -x "$server" ]; then
  echo "server binary not found in archive: $server" >&2
  exit 1
fi

echo "starting $server $*"
exec "$server" "$@"
