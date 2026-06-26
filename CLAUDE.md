# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

heartd is a self-hosted server health monitor: a single Go binary that embeds a React dashboard, samples system metrics, runs service checks, polls peer nodes, and sends alerts. See `README.md` for the product overview and `docs/SPEC.md` for the spec.

## Commands

Toolchain: Go 1.25+, Bun (frontend), `modernc.org/sqlite` (pure Go — no cgo, so cross-compile is trivial).

```sh
make build          # build frontend bundle AND embed it into ./heartd
make cross          # static binaries for linux/darwin × amd64/arm64 into ./bin
make test           # go test ./...
make clean

go test ./internal/alert/...                 # one package
go test ./internal/alert/ -run TestObserveMetricDiskTransitions   # one test
go test -race ./internal/alert/...           # the alert package has concurrency; run with -race
go vet ./...
```

### Dev loop (split mode — what you'll normally use)

```sh
# terminal 1 — Go API on :9300
go run ./cmd/heartd
# terminal 2 — Vite dev server with HMR, proxies /api -> :9300
cd frontend && bun run dev      # http://localhost:5173
```

The Go binary serves the **embedded** dashboard at `:9300`; Vite serves a live-reloading copy at `:5173` proxying the API. After frontend changes, `cd frontend && bun run build` regenerates `internal/web/dist` (the embed target) — a plain `go build` alone will embed a stale bundle. Pass `-config <path>` and `-addr :PORT` to run multiple local nodes.

## Architecture

### One binary, embedded dashboard
`internal/web/embed.go` does `go:embed all:dist`; the React build (`frontend/`, output to `internal/web/dist`) is compiled into the binary and served by `internal/server`. A placeholder `internal/web/dist/index.html` is committed so the Go package always compiles before the frontend is built.

### Two config layers — this distinction is central
- **`internal/config`** parses `heartd.yaml` at startup. It is the source of truth ONLY for **node identity and topology**: server name, port, db path, `advertise_url`, and the `peers` list (names/URLs/shared secrets). These are not runtime-editable.
- **`internal/settings`** is the runtime source of truth for everything **operational**: metric/poll intervals, retention, alert thresholds, notify channels, and the **service-check list**. It is SQLite-backed, **seeded once from the YAML config on first run** (guarded by an `initialized` key), then edited live via the dashboard. It holds an in-memory cache updated on every write.

**Live application without restart** is the core mechanic: long-running loops read `settings` fresh **each cycle** rather than caching at startup. To make a new setting take effect live, read it inside the loop, don't capture it in a constructor. Examples: the collector re-reads its interval each iteration; the **scheduler is a reconciling loop** (`internal/scheduler`) that each tick runs whichever checks from `settings.Checks()` are due — so add/edit/remove/enable just work; the alert engine reads thresholds via a provider func and notifiers via a dynamic dispatcher.

### "The local node is peer zero" — the multi-node data model
`internal/storage` keys metric/check/disk/net rows by **node name**. The cluster poller (`internal/cluster`) fetches each peer's data over `/api/peer/*` and writes it into the **same tables under the peer's name**. So the dashboard's per-node endpoints (`/api/nodes/{name}/...`) serve local and peer data through one uniform code path. When adding a new metric type, wire it in all four places consistently: collector (local sample), a `/api/peer/...` endpoint, the poller (store under peer name), and the dashboard endpoint.

