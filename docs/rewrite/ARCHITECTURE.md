# NCD 2 — architecture

NoClickDock is rewritten on the Cerveau / HubVoice stack: a **Go** core that
knows everything, a thin **Tauri 2** shell that owns windows, and a
**Svelte 5** interface that draws it. One process tree, one install, the same
code on Linux and Windows.

The Python generation (`core/`, `dock/`, `widgets/`, `install.*`,
`tools/make-mockups.py`) stays in the tree as the behavioural reference until
every widget has reached parity (see `specs/`). It is removed in the last
milestone; git keeps it.

## Process model

```
ncd (Tauri, Rust)                       ncd-core (Go, sidecar)
├─ dock window      ─┐                  ├─ hub: scheduler, one goroutine per collector
├─ panel window      ├─ Svelte UI  ◄──► ├─ collectors: claude codex comfyui disk docker power tailscale
├─ toast window      │   (events /      ├─ transitions → toasts
├─ settings window  ─┘    invoke)       ├─ actions (docker start/stop, prune, copy, open…)
└─ tray, autostart, single instance     └─ config (owns config.json, migrates legacy files)
        ▲    stdout: JSON lines (snapshot, toast, progress, config)
        └──► stdin:  JSON lines (action, refresh, config.patch)
```

- **ncd-core** does all data work. Nothing in Rust or Svelte shells out to
  `docker`, reads `/proc`, or parses a status page.
- **Production transport is stdio JSON lines** between the Tauri shell and the
  sidecar. No listening port exists in production, so no web page or local
  process can reach the Docker actions. Rust relays each line to the windows
  as a Tauri event and forwards `invoke('core_send', …)` to stdin.
- **Development transport is HTTP + SSE**, only with `--dev` (real data) or
  `--mock` (invented data), bound to `127.0.0.1`, CORS limited to the Vite dev
  origins. This lets the UI run in a plain browser, which is how the design
  supervisor screenshots it and how README images are made.
- The Svelte side has one `transport.ts` with two implementations,
  `TauriTransport` and `HttpTransport`, behind the same interface. Components
  never know which one is in use.

## Wire protocol (both transports carry the same messages)

Core → UI (stdout line / SSE event name = `type`):

```jsonc
{"type":"hello","version":"2.0.0","platform":"linux","config":{…},"widgets":["claude",…]}
{"type":"snapshot","widget":{WidgetSnapshot}}          // whenever a widget's snapshot changes
{"type":"toast","toast":{"widget":"docker","tone":"crit","text":"api-gateway unhealthy","at":"…"}}
{"type":"config","config":{…}}                           // after any config change
{"type":"result","id":"…","ok":true,"message":"stopped api-gateway"}   // reply to an action
```

UI → core (stdin line / `POST /v1/send` body):

```jsonc
{"type":"action","id":"<uuid>","widget":"docker","action":"stop","args":{"container":"api-gateway"}}
{"type":"refresh","widget":"docker"}                     // omit widget = all
{"type":"config.patch","patch":{"dock":{"orientation":"horizontal"}}}
```

Dev HTTP: `GET /v1/state` (hello + every snapshot), `GET /v1/events` (SSE of
the core→UI messages), `POST /v1/send` (one UI→core message).

### WidgetSnapshot

```jsonc
{
  "id": "docker", "title": "Docker", "order": 50,
  "state": "ok" | "warn" | "crit" | "idle" | "busy",
  "stateLabel": "2 unhealthy",          // short words for the panel header
  "glance": {"text": "12/14", "tone": "warn"},   // the dock readout; tone defaults to neutral
  "progress": 0.42,                     // optional, 0..1 — busy ring on the dial (ComfyUI)
  "updatedAt": "2026-09-26T12:41:07Z", "pollSecs": 20,
  "error": null,                        // collector failure, shown in the panel, never swallowed
  "width": 440,                         // preferred panel width in px (logical)
  "pages": [ {"id":"now","label":"Now","badge":"2","tone":"crit","blocks":[Block…]} ],
  "footer": "engine 27.3 · compose 2.29",
  "menu":   [ {"id":"open-portainer","label":"Open Portainer"} ]
}
```

