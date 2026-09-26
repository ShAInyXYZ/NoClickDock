# Spec — docker

Parity reference: `widgets/docker/docker-status-checker.py` 1.1.1 (shared
runtime, see [dock-core.md](dock-core.md)). Legend: **P** Python, **N** NCD 2,
**BUG** do not carry, **Q** decision for main. Container, stack and image
names below are invented.

## Identity

id `docker` · title `Docker` · order 50 · poll **20 s** · width P 560 GTK px;
N 440 CSS px · body max height P 640 · pages **Now / Stopped / System** (only
when the daemon is available; otherwise single page) · mark: Docker whale/
crate (P path in the file, viewBox 24).

## Data sources

Everything through the `docker` CLI (argv, no shell; Windows `docker.exe`
with `CREATE_NO_WINDOW`). The CLI honours the user's current docker context
(`DOCKER_HOST`, Docker Desktop's `desktop-linux` context) — keep using the CLI
rather than the raw socket for that reason.

| step | command | timeout | cadence |
|---|---|---|---|
| availability + ids | `docker ps -aq` | 20 s | every poll; rc ≠ 0 (or binary missing) → **unavailable** |
| details | `docker inspect <ids…>` | 20 s | every poll, one call for all ids |
| live usage | `docker stats --no-stream --format "{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}"` | 8 s | only when ≥ 1 container is `running` |
| last log line | `docker logs --tail 5 <name>` (stdout + stderr) | 4 s | for broken containers, cached (below), ≤ 8 fetches per poll |
| compose present | `docker compose version` rc == 0 | 5 s | once per core run |
| System page | see "System" | | every **300 s** (and after a prune) |

P parses `docker inspect --format` with a tab-separated template:
`{{.Name}} {{.State.Status}} {{.State.ExitCode}} {{.State.OOMKilled}}
{{.State.StartedAt}} {{.State.FinishedAt}} {{.RestartCount}}
{{if .State.Health}}{{.State.Health.Status}}{{end}} {{.Config.Image}}
{{index .Config.Labels "com.docker.compose.project"}} {{index .Config.Labels
"com.docker.compose.service"}} {{.HostConfig.RestartPolicy.Name}} {{json
.NetworkSettings.Ports}}`. N: call `docker inspect <ids…>` **without**
`--format` and decode the JSON array (same fields; no tab-splitting fragility
when a label contains a tab/newline).

Per container:

| field | rule |
|---|---|
| name | `.Name` without leading `/` |
| state | `.State.Status` lower (`running paused restarting exited dead created removing`) |
| exit_code | `.State.ExitCode`, but `None` while running/paused/restarting |
| oom | `.State.OOMKilled` |
| started / finished | `.State.StartedAt` / `.State.FinishedAt`; year < 2000 (`0001-01-01…`) = unknown |
| restarts | `.RestartCount` |
| health | `.State.Health.Status` only when state is `running` (`starting healthy unhealthy`), else none |
| image | `.Config.Image` |
| project / service | compose labels (empty = standalone) |
| policy | `.HostConfig.RestartPolicy.Name` unless `""`/`no` |
| ports | from `.NetworkSettings.Ports`: per container port, **first binding only**: host IP `0.0.0.0`/`::` → blank; `":8080"` when host port == container port and no IP, `":8080->80"` when they differ, `"127.0.0.1:8108"` with an IP (P drops the container port when an IP is present) |
| up_h | hours since StartedAt when running-ish |
| age_h | hours since FinishedAt when not running-ish |
| **crashed** | state ∈ {`exited`,`dead`} **and** (`oom` **or** exit code ∉ {0, 143, 137}) |
| cpu, mem, mem_b | from stats by name: cpu `%` float; mem text of the used part (`492.1MiB` → `492M`; `22.21GiB` → `22.2G` — one decimal only for G/T below 10); mem_b exact bytes (binary units) |

**Exit-code semantics to keep:** 0 = clean; 143 = SIGTERM = `docker stop`; 137
= SIGKILL = `docker stop` timeout or `docker kill` — **not** a crash unless
`OOMKilled` is true. Everything else non-zero is a crash.

### Units

A compose project is one unit, a standalone container its own unit (key =
project or name). Per unit:

- `running` = members in `running` or `paused`
- `bad` = members `restarting`, or `running` with health `unhealthy`
- `crashed` = members with `crashed`
- `age_h` = min `age_h` of non-running members (most recent activity), unknown if none
- `mem` = Σ mem_b of running members; `cpu` = Σ cpu of running members
- units sorted by name, case-insensitive

### Logs

For containers that are crashed, unhealthy or restarting: last non-empty line
of `docker logs --tail 5` (stdout+stderr concatenated), ANSI escapes removed
(`\x1b\[[0-9;?]*[A-Za-z]`), whitespace collapsed, a leading ISO timestamp
(`^\d{4}-\d\d-\d\dT[\d:.]+Z?\s*`) stripped, cut to 90 chars. Cache key
`(name, "live")` for running/restarting (always refetched), `(name,
FinishedAt)` for stopped ones (never refetched — a stopped container's last
line cannot change). Order of fetching: most recent first; at most **8
fetches per poll**; LRU cache of 200 entries.

### Portainer

If a container named exactly `portainer` is running with published ports: host
port choice 9443 (→ `https://localhost:9443`), else 9000 (`http`), else the
first published port that is not 8000 (edge-agent tunnel, serves no page).

### System (every 300 s)

- `docker version --format "{{.Server.Version}}"` (5 s) → engine
- `docker compose version --short` (5 s) → compose (empty = not installed)
- `docker images -f dangling=true -q` (10 s) → dangling count (lines)
- `docker system df --format "{{.Type}}\t{{.TotalCount}}\t{{.Active}}\t{{.Size}}\t{{.Reclaimable}}"`
  (30 s) → per type (lower-cased: `images`, `containers`, `local volumes`,
  `build cache`): total, active, size, reclaimable, pct. Sizes are **decimal**
  (`kB`=10³ … `TB`=10¹²), format `83.54GB (17%)`. Output formatting `_fmt_dec`:
  `1.2T`, `83G`, `9.5G`, `512M`, `0` for zero.

## State

Attention list = for every unit: each `bad` member → reason `restarting` or
`unhealthy` (any age); each `crashed` member with `age_h < 24` → reason
`OOM-killed` or `exit <code>`.

| state | condition | stateLabel |
|---|---|---|
| `idle` | unavailable | P `DAEMON UNREACHABLE`; N `daemon unreachable` (Windows: `Docker Desktop not running`) |
| `crit` | any attention reason `restarting`/`unhealthy` | `<n> container(s) unhealthy` (counts restarting too) |
| `warn` | attention non-empty (crashes in the last 24 h) | `<n> crashed today` (P wording; it means "in the last 24 h") |
| `ok` | otherwise | `<n> running` (running + paused) |

## Glance

`<running count>`; unavailable → `off`; tone = state tone when warn/crit.

## Toasts

- Transition: default = new stateLabel.
- Alert ids: only `restarting:<name>` and `unhealthy:<name>` (crashes are
  **not** alerts — they surface through the `warn` transition).
- Alert message: `<name>: RESTARTING` / `<name>: UNHEALTHY`; N sentence form
  `<name> is unhealthy` / `<name> is restart-looping`, tone crit.
- Action results (§Actions).

## Pages

### Unavailable (single page)

P: `Section("DAEMON", alert)`, notes "Docker is not running, or this user
cannot reach the socket. Try: docker info". N empty state: "Docker is not
running, or this user cannot reach its socket. Check with `docker info`."
(Windows: "Docker Desktop is not running.") If the CLI is missing: "docker is
not installed".

### Now

1. **Needs attention** (`section {title: "Needs attention", tone: crit}`) —
   one `alert` per attention entry (unit, container, reason):
   - tone `crit` for restarting/unhealthy, `warn` for crashes.
   - title = container name.
   - P meta: running → `<reason> · up <dur>`; stopped → `<why> · <time_ago_h>`;
     plus ` · restarted N×` when restarts > 0. **N meta must fit 24 chars
     (DESIGN A12)**: meta = the short cause (`unhealthy`, `restarting`,
     `exit 1`, `OOM (137)`) + `· 3h` age when it fits; the rest moves to the
     body.
   - body = last log line (mono) when known, else the image; N: when there is
     room, second body line `up 46h · restarted 3× · restart=always but not
     back`.
   - actions = container actions (below); pending → the alert becomes a
     pending row.
2. **Running · <n>** (`section`, n = running containers). Units with ≥ 1
   running member, sorted by memory descending. Row per unit:
   - multi-member stack: value `<r>/<m> up · <mem> · <cpu>`; LED `warn` when
     partial else `ok`; sub = running services (service label or name,
     sorted, comma-joined), plus `   down: a, b` when partial.
   - single container: value `up <dur>` or `paused` · mem · cpu; LED `warn`
     when health `starting`, paused, or restarts > 0, else `ok`; sub =
     `image · ports… · health · restarted N×`.
   - mem `_fmt_bytes_short`: `≥1G → 1.5G`, `≥1M → 492M`, else empty; cpu shown
     only when ≥ 1 % as `7%`.
   - actions = unit actions.
   - nothing running and no attention → note `Nothing running` (idle).
3. **Elsewhere**: when stopped units exist, `section "Elsewhere"` + note
   `<n> stopped stack(s)[, <k> broken]  →  STOPPED page` (P). N: `<n> stopped
   · <k> broken — see Stopped`. No units at all → `No containers` (empty
   state).

Durations: `_dur(h)`: `<1h → "24m"`, `<48h → "46h"`, else `"3d 18h"` below a
week, `"12d"` beyond. `time_ago_h`: `24m ago`, `46h ago`, `<14d → 5d ago`,
`<60d → 3w ago`, else `4mo ago`.

### Stopped

Units with no running member; empty → note `Everything is running` (idle).

Row per unit (`line(u)`):
- worst = first crashed member, else first member.
- value = `time_ago_h(age_h)` or `never started`; when worst has a log line:
  sub = the log line (mono) and value = `OOM · 3h ago` / `exit 1 · 3h ago`.
- sub otherwise: single member → `why(worst)`; stack → `<b>/<m> crashed · ` or
  `<m> services · ` + `why(worst)`.
- LED `crit` when any member crashed (ever, not just 24 h) else `idle`.
- actions = unit actions (pending row when pending).

`why(c)`: `created` → `created, never started`; oom → `OOM-killed (137)`;
unknown code → state; 0 → `exited cleanly`; 143 → `stopped (SIGTERM)`; 137
without OOM → `killed (137, docker stop timeout or kill)`; other → `exit <code>
· <meaning>` from `{1 error, 2 shell misuse, 125 daemon error, 126 command not
executable, 127 command not found, 128 invalid exit, 130 SIGINT, 134 abort
(SIGABRT), 139 segfault (SIGSEGV), 255 exit 255}` else `error`; append ` ·
restart=<policy> but not back` when crashed with a policy.

Groups, each only when non-empty:
- `Broken · <n> · crashed, not touched since` (tone crit) — crashed units,
  most recent first (unknown last).
- `Stopped · <n> · this week` — non-crashed, `age_h < 168`, most recent first.
- `Dormant · <n> · not run in a week` — non-crashed, `age_h ≥ 168` or unknown,
  oldest-known first (unknown last). N: section title + `meta` = the count
  and the phrase.

### System

1. `section "Engine"`: rows `Docker` = engine or `?`; `Compose` = version or
   `not installed`; `Containers` = `<total> · <running> running`; `Images` =
   `<total> · <dangling> dangling`; `Volumes` = `<total> · <active> in use`.
2. If df data: `section "Disk · reclaimable"` then rows (value
   `<reclaimable> of <size>`, LED `warn` when reclaimable > 20 GB else idle):
   - `images` — sub `<d> dangling · prune drops every image no container
     uses`; action `prune unused` (confirm `free <X>?`, danger) only when
     reclaimable > 0.
   - `containers` — sub `<s> stopped · prune removes every one of them`;
     action `prune stopped` (confirm `remove <s>?`, danger) only when stopped
     > 0.
   - `build cache` — sub `rebuilds cost time, not data`; action `prune`
     (confirm `prune?`, danger), always offered.
   - `volumes` (key `local volumes`) — sub `data lives here · never pruned
     from this panel`; **no action, ever**.
   - note `Buttons ask twice. Nothing in use is ever removed.`
3. No df data → note `docker system df did not answer` (idle).

## Actions (the injection surface — read carefully)

Every action is validated by the core against the current snapshot (dock-core
§5.5); args carry only a unit or container **name that exists in the last
inspect**, never a command. All run with a **120 s** timeout.

Unit actions (Now running rows, Stopped rows), hidden while the unit is
pending:

| id | shown when | confirm (P) | command | past tense |
|---|---|---|---|---|
| `unit.resume` | any member paused | — (N: `resume?`) | `docker unpause <running members…>` | Resumed |
| `unit.restart` | unit running | `restart?` | stack+compose: `docker compose -p <project> restart`, fallback `docker restart <members…>`; standalone: `docker restart <name>` | Restarted |
| `unit.stop` | unit running | `stop?` | compose `stop` / `docker stop <members…>` | Stopped |
| `unit.start` | unit not running | — (N: `start?`) | compose `start` / `docker start <members…>` | Started |
| `unit.remove` | unit not running | `remove stack?` (stack > 1 member) / `remove?`, danger | compose `down` / `docker rm <members…>` | Removed |

Container actions (Needs-attention alerts), hidden while the container or its
project is pending:

| id | shown when | confirm | command |
|---|---|---|---|
| `container.restart` / `container.stop` | running or restarting | `restart?` / `stop?` | `docker restart|stop <name>` |
| `container.resume` | paused | (N `resume?`) | `docker unpause <name>` |
| `container.start` / `container.remove` | otherwise | — / `remove?` danger | `docker start|rm <name>` |

System actions: `prune.images` → `docker image prune -a -f`; `prune.containers`
→ `docker container prune -f`; `prune.builder` → `docker builder prune -f`.
After a prune the System data is refetched (cache invalidated).

Flow: mark pending (`("unit", name)` for every member plus the unit label;
prunes use a separate `("prune", label)` scope so a container named `images`
never reads as pruning) → publish snapshot (row shows `<verb>ing…`, P amber;
N tone `busy`, LED busy) → run → fallback command if the compose command
failed → result toast `Stopped <label>` (ok) or `<label>: <last line of
stderr (or stdout)>[:70]` / `<label>: failed` (crit) → clear pending → refresh.

Compose note: `docker compose -p <project> <verb>` works without the compose
file for `stop`/`start`/`restart`/`down` (v2 uses labels); `down` also removes
the project's networks. Always pass `--` before names in plain docker
commands? Container names cannot start with `-` (docker allows
`[a-zA-Z0-9][a-zA-Z0-9_.-]*`), but validate the name against that pattern
anyway before building argv.

## Menu

P: `Open Portainer` when detected. N: menu item `{id: "open-portainer",
label: "Open Portainer", url}` (dock-core §5.6).

## Footer

N `snapshot.footer` = `engine 27.3 · compose 2.29` (from System data), or
empty until known.

## Windows

`docker.exe` (Docker Desktop puts it on PATH; also try `%ProgramFiles%\Docker\Docker\resources\bin\docker.exe`).
Docker Desktop stopped → `docker ps` fails → unavailable with the Windows
wording. `docker stats` over the Desktop VM is slower (can exceed 8 s): on
timeout, show rows without mem/cpu rather than failing the poll. Everything
else identical.

## Subtle behaviours to keep

- 137/143 are stops, not crashes, unless OOM.
- A crash is "news" for 24 h (warn state), but stays in the Stopped page's
  Broken group until the unit is touched.
- Arm state survives the 20 s refresh (keyed, not DOM-bound).
- Pending markers hide the buttons of the unit and of every member, and of the
  whole project when a single member is acted on.
- Memory totals from exact bytes, not the rounded strings.
- Log-line cache keyed by stop time; restart-loopers keyed `live`.
- Stack actions go through compose (order, networks) with a plain-docker
  fallback.

## Bugs not to carry

- Template tab-splitting of `docker inspect` (use JSON).
- `start` and `resume` are one-click in P; DESIGN.md requires two-step (**Q**
  if main wants strict parity).
- Alert meta strings far over DESIGN's 24-char budget (restructure as above).

## Tests (M3)

Fixtures (invented names): `docker inspect` JSON for running healthy,
running unhealthy, restarting, exited 0/1/137/137+OOM/143, created (zero
FinishedAt), paused, a 3-member compose stack with one member down, ports
bound to 0.0.0.0 / 127.0.0.1 / mapped. `docker stats` lines with MiB/GiB,
`docker system df` lines with `0B`, `kB`, `GB (17%)`. Unit grouping,
attention list, state/label, why(), port formatting, action validation
(unknown name → rejected, name with a leading `-` → rejected).
