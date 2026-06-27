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
#   6. Refreshes the optional SMART disk-health collector if you have it, or
#      offers to install it (asks first) — see --diskhealth / --no-diskhealth
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
#   --diskhealth      Install/refresh the optional SMART disk-health collector
#                     (root-owned systemd timer writing /var/lib/diskhealth/smart.json)
#                     without asking.
#   --no-diskhealth   Don't touch or offer the SMART disk-health collector.
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
DISKHEALTH="ask" # ask | yes (--diskhealth) | no (--no-diskhealth)
DOWNLOADED="" # temp file to clean up if we downloaded the binary

PREFIX_BIN="/usr/local/bin/heartd"
UNIT_FILE="/etc/systemd/system/heartd.service"

# Optional SMART disk-health collector (root-owned; heartd stays unprivileged).
# Distinct "heartd-diskhealth" names never collide with a user's own smart-health.* units.
DISKHEALTH_SCRIPT="/usr/local/sbin/heartd-diskhealth.sh"
DISKHEALTH_SERVICE="/etc/systemd/system/heartd-diskhealth.service"
DISKHEALTH_TIMER="/etc/systemd/system/heartd-diskhealth.timer"
DISKHEALTH_DIR="/var/lib/diskhealth"
DISKHEALTH_JSON="${DISKHEALTH_DIR}/smart.json"

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
  --diskhealth)
    DISKHEALTH="yes"
    shift
    ;;
  --no-diskhealth)
    DISKHEALTH="no"
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

# ----- optional SMART disk-health collector (heartd-diskhealth.*) -----
# heartd runs UNPRIVILEGED and never invokes smartctl. This optional, root-owned
# systemd timer is the privileged side that writes the JSON heartd's Disk card
# reads. The collector + unit bodies below are kept BYTE-IDENTICAL to
# packaging/heartd-diskhealth.{sh,service,timer} (the readable source of truth);
# they are embedded here because this script runs via `curl | sudo bash` and
# cannot reach the repo at runtime. The collector heredoc is QUOTED ('EOF') so
# the collector's many shell $vars are NOT expanded by this installer.

