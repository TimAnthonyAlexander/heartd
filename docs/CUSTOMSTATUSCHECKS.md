# Custom status codes for HTTP checks

> Product spec. What we're adding, who it's for, and how we'll know it's done.

## The problem

heartd's HTTP check calls a URL "healthy" only when it answers with a **2xx**
status (200–299). Anything else — 301, 401, 403, 404, 500 — is reported as
**failing** and pages you.

That's wrong for a whole category of perfectly healthy endpoints. The big one is
**anything behind authentication**: an auth-protected page that's working
correctly answers **401 Unauthorized** or **403 Forbidden** — the server saying
"I'm alive, you just haven't logged in." Today heartd treats that as DOWN.

We've already been burned by this: a check pointed at an auth-gated site flapped
"failing" on a healthy 401, woke people up, and made the alert feel untrustworthy.
The moment someone points a check at an admin panel, an internal tool behind
basic-auth, or an API that needs a token, they hit it.

## Who it's for

- Anyone monitoring an **auth-protected** endpoint (basic-auth, SSO, token APIs)
  where the healthy answer is a 401 or 403.
- Anyone whose "I'm up" signal is legitimately **non-2xx** — a redirect (301/302),
  a deliberate 204/418, or a health route that returns 403 to anonymous callers.
- Anyone who really just wants **"is the server answering at all?"** rather than
  "is it answering 200 specifically?"

## What we're adding

A per-check setting that lets the user decide **which status codes count as
healthy**, instead of hard-coding 2xx. Three ways to express it, simplest first:

1. **"Any response = alive"** — a single checkbox. The check passes as long as the
   server returns *any* valid HTTP response, and fails only on a real problem:
   connection refused, timeout, DNS failure, TLS error. This is the right choice
   for "is the box up and serving?" and quietly handles the auth case without the
   user having to think about codes at all.
2. **An explicit list of accepted codes** — e.g. `200, 401, 403`. The check passes
   when the response status is in the list and fails otherwise. For the user who
   wants to be precise: "a healthy login page is exactly a 401."
3. **Default (unchanged)** — set neither and behaviour is exactly as today: 2xx is
   healthy, everything else fails. Existing checks keep working untouched.

The setting lives on the check, right next to URL and method, in the Checks editor.

## How it behaves

| Endpoint | Setting | Server returns | Result |
|----------|---------|----------------|--------|
| auth-gated app | Any response = alive | 401 | ✅ healthy |
| auth-gated app | Accepted: `401, 403` | 401 | ✅ healthy |
| auth-gated app | Accepted: `401, 403` | 500 | ❌ failing |
| public site | default | 200 | ✅ healthy |
| public site | default | 503 | ❌ failing |
| anything | any setting | refused / timeout / TLS error | ❌ failing (always) |

Two rules that keep it intuitive:

- **A connection that never produces an HTTP response is always a failure** —
  regardless of the setting. "Accept any response" means *any HTTP response*, not
  "ignore the server being down." Refused, timed-out, DNS-failed, TLS-broken →
  still failing.
- **The failure detail names actual vs expected** — e.g. `HTTP 500 (expected one
  of 200, 401, 403)` — so an operator instantly sees whether it's the wrong code
  or a genuine outage, not a bare "failing."

## A small related win: identify ourselves

While we're in the HTTP check, send a recognizable **User-Agent** — something like
`heartd/<version> (health-check)` — instead of the default library string. Right
now the check looks like an anonymous bot, so WAFs, rate-limiters, and fail2ban
can flag or block it (we've watched exactly this kind of blocking cause flapping).
A clear User-Agent lets the far end allow-list the monitor on purpose — and it's
just good manners. Optionally let the user override it per check for endpoints
that expect a specific agent.

## Out of scope (for now)

- Matching on response **body** or **headers** ("must contain OK"). Worth a later
  pass; status code covers the 80% case.
- Response-time thresholds — heartd already has a separate check-latency alert.
- Per-code severity ("401 is a warning, 500 is critical").

## Done when

- A check can be set to **accept any HTTP response** as healthy: a 401/403/302
  reports OK, while a refused or timed-out connection still reports failing.
- A check can be given an **explicit accepted-code list**, and only those codes
  pass.
- A check with **neither** set behaves exactly as before (2xx-only) — no surprises
  on upgrade, no migration.
- A failing check's **detail** shows the actual status and what was expected.
- The setting is editable in the dashboard's Checks form and persists like any
  other check field.
- The HTTP check sends a recognizable heartd **User-Agent**.
