# heartd

A lightweight, self-hosted server health monitor. One static binary, one YAML
file, a clean web dashboard. Installable in under two minutes on any Linux or
macOS server.

heartd gives small teams an honest answer to *"is everything okay?"* — system
resources, service checks, and the reachability of every node in a small cluster
— without the weight of a full observability platform.

## Features

- **System metrics** — CPU, memory, disk (per mount), and network throughput,
  sampled on an interval and stored in an embedded SQLite database with a rolling
  retention window. Current value + time-series charts in the dashboard.
- **Service checks** — `http`, `tcp`, `process`, and `shell` checks, each on its
  own schedule, with status, detail, and latency.
- **Multi-node** — point nodes at each other; every node's dashboard shows the
  whole cluster (metrics + checks for each node) and flags any node that goes
  down. Auth is a shared secret; nodes only need HTTP reachability, not a shared
  network.
- **Alerting** — email (SMTP) and/or webhook (POST JSON) on transitions: a check
  fails or recovers, a node goes down or comes back, or a metric crosses a
  threshold. De-duplicated — one alert per state change, never spam.
- **Embedded dashboard** — a React UI served by the binary itself. No separate
  frontend deployment, no reverse proxy required.

## Quick start

Download the binary for your platform (or build it — see below), drop a config
next to it, and run:

```sh
cp heartd.example.yaml heartd.yaml   # then edit
./heartd                             # serves on http://localhost:9300
```

Open <http://localhost:9300>. With no `heartd.yaml` present, heartd runs with
sensible defaults (local node only, 30s sampling, no checks/peers/alerts).

Flags:

```
-config <path>   path to heartd.yaml (default: ./heartd.yaml)
-addr <addr>     listen address override, e.g. :9300 (default: from config port)
```

## Configuration

All configuration lives in a single `heartd.yaml`. See
[`heartd.example.yaml`](./heartd.example.yaml) for a fully-documented reference.
Sections:

| Section | Purpose |
|---|---|
| `server` | node name, port, sample interval, retention, db path, peer settings, optional basic auth |
| `thresholds` | CPU / memory / disk alert thresholds (percent) |
| `peers` | other heartd nodes (name + URL + shared secret) |
| `checks` | service checks (`http` / `tcp` / `process` / `shell`) |
| `notify` | alert channels (`email` and/or `webhook`) |

Durations accept Go units plus a `d` (days) suffix: `30s`, `5m`, `1h`, `7d`.
Configuration is read at startup; restart heartd to apply changes.

## Multi-node

Each node lists its peers under `peers:` with a shared `secret`. Use the **same
secret on both ends of a link**. On startup a node announces itself to its peers
(`advertise_url` tells them how to reach it back) and then polls each peer on
`peer_poll_interval`, fetching their metrics and checks and recording
reachability. The result: every node's dashboard is a full cluster view, and any
node can alert on any other going down.

Nodes communicate over plain HTTP. There is no TLS (out of scope); for traffic
crossing untrusted networks, front each node with a TLS-terminating reverse proxy
and use `https://` peer URLs, or run them over a private network / VPN.

## Alerting

Configure `notify.email` and/or `notify.webhook`. Alerts fire on transitions and
recoveries only, and are de-duplicated (an ongoing failure is not re-alerted; a
restart does not re-alert an already-failing entity).

Webhook payload:

```json
{
  "kind": "check",         // check | peer | metric
  "node": "web-01",
  "subject": "API health", // check name, peer name, or metric (CPU/Memory/Disk)
  "firing": true,
  "status": "firing",      // firing | recovered
  "title": "Check \"API health\" is failing on web-01",
  "detail": "HTTP 503",
  "time": "2026-06-25T19:00:00Z"
}
```

## HTTP API

The dashboard is built on a small JSON API under `/api`:

| Method & path | Description |
|---|---|
| `GET /api/health` | liveness |
| `GET /api/nodes` | local node + peers, each with status |
| `GET /api/nodes/{name}/metrics` | latest CPU/memory sample |
| `GET /api/nodes/{name}/metrics/history?minutes=` | metric time series |
| `GET /api/nodes/{name}/checks` | current status of each check |
| `GET /api/nodes/{name}/disk` | current per-mount disk usage |
| `GET /api/nodes/{name}/network` | latest network throughput |
| `GET /api/nodes/{name}/network/history?minutes=` | network time series |

Node-to-node endpoints under `/api/peer/*` require the shared secret in the
`X-Heartd-Secret` header and are not used by the dashboard.

## Building

Requires [Go](https://go.dev) 1.25+ and [Bun](https://bun.sh) (for the frontend).

```sh
make build        # build the frontend bundle and embed it into ./heartd
make cross        # static binaries for linux/macOS amd64+arm64 into ./bin
make test         # go test ./...
```

The frontend is compiled into the binary via `go:embed`, so a release is a single
self-contained executable. Cross-compilation is pure-Go (no cgo) thanks to the
`modernc.org/sqlite` driver.

## Deployment

For production, run heartd under systemd so it starts on boot and restarts on
failure. A unit template is in [`deploy/heartd.service`](./deploy/heartd.service)
with install steps. No Docker, reverse proxy, or external database required —
SQLite is embedded.

## Development

Split dev mode gives instant frontend reloads while the Go API runs separately:

```sh
# terminal 1 — Go API on :9300
go run ./cmd/heartd

# terminal 2 — Vite dev server with HMR, proxying /api to :9300
cd frontend && bun run dev    # http://localhost:5173
```

### Project layout

```
cmd/heartd/        entrypoint, flags, wiring
internal/
  config/          heartd.yaml parsing, defaults, validation
  metrics/         gopsutil CPU/mem/disk/network reads
  storage/         SQLite schema + persistence
  collector/       metric sampling loop
  checks/          http/tcp/process/shell check runners
  scheduler/       per-check scheduling
  cluster/         peer announce + poll
  alert/           transition detection, dedup, email/webhook
  server/          REST API + embedded dashboard
  web/             go:embed of the built frontend
frontend/          React + Vite + TypeScript + MUI dashboard
deploy/            systemd unit template
```

## Out of scope (v1)

Log aggregation, container/Kubernetes monitoring, per-second resolution,
multi-user access control, historical export, TLS termination, and Windows
support are intentionally not included.

## License

MIT — see [LICENSE](./LICENSE).
