# Spec — tailscale

Parity reference: `widgets/tailscale/tailscale-status-checker.py` 1.0.0 (shared
runtime, see [dock-core.md](dock-core.md)). Legend: **P** Python, **N** NCD 2,
**BUG** do not carry, **Q** decision for main. All host names below are
invented.

## Identity

id `tailscale` · title `Tailscale` · order 70 · poll **30 s** · width P
fitted; N 420 CSS px · mark: the 3×3 dot grid (P path in the file, viewBox 24).

## Data sources

| what | command | timeout | notes |
|---|---|---|---|
| status | `tailscale status --json` | 15 s | empty/invalid output → "not available" |
| serve exposure | `tailscale serve status` | 5 s | only when status succeeded |
| funnel exposure | `tailscale funnel status` | 5 s | only when status succeeded |

Exposure rule: trimmed stdout non-empty **and** not starting with `no `
(case-insensitive; "No serve config") → exposed; the first line is the value.
P ignores exit codes and stderr (errors print to stderr → treated as nothing
exposed).

Binary: Linux `tailscale` on PATH. Windows `tailscale.exe`: PATH first, then
`%ProgramFiles%\Tailscale\tailscale.exe` (the installer's location; not always
on PATH for GUI-launched processes). Run with `CREATE_NO_WINDOW`.

## Parsing `status --json`

Top level: `Self`, `Peer` (map, values used), `BackendState`, `TailscaleIPs`,
`MagicDNSSuffix`, `Version`, `ClientVersion`, `Health`.

Per peer (lower-cased `HostName` is the identity for watch matching and rate
state):

| field | from | notes |
|---|---|---|
| host | `HostName` or `?` | display keeps original case |
| online | `Online` | |
| active | `Active` | recent traffic |
| exit | `ExitNode` | this peer is our exit node |
| os | `OS` | |
| last | `LastSeen` parsed | Tailscale emits one-digit fractions (`…:00.1Z`) and `0001-01-01T00:00:00Z` for never → treat year < 2000 as unknown |
| key_expired | `KeyExpiry` parsed `< now` | |
| direct | `CurAddr` non-empty | |
| relay | `Relay` (DERP region code) | |
| rx, tx | `RxBytes`, `TxBytes` | |
| rate | `((rx−rx₀)+(tx−tx₀))/Δt` vs the previous poll, per lower-cased host | `None` on first sight or when a counter went backwards (reset) |
| exit option | `ExitNodeOption` → list of hosts offering exit | |
| routes | each `PrimaryRoutes` entry → `(host, cidr)` | |

Self: `HostName`, `Online` (→ self_online), `Relay`, `ExitNodeOption` (we
offer exit), `PrimaryRoutes` (we advertise), `KeyExpiry` → `expiry_days =
(KeyExpiry − now)/86400` (absent when key expiry is disabled → unknown),
`DNSName` (strip trailing `.`; fallback `<hostname lower>.<MagicDNSSuffix>`).

- `ip4` = first of `Self.TailscaleIPs` (fallback top-level `TailscaleIPs`)
  containing `.`, else the first IP, else empty.
- `version` = `Version` up to the first `-`.
- `update_available` = `ClientVersion.RunningLatest == false` (explicit false
  only; absent = unknown = no).
- `health` = non-empty strings of `Health`.
- `exit_node` = host of the first peer with `ExitNode: true`.

## Watch list — the semantics that matter

- Config `watch: ["fileserver", "build-agent"]` (lower-cased on load) pins
  exactly those peers (matched against lower-cased `HostName`).
- **No watch list (`null`)**: every peer seen **online at any poll since the
  core started** is watched; the set only grows during a run. A peer that
  blips offline stays watched (that is the point); peers already dormant at
  startup are never watched. N: keep the set in memory per run (not persisted),
  exactly as P. An empty list `[]` means "watch nothing" (distinct from null).
- `watched` = peers whose host is in the watch set, sorted online first then
  by host. `others_online` = online peers not watched (P computes it but never
  shows it).

## State

| state | condition (in order) | stateLabel |
|---|---|---|
| `idle` | status unavailable (tailscale missing, daemon not reachable, bad JSON) | P `TAILSCALED UNREACHABLE`; N `tailscaled unreachable` (Windows: `Tailscale not running`) |
| `crit` | `BackendState != "Running"` **or** `!Self.Online` | `BackendState` lower (`stopped`, `needslogin`, `starting`) — P upper-cased; when Running but self offline P shows the next rule's label; N: `this node offline` |
| `warn` | any watched peer offline | 1 → `<host> offline`; n → `<n> peers offline` |
| `warn` | health messages present | first health message, max 40 chars |
| `warn` | `expiry_days ≤ 7` | `key expires in <d>d` (`%.0f`) |
| `ok` | otherwise | `<up> peer(s) up` where up = watched peers online (`1 peer up`, `3 peers up`) |

(Label order follows the same precedence; for `crit` with Running but
self-offline P's label falls through to the peers/health/key/ok labels — **BUG**,
say `this node offline`.)

## Glance

`crit`/`idle` → `off`; key expiring (≤ 7 d) and nothing else wrong → `<d>d`;
otherwise `<watched up>/<watched total>` (`3/4`); tone = state tone for
warn/crit. If more than 99 watched, `<up>` only.

## Toasts

- Transition: default = new stateLabel (`fileserver offline`, `3 peers up`,
  `stopped`).
- Alert ids (P order): `backend:<State>` (only when available and not
  Running), `down:<host>` per watched offline peer, `health:<message>` per
  health line.
