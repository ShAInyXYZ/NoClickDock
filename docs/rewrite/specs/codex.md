# Spec — codex

Parity reference: `widgets/codex/codex-status-checker.py` 1.1.0 (shared
runtime, see [dock-core.md](dock-core.md)). Legend: **P** Python, **N** NCD 2,
**BUG** do not carry, **Q** decision for main.

## Identity

id `codex` · title `Codex` (P `CODEX STATUS`) · order 20 · poll **30 s** ·
width P 588 GTK px (same as Claude); N 440 CSS px · mark: OpenAI blossom
(Simple Icons path in the file, viewBox 24) — monochrome.

## Data sources

### 1. Service status (every poll)

`https://status.openai.com` — an incident.io mirror of the Statuspage v2 API:

| url | timeout | used |
|---|---|---|
| `/api/v2/status.json` | 10 s | `status.indicator`, `status.description` |
| `/api/v2/components.json` | 10 s | `components[]` `name`, `status` |
| `/api/v2/incidents.json` | 10 s | there is **no** `unresolved.json`; keep incidents whose `status ∉ {resolved, postmortem}` |

Header `User-Agent: noclickdock-codex/1.0`. Any exception anywhere → keep
what was fetched so far (indicator may be set while components are empty);
defaults `indicator: "unknown"`, `description: "Unable to reach status page"`.

Codex-relevant components (chips, in this order): `Codex API`, `Codex Web`,
`Codex in ChatGPT Desktop` (→ `desktop app`), `CLI`, `VS Code extension` (→
`vs code`), `Responses`, `Chat Completions` (→ `completions`), `Login`,
`Conversations`; chip labels are lower-cased short names. Any component on the
whole page that is not operational counts as broken (not only the Codex ones).

Status words: operational, degraded (`degraded_performance`), partial outage,
major outage, maintenance (`under_maintenance`).

### 2. Plan limits and token spend — local rollouts (every 120 s)

Directories `~/.codex/sessions` and `~/.codex/archived_sessions`, walked
recursively (N: honour `CODEX_HOME` → `<home>/sessions`, `<home>/archived_sessions`;
Windows `%USERPROFILE%\.codex\…`). Files `*.jsonl` with mtime within 7 days.
Never raises: unreadable → empty result.

Per file, line by line (substring prefilters before JSON):

- `"session_meta"` (first only): `payload.session_id` or `payload.id` → session id.
- `"turn_context"`: `payload.model` → current model (persists until the next
  turn_context).
- `"token_count"` with `payload.type == "token_count"`:
  - `rate_limits = payload.rate_limits`; if it has `primary` and the record
    has `timestamp`: `limit_id = str(rate_limits.limit_id or "")`; keep the
    **newest by parsed instant** per limit_id (not by string — fraction
    lengths vary: `…00.9Z` vs `…00.15Z`). If `primary` or `secondary` has a
    truthy `used_percent`, the bucket is **live** (moves at least once).
  - turn usage: `payload.info.last_token_usage.total_tokens` with a
    `timestamp`; keyed by timestamp. A timestamp seen twice in the same file
    marks a **resumed session replay** (the resume rewrites history under one
    timestamp): drop **every** turn with that timestamp (deliberate
    under-count).
- per surviving turn: age buckets 1 h / 24 h / 7 d; within 24 h add the
  session id and `model or "unknown"` → per-model total.

Result:
- `1h`, `24h`, `7d`, `sessions` (24 h), `models` top 3 by 24 h tokens,
  `available = 7d > 0`.
- `plan` = first non-empty `rate_limits.plan_type` seen among the newest
  records (N: take it from the most recently seen bucket, deterministic).
- `limits_seen` = newest instant among buckets.
- One **group per limit_id**:
  - `name` = `limit_name` (a bucket label, not "your model" — the same bucket
    bills several models), `seen`, `rows` for `primary` and `secondary` with
    a numeric `used_percent`: `label = window_label(window_minutes)` (10080 →
    `WEEK`; multiple of 1440 above a day → `nD`; multiple of 60 → `nH`; else
    `nM`; unparsable → `LIMIT`), `pct`, `resets = fmt_reset(resets_at)`
    (epoch seconds → local `Sep 12, 3:59pm`), rows sorted by window length
    (shortest first).
  - `idle` = every row is 0 % **and** the bucket was never live.
  - groups sorted: non-idle first, then newest `seen` first.

P keeps the groups apart on purpose: collapsing buckets to "whichever turn ran
last" makes the panel swap between two meanings of "used" (a busy week reading
0 %). Keep.

