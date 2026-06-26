# heartd — Release & Deploy Guide

How to cut a versioned release: cross-compile the static binaries, commit, tag,
and publish a GitHub release with the binaries attached. This is the exact flow
the project uses today; follow it verbatim unless a step is clearly outdated.

> Prerequisites: `go` 1.25+, `bun`, and the `gh` CLI authenticated against
> `github.com/TimAnthonyAlexander/heartd` (`gh auth status` to check).

---

## TL;DR

```sh
# 1. Make sure the change is committed and the tree is clean & tested.
make test
git status --short      # should be empty before you build

# 2. Cross-compile (rebuilds the embedded frontend, then 4 static binaries).
make cross

# 3. Regenerate checksums (the Makefile copy can be stale after a fresh build).
( cd bin && shasum -a 256 heartd-* > SHA256SUMS.txt )

# 4. Push the commit.
git push origin main

# 5. Publish the release (pick the next version — see "Versioning").
gh release create vX.Y.Z \
  bin/heartd-linux-amd64 bin/heartd-linux-arm64 \
  bin/heartd-darwin-amd64 bin/heartd-darwin-arm64 \
  bin/SHA256SUMS.txt \
  --target main \
  --title "vX.Y.Z — <short headline>" \
  --notes "<release notes, see template below>"
```

---

## Step by step

### 1. Land the change first

Commit your code (conventional-commits format, e.g.
`feat(notify): ...`, `fix(alert): ...`). Confirm `make test` is green and
`git status --short` is empty *before* building — `make cross` regenerates the
embedded frontend bundle, and you don't want unrelated working-tree changes
sneaking into the build.

> Attribution trailers are disabled for this project — don't add a
> `Co-Authored-By` line to commits.

### 2. Cross-compile — `make cross`

`make cross` does two things (see the `Makefile`):

1. `frontend` — `bun install && bun run build`, which writes the React bundle to
   `internal/web/dist` (the `go:embed` target).
2. Builds 4 **static, CGO-free** binaries into `bin/`:

   | File | OS / Arch |
   |------|-----------|
   | `bin/heartd-linux-amd64`  | Linux x86-64 |
   | `bin/heartd-linux-arm64`  | Linux ARM64 |
   | `bin/heartd-darwin-amd64` | macOS Intel |
   | `bin/heartd-darwin-arm64` | macOS Apple Silicon |

   Each target is built with `CGO_ENABLED=0` — heartd uses `modernc.org/sqlite`
   (pure Go), so there's no cgo and cross-compilation is trivial.

`bin/` is gitignored — the binaries are release artifacts, never committed.

A Rolldown/Vite warning about chunks larger than 500 kB is expected and harmless.

### 3. Regenerate `SHA256SUMS.txt`

`make cross` may leave an older `bin/SHA256SUMS.txt` in place (its timestamp can
predate the freshly built binaries). Always regenerate it so the published
checksums match the published binaries:

```sh
( cd bin && shasum -a 256 heartd-* > SHA256SUMS.txt )
```

The subshell keeps the filenames in the checksum file relative (no `bin/`
prefix), which is what users expect when they `shasum -c` next to the downloads.

### 4. Push

```sh
git push origin main
```

The project releases from `main` and commits land directly on it. If you're on a
branch, merge to `main` first — the release `--target` must point at the commit
you built.

### 5. Publish the release — `gh release create`

```sh
gh release create vX.Y.Z \
  bin/heartd-linux-amd64 bin/heartd-linux-arm64 \
  bin/heartd-darwin-amd64 bin/heartd-darwin-arm64 \
  bin/SHA256SUMS.txt \
  --target main \
  --title "vX.Y.Z — <short headline>" \
  --notes "<notes>"
```

`gh release create <tag>` creates the git tag *and* the GitHub release in one
step — you don't need a separate `git tag`. Always attach all 4 binaries plus
`SHA256SUMS.txt`; that's the established asset set every prior release ships.

Verify afterward:

```sh
gh release view vX.Y.Z --json assets -q '.assets[].name'
```

---

## Versioning

heartd uses semver tags `vMAJOR.MINOR.PATCH`. Pick the next version off the
current latest (`gh release list -L 1`):

- **PATCH** (`v0.2.0 → v0.2.1`) — a single self-contained feature, bug fix, or
  cleanup with no breaking change. Most releases.
- **MINOR** (`v0.2.x → v0.3.0`) — a batch of features shipped together, or a
  notable new capability (e.g. a whole metrics/alerting wave).
- **MAJOR** — reserved for breaking changes to config, the API, or on-disk
  schema. Pre-1.0, avoid unless truly unavoidable.

When unsure between patch and minor: one focused change → patch.

---

## Release-notes template

Keep notes short and operator-focused — what changed and anything they must know
to upgrade. Markdown is supported.

```md
<one-sentence summary of the headline change>

## Changes
- **<area>:** <what changed, in user-facing terms>

<optional: a paragraph on any behavior/compat nuance — e.g. what is NOT affected>

## Binaries
Static, CGO-free builds for linux/darwin × amd64/arm64. Verify with `SHA256SUMS.txt`.
```

---

## Notes & gotchas

- **A plain `go build` embeds a stale dashboard.** Only `make cross` (or
  `make build`) rebuilds `internal/web/dist` first. Never ship a release built
  without the frontend step.
- **No deploy server to push to.** "Deploy" here means publishing binaries to
  GitHub Releases; operators download and run the single binary themselves.
- **Schema/config changes:** heartd applies its SQLite schema idempotently on
  startup (`storage.Open`) and seeds runtime settings from `heartd.yaml` on first
  run — there's no migration step to run during a release. Still, call out any
  config-format change in the notes (and bump MINOR/MAJOR accordingly).
- **Tag points at what you built.** Push the commit *before* `gh release create`
  so `--target main` resolves to the exact tree you cross-compiled.
</content>
</invoke>