- Alert messages: `tailscaled: <State>` · `<host>: offline` · the health
  message (max 80 chars). N tones: backend crit, down warn, health warn.

## Panel (single page)

Unavailable (P) → N:

- `Section("DAEMON", alert=True)`, `Note("tailscaled is not reachable. Try:",
  CRIT)`, `Note("  tailscale status", FG_DIM)` → `section {title: "Daemon",
  tone: crit}`, `note {text: "tailscaled is not running — start it with sudo
  systemctl start tailscaled", tone: crit}` (DESIGN.md empty-state wording;
  Windows: "Tailscale is not running — start the Tailscale app"). If the binary
  is missing entirely: "tailscale is not installed".

Available, top to bottom:

1. Health (only when present): `section {title: "Health", tone: crit}` (P
   alert-red) then one `note {text, tone: warn}` per message (wrapped).
2. `section "This node"`.
   - row `{name: <self host>, value: <BackendState>, led: ok|crit}` (dim value).
   - row `address` = ip4 (mono) — the string people come to copy.
   - row `magicdns` = dns name (mono).
   - version: update available → row `{name: version, value: "<v> · update
     available", tone: warn, led: warn}`; else row `version <v>` dim.
   - key expiry when known: row `key expires` `<d>d`, `warn` + LED when ≤ 7,
     else dim.
   - exit node in use: row `{name: "exit node", value: <host>, tone: warn,
     led: warn}` (P amber: your traffic is routed).
3. `section "Exposure"`.
   - each exposure: row `{name: serve|funnel, value: first line, tone: warn
     (serve) / crit (funnel), led same}` — funnel = public internet.
   - none: row `{name: "serve / funnel", value: "nothing exposed", led: ok}`
     (P says the reassuring case out loud — keep).
   - we offer exit: row `exit node` · `offered by this node`, warn.
   - each advertised route: row `route advertised` · cidr, warn.
   - exit options: row `exit nodes available` · `a, b` (dim).
   - each peer route: row `route via <host>` · cidr (dim).
4. Online peers (`section "Online · <n>"`), sorted active first then host:
   row `{name: host, value: "<os> · <path>[ · <rate>]", led: ok if active
   else idle, tone: warn if relayed}` where path = `direct` | `relay <code>` |
   `idle` (no CurAddr and no Relay) and rate = `_fmt_rate`: `""` below 1 KiB/s,
   else `1.2 MB/s` / `340.0 KB/s` (binary units, one decimal). Relayed =
   `relay && !direct` — the one thing on this panel you can usually fix.
5. Watched offline (`section "Watched · offline · <n>"`, tone warn; P alert
   red): row `{name: host, value: time_ago(last) or "offline", tone: warn,
   led: warn}`, sorted by host.
6. Dormant = offline and not watched (`section "Dormant · <n>"`): P `Grid`
   2 columns sorted most recently seen first (unknown last): cell `(host,
   IDLE, time_ago)` or `(host, WARN, "key expired")`. N: `chips {columns: 2}`
   with `{label: host, tone: idle, value: "3d ago"}`; per DESIGN.md a
   non-healthy item (key expired — cannot come back without re-auth) leaves
   the grid and becomes a row `{name: host, value: "key expired", tone: warn}`
   above the chips.
7. Footer notes: no peers → `note "No peers in this tailnet" (idle)`; not
   pinned → `note "Pin peers in Settings → Tailscale"` (P: "pin peers in
   status-widgets/tailscale.json").

Every peer is named — "5 dormant" is not enough (P design note; keep).

## Menu

P: `Copy address   <ip>` (three spaces) when ip known; `Copy MagicDNS name`
when dns known → clipboard + toast `Copied  <text>`. N: menu items
`copy-ip` (`Copy address  100.64.0.1`) and `copy-dns`, handled per
dock-core §5.6 (`copy` field). Optional row action `copy` on the address and
magicdns rows (non-destructive, no confirm).

## Config

N `widgets.tailscale.watch`: `null` (auto) or a list of host names; edited in
Settings → Tailscale (list of current peers with checkboxes + free entry).

Legacy: `~/.config/status-widgets/tailscale.json` `{"watch": [...]}` —
migrate only if it actually contains a `watch` list.

## Bugs not to carry

- **The config file is the registry file.** P's watch list lives at
  `status-widgets/tailscale.json`, the very path the shared runtime writes as
  the widget's registration (`{"pid","name","xid"}`) on start and deletes on
  exit. The config is read once, then overwritten; the next start sees no
  `watch` key and silently falls back to auto mode. (Observed on this machine:
  the file holds registration keys.) N keeps the list in `config.json`.
- `crit` because self is offline shows a peer/ok label.
- `LastSeen` zero time → P shows "2026 years ago"-style garbage? (P
  `time_ago` of year 1 → huge `Nd ago`) — treat year < 2000 as unknown (`-`).

## Subtle behaviours to keep

- Throughput is per peer between polls, rx+tx combined, and counter resets
  yield no rate rather than a negative one.
- Exit node in use and anything exposed are always spelled out, even when
  healthy.
- Auto-watch grows only from peers seen **online** during this run.

## Tests (M3)

Fixture JSON (invented names, `100.64.x.x` IPs) with: backend Running/Stopped/
NeedsLogin, self offline, a watched offline peer, a relayed peer, an exit
node, peer routes, health messages, key expiry in 3 days, `LastSeen` zero
time, one-digit fractional seconds. Serve/funnel outputs: "No serve config",
a real config line, an error on stderr. Rate over two polls including a
counter reset.
