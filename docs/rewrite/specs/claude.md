# Spec — claude

Parity reference: `widgets/claude/claude-status-checker.py` 1.0.1 — a
**standalone** GTK program (does not use `core/shinywidget.py`; its toast
logic differs, see below). Shared runtime rules: [dock-core.md](dock-core.md).
Legend: **P** Python, **N** NCD 2, **BUG** do not carry, **Q** decision for
main.

## Identity

id `claude` · title `Claude` (P panel title `ANTHROPIC STATUS`) · order 10 ·
poll **30 s** · width N 440 CSS px · mark: the Claude spark (Simple Icons path
`CLAUDE_LOGO_PATH` in the file, viewBox 24) — monochrome.

## Data sources

### 1. Service status (every poll)

Statuspage v2 JSON at `https://status.claude.com`:

| url | timeout | used |
|---|---|---|
| `/api/v2/status.json` | 10 s | `status.indicator` (`none minor major critical`), `status.description` |
| `/api/v2/components.json` | 10 s | `components[]`: `name`, `status` (`operational degraded_performance partial_outage major_outage under_maintenance`) |
| `/api/v2/incidents/unresolved.json` | 10 s | `incidents[]`: `id`, `name`, `status`, `impact`, `updated_at`, `incident_updates[0].body` |

Header `User-Agent: claude-status-light/2.0` (N: `noclickdock/2 (+status)`).
Requests run in sequence; any failure (network, bad JSON, missing key) leaves
the defaults: `indicator: "unknown"`, `description: "Unable to reach status
page"`, empty components/incidents — but a failure **after** status.json keeps
the indicator it already got (P behaviour; keep).

Component names are shortened: `Claude Console (platform.claude.com)` →
`console`, `Claude API (api.anthropic.com)` → `API`, `Claude Code` → `claude
code`, `Claude Cowork` → `cowork`, `Claude for Government` → `gov`,
`claude.ai` → `claude.ai`; others unchanged. P shows **every** component of
the page. N: skip Statuspage group containers (`"group": true`) if they ever
appear, keep the rest.

### 2. Plan limits — local quota cache (every 30 s, own goroutine)

Read `cachedUsageUtilization` from `~/.claude.json` on Linux or
`%USERPROFILE%\.claude.json` on Windows. An explicit `CLAUDE_CONFIG_DIR`
selects `<dir>/.claude.json`; never fall back to another profile. This is the
numeric observation Claude Code persists for `/usage`, verified with Claude
Code 2.1.303. Its internal schema may change in later versions.

- `fetchedAtMs` supplies the observation time; reading the file never makes
  an old value look fresh. Honor the CLI's one-hour persisted-cache lifetime.
- Check the cache's `accountUuid` against `oauthAccount.accountUuid`. Clear
  old limits on logout, account change, cache removal, or expiration.
- `utilization.five_hour` → Session; `seven_day` → Week; optional
  `seven_day_opus`, `seven_day_sonnet`, `seven_day_oauth_apps`, and
  `seven_day_cowork` → scoped weekly rows. Each contains `utilization`
  (0–100, including a genuine zero) and optional RFC3339 `resets_at`.
- Null, malformed, or absent windows are skipped independently. Unknown
  fields are ignored. A reset that has passed shows a note, never an old bar.
- Transient read/partial-write failures keep the last unexpired observation
  with an error note. Empty, unavailable, and stale caches explain how to
  obtain a reading, including when local token totals are already available.
- Open `/usage` in an up-to-date, signed-in Claude Code to populate/refresh
  the cache; NCD's Refresh now rereads it. Older versions without this cache,
  API-only accounts without plan quotas, and WSL profiles separate from the
  Windows user's profile may have no bars. NCD does not install hooks or
  change Claude settings.
- The reader is bounded to 8 MiB, decodes only usage/account fields and never
  logs raw configuration or JSON errors. It does not read credential stores,
  call private authenticated APIs, or run `claude -p /usage`. The latter is
  prompt mode, not a stable machine-readable usage endpoint, and was the
  source of silently missing bars and unnecessary CLI sessions.

