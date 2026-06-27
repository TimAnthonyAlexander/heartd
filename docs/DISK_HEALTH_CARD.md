# Feature spec: RAID + SMART disk health on the Disk card

## Goal

Surface two pieces of **disk-health information natively in the dashboard's Disk
card**, next to the existing filesystem-usage and Disk-I/O sections:

1. **Software-RAID (mdadm) array state** — is each array clean, degraded,
   rebuilding, or failed?
2. **Per-disk SMART health** — overall self-assessment plus the handful of
   attributes that actually predict drive failure (reallocated / pending /
   uncorrectable sectors, temperature, power-on hours).

This is **informational display only**. It is *not* a service check and *not* an
alert source — heartd already has those layers (`checks`, alert `runner.go`), and
they are unchanged by this feature. (Operators can still point a `check status`
alert rule at a shell check if they want paging; that is orthogonal.)

The data is per-node and must flow through the standard multi-node path so a
peer's disk health shows in the cluster dashboard exactly like its CPU/mem do.

---

## Data source 1 — Linux software RAID (`/proc/mdstat`)

`/proc/mdstat` is **world-readable; no privilege required** — heartd's
unprivileged service user can read it directly.

Real sample (a healthy node, 3 mirrors):

```
Personalities : [raid1] [raid0] [raid6] [raid5] [raid4] [raid10]
md1 : active raid1 sdb2[0] sda2[1]
      1046528 blocks super 1.2 [2/2] [UU]

md2 : active raid1 sdb3[0] sda3[1]
      7808649536 blocks super 1.2 [2/2] [UU]
      bitmap: 3/59 pages [12KB], 65536KB chunk

md0 : active raid1 sdb1[0] sda1[1]
      4189184 blocks super 1.2 [2/2] [UU]

unused devices: <none>
```

### Per-array fields to extract

| Field | Source in the block | Example |
|-------|--------------------|---------|
| name | `mdN :` | `md2` |
| level | token after `active` | `raid1` |
| members | `dev[idx]` tokens | `sda3[1]`, `sdb3[0]` |
| size (blocks) | leading number on line 2 | `7808649536` |
| device count | `[T/A]` — Total / Active | `[2/2]` |
| device state | `[UU]`-style string | `[UU]` |
| rebuild progress | `recovery =`/`resync =` line + `finish=`/`speed=` | (absent when clean) |

### State derivation

- **clean** — `[T/A]` has `T == A` *and* the state string is all `U` (e.g. `[UU]`).
- **degraded** — state string contains `_` (a member down/missing, e.g. `[U_]`),
  or `A < T`, or any member marked `(F)` (faulty).
- **rebuilding / resyncing** — a `recovery =`/`resync =` progress line is present;
  capture the percentage and ETA.
- **failed** — array not `active`, or all members down.

**Important false-positive to avoid:** a member shown as
`active (auto-read-only) ... resync=PENDING` with a healthy `[UU]` is **not
degraded** — it is an idle array with a queued resync that runs on first write.
Key the state off the `[UU]`/`[T/A]`/`(F)` signals, **not** the presence of the
words `resync` or `PENDING`.

Optional richer detail (needs root, so only if a privileged path exists):
`mdadm --detail /dev/mdN` adds `State :`, per-device `active sync`/`faulty`, and
last-resync timing.

---

## Data source 2 — SMART (`smartctl`)

SMART requires **root + raw device access** (`CAP_SYS_RAWIO`); an unprivileged
process cannot read it. See "Privilege constraint" below for how heartd should
obtain it.

Command used: `smartctl -H -A /dev/sdX` (`-H` overall health, `-A` attributes).
Real sample (SATA HGST helium drive):

```
SMART overall-health self-assessment test result: PASSED

ID# ATTRIBUTE_NAME          FLAG     VALUE WORST THRESH TYPE      UPDATED  WHEN_FAILED RAW_VALUE
  5 Reallocated_Sector_Ct   0x0033   100   100   005    Pre-fail  Always       -       0
  9 Power_On_Hours          0x0012   094   094   000    Old_age   Always       -       47798
 12 Power_Cycle_Count       0x0032   100   100   000    Old_age   Always       -       20
194 Temperature_Celsius     0x0002   157   157   000    Old_age   Always       -       38 (Min/Max 19/45)
197 Current_Pending_Sector  0x0022   100   100   000    Old_age   Always       -       0
198 Offline_Uncorrectable   0x0008   100   100   000    Old_age   Offline      -       0
199 UDMA_CRC_Error_Count    0x000a   200   200   000    Old_age   Always       -       0
```

### Per-disk fields to extract

| Field | Where | Notes |
|-------|-------|-------|
| device | argument (`/dev/sda`) | enumerate via `lsblk -dno NAME,TYPE` → `disk` rows |
| model / serial | `smartctl -i` (`Device Model`, `Serial Number`) | optional but useful in UI |
| overall health | `SMART overall-health ... result: PASSED/FAILED` | the headline boolean |
| reallocated sectors | attr **5** RAW_VALUE | >0 = wear; growing = bad |
| pending sectors | attr **197** RAW_VALUE | >0 = urgent (unreadable, awaiting reallocation) |
| offline uncorrectable | attr **198** RAW_VALUE | >0 = urgent |
| UDMA CRC errors | attr **199** RAW_VALUE | cabling/link errors |
| temperature °C | attr **194** RAW_VALUE (first number) | also Min/Max in parens |
| power-on hours | attr **9** RAW_VALUE | drive age (sample drives ≈ 47,798 h ≈ 5.5 yr) |
| power-cycle count | attr **12** RAW_VALUE | optional |

