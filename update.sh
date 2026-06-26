#!/usr/bin/env bash
#
# update.sh — update an existing heartd systemd install in place.
#
# One-liner upgrade (downloads the latest release binary for your CPU):
#   curl -fsSL https://raw.githubusercontent.com/timanthonyalexander/heartd/main/update.sh | sudo bash
#
# What it does (each step is privileged via sudo when you aren't root):
#   1. Verifies heartd is already installed (binary + systemd unit present)
#   2. Resolves the new heartd binary (local build or GitHub Releases download)
#      and sanity-checks it (Linux ELF)
#   3. Backs up the current binary to /usr/local/bin/heartd.bak.<timestamp>
#   4. Stops the service, swaps in the new binary, starts it again
#   5. Health-checks /api/health; on failure, offers to roll back the binary
#
# It does NOT touch your config, the heartd user/group, the data directory,
# the systemd unit, nginx, or TLS. Use install.sh for first-time setup or to
# (re)write any of those. This script only replaces the binary and restarts.
#
# Usage:
#   sudo ./update.sh [options]
#
# Options:
#   --binary PATH     Path to the new linux heartd binary. If omitted, the
#                     script looks for ./bin/heartd-linux-<arch> then ./heartd,
#                     and finally downloads it from GitHub Releases.
#   --version TAG     Release tag to download (default: latest). E.g. v1.2.0.
#   --repo OWNER/REPO GitHub repo to download from (default: timanthonyalexander/heartd).
#   --port PORT       Localhost port for the post-update health check
#                     (default: read from the systemd unit, else 9300).
#   --no-start        Swap the binary but leave the service stopped.
#   --no-backup       Don't keep a timestamped backup of the old binary.
#   --yes             Assume "yes" for prompts (non-interactive).
#   -h, --help        Show this help and exit.

set -euo pipefail

# ----- defaults -----
BINARY=""
VERSION="latest"
REPO="timanthonyalexander/heartd"
PORT=""
DO_START="yes"
DO_BACKUP="yes"
ASSUME_YES="no"
DOWNLOADED="" # temp file to clean up if we downloaded the binary

PREFIX_BIN="/usr/local/bin/heartd"
UNIT_FILE="/etc/systemd/system/heartd.service"

# Resolve our own directory, tolerating `curl | bash` (no script file on disk).
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo .)"
trap 'rm -f "${DOWNLOADED:-}"' EXIT

# ----- pretty output -----
c_blue() { printf '\033[1;34m%s\033[0m\n' "$*"; }
c_green() { printf '\033[1;32m%s\033[0m\n' "$*"; }
c_yellow() { printf '\033[1;33m%s\033[0m\n' "$*"; }
c_red() { printf '\033[1;31m%s\033[0m\n' "$*" >&2; }
info() { printf '  %s\n' "$*"; }
die() {
  c_red "error: $*"
  exit 1
}

usage() {
  sed -n '2,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//; s/^#//' | sed '$d'
  exit "${1:-0}"
}

# ----- parse args -----
while [ $# -gt 0 ]; do
  case "$1" in
  --binary)
    BINARY="${2:-}"
    shift 2
    ;;
  --version)
    VERSION="${2:-}"
    shift 2
    ;;
  --repo)
    REPO="${2:-}"
    shift 2
    ;;
  --port)
    PORT="${2:-}"
    shift 2
    ;;
  --no-start)
    DO_START="no"
    shift
    ;;
  --no-backup)
    DO_BACKUP="no"
    shift
    ;;
  --yes)
    ASSUME_YES="yes"
    shift
    ;;
  -h | --help) usage 0 ;;
  *) die "unknown option: $1 (try --help)" ;;
  esac
done