# diskhealth_script_body emits the collector script to stdout (verbatim).
diskhealth_script_body() {
  cat <<'HEARTD_DISKHEALTH_SH'
#!/usr/bin/env bash
#
# heartd-diskhealth.sh — root-owned SMART collector for heartd.
#
# heartd runs UNPRIVILEGED and never invokes smartctl itself. This script is the
# privileged side of that split: a small root-owned collector, driven by a
# systemd timer (heartd-diskhealth.timer), that runs `smartctl` against every
# physical disk and writes the JSON file the heartd dashboard's Disk card reads
# for its SMART section:
#
#     /var/lib/diskhealth/smart.json   (schema "model 1" — see docs/DISK_HEALTH_CARD.md)
#
# It writes ONLY that JSON file (no legacy ".status" line). The file is written
# atomically (temp file + mv) and world-readable (0644) so the unprivileged
# heartd service user can read it. If the file is absent or stale heartd simply
# hides the SMART section — declining this collector has zero downside.
#
# This is the canonical source of truth for the collector. install.sh and
# update.sh embed a byte-identical copy of this file as a heredoc (they run via
# `curl | sudo bash` and so cannot reach this repo at runtime). Keep the two in
# sync — see the "embedded copy" note in those scripts.
#
# Standalone usage (normally invoked by the systemd unit, not by hand):
#   sudo /usr/local/sbin/heartd-diskhealth.sh

# NOTE: intentionally NOT `set -e` — smartctl exits non-zero on a disk that has
# health problems, which is exactly the case we must still report. We guard the
# commands that may fail individually instead.
set -uo pipefail

OUT_DIR="/var/lib/diskhealth"
OUT_FILE="${OUT_DIR}/smart.json"

# json_escape escapes the two characters JSON strings must not contain raw: the
# backslash and the double quote. Model/serial strings are the only free-form
# text we emit, and these come from smartctl, so this minimal escaping is enough.
json_escape() {
  local s="$1"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  printf '%s' "$s"
}

# first_int echoes the first run of digits found in its argument, or 0 when there
# is none. Commas are stripped first so grouped numbers ("1,234" — NVMe logs use
# them) parse, and a trailing parenthetical ("38 (Min/Max 19/45)" — ATA temp RAW
# values) is ignored because only the FIRST integer is taken.
first_int() {
  local s="${1//,/}"
  if [[ "$s" =~ ([0-9]+) ]]; then
    printf '%s' "${BASH_REMATCH[1]}"
  else
    printf '0'
  fi
}

# info_field pulls a single "Label: value" line out of smartctl output and echoes
# the trimmed value, or empty when the label is absent.
info_field() {
  local out="$1" label="$2"
  printf '%s\n' "$out" |
    sed -n "s/^${label}:[[:space:]]*\(.*\)\$/\1/p" |
    head -n1 |
    sed 's/[[:space:]]*$//'
}

# attr_raw echoes the first integer of an ATA attribute's RAW_VALUE (the column
# layout is fixed: ID is field 1 and RAW_VALUE begins at field 10 and may run to
# end of line). Missing attribute => 0.
attr_raw() {
  local out="$1" id="$2" line raw
  line="$(printf '%s\n' "$out" | awk -v id="$id" '$1==id {print; exit}')"
  if [ -z "$line" ]; then
    printf '0'
    return
  fi
  raw="$(printf '%s\n' "$line" | awk '{for (i=10; i<=NF; i++) printf "%s ", $i}')"
  first_int "$raw"
}

# parse_health extracts the overall self-assessment word: ATA prints "SMART
# overall-health self-assessment test result: PASSED", some controllers print
# "SMART Health Status: OK". Defaults to UNKNOWN.
parse_health() {
  local out="$1" h=""
  h="$(printf '%s\n' "$out" |
    sed -n 's/.*SMART overall-health self-assessment test result:[[:space:]]*\([^[:space:]]*\).*/\1/p' |
    head -n1)"
  if [ -z "$h" ]; then
    h="$(printf '%s\n' "$out" |
      sed -n 's/.*SMART Health Status:[[:space:]]*\([^[:space:]]*\).*/\1/p' |
      head -n1)"
  fi
  [ -z "$h" ] && h="UNKNOWN"
  printf '%s' "$h"
}

# Ensure smartctl exists; without it there is nothing to collect.
if ! command -v smartctl >/dev/null 2>&1; then
  echo "heartd-diskhealth: smartctl not found (install smartmontools); nothing written" >&2
  exit 0
fi

mkdir -p "$OUT_DIR"

# Enumerate physical disks only (TYPE == disk skips partitions, loop, lvm, rom).
disks="$(lsblk -dno NAME,TYPE 2>/dev/null | awk '$2=="disk"{print $1}')"

