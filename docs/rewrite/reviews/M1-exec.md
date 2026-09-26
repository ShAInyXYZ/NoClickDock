# M1 — skeleton · executor report

Branch `rewrite/tauri-go-svelte`. Built against ARCHITECTURE.md (with ec01f71's
decisions), DESIGN.md (7542561, the 17 amendments), `research/tauri-windows.md`,
`specs/dock-core.md` and `specs/disk.md`, and the reference prototype
(`design/reference.css` lifted into the components).

## What was built

### Go core — `core/` (module `github.com/ShAInyXYZ/NoClickDock/core`, go 1.23, stdlib only)

| package | what |
|---|---|
| `cmd/ncd-core` | `ncd-core` (stdio, production) · `--dev` (real data, HTTP+SSE) · `--mock` (invented data, HTTP+SSE) · `--mock --stdio` (invented data to the shell) · `--once <widget> [--mock]` · `--port` (default 47810) · `--config DIR`. `--dev`/`--mock` use a scratch config dir per port (`$TMPDIR/ncd-core-<port>`). Exits on stdin EOF (death pact). |
| `internal/model` | Snapshot, Page, Glance, MenuItem, Action, Issue; all 12 block kinds of the ARCHITECTURE table (`Blocks` marshals each with its `kind`, empty as `[]`); every wire message. |
| `internal/hub` | One goroutine per collector; period measured from the start of each collect, 1 s minimum gap; refresh coalesced (a refresh during a collect runs once after it). A snapshot when content changed, else `{"type":"tick","widget","at"}`. `recover()` around collects; a collect still busy after 2 × poll emits the last snapshot with `error: "still collecting…"`; a failed collect keeps the last good content and `updatedAt`, sets `error`, state `idle`, and does no transition diffing (dock-core §5.3). Transitions per §5.4/Q8: nothing before the first good collect; new issues toast (≤ 3, most severe first) and replace the generic state-change toast. Actions run only if the `(action, args)` pair is in the snapshot last shown (row/alert buttons, split columns, or a menu item without url/copy) — anything else is `result ok:false "unknown action"` before the widget sees it. Slow transports are dropped and resubscribe from the full state. |
| `internal/transport` | stdio JSON lines (1 MB line cap; hello + state first; restarts from full state if it ever falls behind); dev HTTP on 127.0.0.1 only: `GET /v1/state`, `GET /v1/events` (SSE, event = type, 15 s keep-alive), `POST /v1/send` (JSON only, 1 MB). Guards: Host must be `127.0.0.1:<port>`/`localhost:<port>` (DNS rebinding); `Origin`, when sent, must be one of `http://{localhost,127.0.0.1}:{1420,1430,1440}`. |
| `internal/config` | `config.json` in `os.UserConfigDir()/noclickdock` (`%APPDATA%\NoClickDock` on Windows); RFC 7396 merge patch, validated, unknown keys rejected, atomic temp+rename (0600); nothing written until the first change; a corrupt file is kept as `config.json.bad`. Shape so far: `{"dock":{"orientation","density","readouts","position":{"x","y"}}}`. |
| `internal/sys` | `Run(ctx, timeout, name, args…)` (argv only, `WaitDelay`, stderr in the error, `CREATE_NO_WINDOW` via `exec_windows.go`), `Start` (detached: `Setsid` / `DETACHED_PROCESS`), `Has`; `FmtBytes`, `FmtCount`, `TimeAgo`, `RatioTone`, `Clamp01` with expectations taken from the Python output. |
| `internal/widgets/disk` | **Real.** Linux `/proc/mounts` (whole-path-component skips, `\040` decoded, dedupe by `st_dev`, `shutil.disk_usage` arithmetic); **Q6**: `cifs/smb3/nfs/nfs4` in a NETWORK section and `zfs` in POOLS, each network statfs in its own goroutine with a 2 s deadline and never re-asked while still stuck — a silent share is a warn row "not responding", an issue, and at least `warn` state. Windows: drive letters, fixed+remote, label `C: System`, per-drive 3 s probe (`disk_windows.go`, syscall only). Docker reclaimable: decimal sizes (spec BUG fixed), queried in the background every 5 min, the widget asks the hub for a refresh when the answer lands; Windows says "Docker Desktop's disk image" instead of a VM path. State/label/glance/issues exactly as specs/disk.md; `open-analyzer` menu item when `baobab` is on PATH. Mock: invented mounts, one pool at 88 %, one silent share. |
| `internal/widgets/{claude,codex,comfyui,docker,power,tailscale}` | Meta + `Mock()` only (their collectors are M3–M5), so `--mock` shows all seven. Invented names throughout; comfyui's progress advances with the clock. |

### Svelte app — `app/` (SvelteKit 2 + adapter-static, Svelte 5 runes, TS, Vite 8, pnpm)