### 3. Token spend — local transcripts (every 120 s)

Directory `~/.claude/projects` (N: honour `CLAUDE_CONFIG_DIR` → `<dir>/projects`;
Windows `%USERPROFILE%\.claude\projects`).

P algorithm:
- For each `<project>/<file>.jsonl` **one level deep** with mtime within 7 days:
- lines containing `"usage"`; JSON; `type == "assistant"`; `message.usage`
  is a dict; `timestamp` parses.
- total = `input_tokens + output_tokens + cache_creation_input_tokens +
  cache_read_input_tokens` (missing = 0); skip 0.
- age buckets `< 1 h`, `< 24 h`, `< 7 d` (negative age → 0).
- within 24 h: `sessionId` into the session set; `message.model` (default
  `unknown`, skip names starting with `<` such as `<synthetic>`) into the
  per-model total.
- models = top 3 by 24 h tokens; `available = 7d > 0`.

**BUG — over-count (measured):** Claude Code writes one line per content block
of a streamed assistant message, each repeating the same `message.id`,
`requestId` and `usage`. On a sample transcript here: 109 usage lines, 40
unique `(message.id, requestId)`, up to 5 copies — P over-counts ~2.7×.
N: de-duplicate by `(message.id, requestId)` (keep the entry with the largest
`output_tokens`; fall back to the line `uuid` when ids are missing).

**BUG — under-count:** subagent transcripts live one level deeper
(`<project>/<session>/subagents/*.jsonl`, 149 such files here) and are not
read by P. N: walk the tree (any depth under `projects/`), same filters.

Performance: thousands of files (6 700 here). Filter by mtime first; cache
per-file results keyed by `(path, size, mtime)` so a 120 s rescan only parses
files that changed; stream lines (no whole-file reads); bail out of a file
cheaply on lines without `"usage"`.

## Derived state

| indicator | state | stateLabel |
|---|---|---|
| `none` | `ok` | description (`All Systems Operational`) |
| `minor` | `warn` | description |
| `major` | `crit` | description |
| `critical` | `crit` | description |
| `unknown` (unreachable) | `idle` | `Unable to reach status page` (N: `status page unreachable`) |

P dot breathes for any indicator except none/unknown; N ring only for `crit`
(DESIGN.md).

## Glance

Highest limit `pct` → `54%` (tone crit ≥ 90, warn ≥ 75); no limits → 24 h
tokens `fmt_tokens` (`1.2M`, neutral); nothing → empty.

## Toasts (P logic is its own — keep the meaning)

P `_apply_data` after the first poll:
1. Indicator changed → `All systems operational` when new is `none`, else the
   new description (incl. `Unable to reach status page` when it goes dark),
   colour of the indicator.
2. Every component whose status changed from a known previous status → `<short
   name>: <LABEL>` (`API: PARTIAL OUTAGE`, and also **recoveries** `API:
   OPERATIONAL`) — only when the previous component map was non-empty.
3. New incident ids → the incident name — only when the previous incident set
   was non-empty (the dock-core quirk).

N mapping onto the hub rule (dock-core §5.4):
- transition message = (1).
- alert ids: `comp:<name>:<status>` for every non-operational component and
  `inc:<id>` per unresolved incident; messages `<short name>: <label>`
  (sentence form allowed: `API partial outage`) and the incident name; tone
  from status/impact (degraded/minor → warn, partial → warn, major → crit,
  maintenance → idle).
- cleared-alert messages (claude only): a `comp:` id that disappears because
  the component is operational again → `<short name> operational` (tone ok).
  Incidents resolving do not toast (P does not).
- An unreachable poll is an observed state (idle + transition toast), but it
  must **not** clear the previous component/incident sets (P's empty lists
  would make every existing incident "new" again after recovery — P avoids it
  only by accident of the non-empty check).