disks_json=""
sep=""
for name in $disks; do
  dev="/dev/${name}"

  # One smartctl call gathers identity (-i), overall health (-H) and attributes
  # or the NVMe health log (-A). Non-zero exit (failing disk) is fine; we parse
  # whatever it printed.
  out="$(smartctl -i -H -A "$dev" 2>/dev/null || true)"
  [ -z "$out" ] && continue

  health="$(parse_health "$out")"
  serial="$(info_field "$out" "Serial Number")"

  # Defaults; ATA-only counters stay 0 on NVMe (no equivalent attribute).
  reallocated=0
  pending=0
  uncorrectable=0
  crc_errors=0
  temp_c=0
  power_on_hours=0
  power_cycle_count=0

  is_nvme="no"
  if [[ "$name" == nvme* ]] || printf '%s\n' "$out" | grep -qi 'NVMe'; then
    is_nvme="yes"
  fi

  if [ "$is_nvme" = "yes" ]; then
    # NVMe: no ATA attribute table — read the NVMe SMART/Health log instead.
    model="$(info_field "$out" "Model Number")"
    temp_c="$(first_int "$(info_field "$out" "Temperature")")"
    power_on_hours="$(first_int "$(info_field "$out" "Power On Hours")")"
    power_cycle_count="$(first_int "$(info_field "$out" "Power Cycles")")"
    # Media and Data Integrity Errors is the NVMe analogue of uncorrectable.
    uncorrectable="$(first_int "$(info_field "$out" "Media and Data Integrity Errors")")"
  else
    # ATA/SATA: pull the failure-predicting RAW values by attribute ID.
    model="$(info_field "$out" "Device Model")"
    [ -z "$model" ] && model="$(info_field "$out" "Model Number")"
    reallocated="$(attr_raw "$out" 5)"
    pending="$(attr_raw "$out" 197)"
    uncorrectable="$(attr_raw "$out" 198)"
    crc_errors="$(attr_raw "$out" 199)"
    temp_c="$(attr_raw "$out" 194)"
    power_on_hours="$(attr_raw "$out" 9)"
    power_cycle_count="$(attr_raw "$out" 12)"
  fi

  obj="$(printf '{ "device": "%s", "model": "%s", "serial": "%s", "health": "%s", "reallocated": %s, "pending": %s, "uncorrectable": %s, "crc_errors": %s, "temp_c": %s, "power_on_hours": %s, "power_cycle_count": %s }' \
    "$(json_escape "$dev")" \
    "$(json_escape "$model")" \
    "$(json_escape "$serial")" \
    "$(json_escape "$health")" \
    "$reallocated" "$pending" "$uncorrectable" "$crc_errors" \
    "$temp_c" "$power_on_hours" "$power_cycle_count")"

  disks_json="${disks_json}${sep}${obj}"
  sep=",
    "
done

# RFC3339 with numeric offset, matching the schema's generated_at example.
generated_at="$(date +%Y-%m-%dT%H:%M:%S%:z)"

json="$(printf '{\n  "generated_at": "%s",\n  "disks": [%s%s%s]\n}\n' \
  "$generated_at" \
  "${disks_json:+
    }" \
  "$disks_json" \
  "${disks_json:+
  }")"

# Write atomically: a reader never sees a half-written file.
tmp="$(mktemp "${OUT_DIR}/smart.json.XXXXXX")" || {
  echo "heartd-diskhealth: could not create temp file in ${OUT_DIR}" >&2
  exit 1
}
printf '%s' "$json" >"$tmp"
chmod 0644 "$tmp"
mv -f "$tmp" "$OUT_FILE"
HEARTD_DISKHEALTH_SH
}

# write_diskhealth_collector installs the collector script and its two systemd
# units, then reloads systemd. It does NOT enable/start anything by itself.
write_diskhealth_collector() {
  local tmp
  tmp="$(mktemp)" || die "could not create a temp file"
  diskhealth_script_body >"$tmp"
  $SUDO install -m 0755 "$tmp" "$DISKHEALTH_SCRIPT"
  rm -f "$tmp"

  $SUDO tee "$DISKHEALTH_SERVICE" >/dev/null <<'HEARTD_DISKHEALTH_SERVICE'
# heartd-diskhealth.service — root-owned SMART collector for heartd.
#
# A oneshot run of the collector that writes /var/lib/diskhealth/smart.json.
# Driven by heartd-diskhealth.timer (run via the timer, not enabled directly).
# The distinct "heartd-diskhealth" name never collides with a user's own
# smart-health.* units.
[Unit]
Description=heartd SMART disk-health collector (writes /var/lib/diskhealth/smart.json)
Documentation=https://github.com/timanthonyalexander/heartd
After=multi-user.target

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/heartd-diskhealth.sh
HEARTD_DISKHEALTH_SERVICE

  $SUDO tee "$DISKHEALTH_TIMER" >/dev/null <<'HEARTD_DISKHEALTH_TIMER'
# heartd-diskhealth.timer — periodic SMART collection for heartd.
#
# Runs heartd-diskhealth.service shortly after boot and every 15 minutes
# thereafter. SMART is slow-moving state, so a coarse cadence is plenty and well
# within heartd's 30-minute staleness window. Persistent=true catches up a missed
# run after downtime. The distinct "heartd-diskhealth" name never collides with a
# user's own smart-health.* units.
[Unit]
Description=Periodic heartd SMART disk-health collection

[Timer]
OnBootSec=2min
OnUnitActiveSec=15min
Persistent=true

[Install]
WantedBy=timers.target
HEARTD_DISKHEALTH_TIMER

  $SUDO systemctl daemon-reload
}