- `src/lib/tokens.css`: Cerveau's `tokens.css` byte-for-byte, then one NCD block
  (`--dial --rim --led --mark --slot-gap --readout-h --ok-led --recess-shade
  --recess-open --inset --row-h --panel-w --r-toast`). `ncd.css`: tone → token
  mapping, 2px/offset-2 focus, density geometry, overlay transparency, and the
  `tabular-nums` invariant last.
- Fonts self-hosted: Manrope (woff2 + OFL from HubVoice) in `static/fonts`;
  IBM Plex Mono 400/500/600 latin from `@fontsource/ibm-plex-mono` (OFL copied to
  `static/fonts/IBMPlexMono-LICENSE.txt`).
- `transport.ts`: `TauriTransport` (listen `core` → hold → `invoke('core_state')`
  replay → release held, so nothing is lost or regressed) and `HttpTransport`
  (EventSource `/v1/events`, `fetch /v1/send`; base from `?core=`, then
  `VITE_NCD_CORE`, then `http://127.0.0.1:47810`). `core.svelte.ts`: runes store
  (snapshots, when each widget was last heard, config, toasts, a 1 s clock,
  `isStale()` = 3 × pollSecs without a snapshot or tick — DESIGN A11).
- Components named and styled after `reference.css`: `kit/{Led,Seg,Segmented,PillButton}`,
  `dock/{Rail,Dial,Readout}`, `panel/{Panel,PanelHeader,PanelFooter,Blocks}`,
  `blocks/{Section,Row,Meter,Note,Stats,Alert}`, `toast/{Toast,ToastStack}`.
  Dial: rim, recess, monochrome mark with per-mark optical scale, 45° lamp with
  gasket, breathing crit ring, progress arc from 12 o'clock / 1.2 s spinner,
  open index on the rail wall, `--text` focus ring, idle and stale states.
  Meters are SVG `crispEdges`, neutral unless warn/crit, and consecutive meters
  share one grid. Pills are ghosts at rest and never change width when armed
  (arm/busy/failed state keyed by `widget/action/args`, 4 s).
- `marks.ts`: the seven glyph paths from `assets/logos/*.svg` (ComfyUI cropped
  to its 26-unit box).
- Routes (all prerendered): `dock`, `panel`, `toast` (draws the stack; its
  window is M2), `settings` (stub), `gallery`. The gallery follows
  `design/README.md`: `data-surface` on 28 surfaces (5 docks, the state strip +
  10 dial specimens, the toast stack, every page of every panel, the disk
  collector-error panel and a stale panel), `data-gallery-ready`, `?surface=`.

### Tauri shell — `app/src-tauri/` (tauri 2.11.5, Cargo.lock from HubVoice)

- `core.rs`: sidecar via `tauri-plugin-shell` from Rust only (no `shell:*`
  capability), supervised with 250 ms → 10 s backoff, killed on `RunEvent::Exit`;
  stdout lines relayed as the `core` event to every window; a cache of hello,
  config, snapshot and tick per widget for `core_state`; `core_send` parses,
  allows only `action | refresh | config.patch`, re-serialises one line.
- `overlay.rs`: rail and panel built in Rust per research (transparent,
  undecorated, no shadow, always on top, skip taskbar, sticky, **focusable(false)**,
  GTK `Utility` hint before first map); `fit()` for every resize;
  `reveal()` = position → show (+60 ms re-position on Linux); UI scale from
  `gtk-xft-dpi/1024/96`, confirmed against the rail page's `innerWidth`
  once settled; `win.rs` (research's `harden/show/hide`) wired behind `cfg(windows)`.
- `dock.rs`: rail sized to the page's measurement (+1px ring margin), first run
  flush right and centred on the primary work area, saved positions restored and
  clamped to the monitor's work area, drag via `startDragging` (rail-only
  capability), settle after 400 ms without `Moved`: snap to a work-area edge
  within 24 CSS px, then `config.patch` the position to the core.
- `hover.rs`: the DESIGN choreography in one state machine fed by both pages —
  120 ms dwell, immediate switch while open, 280 ms grace across dial and panel,
  stale timers cancelled by a generation counter, stale measurements by a
  sequence number. Panel on the side with more room, 8 CSS px off the rail,
  centred on the dial, clamped along the rail (never across it); max height =
  work area − 32; open emits `panel-open` (the page measures and answers),
  the rail gets `panel-state` for its index tick; the panel page fades/slides in.
- `page_error` command: uncaught page errors and rejected invokes go to the
  shell's stderr (a production webview has no console).
- CSP from the research doc. `ncd --mock [--instance NAME]` / `NCD_INSTANCE`:
  runs the real windows on `ncd-core --mock --stdio --config $TMPDIR/ncd-<instance>-mock`.