A single-page widget has one page with an empty label; the tab strip is only
drawn for two or more pages.

### Blocks

The Python primitives, typed. Every block has `"kind"`. Tones are
`ok | warn | crit | idle | info | busy | null` — never raw colours; the UI maps
tones to tokens.

| kind | fields | Python origin |
|---|---|---|
| `section` | `title`, `meta?`, `tone?` | `Section` |
| `row` | `name`, `sub?`, `value?`, `tone?`, `led?`, `mono?`, `actions?` | `Row` |
| `meter` | `label`, `frac`, `value?`, `note?`, `tone?` | `Meter` |
| `limit` | `label`, `pct`, `resets?`, `tone?` | `Limit` |
| `stats` | `items:[{key,value,tone?}]` — one inline mono line, not tiles | `Tiles` |
| `chips` | `items:[{label,tone,value?}]`, `columns` | `Chips`, `Grid` |
| `alert` | `title`, `meta?`, `body?`, `mono?`, `tone`, `actions?` | `Alert` |
| `note` | `text`, `tone?`, `mono?` | `Note` |
| `table` | `columns:[{label,align,mono}]`, `rows:[{cells[],tone?,led?,sub?}]` — a cell is `"text"` or `{"text","tone"}` | Power's draw table |
| `donut` | `segments:[{label,value,display}]`, `center`, `unit` | Power's cost split |
| `bars` | `values[]`, `labels?`, `highlight?`, `unit?` | Power's 24 h sparkline |
| `split` | `columns:[[Block…],[Block…]]` | Power's two-column layout |

An **action** is `{"id","label","confirm?","danger?","args"}`. With `confirm`,
the first click arms it (label becomes the confirm text) and a second click
within 4 s sends it; the arm survives snapshot refreshes because it is keyed
by `widget/id/args`, not by DOM node.

The block set grows only when a widget genuinely cannot be expressed with
the existing kinds; a new kind needs a Svelte renderer, a mock, and a line in
this table.

A **menu item** is `{"id","label"}` (routed to the core as an action) or
`{"id","label","url"}` / `{"id","label","copy"}`, which the Rust shell handles
itself (open with the system opener / put on the clipboard) without a round
trip to the core.

Snapshots are emitted **every poll**, changed or not (as a lightweight
`{"type":"tick","widget","at"}` when unchanged), because the UI's stale rule
(DESIGN.md A11) depends on hearing from each collector.

## Decisions on the specs' open questions (2026-09-26)

| # | decision |
|---|---|
| Q1 | Every Docker action is two-step, `start` and `resume` included — the user asked for confirmations on all of them. |
| Q2 | A status-page indicator of `major`/`critical` maps to `crit`. |
| Q3 | Power level-change toasts stay, with 60 s hysteresis. |
| Q4 | Power utilisation tones: RAM/VRAM warn ≥ 90 %, crit ≥ 97 %; CPU utilisation carries no tone. |
| Q5 | ComfyUI VRAM meters: warn ≥ 85 %, crit ≥ 95 %. |
| Q6 | **Overrides the recommendation.** Disk includes network filesystems (`cifs`, `smb3`, `nfs`, `nfs4`) and `zfs` datasets, in their own `NETWORK` / `POOLS` sections, deduplicated by device id. Each network `statfs` runs with a 2 s timeout in its own goroutine so a hung share can never stall the poll; a share that times out is a `warn` row reading "not responding". A full share drives the dial like a local disk. |
| Q7 | The installer stops the legacy Python widgets before first run; the core recreates `config.json` if it vanishes. |
| Q8 | A new-alert toast replaces the generic transition toast in the same poll. |

