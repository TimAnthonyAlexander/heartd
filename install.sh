#!/usr/bin/env bash
#
# install.sh — install heartd as a systemd service on Linux.
#
# One-liner (downloads the latest release binary for your CPU, no clone needed):
#   curl -fsSL https://raw.githubusercontent.com/timanthonyalexander/heartd/main/install.sh | sudo bash
#   curl -fsSL https://raw.githubusercontent.com/timanthonyalexander/heartd/main/install.sh | sudo bash -s -- --domain heartd.example.com
#
# What it does (each step is privileged via sudo when you aren't root):
#   1. Installs a heartd binary to /usr/local/bin/heartd (a local one if present,
#      otherwise downloaded from GitHub Releases for your architecture)
#   2. Creates a dedicated, unprivileged `heartd` system user + group
#   3. Creates /etc/heartd (config) and /var/lib/heartd (database)
#   4. Writes /etc/heartd/heartd.yaml (only if absent — never clobbers yours)
#   5. Installs and starts a hardened systemd unit bound to the address you pick
#      (it asks: 127.0.0.1, 0.0.0.0, or a specific IP; default 127.0.0.1)
#   6. Prints an nginx reverse-proxy block for you to add (does NOT touch nginx)
#   7. Optionally installs a root-owned SMART disk-health collector — asks first
#   8. Optionally adds a firewall rule — but ONLY after asking
#
# It does NOT configure nginx or TLS for you. By default heartd listens on
# localhost and is meant to sit behind your own reverse proxy; you may instead
# bind it to a public IP (plain HTTP) when prompted or via --bind.
#
# Usage:
#   sudo ./install.sh [options]
#
# Options:
#   --binary PATH     Path to a linux heartd binary to install. If omitted, the
#                     script looks for ./bin/heartd-linux-<arch> then ./heartd,
#                     and finally downloads it from GitHub Releases.
#   --version TAG     Release tag to download (default: latest). E.g. v1.2.0.
#   --repo OWNER/REPO GitHub repo to download from (default: timanthonyalexander/heartd).
#   --name NAME       Node name shown in the dashboard (default: this hostname).
#   --domain HOST     Public hostname (e.g. heartd.example.com). Used for the
#                     printed nginx block and as advertise_url in a fresh config.
#   --port PORT       Localhost port heartd binds to (default: 9300).
#   --force-config    Overwrite an existing /etc/heartd/heartd.yaml (a timestamped
#                     backup is kept).
#   --no-start        Install everything but don't enable/start the service.
#   --yes             Assume "yes" for prompts (non-interactive). Still does NOT
#                     change the firewall unless combined with --ufw.
#   --ufw             Allow the firewall step to run (still prints what it does).
#   --diskhealth      Install the optional SMART disk-health collector without
#                     asking (a root-owned systemd timer that runs smartctl and
#                     writes /var/lib/diskhealth/smart.json for the Disk card).
#   --no-diskhealth   Never offer/install the SMART disk-health collector.
#   --headless        Install as a headless AGENT: no dashboard/nginx/TLS. Binds
#                     0.0.0.0:<port>, serves only health + the secret-protected
#                     peer API, and prints how to add it on your HQ node.
#   --secret SECRET   Shared secret for the peer link (headless mode). If omitted
#                     in --headless, one is generated and printed.
#   --bind HOST       Bind address override: 127.0.0.1 (local; front it with your
#                     own TLS), 0.0.0.0 (all interfaces, plain HTTP), or a specific
#                     IP. If not given, the installer asks interactively (both
#                     dashboard and headless installs); piped/--yes runs use the
#                     per-mode default (dashboard 127.0.0.1, headless 0.0.0.0).
#   -h, --help        Show this help and exit.
#
# Headless agent one-liner (public box you just want HQ to watch):
#   curl -fsSL https://raw.githubusercontent.com/timanthonyalexander/heartd/main/install.sh | sudo bash -s -- --headless --secret <SHARED_SECRET>

set -euo pipefail

# ----- defaults -----
BINARY=""
VERSION="latest"
REPO="timanthonyalexander/heartd"
NODE_NAME=""
DOMAIN=""
PORT="9300"
FORCE_CONFIG="no"
DO_START="yes"
ASSUME_YES="no"
ALLOW_UFW="no"
DISKHEALTH="ask" # ask | yes (--diskhealth) | no (--no-diskhealth)
HEADLESS="no"
SECRET=""
BIND_HOST_OVERRIDE="" # 127.0.0.1 or 0.0.0.0; empty = ask (headless) / default
DOWNLOADED="" # temp file to clean up if we downloaded the binary

