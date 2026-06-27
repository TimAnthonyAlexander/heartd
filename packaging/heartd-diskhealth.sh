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