# ----- prompt helper (defaults to NO; honours --yes; safe when non-interactive) -----
confirm() {
  local prompt="$1"
  if [ "$ASSUME_YES" = "yes" ]; then return 0; fi
  if [ ! -t 0 ]; then return 1; fi # no TTY -> treat as "no"
  local reply
  read -r -p "$prompt [y/N] " reply || true
  case "$reply" in [yY] | [yY][eE][sS]) return 0 ;; *) return 1 ;; esac
}

# ----- preflight -----
[ "$(uname -s)" = "Linux" ] || die "this updater is for Linux only (you're on $(uname -s))."
[ -d /run/systemd/system ] || die "systemd not detected; this updater requires systemd."

# Privilege escalation: use sudo when not already root.
if [ "$(id -u)" -eq 0 ]; then
  SUDO=""
else
  command -v sudo >/dev/null 2>&1 || die "not root and sudo not found. Re-run as root."
  SUDO="sudo"
  c_yellow "Not running as root — privileged steps will use sudo (you may be prompted)."
fi

# Require an existing install: don't silently become a first-time installer.
[ -f "$PREFIX_BIN" ] || die "no heartd binary at $PREFIX_BIN — run install.sh first."
[ -f "$UNIT_FILE" ] || die "no systemd unit at $UNIT_FILE — run install.sh first."

# Detect CPU arch and map to Go's naming.
case "$(uname -m)" in
x86_64 | amd64) ARCH="amd64" ;;
aarch64 | arm64) ARCH="arm64" ;;
*) die "unsupported architecture: $(uname -m) (heartd ships amd64 and arm64)." ;;
esac

# download_binary fetches the release asset for this architecture into a temp
# file and points BINARY at it (used by the `curl | bash` one-liner).
download_binary() {
  local asset="heartd-linux-${ARCH}" url tmp
  if [ -z "$VERSION" ] || [ "$VERSION" = "latest" ]; then
    url="https://github.com/${REPO}/releases/latest/download/${asset}"
  else
    url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
  fi

  tmp="$(mktemp)" || die "could not create a temp file"
  DOWNLOADED="$tmp"
  c_blue "Downloading ${asset} (${VERSION:-latest})"
  info "$url"
  if command -v curl >/dev/null 2>&1; then
    curl -fSL --proto '=https' --tlsv1.2 -o "$tmp" "$url" ||
      die "download failed. Is there a published '${asset}' asset for release '${VERSION}' at github.com/${REPO}? Otherwise build locally (make cross) and pass --binary."
  elif command -v wget >/dev/null 2>&1; then
    wget -O "$tmp" "$url" ||
      die "download failed. Is there a published '${asset}' asset for release '${VERSION}' at github.com/${REPO}?"
  else
    die "need curl or wget to download the binary (or pass --binary PATH)."
  fi
  chmod +x "$tmp"
  BINARY="$tmp"
}

# Resolve the new binary: explicit --binary, then a local build, then a download
# from GitHub Releases (same lookup order as install.sh).
if [ -z "$BINARY" ]; then
  if [ -x "${SCRIPT_DIR}/bin/heartd-linux-${ARCH}" ]; then
    BINARY="${SCRIPT_DIR}/bin/heartd-linux-${ARCH}"
  elif [ -x "${SCRIPT_DIR}/heartd" ]; then
    BINARY="${SCRIPT_DIR}/heartd"
  else
    download_binary
  fi
fi
[ -f "$BINARY" ] || die "binary not found: $BINARY"

# Sanity-check it's a Linux ELF so we fail loudly on a macOS/Windows binary.
# Read the 4-byte magic as hex (locale-safe; no tr on raw bytes): 7f 45 4c 46.
ELF_MAGIC="$(LC_ALL=C head -c 4 "$BINARY" | od -An -tx1 | tr -d ' \n')"
if [ "$ELF_MAGIC" != "7f454c46" ]; then
  die "$BINARY is not a Linux ELF binary (magic: ${ELF_MAGIC:-empty}). Use the linux/${ARCH} build (make cross)."
fi