Parsing note: the **RAW_VALUE is the last column**; some rows append a
parenthetical (`341 (Average 397)`, `38 (Min/Max 19/45)`) so take the first
integer of the last field, not the whole field.

### Per-disk health rollup (suggested)

- **ok** — overall PASSED and pending == 0 and uncorrectable == 0.
- **warn** — reallocated > 0 (and stable), or temperature above a soft ceiling.
- **fail** — overall FAILED, or pending > 0, or uncorrectable > 0.

NVMe drives differ (no ATA attribute table; use `smartctl -H` plus the NVMe
health log: `percentage_used`, `media_errors`, `critical_warning`,
`temperature`). The current fleet is all SATA, but a robust parser should branch
on device type rather than assume the ATA table.

---

## Privilege constraint (decide before implementing SMART)

heartd's service runs unprivileged and sandboxed (`User=heartd`,
`NoNewPrivileges=true`, `ProtectSystem=strict`). Consequences:

- **RAID is free** — `/proc/mdstat` is readable as-is; the collector can parse it
  inline every cycle.
- **SMART is not** — `smartctl` needs raw device access the sandbox denies, and
  `NoNewPrivileges` blocks `sudo`.

Three viable models for getting SMART into heartd; the implementer should pick one
(or support more than one):

1. **Read an external status file** *(lowest privilege, recommended default).*
   A root-owned, host-side collector (systemd timer) runs `smartctl` periodically
   and writes a small machine-readable file (e.g. JSON at a documented path such
   as `/var/lib/diskhealth/smart.json`); heartd reads and surfaces it, and marks
   the data **stale** if the file's mtime is older than a threshold. Keeps heartd
   fully unprivileged. (This mirrors how the data is collected on the current
   fleet today.)
2. **Invoke a privileged helper** *(opt-in).* heartd shells out to `smartctl`
   only when the operator has granted access — e.g. `setcap cap_sys_rawio+ep` on a
   wrapper, a `sudoers` NOPASSWD entry plus relaxing `NoNewPrivileges`, or
   widening `DeviceAllow`/`ReadWritePaths`. Document the exact unit changes.
3. **Bridge `smartd`** — parse the output/state of the distro `smartd` daemon if
   present.

Recommended: implement (1) as the portable default (define and document the JSON
schema heartd expects), and optionally (2) behind a config flag for single-box
installs that prefer no external collector.

---

## Suggested data model & storage

Follow the existing storage conventions (single `schemaSQL` constant, **singular
snake_case** table names, UTC unix-epoch integers, `uint64`→INTEGER, `scan*`
helpers, `storage:` error prefix), keyed by **node name** so peer rows coexist
with local rows.

Two new tables (latest-state is enough; history optional):

- `raid_array(node, name, level, state, total_devices, active_devices,
  resync_percent, detail, at)`
- `smart_disk(node, device, model, health, reallocated, pending, uncorrectable,
  crc_errors, temp_c, power_on_hours, at)`

A small rolling history of `smart_disk.pending` / `.reallocated` would later
enable trend/forecast ("pending sectors climbing"), consistent with the
disk-history direction noted in the gap analysis — optional for v1.

## Wiring (the four-places rule)

Per heartd's "add a new metric type, wire it in all four places" convention:

1. **collector** — sample RAID (parse `/proc/mdstat`) and SMART (per the chosen
   privilege model) into the new tables for the local node. SMART changes slowly:
   sample it on a **slower cadence** (e.g. every few minutes) rather than every
   metrics tick; RAID parsing is cheap and can run at the normal interval. Read
   any interval/threshold from settings so it stays live-reconfigurable.
2. **`/api/peer/...`** — expose the local node's RAID + SMART state (e.g.
   `GET /api/peer/diskhealth`).
3. **poller (`internal/cluster`)** — fetch each peer's disk health and store it
   under the **peer's name** in the same tables.
4. **dashboard endpoint** — serve it via `GET /api/nodes/{name}/diskhealth` (or
   fold it into the existing per-node disk endpoint the Disk card already calls).

## UI — Disk card additions

Extend the existing **Disk card** (today: Disk-I/O throughput/IOPS + filesystem
usage). Add two compact subsections, styled with the theme tokens (`colors`,
`statusColor`, `percentColor`) and inline SVG (no `@mui/icons-material`):

- **RAID** — one row per array: `md2  raid1  [UU]  clean`, with the state string
  colored (green all-`U`, amber rebuilding + `%`, red degraded/failed). Show
  rebuild progress when present.
- **SMART** — one chip/row per disk: device + model, a PASSED/FAILED badge, and
  the key counters (reallocated / pending / uncorrectable), temperature, and
  power-on hours. Color by the per-disk rollup. Surface a "stale" indicator if
  the SMART data is older than its freshness threshold.

A single **disk-health badge** on the card header (green/amber/red) summarizing
the worst of {RAID state, SMART rollup} gives an at-a-glance signal.

## Explicitly out of scope

- Not a `check` and not an alert rule — those layers are separate and unchanged.
- No per-second sampling; disk health is slow-moving state.
- Hardware-RAID controllers (megacli/storcli) — current fleet is mdadm + AHCI.
