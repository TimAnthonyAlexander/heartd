# heartd vs. Netdata — Gap Analysis

> Generated 2026-06-26 by comparing a feature/UI inventory of **Netdata**
> (per-second real-time monitoring) against heartd's current implementation.
> heartd is intentionally leaner — a single static Go binary with an embedded
> dashboard, live-reconfigurable, multi-node by peer-polling. This document
> ranks the highest-leverage gaps and records which were chosen for build-out.

## Where heartd already holds its own

- Single static binary, embedded dashboard, no cgo, trivial cross-compile.
- Everything runtime-reconfigurable with **no restart** (collector, scheduler,
  alert engine all read settings fresh each cycle).
- Built-in multi-node polling with shared-secret auth + **leaderless cross-node
  alert dedup** — a lightweight alternative to Netdata Cloud.
- 4 service-check types (http/tcp/process/shell) + a flexible 9-source custom
  alert-rule engine with sustained-breach and anti-flap timers.
- Built-in session auth + user management (Netdata splits this across Agent +
  Cloud).

## Top 8 gaps (ranked by impact × feasibility)

| # | Gap | Why it matters | Effort | Build? |
|---|-----|----------------|--------|--------|
| 1 | **Disk I/O metrics** (read/write throughput, IOPS, per device) + disk history | heartd stores disk *capacity* only and keeps **no disk history at all** — a glaring blind spot for the single most common server problem (a saturated or filling disk). | M | ✅ |
| 2 | **Load average + Swap + richer memory** | Load average is the canonical "is this box healthy" number and is entirely absent. Swap usage is a leading OOM indicator. Both are one gopsutil call away. | S | ✅ |
| 3 | **Alert history & live firing-state view** | The Alerts UI shows *configured rules only* and explicitly **not** whether they're firing. There is no record of past incidents anywhere. Operators are flying blind on the one thing a monitor exists to surface. | M | ✅ |
| 4 | **More notification channels (Slack / Discord / Telegram)** | Only Email + generic Webhook exist. Slack/Discord/Telegram are where teams actually watch for alerts; each is a small, well-shaped adapter. | M | ✅ |
| 5 | **Advanced time controls + chart zoom** | Only 15m / 1h / 24h presets; no custom range, no 7-day view, no zoom/brush/pan despite 7 days of data being retained. The charting weakness most visible day-to-day. | M | ✅ |
| 6 | Disk-space history + fill-rate forecast ("disk full in ~3 days") | Predictive alerting; unlocked once disk history (gap #1) exists. | M | — |
| 7 | Per-interface network detail (per-NIC, packets, errors/drops) | Network is aggregate-only today; per-interface + error counters aid real diagnosis. | M | — |
| 8 | Light theme toggle / faster (near-real-time) sampling option | Polish + the "feels live" factor Netdata is loved for. | S | — |

## Explicitly out of scope (too large / wrong shape for heartd)

- **Per-second streaming** and tiered downsampling (`dbengine`) — architectural.
- **ML anomaly detection** (18-model k-means consensus, Anomaly Advisor) — large.
- Collector plugin ecosystem, Prometheus/StatsD scraping, processes/cgroups/
  containers, sensors/temperature, RBAC/SSO/audit.

## Decision

Implement gaps **1–5** in five sequential waves, one commit each. Gaps 6–8 are
recorded here as the natural next iteration (gap 6 in particular becomes cheap
once gap 1 lands the disk-history table).
