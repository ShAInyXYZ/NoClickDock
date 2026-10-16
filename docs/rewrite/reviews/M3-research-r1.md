# M3 — research review, round 1

Reviewed: `4c8747b` (sys), `9b32ace` (docker), `a86e646` (config + legacy
watch list), `34434d9` (tailscale), `5c83d5b`/`38209da`/`dab3002` (writer
rules applied to the real writers), report `97a77d5`, contract `bd88c98`.
Tested on a clean `git archive bd88c98` tree against specs/docker.md,
specs/tailscale.md and dock-core.md.

## What I ran

| check | result |
|---|---|
| `gofmt` · `go vet` (linux + `GOOS=windows`) · `go test -race ./...` | clean; 9 packages ok |
| `GOOS=windows go build -ldflags -H=windowsgui` | ok |
| `--once docker` (this machine, read-only) | 5.4 s including the background System query; pages Now / Stopped / System; glance ≤ 5, label ≤ 28; every action has a confirm |
| `--once tailscale` (read-only) | ok, but the state label was **40 characters** (a health message) — see SHOULD-FIX 1 |
| actions through `--dev` with a **fake `docker` on PATH** (fixtures, argv logged; the real daemon never touched) | `unit.stop ledger` → `compose -p ledger stop` → `Stopped ledger`. Refused as `unknown action` before the widget saw them: a container that was not offered, `-rf`, `ledger; rm -rf ~`, an extra `--all` argument, a `remove` not on screen. |
| action safety in code | the name is checked again against the last inspect and `^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`; `--` before every name list; the compose project is its own argv element; compose first, plain docker as fallback; 120 s timeout; busy rows while running |
| exit semantics | `crashed = exited/dead && (OOM || code ∉ {0,143,137})` — 137/143 are stops unless OOM |
| log cache | stopped containers keyed by `FinishedAt` (never refetched); live ones asked every poll up to the 8-lookup cap, then served from cache; LRU 200 |
| stats cadence | every poll with something running, 8 s timeout, rows without numbers on timeout |
| config | `widgets.tailscale.watch` (`null` / `[]` / list), validated; migration only reads `status-widgets/tailscale.json`, only when `config.json` does not exist, only on Linux, never with `--dev`, `--mock`, `--config` or `--once` (`main.go:65`) |
| watch semantics | `null` → peers seen online this run (grows only from online peers); `[]` → none; a list → exactly those (lower-cased); read every poll |
| Windows paths | `docker.exe` on PATH, else `%ProgramFiles%\Docker\Docker\resources\bin\docker.exe`; `tailscale.exe` on PATH, else `%ProgramFiles%\Tailscale\tailscale.exe`, through `sys.Find` |
| leak scan (68 real values from this machine: tailnet, peers, IPs, containers, projects, images, hostname) against core/ and docs/rewrite | only public, generic image names matched (`postgres`, `portainer`, `frontend`, …); no tailnet name, peer, IP or hostname |

## MUST-FIX

1. **Foreground `serve`/`funnel` sessions are invisible — a public funnel
   reads "nothing exposed".** `core/internal/widgets/tailscale/parse.go:191`
   decodes only the top-level `TCP`, `Web` and `AllowFunnel` of
   `tailscale serve status --json`.
   - Tailscale keeps every `tailscale serve …` / `tailscale funnel …` run
     *without* `--bg` — the default when typed in a terminal — under
     `Foreground[<session id>]`. This is in the `ServeConfig` in
     `ipn/serve.go`. Its own `IsFunnelOn()` walks `Foreground` for that
     reason.
   - Reproduced with a probe test in a scratch copy:
     `{"Foreground":{"b3a1c9":{"TCP":…,"Web":…,"AllowFunnel":{"node.example.ts.net:443":true}}}}`
     parses to `serve=[] funnel=[]`. `render.go:194` then shows **"Serve /
     funnel · nothing exposed"** with an `ok` lamp while the machine is on the
     public internet.
   - Correct: merge each `Foreground` entry's `TCP`, `Web` and `AllowFunnel`
     into the exposure (the value is itself a `ServeConfig`, one level deep).
     Add a fixture for a foreground funnel and one for a foreground
     tailnet-only serve.
   - `Services` (`svc:` names, newer clients) carries `TCP`/`Web` too. At
     least count it as served, or say "unknown" if it cannot be read, rather
     than "nothing exposed".

## SHOULD-FIX

1. **A health message makes the state label 40 characters**
   (`render.go:58`, `cut(st.Health[0], 40)`, the P rule). dock-core §11 and
   `widgets_test.go` hold labels to ≤ 28, which the mocks pass but the real
   writer does not (observed on this machine). Cut to 28 on a word boundary.
   The full message is already in the Health section and the toast.
2. **Peers are identified by lower-cased `HostName`** (`parse.go:140`, the P
   rule). Two devices with the same OS hostname (two `ubuntu` VMs, say) share
   one entry in the rate map, so each peer's rate is computed against the
   other's counters. They also cannot be pinned apart. Key the rate map by the
   peer's node key (the `Peer` map key). Match the watch list on the first
   label of `DNSName`, which Tailscale keeps unique (`ubuntu`, `ubuntu-1`),
   and fall back to `HostName`.
3. **Duplicate in-flight actions** (carried from M2; also M2-r2 MUST 1): the
   widget runs a second `unit.stop` for a unit already stopping. Refuse an
   action whose marks are already pending: `busy: <label> is still
   stopping`.

## Checked and fine

- **Spec deltas, all improvements:**
  - `inspect` as JSON;
  - a restart-looping unit shown as an alert;
  - `prune stopped` counting only what prune removes;
  - `resume` unpausing only paused members;
  - `<1m`;
  - alert meta within 24 characters.
- **Portainer choice** (9443 → 9000 → first port that is not 8000).
- **Issues:** `restarting:`/`unhealthy:` only, crashes through the `warn`
  transition. Portainer menu `url`, Tailscale `copy` items.
- **Failed actions:** a failure refreshes by itself (the hub refreshes only
  after a success); the error tail is 70 characters.
- **Tailscale reads:**
  - a stopped or logged-out daemon's JSON is still read despite the non-zero exit;
  - no JSON → unreachable;
  - `serve status` failing → exposure `unknown` rather than "nothing exposed";
  - year-1 `LastSeen` is unknown;
  - one-digit fractions parse;
  - rate reset → no rate.
- **Migration:** only reads the legacy file; never writes it. The registration
  record (no `watch`) found on this machine is correctly ignored.

VERDICT: FAIL