PREFIX_BIN="/usr/local/bin/heartd"
CONFIG_DIR="/etc/heartd"
CONFIG_FILE="${CONFIG_DIR}/heartd.yaml"
DATA_DIR="/var/lib/heartd"
UNIT_FILE="/etc/systemd/system/heartd.service"
SVC_USER="heartd"
SVC_GROUP="heartd"

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
  --name)
    NODE_NAME="${2:-}"
    shift 2
    ;;
  --domain)
    DOMAIN="${2:-}"
    shift 2
    ;;
  --port)
    PORT="${2:-}"
    shift 2
    ;;
  --force-config)
    FORCE_CONFIG="yes"
    shift
    ;;
  --no-start)
    DO_START="no"
    shift
    ;;
  --yes)
    ASSUME_YES="yes"
    shift
    ;;
  --headless)
    HEADLESS="yes"
    shift
    ;;
  --secret)
    SECRET="${2:-}"
    shift 2
    ;;
  --bind)
    BIND_HOST_OVERRIDE="${2:-}"
    shift 2
    ;;
  --ufw)
    ALLOW_UFW="yes"
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
# Reads from /dev/tty (not stdin) so prompts still work under `curl | bash`,
# where stdin is the piped script. Falls back to "no" when no terminal exists.
confirm() {
  local prompt="$1"
  if [ "$ASSUME_YES" = "yes" ]; then return 0; fi
  if [ ! -r /dev/tty ]; then return 1; fi # no terminal -> treat as "no"
  local reply
  read -r -p "$prompt [y/N] " reply </dev/tty || true
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

# maybe_install_diskhealth_collector offers the optional SMART collector during a
# fresh install. Mirrors the firewall step: asks first, default NO. Honours
# --diskhealth (force on), --no-diskhealth (force off), --yes, and no-TTY runs.
maybe_install_diskhealth_collector() {
  [ "$DISKHEALTH" = "no" ] && return 0

  echo
  c_blue "Optional: SMART disk-health collector"
  info "A small root-owned systemd timer runs smartctl and writes"
  info "${DISKHEALTH_JSON}; heartd reads it for the Disk card's SMART section."
  info "heartd itself stays unprivileged. Declining has zero downside."

  if ! diskhealth_capable; then
    info "No physical disks detected here (VM/container?) — skipping."
    return 0
  fi

  local foreign
  if foreign="$(diskhealth_foreign)"; then
    info "An existing disk-health collector was detected: ${foreign}."
    info "Leaving it untouched — heartd reads whatever writes ${DISKHEALTH_JSON}."
    return 0
  fi

  local refresh="no"
  [ -f "$DISKHEALTH_TIMER" ] && refresh="yes"

  # Decide whether to proceed. --diskhealth forces yes; otherwise ask (confirm
  # defaults to NO, honours --yes, and is "no" on a non-interactive shell).
  local do_it="no"
  if [ "$DISKHEALTH" = "yes" ]; then
    do_it="yes"
  elif [ "$refresh" = "yes" ]; then
    if confirm "heartd's SMART collector is already installed — refresh it to this version?"; then do_it="yes"; fi
  else
    if confirm "Install it now? (root-owned; heartd stays unprivileged)"; then do_it="yes"; fi
  fi

  if [ "$do_it" != "yes" ]; then
    if [ "$refresh" = "yes" ]; then
      info "Left the existing heartd SMART collector unchanged."
    elif [ ! -r /dev/tty ] && [ "$ASSUME_YES" != "yes" ]; then
      info "Non-interactive run — skipping. Enable later: re-run install.sh with --diskhealth."
    else
      info "Skipped. Enable later by re-running install.sh with --diskhealth."
    fi
    return 0
  fi

  diskhealth_ensure_smartctl || return 0
  diskhealth_activate

  if [ "$refresh" = "yes" ]; then
    c_green "Refreshed heartd's SMART collector -> ${DISKHEALTH_SCRIPT}"
  else
    c_green "Installed heartd's SMART collector -> ${DISKHEALTH_SCRIPT}"
  fi
  info "Runs now and every 15 min, writing ${DISKHEALTH_JSON}."
  info "heartd shows SMART on the Disk card within a cycle."
}

# ----- preflight -----
[ "$(uname -s)" = "Linux" ] || die "this installer is for Linux only (you're on $(uname -s))."
[ -d /run/systemd/system ] || die "systemd not detected; this installer requires systemd."

# Privilege escalation: use sudo when not already root.
if [ "$(id -u)" -eq 0 ]; then
  SUDO=""
else
  command -v sudo >/dev/null 2>&1 || die "not root and sudo not found. Re-run as root."
  SUDO="sudo"
  c_yellow "Not running as root — privileged steps will use sudo (you may be prompted)."
fi

# Detect CPU arch and map to Go's naming.
case "$(uname -m)" in
x86_64 | amd64) ARCH="amd64" ;;
aarch64 | arm64) ARCH="arm64" ;;
*) die "unsupported architecture: $(uname -m) (heartd ships amd64 and arm64)." ;;
esac

