#!/usr/bin/env bash
#
# run.sh — download heartd for this machine and run it. No install, no sudo.
#
# One-liner:
#   curl -fsSL https://raw.githubusercontent.com/timanthonyalexander/heartd/main/run.sh | bash
#
# It fetches the latest release binary for your OS/arch into a cache dir and
# launches heartd on http://localhost:9300 (Ctrl-C to stop). Any extra args are
# passed straight to heartd, e.g. `... | bash -s -- -addr :8080`.

set -euo pipefail

REPO="${HEARTD_REPO:-timanthonyalexander/heartd}"
VERSION="${HEARTD_VERSION:-latest}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
case "$os" in linux | darwin) ;; *) echo "unsupported OS: $os (Linux/macOS only)" >&2; exit 1 ;; esac
case "$arch" in amd64 | arm64) ;; *) echo "unsupported arch: $arch (amd64/arm64 only)" >&2; exit 1 ;; esac
asset="heartd-${os}-${arch}"

if [ "$VERSION" = "latest" ]; then
  url="https://github.com/${REPO}/releases/latest/download/${asset}"
else
  url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
fi

dir="${XDG_CACHE_HOME:-$HOME/.cache}/heartd"
mkdir -p "$dir"
bin="$dir/heartd"

printf '\033[1;34m↓ downloading %s (%s)\033[0m\n' "$asset" "$VERSION" >&2
if command -v curl >/dev/null 2>&1; then
  curl -fSL --proto '=https' --tlsv1.2 -o "$bin" "$url" ||
    { echo "download failed — is there a published '${asset}' release at github.com/${REPO}?" >&2; exit 1; }
elif command -v wget >/dev/null 2>&1; then
  wget -O "$bin" "$url" || { echo "download failed ($url)" >&2; exit 1; }
else
  echo "need curl or wget" >&2
  exit 1
fi
chmod +x "$bin"

printf '\033[1;32m▶ heartd → http://localhost:9300\033[0m   (data in %s, Ctrl-C to stop)\n' "$dir" >&2

# Run from the cache dir so heartd.db lands there, not in your current folder.
cd "$dir"
exec "$bin" "$@"