# diskhealth_capable returns 0 when this host has at least one physical disk to
# probe (a VM/container with only virtual block devices, or no lsblk, returns 1).
diskhealth_capable() {
  command -v lsblk >/dev/null 2>&1 || return 1
  local disks
  disks="$(lsblk -dno NAME,TYPE 2>/dev/null | awk '$2=="disk"{print $1}')"
  [ -n "$disks" ] || return 1
  return 0
}

# diskhealth_foreign echoes a description of a NON-heartd collector already
# feeding /var/lib/diskhealth and returns 0; returns 1 when none is found. Two
# signals: (1) a systemd unit other than ours referencing the diskhealth dir,
# (2) a smart.json that exists although OUR timer is not installed.
diskhealth_foreign() {
  local hit
  hit="$(grep -rlE '/var/lib/diskhealth' /etc/systemd/system 2>/dev/null | grep -v 'heartd-diskhealth' | head -n1 || true)"
  if [ -n "$hit" ]; then
    echo "systemd unit $hit"
    return 0
  fi
  if [ -f "$DISKHEALTH_JSON" ] && [ ! -f "$DISKHEALTH_TIMER" ]; then
    echo "$DISKHEALTH_JSON (written by an existing collector)"
    return 0
  fi
  return 1
}

# diskhealth_ensure_smartctl makes sure smartctl is available, installing the
# smartmontools package via apt-get when missing. Returns 1 (and explains) when
# smartctl cannot be obtained, so callers can skip gracefully.
diskhealth_ensure_smartctl() {
  if command -v smartctl >/dev/null 2>&1; then
    return 0
  fi
  if command -v apt-get >/dev/null 2>&1; then
    info "Installing smartmontools (provides smartctl)…"
    if $SUDO apt-get install -y smartmontools; then
      return 0
    fi
    c_yellow "Could not install smartmontools — skipping the SMART collector."
    return 1
  fi
  c_yellow "smartctl not found and no apt-get to install it — skipping the SMART collector."
  info "Install your distro's 'smartmontools' package, then re-run with --diskhealth."
  return 1
}

# diskhealth_activate writes the collector + units, enables the timer, and runs
# the collector once now so the dashboard populates without waiting 15 minutes.
diskhealth_activate() {
  write_diskhealth_collector
  $SUDO systemctl enable --now heartd-diskhealth.timer
  $SUDO systemctl start heartd-diskhealth.service || true # populate immediately
}

