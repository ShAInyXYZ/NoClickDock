# M2 — research review, round 1

Reviewed: `7383736`, `aa0bbca`, `d5ca493`, `6c7deed`, `f97655d`, `cb51505`,
`d184041`, the contract in `5042e69`, and — at main's request —
`25a7717` (M2-a) and `fde7211` (main-thread fix). Everything was tested on
clean `git archive` trees in my scratchpad, on a **private Xvfb `:98`** with
`--instance research-m2` and a dead session-bus address (so no tray reached
the real session). Focus as briefed: the shell (toasts, menus, tray,
position restore, core-down, parked overlays), the hub hooks and mocks, and
the Windows compile paths.

## What I ran

| check | result |
|---|---|
| `gofmt` · `go vet` (linux + `GOOS=windows`) · `go test -race ./...` at `fde7211` | clean, 7 packages ok |
| `svelte-check` at `fde7211` | 0 errors, 0 warnings |
| `cargo test --lib` (shell) | 7 passed |
| `cargo check --target x86_64-pc-windows-msvc` at `fde7211` | ok, no warnings |
| mocks, `--once <w> --mock` for all seven | layouts checked against each spec below; budgets enforced by `widgets_test.go` |
| real tailnet suffix, peer names, IPs, hostname in the rewrite tree | none |
| toast flood (stand-in core: captured mock state, then 200 toasts/s for one widget) at `5042e69` | **0 of 9 runs survived**: xcb abort (`Unknown request in queue while dequeuing … XInitThreads has not been called`), silent exit 1, or the shell stopped reading the core's stdout (stand-in blocked in `anon_pipe_write`, main thread in `futex_do_wait` for > 3 s) |
| same flood, `5042e69` with only `toast::push` marshalled to the main thread | 6/6 survived |
| same flood at `fde7211` | **8/8 survived** (~788 toasts each) |
| `fde7211` debug build (main-thread asserts on) under a flood while hovering five dials, entering a panel, right-click → menu → Escape, and dragging the rail | alive, no assert, flood completed |
| startup with `libayatana-appindicator3` hidden (user-namespace bind mount) | **aborts, exit 134**: `Failed to load ayatana-appindicator3 or appindicator3 dynamic library` |
| CPU/RSS on Xvfb, `--mock` live, a panel parked | parked panel's WebKit process ≈ 0 CPU (3 ticks / 10 s); rail's ≈ 5.6 % of a core (crit ring + busy arc, software rendering); each WebKit process ≈ 190 MB RSS — parking costs nothing extra over hidden windows |

## The off-main-thread finding (fixed in `fde7211`)

