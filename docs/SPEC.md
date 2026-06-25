# heartd — Product Specification

## What is heartd?

heartd is a lightweight, self-hosted server health monitoring tool. It is designed to be installed on any Linux or macOS server in minutes, with zero dependencies beyond the single binary. It gives operators a real-time view of their infrastructure's health: system resources, running services, and reachability of external endpoints — all from a clean web dashboard.

heartd is intentionally not Netdata. Netdata is a full observability platform with per-second metrics, machine learning anomaly detection, 800+ integrations, and a cloud backend. It is powerful but heavy, complex to configure, and aimed at larger infrastructure teams. heartd targets individual developers and small teams who run a handful of servers and need a simple, honest answer to "is everything okay?".

---

## Core Philosophy

- One binary. No runtime, no package manager, no web server required.
- One config file. Plain YAML, human-readable, lives next to the binary.
- Installable in under two minutes on any server.
- Designed for 2–10 nodes, not thousands.
- Alerts when things break. Stays quiet when they don't.

---

## Technology

- Written in **Go**. Compiles to a single static binary for Linux (amd64, arm64) and macOS.
- Serves a **React** web dashboard embedded directly in the binary. No separate frontend deployment.
- Communicates over **REST** (JSON over HTTP).
- Default port: **`9300`**

---

## Features

### 1. System Metrics

heartd collects core server health metrics at a configurable interval (default: every 30 seconds):

- **CPU** — overall usage percentage
- **Memory** — used / total, usage percentage
- **Disk** — used / total per mount point, usage percentage
- **Network** — bytes in/out per interface

Metrics are stored in a local SQLite database with a rolling retention window (default: 7 days). The dashboard displays current values and a sparkline chart for each metric showing the recent trend.

---

### 2. Service Checks

In addition to system metrics, heartd runs configurable health checks against services. Each check has a name, a type, and runs at a configurable interval.

**HTTP check**
Sends a GET (or configurable method) request to a URL. Reports the HTTP status code and response time in milliseconds. A check is considered failing if the status code is outside the expected range (default: 2xx) or if the request times out.

**TCP check**
Attempts a TCP connection to a host and port. Reports success or failure and connection time. Useful for databases, Redis, mail servers, or any non-HTTP service.

**Process check**
Checks whether a process matching a given name is currently running on the local machine.

**Shell check**
Runs a local shell command. Exit code 0 = healthy, anything else = failing. Output (stdout) is captured and displayed in the dashboard.

Each check has:
- `name` — human-readable label
- `type` — `http`, `tcp`, `process`, `shell`
- `interval` — how often to run (e.g. `30s`, `1m`)
- `timeout` — how long before the check is considered failed
- Type-specific parameters (URL, host/port, process name, command)

---

### 3. Multi-Node Awareness

heartd supports being installed on multiple servers. Nodes are aware of each other and can check each other's health.

**How it works:**

Each node is configured with a list of peer nodes (by name and URL). On startup, each node announces itself to its peers via the REST API. Peers are stored in the local database.

Each node periodically polls its configured peers:
- Fetches their current system metrics
- Fetches the status of their checks
- Checks that the peer itself is reachable (acts as an uptime ping)

**The dashboard on each node shows all nodes** — not just the local one. You can switch between nodes in the UI and see their metrics and check statuses. If a peer is unreachable, it is shown as "down" in the sidebar.

This means:
- Node A (web server) can see and alert on Node B (database server) being down
- Node B (database server) can see and alert on Node A (web server) being down
- Each node's dashboard is a full view of the cluster, not just itself

Nodes do not need to be in the same network as long as they can reach each other over HTTP. Auth between nodes is handled by a shared secret configured in the YAML.

---

### 4. Alerting

heartd sends alerts when a check transitions from healthy to failing, and again when it recovers.

**Alert conditions:**
- A service check fails (status changes from `ok` to `failing`)
- A peer node becomes unreachable
- A system metric exceeds a configured threshold (e.g. disk usage > 90%)
- A peer node recovers (sends a "back up" notification)

**Notification channels:**
- **Email** — via SMTP. Configurable sender, recipient(s), subject prefix.
- **Webhook** — HTTP POST with a JSON payload to any URL (Slack, Discord, custom endpoint, etc.)

Both channels are optional. At least one should be configured for alerting to be useful.

**Alert deduplication:** heartd does not send repeated alerts for an ongoing failure. It sends one alert when the check first fails and one when it recovers. No spam.

---

### 5. Dashboard

The web dashboard is served by the heartd binary itself and accessible in a browser at `http://<host>:9300`.

**Node sidebar:** Lists all known nodes (local + peers). Shows each node's name and a green/red status indicator. Clicking a node switches the main view to that node's data.

**Overview panel (per node):**
- System metrics: CPU, memory, disk, network — current value + sparkline
- Check list: each configured check with its current status (ok / failing / unknown), last checked time, and last result detail (e.g. HTTP 200, 45ms)

**No authentication by default.** heartd is designed to run on a private network or behind a firewall. Optional basic auth can be enabled in config for cases where the port is exposed publicly.

---

## Configuration

All configuration lives in a single `heartd.yaml` file. heartd looks for it in the same directory as the binary by default, or at a path passed via flag.

**Top-level sections:**

- `server` — node name, port, optional basic auth
- `peers` — list of other heartd nodes (name + URL + shared secret)
- `checks` — list of service checks
- `thresholds` — system metric alert thresholds (CPU %, disk %, memory %)
- `notify` — email and/or webhook config

---

## Deployment

Installation on a new server:

1. Download the binary for the target platform
2. Place `heartd.yaml` next to it
3. Run `./heartd`

For production use, heartd should be registered as a systemd service so it starts on boot and restarts on crash. A sample unit file is included in the repository.

No Docker required. No reverse proxy required. No database server required (SQLite is embedded).

---

## Out of Scope (v1)

The following are explicitly not in scope for the initial version:

- Log aggregation or log tailing
- Container / Kubernetes monitoring
- Per-second metric resolution
- User accounts or multi-user access control
- Historical data export
- Custom dashboards or metric graphing beyond sparklines
- SSL/TLS termination (use a reverse proxy if needed)
- Windows support

---

## Open Source

heartd is open-source. License: MIT.

The repository includes:
- The Go source for the binary
- The React source for the dashboard
- A sample `heartd.yaml`
- A systemd unit file template
- A `Makefile` with build targets for common platforms