## Panel (single page), top to bottom

P → N blocks:

1. P badge = description upper-cased → N header LED + stateLabel.
2. **Usage** (`section "Usage"`, including a note when plan limits are unavailable):
   - per limit row: P one line `LABEL | pill meter | 54% | resets Sep 5,
     3:59pm`, colour by 75/90 → N `limit {label, pct, resets, tone}` (tone
     `warn` ≥ 75, `crit` ≥ 90, else none — DESIGN A6: no green bars).
   - usage available: P four tiles `1H 24H 7D SESSIONS` → N `stats {items:
     [{key:"1h",value:"348K"},{key:"24h",value:"1.2M"},{key:"7d",value:"9.8M"},
     {key:"sessions",value:"6"}]}`.
   - models: P up to 3 chips `model  tokens`, first in amber → N `chips
     {items:[{label:"opus 5", value:"1.1M"}…], columns: 3}` with no highlight
     (DESIGN: accent is interaction only; order already ranks them). Model
     names: `claude-opus-5` → `opus 5`, `claude-sonnet-5` → `sonnet 5`,
     `claude-fable-5` → `fable 5`, `claude-haiku-4-5-20251001` → `haiku 4.5`;
     unknown ids: strip `us.anthropic.`/`anthropic.` prefix, `-YYYYMMDD`
     suffix, `claude-` prefix, dashes → spaces.
   - no usage and no limits: note `No recent Claude Code sessions` (P) — N
     empty-state sentence.
3. **Services** (`section "Services"`): P a 3-column chip grid of every
   component, non-operational chips carry their status label. N (DESIGN.md
   chips): operational components as `chips {columns: 3}` with LED ok; each
   non-operational component leaves the grid and becomes a row above it
   (`section "Degraded · <n>"` tone per worst, rows `{name, value: label, tone,
   led}`) — the same shape the codex panel already has. Maintenance = `idle`.
4. **Active incidents** (`section "Active incidents"`, tone crit), up to 3:
   `alert {title: name, meta: "<STATUS> · <time_ago(updated_at)>" (≤ 24 chars:
   e.g. "investigating · 12m"), body: latest update body[:160], tone: impact
   (minor warn, major crit, critical crit, none ok)}`.
5. Footer: P `Last checked: HH:MM:SS UTC · Click: status page · ~: move ·
   Right-click: menu` → N standard footer (dock-core §5.7); `snapshot.footer`
   = `limits as of <age>` when limits exist.

## Menu

P: `Refresh now`, `Open status page`, separator, `Quit Claude widget`;
left-click opens the page. N: `menu = [{id:"open-status", label:"Open status
page", url:"https://status.claude.com"}]`.

## Config

None (no legacy file). Possible N option (not P): hide the Usage section.

## Windows

Status page and quota-cache reader are identical. Transcripts live in
`%USERPROFILE%\.claude\projects`. No CLI, npm shim, Bash, or PowerShell
process is needed to read usage limits.

## Subtle behaviours to keep

- Limits come only from Claude Code’s numeric quota observations; token
  counts are local and never presented as a percentage.
- Limits survive transient cache read failures until the observation expires.
- Usage before services before incidents (the headline is how much of the plan
  is gone).
- `<synthetic>` model entries excluded from the model split.

## Bugs not to carry

- Over-count from duplicated streaming lines; subagent transcripts ignored.
- "Refresh now" blanks usage and limits until the next poll
  (`_refresh_now` merge, line 1088).
- UTC footer time.
- First incident after a clean period never toasts by name.

## Tests (M4)

Fixtures: status/components/incidents JSON (invented incident), unreachable;
numeric cache windows (session, week, scoped weeks, zero/null/malformed
percentages, optional resets), observation freshness, logout/account changes,
partial writes, bounded reads, and default/override profile paths; a
transcript directory with duplicated streamed lines, `<synthetic>` model, a
subagents file, a line > 64 KB, an unparsable line. Assert de-dup totals.