Root cause at `5042e69`: `core.rs` `relay` (sidecar reader, a tokio worker)
called `toast::push` → `pump` → `render` → `hover::Geometry::of` →
`dock::work_area` → Tauri `monitor_from_point`/`work_area`. Tauri converts
the GDK monitor on the **calling** thread (`tauri-runtime-wry-2.11.4/src/lib.rs:2082-2085`),
and `gdk_monitor_get_workarea` reads `_GTK_WORKAREAS_Dn` with
`XGetWindowProperty` — Xlib off the main thread. The same path held the
Toasts mutex while waiting for main-thread window getters, while the main
thread's 100 ms toast tick waited for that mutex: a deadlock that stops the
shell reading the core. Design's crash (`Gdk-ERROR BadImplementation on
X_GetProperty`) matches this path.

`fde7211` verified: `toast::push` hops to the main thread; `relay` only
parses, caches and emits; every other `async_runtime::spawn` in the shell
reaches geometry through `main_thread::run`; no command is `async` (sync
commands run on the main thread); the menu, window and tray handlers run on
the event loop; the clipboard plugin uses its own X connection. I found
nothing else that touches GDK/Xlib off the main thread. The debug asserts
are the right regression guard.

## MUST-FIX

1. **No appindicator library → the app aborts at startup**
   (`app/src-tauri/src/menu.rs:107-122`, called at `lib.rs:108`).
   `libappindicator-sys-0.9.0/src/lib.rs:41` panics when neither
   `libayatana-appindicator3.so.1` nor `libappindicator3.so.1` loads, and the
   release profile is `panic = "abort"`, so the `if let Err(error)` around
   `menu::tray` never runs. Reproduced: exit 134 at startup. The library is
   not installed by default on Fedora/Arch/openSUSE GNOME. The code comment and
   DESIGN.md promise "no tray host → the rail's menu has everything".
   Correct: probe before building the tray — e.g. try
   `libloading::Library::new` for `libayatana-appindicator3.so.1`, then
   `libappindicator3.so.1` (libloading is already a dependency of
   libappindicator-sys) — and skip the tray with the existing message when
   neither loads. Add the library as a *recommended* dependency in the M8
   installer notes.

## SHOULD-FIX

1. **A parked window keeps a 1×1 input pixel.** `overlay.rs:145`
   (and the toast at `toast.rs:157`) use `set_ignore_cursor_events(true)`,
   which in tao is a **1×1 input region at (0,0)**, not an empty one
   (`tao-0.35.3/src/platform_impl/linux/event_loop.rs:454`). A parked panel
   stays mapped and always on top at its last position, so the pixel at its
   top-left corner can swallow a click meant for the application below (on
   Xvfb the pointer at that exact pixel reported a different window than one
   pixel further in). Set an empty input shape directly on the GdkWindow when
   parking (`input_shape_combine_region(&cairo::Region::create(), 0, 0)`), or
   move the parked window so the pixel lies on the rail.
2. **Toast starvation and unbounded backlog** (`toast.rs:163-178`, `92`).
   A widget whose toasts keep arriving within 5 s of each other keeps the
   stack forever, so another widget's `crit` toast waits indefinitely. While a
   panel is open, `pending` grows without bound and old items come out after it
   closes. Suggested rule: once another widget is waiting, the current
   widget's new toasts queue behind it. Keep at most 3 pending per widget
   (newest), and drop pending toasts older than ~30 s when they would be
   shown.
3. **`placing` can drift** (`dock.rs:209-211`, `284`). Every programmatic
   placement increments the counter, but a `set_position` to the position the
   rail already has emits no `Moved`. Monitor/work-area bursts (`replace`) and
   restores therefore leave it high, and the next *user* drag's first
   `Moved` events are taken for the shell's own. A short drag can then be
   neither snapped nor saved. Count only moves that change the position, or
   make "ours" time-bounded (ignore `Moved` for ~300 ms after our own
   placement).
4. **GNOME X11 per-monitor work areas may not notify** (`dock.rs:372`). Mutter
   publishes them in `_GTK_WORKAREAS_Dn` without always emitting
   `GdkMonitor::notify::workarea` (the sibling app's `desktop.rs` watches root
   `PropertyNotify` with x11rb for exactly this). A top-bar or dock change
   may not re-place the rail. Port the sibling app's `observe_x11_workareas`.
5. **Windows has no work-area/display observer** (`dock.rs:418`): only
   `ScaleFactorChanged`. A taskbar move or monitor unplug leaves the rail
   where it was. Subclass for `WM_DISPLAYCHANGE` and
   `WM_SETTINGCHANGE(SPI_SETWORKAREA)` (M7).
6. **URL check is a prefix test** (`menu.rs:169`). It is safe as far as it
   goes (argv or ShellExecute, no shell, no `file:`). Parse it with the
   `url` crate instead, and reject control characters and whitespace. When a
   `url` item is refused, log it rather than doing nothing. Note for M5: the
   power report is a local file, so `open-report` must stay a core action
   (the core regenerates the report, then opens it itself or answers with a
   path the shell opens through a dedicated command). It must not become a
   `file://` menu URL.
7. **Toasts while the dock is hidden** (`dock.rs:232` / `toast.rs:76`).
   A toast still appears beside the parked, invisible rail after "Hide dock".
   Either drop toasts while hidden, or decide that they are wanted and say so
   in DESIGN.md.
8. **Mock details vs specs** (the gallery is the design reference):
   - Power `GPU0` core `71°C` is untoned, but the power spec tones core
     temperatures > 70 as `warn`.
   - The display estimates (`~32W` for 27", `~18W` for 24") do not follow the
     spec's `(in·1.1 + 2)·1.5` (≈ 48 W, 43 W).
   - The Codex idle bucket shows `resets —`: omit `resets` when unknown.

## Checked and fine

- **Toast rules (M2-a/b, A14)**:
  - one widget's stack at a time, newest nearest, ≤ 3, 5 s + 160 ms;
  - a panel open → toasts wait, and the open widget's toasts are dropped (success and failure alike; failures show inline via `result`, `25a7717`);
  - click-through only after the first show (the tao abort from the research doc is respected);
  - Windows dismiss-on-hover.
- **Menus**: dial items resolved from the last cached snapshot on choice, never from the page; menu ids namespaced; `url` only `http(s)`; `copy` through the clipboard plugin with the `Copied  <text>` toast; other items become core actions that the hub validates against the shown snapshot; the rail menu writes config through `config.patch`; no `shell:*`, opener or clipboard permission granted to any page.
- **Core-down**: link state from spawn failure, exit (with the core's last stderr line) and an 8 s hello watchdog. It is set up again on every hello.
- **Position restore**: named monitor → monitor under the saved corner → primary; edge + `along` survives resolution and scale changes; unit tests cover `on_edge`/`clamp`.
- **Hub hooks**: `Transitioner`, `Varianter`, and action results that also toast. `/v1/variants` sits behind the same Host/Origin guards.
- **Mocks**: all seven follow their spec's panel order, the Q1–Q8 and M2-d decisions, and the budgets (glance ≤ 5, state label ≤ 28, alert meta ≤ 24, every action two-step). The names are invented.
- **Windows compile paths**: `win.rs` subclass with the `WM_STYLECHANGING` guard, `popup_menu_keep_foreground`, and `win::show`/`hide` for re-shows; msvc check ok.

## Process note

I first reused the instance name `review`, which design's review instance
also uses. All my runs had `DISPLAY=:98` exported, but the collision was my
error. Every later run used `--instance research-m2` on `:98`, and the scratch
config it wrote was removed.

VERDICT: FAIL