## Repository layout

```
core/                     Go module (module path github.com/ShAInyXYZ/NoClickDock/core)
  cmd/ncd-core/           main: stdio mode (default), --dev, --mock, --once <widget>
  internal/model/         Snapshot, Block, Tone, Action
  internal/hub/           scheduler, change detection, transitions → toasts, action routing
  internal/transport/     stdio + http/sse
  internal/config/        config.json, legacy migration
  internal/sys/           exec with timeouts (hidden console on Windows), paths, fmt helpers
  internal/widgets/<name> one package per widget: collect(), snapshot(), actions(), mock()
app/                      Tauri 2 + SvelteKit (adapter-static), pnpm
  src/lib/tokens.css      Cerveau tokens (see DESIGN.md)
  src/lib/transport.ts
  src/lib/blocks/*.svelte one renderer per block kind
  src/lib/marks.ts        the monochrome brand marks (paths from the Python widgets)
  src/routes/dock  panel  toast  settings  gallery
  src-tauri/              Rust: sidecar, windows, geometry, tray, autostart, single-instance
scripts/                  build-core.sh, build-linux.sh, build-windows.sh, install.sh, install.ps1
docs/rewrite/             this contract, specs, research, reviews
```

`core/` currently holds the Python `shinywidget.py`; the Go module takes the
directory over when the Python tree is retired. Until then the Go module lives
at `core/` alongside it — Python files are ignored by `go build`.

## Windows

Windows is a first-class target, not a port afterwards:

- Every command the core runs goes through `internal/sys.Run`, which applies
  a timeout and, on Windows, `CREATE_NO_WINDOW` — no console flash, ever.
- The sidecar is linked with `-H=windowsgui`.
- Paths come from `os.UserConfigDir()` / `os.UserHomeDir()`: config is
  `%APPDATA%\NoClickDock\config.json` on Windows and
  `~/.config/noclickdock/config.json` on Linux.
- Per-widget platform code uses build tags (`disk_linux.go`,
  `disk_windows.go`). A widget that cannot measure something on a platform
  says so in its panel ("CPU package power needs a sensor driver on
  Windows") instead of showing zero.
- Power on Windows: GPUs through `nvidia-smi.exe`, drives and board as
  estimates, CPU power only if LibreHardwareMonitor's WMI namespace is
  present. It is no longer excluded.
- Windows geometry uses the monitor **work area** (taskbar excluded) and
  logical pixels; windows are `skipTaskbar`, undecorated, transparent,
  always-on-top, never focused on show.
- Autostart: `tauri-plugin-autostart` (Run key on Windows, `.desktop` on
  Linux). Single instance: `tauri-plugin-single-instance`.
- Build from Linux: Go cross-compiles natively; the Tauri exe is built with
  `cargo-xwin` (msvc) or the mingw `x86_64-pc-windows-gnu` target, whichever
  the research milestone proves. Output: a portable `ncd.exe` +
  `ncd-core.exe` and `install.ps1` that copies them to
  `%LOCALAPPDATA%\NoClickDock` and registers autostart.

## Linux

- X11 is the primary target (absolute window positioning, transparency with a
  compositor). On Wayland, windows cannot position themselves; the dock is
  placed by the compositor and panels open centred on it — documented, not
  hidden.
- Power keeps RAPL, `nvidia-smi`, `lm-sensors`, `lsusb`, and the optional
  `sudo -n gddr6-oneshot` VRAM junction temperatures.

## Safety rails for development

- `--dev` and `--mock` never write to `~/Documents/PM-Log`; the power CSV log
  goes to a scratch directory unless the core runs in production mode.
- Nothing in development touches `~/.config/status-widgets`,
  `~/.local/share/noclickdock` or the running Python widgets.
- The repository is public: fixtures and mocks use invented names only —
  no real hostnames, tailnet addresses, container names, GPU UUIDs or serials.
