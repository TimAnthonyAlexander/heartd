# heartd vs. Netdata — Gap Analysis (Round 2)

> Regenerated 2026-06-27. Round 1 (disk I/O, load average + swap, alert
> history + live firing-state, Slack/Discord/Telegram channels, custom time
> range + brush zoom) has **shipped** — this is a fresh comparison of what
> heartd has *today* against Netdata, ignoring already-closed gaps.
>
> heartd stays intentionally lean: one static Go binary, embedded dashboard,
> live-reconfigurable, multi-node by peer-polling, no cgo. This round is
> weighted toward **understanding *why* a resource is busy** — the question an
> operator actually asks when CPU spikes — rather than collecting more numbers.

## Where heartd already holds its own (post round 1)

- Single static binary, embedded dashboard, trivial cross-compile.
- Everything runtime-reconfigurable with **no restart**.
- Multi-node peer-polling + shared-secret auth + **leaderless cross-node alert
  dedup** (a lightweight Netdata-Cloud substitute).
- 4 service-check types (http/tcp/process/shell), 9-source custom alert engine
  with sustained-breach + anti-flap timers, 5 notification channels.
- Built-in session auth + user management.
- Time-series history for CPU%, mem, load, swap, network rates, disk I/O, with
  custom ranges, 7-day retention, brush zoom, and an incident timeline.

## The core weakness this round targets

Every resource panel today answers **"how much"** but never **"why"** or
**"who"**. CPU is a single aggregate percentage: you can see it's at 70%, but
not whether that's your application (user time), the kernel (system), waiting on
disk (iowait), a noisy neighbor on a VM (steal), or *which process* is
responsible. Netdata's signature strength is exactly this drill-down
(`apps.plugin` + the Processes function + CPU-state dimensions + per-core
charts). That is the highest-leverage thing heartd is missing.

## Top 8 gaps (ranked by impact × feasibility, current state)

| # | Gap | Why it matters | Effort | Build? |
|---|-----|----------------|--------|--------|
| 1 | **Top processes — per-process CPU & memory** | The #1 operator question: "CPU is at 70% — *what's eating it?*" heartd has zero process-resource attribution today. A live top-N-by-CPU/MEM table (htop-in-the-browser) is the single biggest leap in usefulness. gopsutil's `process` package is already a dependency. | L | ✅ Wave 1 |
| 2 | **CPU state breakdown** (user / system / iowait / steal / idle / nice / irq) | Turns one opaque number into a *diagnosis*: high iowait = disk-bound, high steal = starved VM, high system = syscall/IRQ storm, high user = your code. One `cpu.Times` diff per cycle. | M | ✅ Wave 2 |
| 3 | **Per-core CPU utilization** | A single pegged core (a hot single-threaded process) is invisible in an 8-core average that reads ~12%. Per-core bars expose saturation the aggregate hides. `cpu.Percent(percpu=true)`. | M | ✅ Wave 3 |
| 4 | **Disk *capacity* history + fill-rate forecast** ("full in ~3 days") | heartd keeps disk **capacity as a snapshot only — no history at all**, while it *does* keep I/O history. Capacity trend + a linear fill-rate ETA is the highest-value predictive alert for the most common server outage (disk full). | M | ✅ Wave 4 |
| 5 | **Per-interface network detail** (per-NIC + packets / errors / drops) | Network is a single aggregate rate today. Per-interface rates plus error/drop counters are what actually localize a flaky link or a saturated NIC. `net.IOCounters(pernic=true)`. | M | ✅ Wave 5 |
| 6 | Pressure Stall Information (PSI) | Linux `/proc/pressure/{cpu,memory,io}` — the modern "is contention *hurting* me" signal. Deferred: Linux-only, so invisible on the macOS box being used to verify this round. | S | — |
| 7 | Sensors / temperature | Thermal + fan data. Deferred: gopsutil's reading is unreliable/empty on macOS and many VMs, so it would frequently render blank. | S | — |
| 8 | ML anomaly highlighting / anomaly ribbon | Netdata's k-means consensus + per-chart anomaly ribbon. Deferred: large, and a different product shape than heartd's threshold-rule model. | L | — |

## Explicitly out of scope (unchanged — too large / wrong shape)

- Per-second streaming + tiered downsampling (`dbengine`) — architectural.
- Collector plugin ecosystem, Prometheus/StatsD scraping, cgroups/containers,
  systemd-journal log views, RBAC/SSO/audit.

## Decision

Build gaps **1–5** in five sequential waves, one commit each, weighted to the
"why is CPU busy" question (waves 1–3 all attack CPU attribution directly).
Gaps 6–8 are recorded as the next iteration. Each wave threads its metric
through the established seven-layer path: `metrics` →  `storage` → `collector`
→ `/api/peer/*` → `cluster` poller → dashboard endpoint → frontend hook/panel.