# Determine the port for the health check: explicit flag wins, else parse the
# unit's ExecStart (-addr 127.0.0.1:PORT), else fall back to 9300.
if [ -z "$PORT" ]; then
  PORT="$(grep -oE -- '-addr[= ]+127\.0\.0\.1:[0-9]+' "$UNIT_FILE" | grep -oE '[0-9]+$' | head -n1 || true)"
  PORT="${PORT:-9300}"
fi

# Try to read current vs incoming version (best-effort; heartd may not support --version).
CUR_VER="$("$PREFIX_BIN" --version 2>/dev/null | head -n1 || true)"
NEW_VER="$("$BINARY" --version 2>/dev/null | head -n1 || true)"

c_blue "heartd updater"
info "current:   $PREFIX_BIN${CUR_VER:+  ($CUR_VER)}"
info "new:       $BINARY${NEW_VER:+  ($NEW_VER)}"
info "arch:      $ARCH"
info "health:    http://127.0.0.1:${PORT}/api/health"
echo

confirm "Replace the heartd binary and restart the service?" || die "aborted."

# ----- 1. back up the current binary (so a bad update is reversible) -----
BACKUP=""
if [ "$DO_BACKUP" = "yes" ]; then
  BACKUP="${PREFIX_BIN}.bak.$(date +%Y%m%d%H%M%S)"
  c_blue "1/4  Backing up current binary -> ${BACKUP}"
  $SUDO cp -a "$PREFIX_BIN" "$BACKUP"
else
  c_blue "1/4  Skipping backup (--no-backup)"
fi

# ----- 2. stop the service -----
WAS_ACTIVE="no"
if $SUDO systemctl is-active --quiet heartd; then
  WAS_ACTIVE="yes"
fi
c_blue "2/4  Stopping heartd"
$SUDO systemctl stop heartd || true

# ----- 3. swap in the new binary -----
c_blue "3/4  Installing new binary -> ${PREFIX_BIN}"
$SUDO install -m 0755 "$BINARY" "$PREFIX_BIN"

# ----- 4. start + health check -----
restore_backup() {
  if [ -n "$BACKUP" ] && [ -f "$BACKUP" ]; then
    c_yellow "Rolling back to previous binary…"
    $SUDO install -m 0755 "$BACKUP" "$PREFIX_BIN"
    $SUDO systemctl restart heartd || true
    info "restored $PREFIX_BIN from $BACKUP"
  else
    c_yellow "No backup available to roll back to (was --no-backup used?)."
  fi
}

if [ "$DO_START" = "no" ]; then
  c_blue "4/4  Leaving service stopped (--no-start)"
  info "start later with: $SUDO systemctl start heartd"
  echo
  c_green "Done. New binary in place; service not started."
  exit 0
fi

if [ "$WAS_ACTIVE" = "no" ]; then
  info "service was not running before the update — starting it now."
fi

c_blue "4/4  Starting heartd"
$SUDO systemctl start heartd

# Brief health check.
if command -v curl >/dev/null 2>&1; then
  ok="no"
  for _ in 1 2 3 4 5; do
    if curl -fsS "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
      ok="yes"
      break
    fi
    sleep 1
  done
  if [ "$ok" = "yes" ]; then
    echo
    c_green "heartd is responding on http://127.0.0.1:${PORT} — update complete."
    [ -n "$BACKUP" ] && info "Previous binary kept at $BACKUP (remove once you're happy)."
    info "Logs: journalctl -u heartd -f"
    exit 0
  fi

  c_red "heartd did not answer /api/health after the update."
  info "Recent logs:"
  $SUDO journalctl -u heartd -n 20 --no-pager || true
  echo
  if confirm "Roll back to the previous binary?"; then
    restore_backup
    die "rolled back. Investigate the new binary before retrying."
  fi
  die "left new binary in place. Check: journalctl -u heartd -e"
else
  echo
  c_green "Done. New binary installed and service started."
  c_yellow "curl not found — could not health-check. Verify: systemctl status heartd"
fi
