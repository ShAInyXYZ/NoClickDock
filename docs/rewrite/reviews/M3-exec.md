# M3 — collectors A · executor report

Branch `rewrite/tauri-go-svelte`. Commits, in order:
- `4c8747b` — `sys.Output` / `sys.Find`
- `9b32ace` — the Docker collector
- `a86e646` — `widgets.tailscale.watch`, plus the one-time legacy migration
- `34434d9` — the Tailscale collector
- `dab3002` — both writers follow DESIGN's new "What the panel says" rules
  (design M2 r1)

Disk reached parity in M1/M2 (Q6 included). The M2 r2 shell fixes landed in
between and are described in M2-exec.md.

## Docker (`core/internal/widgets/docker/`)

Everything goes through the `docker` CLI, so the user's context is honoured
(`DOCKER_HOST`, Docker Desktop's).

**Reading, per spec:**
- `ps -aq` (20 s) and one `inspect <ids…>` (20 s).
- `stats` (8 s), only when something runs. On a timeout the rows show
  without numbers.
- The last log line (4 s) of each broken, unhealthy or restarting container.
- The System numbers every 5 min, in the background (the poll never waits
  for `system df`), and again after a prune.

**Parity points kept:**
- 143 and 137 are stops unless the OOM killer sent them.
- A crash is news for 24 h (`warn`) and stays in *Broken* until the unit is
  touched.
- Compose projects are one unit; totals come from exact bytes.
- Log lines are cached by stop time; live containers are asked again every
  poll. At most 8 lookups a poll, LRU of 200.
- Portainer: 9443, then 9000, then the first port that is not 8000.
- Issues are `restarting:` and `unhealthy:` only; crashes surface through
  the `warn` transition.

**Actions:**
- Every action asks twice (Q1).
- The hub already checks the action against the shown snapshot. The widget
  checks again that the name exists in the last inspect and matches
  `^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`, and puts `--` before names in argv.
- A stack goes through `docker compose -p <project> stop|start|restart|down`,
  with plain `docker … -- <members>` as the fallback.
- While an action runs, the unit and all its members are busy rows (`busy`
  tone and lamp) with their buttons hidden. The action has a 120 s timeout.
- Results read `Stopped <label>` or `<label>: <last stderr line, 70 chars>`.
  A failure refreshes by itself, because the hub only refreshes after a
  success.
- Compose presence is asked once per run, on first use.

**Where N differs from P or the spec:**
- `docker inspect` is decoded as JSON, not a tab template (spec).
- The alert meta fits 24 characters (`OOM (137) · 2h`). A second body line
  (`up 46h · restarted 3×`, `restart=always but not back`) shows when the
  log line leaves room: Alert's body keeps newlines, and the 2-line clamp
  decides.
- A restart-looping unit is an alert, not a *Dormant* row on Stopped. P
  listed it as "not run in a week · never started".
- `prune stopped` counts exited, created and dead containers. P counted
  paused and restarting ones too, which prune does not remove.
- `unit.resume` unpauses only the paused members. P sent `unpause` to
  running ones too, which fails.
- Durations under a minute read `<1m` / `just now`, not `0m`.
- Design M2 r1 changes:
  - `4/4 · 1.2G · 6%`;
  - `5 stacks · 10 containers`;
  - a crash body no longer repeats its meta;
  - the System page is one row per kind of thing docker keeps (counts in
    the sub, reclaimable space in the value), and the versions are the
    footer's (`no compose` when compose is absent).
- The mock is the real `render` fed an invented host. It is its own type
  (`docker.Mock`), so mock mode can never reach the real actions.

**Windows:** `docker.exe` on PATH, else
`%ProgramFiles%\Docker\Docker\resources\bin\docker.exe`, with the "Docker
Desktop not running" wording. Compiled only (M7).

## Tailscale (`core/internal/widgets/tailscale/`)

- **Status:** `tailscale status --json` (15 s). A stopped or logged-out
  daemon still answers in JSON, sometimes with a non-zero exit, and what it
  says is the news. No JSON means unreachable.
- **Watch list:** comes from `widgets.tailscale.watch`, read on every poll,
  so a change needs no restart.
  - `null`: every peer seen online during this run.
  - `[]`: none.
  - A list: those hosts, lower-cased.
  - Peers seen online are remembered in every mode, so switching back to
    automatic loses nothing. A peer already dormant at start is never
    watched.
- **Rate:** per peer between polls, rx and tx combined; no rate after a
  counter reset.
- **Times:** year-1 `LastSeen` is unknown; one-digit fractions parse.
- **State:** follows the spec's order. For "Running but this node offline"
  it says `this node offline`, where P fell through to a peer label.
- **Glance:** `up/total`; the days left when only the key is expiring; `off`
  when critical.
- **Issues:** `backend:`, `down:` and `health:`, in sentence form.
- **Exposure:** read from `tailscale serve status --json` (5 s). A port
  counts as a funnel only where `AllowFunnel` says so.
  - **Found on this machine:** `tailscale funnel status` prints the same
    text as `serve status`, including tailnet-only serves. P's text rule
    would have shown them as public funnels (crit).
  - If `serve status` does not answer, the row reads `unknown` rather than
    "nothing exposed".
- **Panel:** after design M2 r1:
  - problems come first (watched peers offline, then health; a node that is
    down leads with This node);
  - peer names are in Manrope and labels in sentence case;
  - backend states are words (`needs login`);
  - every online peer has the `ok` lamp; a relayed peer is `warn` in both
    lamp and value;
  - dormant peers are chips, with an expired key pulled out as a row.
- **Menu:** `copy-ip` and `copy-dns` (`copy` field).
- **Windows:** `tailscale.exe` on PATH, else
  `%ProgramFiles%\Tailscale\tailscale.exe`.

## Config (`core/internal/config/`)

- **The setting:** `widgets.tailscale.watch` (`*[]string`). An empty host
  name is refused, and unknown keys are refused as before.
- **Legacy migration:** `Store.MigrateLegacy` copies the list from
  `~/.config/status-widgets/tailscale.json`, and only reads that file. It
  runs:
  - only when `config.json` does not exist yet (NoClickDock 2's first run);
  - only on Linux;
  - only in production (never with `--dev`, `--mock`, `--config` or
    `--once`);
  - only if the file really holds a `watch` list. The Python runtime
    overwrote the file with its registration, so most copies hold none.
- Collectors now receive the config store (`widgets.Real(cfg)`).

## Shared

- **`sys.Output`:** stdout and stderr kept apart. Errors name the program,
  not its full path.
- **`sys.Find`:** looks on PATH, then at given absolute paths.
- **`sys.Clock`:** one time format — `16:40` within the day, `Mon 09:00`
  beyond it — for the M4 writers.

## Tests

`go vet` passes on linux and `GOOS=windows`, `go test -race ./...` passes,
and `gofmt` is clean. All fixtures are invented (the Tailscale IPs are in
100.64/10, the documentation range 203.0.113/24, the tailnet `tail0c4fe`,
the domain example.org).

- **docker:**
  - an inspect fixture of 15 containers: healthy, unhealthy, restarting,
    exited 0/1/137/137+OOM/143, created, paused, two stacks with a member
    down, and ports on 0.0.0.0, `::`, 127.0.0.1 and mapped. A label holds a
    tab and a newline;
  - stats lines in KiB, MiB and GiB;
  - df lines with `0B`, `kB` and `GB (62%)`;
  - unit grouping, the attention list, state and label, `why()`, port
    formatting, and the three pages line by line;
  - busy rows and hidden buttons, and a prune's own scope;
  - action validation: unknown name, `-rf`, `--all`, empty name, unknown
    verb;
  - the compose plan and its fallback; unpause of paused members only;
  - a fake `docker` script for the fallback, the error tail cut to 70
    characters, the prune invalidating df, the log cache (stopped cached,
    live asked again), and the 8-lookup cap.
- **tailscale:**
  - status fixtures: Running (health, key expiring in 3 days, zero
    LastSeen, one-digit fractions, a relayed peer, an exit node, peer
    routes, update available), Stopped and NeedsLogin;
  - state, label and glance across 9 cases;
  - issue order;
  - automatic watching (grows only from peers seen online, a blip stays
    watched, `[]` watches none);
  - the rate over two polls, including a counter reset;
  - serve and funnel JSON, empty config, non-JSON;
  - the whole panel line by line, the "unknown" exposure, and the no-peers
    and not-pinned notes.
- **config:** null, `[]` and list round trips; migration moves a list only
  on the first run, never writes the legacy file, and ignores a
  registration record, a non-list and a missing file.
- **sys:** `Clock`.

**Checked on this machine:**
- `--once docker` and `--once tailscale` against the real daemons (output
  kept in the scratchpad).
- `--dev` for 75 s: disk, docker and tailscale stable, with no collector
  errors.
- No action ever ran against a real container: the action path is tested
  only through the fake script.
- A scan of every added line and commit message since `25a7717` against
  this machine's real container names, peer names, IPs and tailnet found
  only generic words (`docker`, `desktop`).

## Known gaps

- The Settings page for the watch list is M6. Until then the list is set by
  editing `config.json` or with a `config.patch`.
- Windows runtime behaviour (Docker Desktop's slow `stats`, the
  installer's `tailscale.exe`) is compiled only: M7.
- `tailscale serve status --json` needs tailscale ≥ 1.38 or so. Older
  clients show exposure as `unknown`.
- Docker actions have not been exercised against a live daemon, on purpose
  (they would change the user's real containers). A disposable container
  of our own on a throwaway context would be the M7/M8 end-to-end check.

## M3 r2 and r3 (research M3 r1, r2) — `e625c2f`, and with M4's final commit

- **Exposure:**
  - foreground (`Foreground[session]`) serves and funnels are read and kept
    apart, as `Serve · session` / `Funnel · session` rows;
  - services are listed;
  - an unknown shape, at the top or inside a foreground entry, reads
    `unknown`;
  - funnel ports are sorted as numbers.
- **Peers:** known by node key and named by their MagicDNS label. A pin
  matches the label **or** the lower-cased HostName (`office pc` for
  `Office PC`).
- **Labels:** `1 health warning`; `<host> offline` cut to the 28-character
  budget.
- **Duplicate presses:** the hub refuses a second press of an action still
  running with an answer that names nothing and raises no toast. Docker
  refuses a new action on a unit it is already acting on.