# maybe_offer_diskhealth_on_update keeps the optional SMART collector current on
# update: if it's OURS, silently refresh the script when it changed; if a foreign
# collector feeds the file, leave it; otherwise offer to install it (same rules
# as install.sh). Honours --diskhealth / --no-diskhealth / --yes / no-TTY.
maybe_offer_diskhealth_on_update() {
  [ "$DISKHEALTH" = "no" ] && return 0

  # Ours already installed: refresh the collector script only when it changed so
  # bug-fixes propagate, staying quiet otherwise.
  if [ -f "$DISKHEALTH_TIMER" ]; then
    local new cur
    new="$(diskhealth_script_body)"
    cur="$(cat "$DISKHEALTH_SCRIPT" 2>/dev/null || true)"
    if [ "$new" != "$cur" ]; then
      write_diskhealth_collector
      $SUDO systemctl enable --now heartd-diskhealth.timer >/dev/null 2>&1 || true
      info "Refreshed the heartd SMART collector to the latest version."
    fi
    return 0
  fi

  # A foreign collector already feeds the file — never clobber it.
  local foreign
  if foreign="$(diskhealth_foreign)"; then
    info "Existing disk-health collector detected (${foreign}) — left untouched."
    return 0
  fi

  # Neither ours nor foreign. Only offer when the box can actually do SMART;
  # stay silent on a plain update otherwise (don't nag).
  diskhealth_capable || return 0
  if ! command -v smartctl >/dev/null 2>&1 && ! command -v apt-get >/dev/null 2>&1; then
    return 0
  fi

  echo
  c_blue "Optional: SMART disk-health collector"
  info "heartd can show per-disk SMART health if a root-owned collector writes"
  info "${DISKHEALTH_JSON}. It isn't installed yet."

  local do_it="no"
  if [ "$DISKHEALTH" = "yes" ]; then
    do_it="yes"
  elif confirm "Install it now? (root-owned; heartd stays unprivileged)"; then
    do_it="yes"
  fi

  if [ "$do_it" != "yes" ]; then
    info "Skipped. Add it later by re-running with --diskhealth (or via install.sh)."
    return 0
  fi

  diskhealth_ensure_smartctl || return 0
  diskhealth_activate
  c_green "Installed heartd's SMART collector -> ${DISKHEALTH_SCRIPT}"
  info "Runs now and every 15 min; heartd shows SMART within a cycle."
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

# Determine where to health-check: parse the unit's ExecStart "-addr HOST:PORT".
# heartd may be bound to a non-loopback address (e.g. a VLAN IP so peers can
# reach it), so we must probe the host it actually listens on — NOT a hardcoded
# 127.0.0.1, which would always fail and trigger a needless rollback. An explicit
# --port flag still overrides the parsed port. Wildcard/empty binds → loopback.
ADDR="$(grep -oE -- '-addr[= ]+[^ ]+' "$UNIT_FILE" 2>/dev/null | head -n1 | sed -E 's/-addr[= ]+//' || true)"
HEALTH_HOST="${ADDR%:*}"
[ -z "$PORT" ] && PORT="${ADDR##*:}"
PORT="${PORT:-9300}"
case "$HEALTH_HOST" in
  "" | "0.0.0.0" | "::" | "[::]" | "*") HEALTH_HOST="127.0.0.1" ;;
esac

# Try to read current vs incoming version (best-effort; heartd may not support --version).
CUR_VER="$("$PREFIX_BIN" --version 2>/dev/null | head -n1 || true)"
NEW_VER="$("$BINARY" --version 2>/dev/null | head -n1 || true)"

c_blue "heartd updater"
info "current:   $PREFIX_BIN${CUR_VER:+  ($CUR_VER)}"
info "new:       $BINARY${NEW_VER:+  ($NEW_VER)}"
info "arch:      $ARCH"
info "health:    http://${HEALTH_HOST}:${PORT}/api/health"
echo

# Confirm before swapping. An interactive run asks; a piped one-liner or --yes
# proceeds — running the updater is the consent, and the current binary is backed
# up and automatically rolled back if the new one fails to start.
if [ "$ASSUME_YES" != "yes" ] && [ -t 0 ]; then
  confirm "Replace the heartd binary and restart the service?" || die "aborted."
elif [ "$ASSUME_YES" != "yes" ]; then
  info "Non-interactive: proceeding (current binary is backed up; auto-rollback on failure)."
fi

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
    if curl -fsS "http://${HEALTH_HOST}:${PORT}/api/health" >/dev/null 2>&1; then
      ok="yes"
      break
    fi
    sleep 1
  done
  if [ "$ok" = "yes" ]; then
    echo
    c_green "heartd is responding on http://${HEALTH_HOST}:${PORT} — update complete."
    [ -n "$BACKUP" ] && info "Previous binary kept at $BACKUP (remove once you're happy)."
    info "Logs: journalctl -u heartd -f"
    # Keep the optional SMART collector current / offer it (asks first).
    maybe_offer_diskhealth_on_update
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
  # Keep the optional SMART collector current / offer it (asks first).
  maybe_offer_diskhealth_on_update
fi