# download_binary fetches the release asset for this architecture into a temp
# file and points BINARY at it. Used when no local binary is available (e.g. the
# `curl | bash` one-liner).
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

# Resolve the binary to install: explicit --binary, then a local build, then a
# download from GitHub Releases.
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

NODE_NAME="${NODE_NAME:-$(hostname)}"

# gen_secret prints a random hex secret (openssl, else /dev/urandom).
gen_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  else
    LC_ALL=C head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'
  fi
}

# detect_ip best-effort: public IP, else first local IPv4, else a placeholder.
detect_ip() {
  local ip=""
  if command -v curl >/dev/null 2>&1; then
    ip="$(curl -fsS --max-time 4 https://api.ipify.org 2>/dev/null || true)"
  fi
  [ -z "$ip" ] && ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
  [ -z "$ip" ] && ip="<this-server-ip>"
  echo "$ip"
}

# In headless mode, ensure we have a shared secret (generate one if not given).
if [ "$HEADLESS" = "yes" ] && [ -z "$SECRET" ]; then
  SECRET="$(gen_secret)"
  SECRET_GENERATED="yes"
fi

# Bind address. --bind overrides everything and is taken verbatim. Otherwise we
# ask interactively (both dashboard and headless modes); when piped / --yes we
# fall back to a per-mode default: dashboard → 127.0.0.1 (front it with your own
# reverse proxy + TLS), headless → 0.0.0.0 (reached directly by your HQ).
if [ "$HEADLESS" = "yes" ]; then
  DEFAULT_BIND_HOST="0.0.0.0"
else
  DEFAULT_BIND_HOST="127.0.0.1"
fi

# ask_bind_host prompts for a bind address and echoes the chosen host. Accepts a
# blank line (use the default), the shortcuts 1/2, or a literal address typed in.
ask_bind_host() {
  local default="$1" reply
  c_blue "Which address should heartd bind to?" >&2
  info "  1) 127.0.0.1  — localhost only; front it with your own reverse proxy + TLS (recommended)" >&2
  info "  2) 0.0.0.0    — all interfaces; reachable directly over plain HTTP on every IP" >&2
  info "  or type a specific IP on this host (e.g. ${PUBLIC_IP_HINT})" >&2
  read -r -p "Bind address [default ${default}]: " reply </dev/tty || reply=""
  case "$reply" in
  "") echo "$default" ;;
  1) echo "127.0.0.1" ;;
  2) echo "0.0.0.0" ;;
  *) echo "$reply" ;;
  esac
}

if [ -n "$BIND_HOST_OVERRIDE" ]; then
  BIND_HOST="$BIND_HOST_OVERRIDE"
elif [ "$ASSUME_YES" != "yes" ] && [ -r /dev/tty ]; then
  # Read from /dev/tty (not stdin) so this still prompts under `curl | bash`,
  # where stdin is the piped script rather than the terminal.
  PUBLIC_IP_HINT="$(detect_ip)"
  BIND_HOST="$(ask_bind_host "$DEFAULT_BIND_HOST")"
else
  BIND_HOST="$DEFAULT_BIND_HOST" # non-interactive: pass --bind to change
fi
BIND="${BIND_HOST}:${PORT}"

