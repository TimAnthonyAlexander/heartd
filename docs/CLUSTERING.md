# heartd — Clustering, Identity & Display Names

How multiple heartd nodes find each other, agree on who's who, authenticate, and
share human-friendly names. This is the design *and* the hard-won lessons behind
it — read the **Invariants** section before changing anything here; several of
those rules exist because breaking them caused real bugs.

Code: `internal/cluster` (poller + gossip), `internal/server` (peer endpoints),
`internal/storage/peers.go` + `aliases.go`, `internal/alert/coordination.go`.

---

## The model in one paragraph

Every node keeps a local `peer` table. It polls each peer over `/api/peer/*` and
stores the peer's metrics/checks **under the peer's name**, so the dashboard
renders local and remote nodes through one code path ("the local node is peer
zero"). On top of that base, three things make a cluster feel like one system:
**gossip** (membership propagates automatically), **identity** (a node is known
by what it says it is, not by how it was labelled or where it claims to live),
and **display names** (a rename done once shows up everywhere).

---

## Membership & gossip

- **Manual add** seeds a link: `POST /api/peers` writes a peer row (name, URL,
  per-link secret) on *that* node only.
- **Gossip propagates it.** Each poll cycle a node fetches every reachable peer's
  `GET /api/peer/members` (its view: itself + the peers it polls) and adds any
  node it can reach but doesn't yet know. Add a node on **one** machine and the
  whole cluster discovers it within a cycle or two — no manual fan-out.
- **Announce runs every cycle** (not just at startup), so a node added after
  startup is still told about itself and can bootstrap into the mesh.
- Gossip is **add-only**: it never edits or deletes an existing peer, so a
  hand-set URL/secret is never clobbered.

To add a node to an existing cluster: install heartd on it, then add it once (with
the shared secret) on any one existing node. That node announces to it; it
bootstraps; the rest discover it by gossip.

## Identity — the core lesson

**A node's identity is resolved by asking it, never inferred.** Two tempting
signals are both unreliable:

- **`advertise_url`** is frequently unset or wrong (a node often doesn't know its
  own public URL behind a reverse proxy). Its *peers*, however, all record it at
  the same correct URL.
- **Node name** defaults to the OS hostname and rarely matches the labels humans
  type when adding peers.

So gossip resolves identity by probing `GET /api/peer/whoami`, which returns the
node's **canonical name**. Probing a candidate URL:

1. confirms it's **reachable** (we only add nodes we can actually monitor), and
2. detects a URL that **loops back to us** — if `/whoami` returns our own name,
   that "peer" is really this node, so we skip it. This is what prevents a node
   adding **itself** as a phantom peer, and it works regardless of our own
   `advertise_url`.

Discovered peers are stored under their **canonical name** so the same node is
named consistently cluster-wide.

**Self-heal:** any auto-added (secret-less) peer whose URL `/whoami` reports as
this node is removed automatically — clearing phantom-self rows. Manually added
peers (which carry a per-link secret) are trusted as distinct and never
auto-removed.

## Secrets & the cluster fallback

Node-to-node calls carry `X-Heartd-Secret`. Inbound validation (`validSecret`)
accepts a presented secret if it matches **any** stored peer secret or any
configured `peer_secret` — so the model is nominally per-link but operationally
"any secret I trust."

Gossip- and announce-created peers arrive with **no per-link secret**, so every
outbound path resolves an **effective secret**:

1. the peer's own per-link secret, else
2. the configured cluster secret (`server.peer_secret`), else
3. the secret the peer table already shares (`storage.CommonSecret` — the most
   common non-empty secret).

Step 3 is why a cluster that already uses one shared secret gets gossip with
**zero config changes**. This fallback is applied in the poller (`effectiveSecret`),
the alert coordinator (`secretFor`), and the rename/settings proxies
(`outboundSecret`). If you add a new outbound node-to-node call, use the same
fallback — don't read `peer.Secret` directly.

## Display names (aliases)

A display name (alias) relabels a node in the dashboard without changing its
identity key. The model is simple and must stay that way:

> **A node owns its display name. Every dashboard shows what the node advertises.**

- Stored in `node_alias` (keyed by real node name). Seeded once from
  `server.display_name` in config.
- **Renaming** node X (`PUT /api/nodes/{name}/alias`): if X is local, write
  locally; if X is a peer, **push the name to X** over the secret link
  (`/api/peer/settings/alias`) so X becomes the source of truth, then mirror it
  locally for instant feedback.
- **Propagation:** each node advertises its own alias via `GET /api/peer/identity`
  (`{name, display_name}`). Every poller caches a peer's advertised `display_name`
  under that peer's row (`storePeerIdentity`): non-empty → cache it; empty →
  clear it. So a rename done anywhere converges to the same label everywhere.

### The revert-to-hostname bug (don't reintroduce it)

`/api/peer/identity` must return an **empty** `display_name` when a node has no
alias — **never** its real name. A poller caches a non-empty advertised name as
the peer's alias, so returning the hostname makes every dashboard overwrite its
label with the hostname on the next poll (~one cycle later). Likewise
`storePeerIdentity` caches the advertised value **verbatim** and must not compare
it against the local row label. These two rules are guarded by
`TestPeerIdentityEmptyWhenNoAlias` and `TestStorePeerIdentityNeverCachesHostname`.

---

## Peer API surface (`/api/peer/*`, shared-secret auth)

| Endpoint | Purpose |
|----------|---------|
| `POST /announce` | "I exist at this URL" — auto-creates a peer row (no secret granted) |
| `GET /members` | This node's membership view (self + polled peers) — gossip source |
| `GET /whoami` | Canonical name — authoritative identity probe |
| `GET /identity` | `{name, display_name}` — advertises this node's own alias |
| `GET /metrics` `/checks` `/disk` `/network` `/diskio` | This node's own data, polled and stored under its name |
| `POST /alert-claim` `/alert-sent` | Cross-node alert dedup (one node mails per incident) |
| `GET/PUT/POST/DELETE /settings/*` | Remote management of a (headless) node, incl. `/settings/alias` |

`/whoami` and `/identity` are distinct on purpose: a node with no `advertise_url`
omits itself from `/members`, but `/whoami` always answers, so identity probing
works even for nodes that don't advertise.

---

## Invariants (do not break)

1. **Identity is probed, never inferred.** Don't identify a node (or self) by
   `advertise_url` or by a local label. `/whoami` is the source of truth.
2. **`/api/peer/identity` returns `""` when there is no alias** — never the real
   name. The poller caches whatever it returns.
3. **`storePeerIdentity` caches verbatim** (non-empty → set, empty → clear). No
   comparison against the local label.
4. **The node owns its display name.** Renaming a peer pushes to that peer; the
   local write is only a mirror for instant feedback.
5. **The real node name is the identity/dedup key everywhere** — storage rows,
   alert dedup (`incidentKey`), the coordinator. Display names and gossip only
   ever add *reachability* and *labels*, never change keys.
6. **Outbound node-to-node calls use the effective-secret fallback**, not
   `peer.Secret` directly, so secret-less (gossiped) peers stay reachable.
7. **Gossip is add-only**, and only adds **reachable** nodes (a successful
   `/whoami`). Never auto-delete a peer that carries a per-link secret.

## Operational notes

- **One shared secret across the cluster** is the assumption gossip is built on.
  If your links already share a secret, no config change is needed (auto-derive).
- **Hairpin routing:** self-detection and self-heal need a node to reach its own
  public URL. If it can't (no hairpin), a *new* phantom is still never added (the
  probe fails → skip), but a *pre-existing* phantom can't be auto-confirmed and
  must be deleted once from the dashboard.
- **Unreachable nodes are not gossiped in.** A node you can't poll is one you
  can't monitor; gossip only adds what it can reach.
- heartd serves plain HTTP; terminate TLS at a reverse proxy. The node-to-node
  secret is the only node-to-node auth.

See [DEPLOY.md](./DEPLOY.md) for cutting releases.