- `scripts/build-core.sh [triple]` builds `binaries/ncd-core-<triple>[.exe]`
  (`-trimpath -buildvcs=false`, `-H=windowsgui` for Windows); it is the
  `beforeBuildCommand`/`beforeDevCommand` step, so `cargo tauri build` is self-contained.

## How it was verified

```bash
cd core && gofmt -l . && go vet ./... && GOOS=windows go vet ./... && go test -race -count=1 ./...
# all packages ok (config, hub, model, sys, transport, widgets/disk)
GOOS=windows go build -ldflags -H=windowsgui ./cmd/ncd-core   # ok
go run ./cmd/ncd-core --once disk          # real snapshot of this machine (not committed)

cd app && pnpm check                       # 0 errors, 0 warnings
cargo tauri build --no-bundle              # target/release/ncd (runs build-core.sh + pnpm build)
cd src-tauri && PATH=<llvm-rc shim>:$PATH cargo check --target x86_64-pc-windows-msvc   # ok

# gallery, against invented data
ncd-core --mock --port 47810 &  pnpm dev   # :1420
node docs/rewrite/design/shoot.mjs http://127.0.0.1:1420 "$SCRATCH/M1-final" --audit --scales 1,2
# 28 surfaces, 0 findings, 0 console errors, Manrope + IBM Plex Mono loaded
```

Hub tests cover: tick vs snapshot, meta normalisation, the §5.4 toast rule,
≤ 3 issue toasts by severity, failure keeps panel/time and toasts nothing,
panic recovery, "still collecting…", replay order, action allow-listing
(foreign container, extra arg, unknown action, url-only menu item refused),
config broadcast, bad requests, slow subscriber drop. Transport tests cover
state, SSE start, CORS/Host guards, JSON-only POST, stdio EOF exit. Disk tests
cover the mounts fixture (tmpfs, snap, `/snapshots`, `\040`, zfs, nfs4, cifs),
decimal sizes, the 0.849/0.85/0.919/0.92 table, a full share, a silent share,
sections, issues, the docker section, and the probe never piling up.

**On X11 (DISPLAY=:0, `ncd --mock`)**: the rail appeared at 81×717 physical
(54×478 CSS at 1.5) flush right on the primary monitor's work area; hovering
the Docker dial with `xdotool` opened its panel on the left, centred on the dial,
with the index tick; moving to Power switched immediately; leaving unmapped the
panel. `_NET_ACTIVE_WINDOW` was identical before, during and after. The position
was saved to the scratch config only; killing `ncd` took `ncd-core` with it.
The pointer was restored. Screenshots stay in the scratchpad.

## Contract notes for main

1. `meter` gained `mono?` (label is an identifier — mount paths), as the
   reference draws `/mnt/archive`. Needs a line in the ARCHITECTURE table.
2. `MenuItem` carries `url?`/`copy?` in the model and TS types; the shell's
   handling (and the context menu itself) is M2.
3. Density values are `compact | normal | large` (the reference and the gallery
   ids use `normal`; dock-core §2 says `default`). One word should win.
4. `dock.position` is `{x, y}` physical only; dock-core §2's
   `{monitor, edge, along}` restore is M2 with the snapping work.
5. Disk's Docker section is `section + row ("63G could be freed" · root) + stats
   (per kind, warn > 10 GB) + prune note`, following the reference's Disk plate,
   rather than the spec's `chips` (chips are an M2 renderer, and the reference
   drew a row). Toast tones for disk issues are warn/crit by threshold (spec N).
6. The design README runs the core with `--addr 127.0.0.1:47811`; the flag is
   `--port 47811`.

## Known gaps (by milestone)

- **M2**: toast window (the route exists), right-click menu (`url`/`copy`
  items), tray, renderers for `limit chips table donut bars split` (they draw
  "`<kind>` · not drawn yet"), close fade (100 ms), monitor/work-area change
  observers, rich mocks with problem states for every widget, rail
  orientation/density from a menu, `{monitor, edge, along}` persistence,
  "the panel must not close while a button is armed".
- **M3**: Refresh forcing slow sub-sources only when > 10 s old (docker df is
  simply on its own 5 min cadence now); snapshot `still collecting…` threshold
  is 2 × poll for every widget.
- **M6**: single instance (`--instance` already names the mock config dir),
  settings window, config `version`/`widgets`/`start` sections, legacy migration.
- **M7**: the Windows exe. `cargo check --target x86_64-pc-windows-msvc` passes
  but needs `llvm-rc` on PATH (`/usr/bin/llvm-rc-14` exists only under that
  name here); `win.rs` is compiled, not run.
- The rail is revealed only once it has at least one dial: a core that never
  starts leaves no rail on screen (its errors are on stderr). Worth a
  "core not running" rail state in M2.
- WebKit runs no rendering updates in a never-shown window, so the rail page
  measures after every update instead of relying on `ResizeObserver` alone —
  keep that in mind for the toast window.