# Where to health-check after start. 0.0.0.0 includes loopback; a specific
# public/private IP may not, so probe the bound host directly in that case.
if [ "$BIND_HOST" = "0.0.0.0" ] || [ "$BIND_HOST" = "127.0.0.1" ]; then
  HEALTH_HOST="127.0.0.1"
else
  HEALTH_HOST="$BIND_HOST"
fi

c_blue "heartd installer"
info "binary:    $BINARY"
info "arch:      $ARCH"
info "node name: $NODE_NAME"
if [ "$HEADLESS" = "yes" ]; then
  info "mode:      headless agent (no dashboard; managed from your HQ)"
  if [ "$BIND_HOST" = "127.0.0.1" ]; then
    info "bind:      ${BIND}  (localhost — front it with your own nginx/TLS)"
  else
    info "bind:      ${BIND}  (reachable directly by your HQ over HTTP)"
  fi
else
  info "mode:      dashboard (behind your reverse proxy)"
  if [ "$BIND_HOST" = "127.0.0.1" ]; then
    info "bind:      ${BIND}  (localhost only — front it with your own reverse proxy/TLS)"
  else
    info "bind:      ${BIND}  (reachable directly over plain HTTP — no TLS!)"
  fi
fi
info "config:    $CONFIG_FILE"
info "database:  ${DATA_DIR}/heartd.db"
echo

# ----- 1. install binary -----
c_blue "1/6  Installing binary -> ${PREFIX_BIN}"
$SUDO install -m 0755 "$BINARY" "$PREFIX_BIN"

# ----- 2. system user + group -----
c_blue "2/6  Creating ${SVC_USER} system user"
if ! getent group "$SVC_GROUP" >/dev/null 2>&1; then
  $SUDO groupadd --system "$SVC_GROUP"
  info "created group $SVC_GROUP"
else
  info "group $SVC_GROUP already exists"
fi
if ! id "$SVC_USER" >/dev/null 2>&1; then
  $SUDO useradd --system --gid "$SVC_GROUP" --no-create-home \
    --home-dir "$DATA_DIR" --shell /usr/sbin/nologin "$SVC_USER"
  info "created user $SVC_USER"
else
  info "user $SVC_USER already exists"
fi

# ----- 3. directories -----
c_blue "3/6  Creating directories"
$SUDO mkdir -p "$CONFIG_DIR" "$DATA_DIR"
$SUDO chown "${SVC_USER}:${SVC_GROUP}" "$DATA_DIR"
$SUDO chmod 0750 "$DATA_DIR"

# ----- 4. config (never clobber an existing one unless --force-config) -----
c_blue "4/6  Writing config"
ADVERTISE_LINE="# advertise_url:                       # set to https://<your-domain> once peers exist"
if [ -n "$DOMAIN" ]; then
  ADVERTISE_LINE="advertise_url: https://${DOMAIN}"
fi

write_config() {
  if [ "$HEADLESS" = "yes" ]; then
    $SUDO tee "$CONFIG_FILE" >/dev/null <<YAML
# heartd HEADLESS AGENT config — generated by install.sh.
# This node has no dashboard. It exposes only /api/health and the
# secret-protected peer API, and is configured remotely from your HQ node.

server:
  name: ${NODE_NAME}
  headless: true
  peer_secret: ${SECRET}     # the HQ must use this same secret when adding this node
  metrics_interval: 30s
  retention: 30d
  db_path: ${DATA_DIR}/heartd.db

# Seeds the default CPU/memory/disk alert rules on first run. Configure checks,
# notifications, and alerts for this node from your HQ's dashboard.
thresholds:
  cpu_percent: 90
  mem_percent: 90
  disk_percent: 90
YAML
  else
    $SUDO tee "$CONFIG_FILE" >/dev/null <<YAML
# heartd configuration — generated by install.sh.
# Most operational settings (checks, notifications, alerts, intervals) are
# editable live from the dashboard; this file only seeds them on first run.
# Durations accept Go units plus "d" (days): 30s, 5m, 1h, 7d.

server:
  name: ${NODE_NAME}
  metrics_interval: 30s
  retention: 30d
  db_path: ${DATA_DIR}/heartd.db
  peer_poll_interval: 15s
  ${ADVERTISE_LINE}

# Seeds the default CPU/memory/disk alert rules on first run (percent; 0 skips a
# metric). Edit alerts live on each node's Alerts tab thereafter.
thresholds:
  cpu_percent: 90
  mem_percent: 90
  disk_percent: 90

# Add peers / checks / notify here, or (preferred) from the dashboard tabs.
YAML
  fi
  $SUDO chown "root:${SVC_GROUP}" "$CONFIG_FILE"
  $SUDO chmod 0640 "$CONFIG_FILE"
}