### Cluster membership, identity, and display names
**See `docs/CLUSTERING.md` for the full design and its invariants — read it before touching `internal/cluster` or the alias/identity paths.** The short version:
- **Gossip**: the poller pulls each peer's `GET /api/peer/members` every cycle and adds any *reachable* node it doesn't know (add-only). Adding a node on one machine propagates to the whole cluster. Announce runs every cycle so late-added nodes bootstrap.
- **Identity is probed, never inferred.** A node's `advertise_url` is often unset/wrong and its name defaults to the hostname, so gossip resolves identity via `GET /api/peer/whoami` (canonical name). A probe that returns *our own* name means the URL loops back to us → skip (no phantom-self). Secret-less auto-added rows whose URL `/whoami`s as self are self-healed away.
- **Secrets**: outbound node-to-node calls use an **effective secret** — per-link secret, else configured `server.peer_secret`, else `storage.CommonSecret` (the secret the peer table already shares). Always use this fallback (poller `effectiveSecret`, coordinator `secretFor`, server `outboundSecret`), never `peer.Secret` directly, or gossiped peers become unreachable.
- **Display names (aliases)**: a node **owns** its display name. Renaming a peer pushes the name to that peer (`/api/peer/settings/alias`); every node advertises its own alias via `GET /api/peer/identity` and pollers cache it (`storePeerIdentity`). Hard rule: `/api/peer/identity` returns `""` (never the real name) when there's no alias, and the poller caches it verbatim — otherwise a rename reverts to the hostname on the next poll.

### Storage conventions (`internal/storage`)
Hand-rolled `database/sql` over `modernc.org/sqlite` (`sql.Open("sqlite", path)`, `SetMaxOpenConns(1)`, WAL). **All schema lives in the single `schemaSQL` constant** applied idempotently in `Open` — add new tables there, never via separate migration paths. Conventions to match: timestamps stored as UTC unix-epoch integers (read back via `time.Unix(x,0).UTC()`), `uint64` stored as INTEGER, per-type `scan*` helpers, errors wrapped with a `storage:` prefix. Table names are **singular snake_case** (`metric_sample`, `check_config`, `user`, `session`, `peer`, `node_alias`). `node_alias` (node → display name) caches both the local node's own alias and each peer's self-advertised one; new peer rows default to `enabled=1`.

### Auth (`internal/auth`) and the API wall
Session-based: bcrypt passwords + opaque session tokens, both in SQLite, carried in an HttpOnly `heartd_session` cookie. **First-run init**: when zero users exist the dashboard shows a "create first admin" flow (`/api/auth/init`, allowed only while uninitialized). Every user is an admin (auth == authorization). In `internal/server`, **all data endpoints are wrapped in `requireAuth` → 401 without a session**; only `/api/auth/*` and `/api/health` are public. Node-to-node `/api/peer/*` endpoints are NOT session-auth'd — they use the per-link **shared secret** (`X-Heartd-Secret`, constant-time compared against configured peer secrets). heartd serves plain HTTP; TLS is expected to be terminated by a reverse proxy.

### Alerting (`internal/alert`)
Edge-triggered dedup: the `Engine` records last-known state per entity and dispatches only on a transition (one alert when a problem starts, one on recovery, never repeats). **Restart-safety** comes from `Seed*` methods that prime baseline state from the DB at startup so an already-failing check/peer doesn't re-alert. Producers (scheduler, poller, collector) call `Observe*`. Delivery is non-blocking (background goroutine, per-send timeout) via Email/Webhook notifiers.

### Frontend (`frontend/`)
React + Vite + TypeScript + MUI v9, Bun as package manager/bundler. **Routing uses `react-router` v7 — import from `'react-router'`, never `react-router-dom`** (that's a deprecated v6 compat shim). `HashRouter` with routes `/node/:name`, `/settings`, `*`→`/`. Data flows through hooks: `useCluster` (node list + sidebar sparklines), `useNodeData` (selected-node detail; self-scheduling poll with `AbortController`, history seeded then live-appended with timestamp dedup). Styling reads tokens from `src/theme.ts` (`colors`, `statusColor`, `percentColor`) — don't hardcode hex. A 401 from any request triggers `setUnauthorizedHandler` → back to login. `@mui/icons-material` is intentionally not installed; icons are inline SVG.

### Request lifecycle
`cmd/heartd/main.go` loads config, opens the DB, builds the `settings` service (seeding on first run) and the `auth` service, constructs the alert engine, then launches the collector / scheduler / (peer poller if peers configured) as goroutines under a context cancelled on SIGINT/SIGTERM, and serves `internal/server`.