Performance: rollouts are large. Cache per file by `(path, size, mtime)`;
only re-parse changed files; stream lines.

## State

| indicator | state | stateLabel |
|---|---|---|
| `none` | `ok` | description |
| `minor` | `warn` | description |
| `major` | P `bad` (orange) → N `crit` | description |
| `critical` | `crit` | description |
| unknown/unreachable | `idle` | `Unable to reach status page` (N `status page unreachable`) |

(**Q**: P's `bad` sits between warn and crit; N has no `bad`. Mapping `major` →
`crit` makes the ring breathe for a major outage, which is the right signal.)

## Glance

Highest `pct` among **non-idle** groups → `47%` (warn ≥ 75, crit ≥ 90); no
limits → 24 h tokens (`2.3M`); nothing → empty.

## Toasts

- Transition: the new description (P `transition_message`), empty description →
  none.
- Alert ids: `inc:<id>` per active incident; `comp:<name>:<status>` per
  broken component.
- Messages: incident name; `<short name>: <status label>` (`vs code: partial
  outage`). Tones: incident impact / component status (dock-core §7).

## Panel (single page), top to bottom

1. **Usage** — when limits or usage exist: `section {title: "Usage", meta:
   "<PLAN> plan"}` (P `USAGE · PLUS PLAN`).
   - For each group: when there is more than one group, a `note` heading
     `<name>   ·   <age>   ·   never reports usage` (age omitted when "just
     now"; the last part only when idle; note tone idle when the group is idle,
     else dim).
   - `limit {label, pct, resets, tone}` per row (warn ≥ 75, crit ≥ 90).
   - usage available: `stats` 1h / 24h / 7d / sessions (fmt_tokens).
   - models: `chips {label: short model, value: tokens}` columns = min(3, n);
     short model = strip a `-YYYY-MM-DD` suffix. P amber first chip → dropped
     (DESIGN).
   - none of the above: `section "Usage"` + note `No Codex sessions in the last
     7 days` (idle).
2. **Degraded · <n>** (tone crit) when any component on the page is broken:
   row per broken component `{name, value: STATUS LABEL, tone, led}`.
3. **Services** when components were fetched: `chips {columns: 3}` of the
   Codex components found (each at most once, in list order), LED by status;
   then note `<k> other OpenAI services operational` when others exist and
   nothing is broken.
   If components are empty **and** indicator is unknown: `section "Status
   page"` (tone crit) + note `status.openai.com unreachable` (idle).
4. **Active incidents** (tone crit), max 3: `alert {title: name, meta:
   "<STATUS> · <age>"` from the latest update's `created_at` (fallback
   `updated_at`) — ≤ 24 chars, `body: latest body[:160]`, tone by impact}`.
5. Footer: P prefixes `Limits as of <time_ago(limits_seen)>` → N
   `snapshot.footer = "limits as of 3h ago"`.

## Menu

P: `Open Codex Status page` (from `click_url`), left-click opens it. N:
`{id:"open-status", label:"Open status page", url:"https://status.openai.com"}`.

## Config

None.

## Windows

Rollouts under `%USERPROFILE%\.codex` (or `CODEX_HOME`); status page
identical. No external commands.

## Subtle behaviours to keep

- Rate limits come from the **last local rollout** per bucket — purely local,
  no API key, no account call. The reading is as old as the last Codex turn
  (the footer and group headings say how old).
- Replayed (resumed) turns are dropped, not double counted.
- Parsed-instant comparison for "newest".
- A bucket that never moves is kept (panel shape stable) but labelled "never
  reports usage" and sunk below live ones.

## Bugs / improvements

- **Stale windows after reset (SHOULD):** P shows the last recorded
  `used_percent` even after `resets_at` has passed (e.g. a 5 h window from
  yesterday still reads 80 %). N: when `resets_at < now`, show that row as
  `0%` with `resets` replaced by `reset <time_ago>` (dim), and exclude it from
  the glance. This is not P parity; it is what the numbers mean.
- `plan` depends on dict order (nondeterministic across files) — take it from
  the newest bucket.

## Tests (M4)

Fixture rollouts (invented ids): session_meta, turn_context model switch,
token_count with two limit ids (one idle), resumed-session replay (duplicate
timestamps), fractional-second ordering (`.9Z` vs `.15Z`), window minutes
300/10080/2880/45/`null`, a file older than 7 days (skipped by mtime).
Status fixtures: incidents list with resolved/postmortem/investigating; a
non-Codex component broken.