if [ -f "$CONFIG_FILE" ] && [ "$FORCE_CONFIG" != "yes" ]; then
  info "$CONFIG_FILE already exists — keeping it (use --force-config to replace)."
elif [ -f "$CONFIG_FILE" ]; then
  BACKUP="${CONFIG_FILE}.bak.$(date +%Y%m%d%H%M%S)"
  $SUDO cp -a "$CONFIG_FILE" "$BACKUP"
  info "backed up existing config to $BACKUP"
  write_config
  info "wrote fresh $CONFIG_FILE"
else
  write_config
  info "wrote $CONFIG_FILE"
fi

# ----- 5. systemd unit -----
c_blue "5/6  Installing systemd service"
$SUDO tee "$UNIT_FILE" >/dev/null <<UNIT
[Unit]
Description=heartd server health monitor
Documentation=https://github.com/timanthonyalexander/heartd
After=network-online.target
Wants=network-online.target

[Service]
User=${SVC_USER}
Group=${SVC_GROUP}
ExecStart=${PREFIX_BIN} -config ${CONFIG_FILE} -addr ${BIND}
Restart=on-failure
RestartSec=2

# Sandboxing. NOTE: ProtectSystem=strict makes the FS read-only except the paths
# below. http/tcp/process metric checks work fine; a 'shell' check that writes or
# reads a protected path needs its path added to ReadWritePaths/ReadOnlyPaths.
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=${DATA_DIR}
ProtectHome=true
PrivateTmp=true
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true

[Install]
WantedBy=multi-user.target
UNIT

$SUDO systemctl daemon-reload

if [ "$DO_START" = "yes" ]; then
  $SUDO systemctl enable --now heartd
  info "enabled and started heartd.service"

  # Brief health check against the address we actually bound.
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
      c_green "heartd is responding on http://${HEALTH_HOST}:${PORT}"
    else
      c_yellow "heartd did not answer /api/health yet — check: journalctl -u heartd -e"
    fi
  fi
else
  info "skipping start (--no-start). Start later with: $SUDO systemctl enable --now heartd"
fi

# ----- optional SMART disk-health collector (asks first; see firewall step) -----
maybe_install_diskhealth_collector

# ----- Headless agent: print how to add it on the HQ, then finish -----
if [ "$HEADLESS" = "yes" ]; then
  echo
  c_blue "Headless agent ready — add it on your HQ"

  if [ "$BIND_HOST" = "127.0.0.1" ]; then
    # Behind-your-own-TLS path: bound to localhost, user fronts it with a proxy.
    cat <<TXT

This node is bound to 127.0.0.1:${PORT} and has no dashboard. Front it with your
own reverse proxy + TLS, then add it on your HQ with the HTTPS URL.

Caddy (auto-HTTPS) — /etc/caddy/Caddyfile:

  agent.example.com {
      reverse_proxy 127.0.0.1:${PORT}
  }

…or nginx: proxy_pass http://127.0.0.1:${PORT};  then  certbot --nginx -d agent.example.com

On your HQ's dashboard, click "+ Add node":

    name:    ${NODE_NAME}
    URL:     https://agent.example.com
    secret:  ${SECRET}

TXT
  else
    # Direct/plain-HTTP path: reachable on IP:port.
    AGENT_IP="$(detect_ip)"
    cat <<TXT

This node has no dashboard. On your HQ node's dashboard, click "+ Add node":

    name:    ${NODE_NAME}
    URL:     http://${AGENT_IP}:${PORT}
    secret:  ${SECRET}

TXT
  fi

  cat <<TXT
Then configure its checks, alerts, and notifications from the HQ — those edits
proxy to this node over the peer link.

TXT

  if [ "${SECRET_GENERATED:-no}" = "yes" ]; then
    c_yellow "A shared secret was generated. Use the SAME secret on every agent and"
    c_yellow "when adding them on the HQ. It won't be shown again:"
    info "  ${SECRET}"
    echo
  fi
  if [ "$BIND_HOST" = "127.0.0.1" ]; then
    info "Your reverse proxy gives you TLS, so the peer link is encrypted."
  else
    c_yellow "Heads up: the peer API is plain HTTP. On a public network the secret and"
    info "metrics travel unencrypted — prefer a private network / VPN, or use --bind 127.0.0.1"
    info "with your own TLS proxy. Make sure port ${PORT} is reachable from your HQ."
  fi
  echo
  c_green "Done."
  info "Logs:   journalctl -u heartd -f"
  info "Config: $CONFIG_FILE   (restart after edits: $SUDO systemctl restart heartd)"
  exit 0
fi

# ----- 6. nginx instructions (printed, never applied) -----
c_blue "6/6  Reverse proxy (manual step — nothing was changed)"
NGINX_HOST="${DOMAIN:-heartd.example.com}"

# If the dashboard was bound to a public/non-loopback address, it is reachable
# directly over plain HTTP. Warn loudly and still recommend fronting with TLS.
if [ "$BIND_HOST" != "127.0.0.1" ]; then
  DASH_IP="$BIND_HOST"
  [ "$DASH_IP" = "0.0.0.0" ] && DASH_IP="$(detect_ip)"
  c_yellow "You bound the dashboard to ${BIND} — it is reachable directly at:"
  info "  http://${DASH_IP}:${PORT}/"
  c_yellow "This is PLAIN HTTP: login credentials and the session cookie are sent"
  info "unencrypted. Fine on a trusted LAN/VPN; for anything internet-facing, bind"
  info "127.0.0.1 instead and put nginx + TLS in front (see below). You must also"
  info "open port ${PORT} in your firewall for it to be reachable."
  echo
fi

cat <<NGINX

heartd serves plain HTTP on ${BIND}. Put it behind your existing nginx
and terminate TLS there. Add a server block like this:

  # /etc/nginx/sites-available/heartd
  server {
      listen 80;
      listen [::]:80;
      server_name ${NGINX_HOST};

      location / {
          proxy_pass http://127.0.0.1:${PORT};
          proxy_http_version 1.1;
          proxy_set_header Host              \$host;
          proxy_set_header X-Real-IP         \$remote_addr;
          proxy_set_header X-Forwarded-For   \$proxy_add_x_forwarded_for;
          proxy_set_header X-Forwarded-Proto \$scheme;
          proxy_read_timeout 30s;
      }
  }

Then enable it and add TLS (no websocket config needed):

  sudo ln -s /etc/nginx/sites-available/heartd /etc/nginx/sites-enabled/heartd
  sudo nginx -t && sudo systemctl reload nginx
  sudo certbot --nginx -d ${NGINX_HOST}

NGINX

# ----- optional firewall step (asks first) -----
if command -v ufw >/dev/null 2>&1 && $SUDO ufw status 2>/dev/null | grep -q "Status: active"; then
  if [ "$BIND_HOST" != "127.0.0.1" ]; then
    # Bound publicly: heartd is reached directly, so its own port must be open.
    c_yellow "ufw is active and heartd is bound to ${BIND} (reachable directly)."
    if [ "$ALLOW_UFW" = "yes" ] || confirm "Open port ${PORT}/tcp through ufw now?"; then
      $SUDO ufw allow "${PORT}/tcp"
      c_green "added ufw rule for ${PORT}/tcp."
    else
      info "left firewall unchanged. To open it later: sudo ufw allow ${PORT}/tcp"
    fi
  else
    c_yellow "ufw is active. heartd itself needs NO open port (it's localhost-only)."
    info "Public access goes through nginx on 80/443, which you may already allow."
    if [ "$ALLOW_UFW" = "yes" ] || confirm "Allow nginx (80,443) through ufw now? ('Nginx Full')"; then
      $SUDO ufw allow 'Nginx Full'
      c_green "added ufw rule 'Nginx Full' (80,443). Port ${PORT} remains closed."
    else
      info "left firewall unchanged. To allow nginx later: sudo ufw allow 'Nginx Full'"
    fi
  fi
fi

echo
c_green "Done."
info "Next: point your nginx block above at this host, get a cert, then open"
info "  https://${NGINX_HOST}/  and create the first admin account."
info "Logs:    journalctl -u heartd -f"
info "Config:  $CONFIG_FILE   (restart after edits: $SUDO systemctl restart heartd)"
