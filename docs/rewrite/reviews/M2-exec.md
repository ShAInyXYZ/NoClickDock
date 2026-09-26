# M2 — design system · executor report

Branch `rewrite/tauri-go-svelte`. Commits, in order: `7383736` (core: mocks
and hub hooks), `aa0bbca` (block renderers, gallery), `d5ca493` (shell:
toasts, menus, tray, dock restore, fade, core-down), `f97655d` (Rust
placement tests), `cb51505` (core-down wording). The M1 fixes landed in
between (`dabc926` research r2, `6c7deed` design r2) and are described in
M1-exec.md; several touch the same files (panel placement, latency,
parking).

## What was built

### Core (`core/`)

- **Mocks for all seven widgets**, each following its spec's panel top to
  bottom, invented names only, most with a problem:
  - Docker: three pages (Now / Stopped / System) with an unhealthy service
    and an OOM kill (`crit`, glance `11`).
  - Power: `split` into a draw table with a hot VRAM cell, storage,
    displays and USB, beside a cost donut, today's 24 h bars and
    utilisation meters (`warn`, `GPU0 VRAM 92°C`).
  - Claude: limits, stats, model chips, a degraded component and an
    incident (`warn`).
  - Codex: two limit buckets (one that never moves), a degraded surface and
    an incident (`warn`).
  - ComfyUI: a job in progress, `busy` with `progress`.
  - Disk: pool at 88 %, a silent share.
  - Tailscale: a watched peer offline, a key-expired dormant peer,
    `serve` exposure (`warn`).
- **Variants** (`hub.Varianter`, `GET /v1/variants`): Docker "daemon
  unreachable", ComfyUI "offline", Tailscale "tailscaled unreachable",
  Claude "no sessions".
- **Still vs live mocks.** `--mock` draws every mock at one fixed instant, so
  gallery shots never change. `--mock-live` lets them move: the Docker
  service falls ill for 40 s of every minute, the ComfyUI job finishes each
  minute, and power breathes. This gives toasts to review on the real
  windows. `ncd --mock` runs the core live.
- **`hub.Transitioner`**: a widget may word its own state changes (ComfyUI
  "Generating…", "Generation complete").
- **Action results also toast** (ok / crit), for actions picked from a menu.
  The shell drops a toast for the widget whose panel is open.
- **Model changes:**
  - `empty` block;
  - table cells as `"text"` or `{text, tone}`;
  - `MenuItem.url` / `.copy`;
  - `dock.position` as `{monitor, edge, along, x, y}`, with edge and range
    checks.
- **Contract test** (`widgets_test.go`) run on every mock and variant, still
  and live:
  - glance ≤ 5 characters;
  - state label ≤ 28 characters;
  - alert meta ≤ 24 characters;
  - meter and limit ranges valid;
  - tab labels present on multi-page panels;
  - every action two-step.

### UI (`app/src`)

- **New renderers**, lifted from `reference.css`:
  - `Limit`: shares one grid, like meters.
  - `Chips`: a warn/crit item leaves the grid and becomes a full `Row` above
    it; a chip may carry a value.
  - `Table`: a left mono column is an identifier (48 px, so the tables of a
    page line up); a left prose column takes the slack and ellipsises;
    per-cell tone.
  - `Donut`: monochrome ramp, largest first, legend as rows.
  - `Bars`: 24 slots filling the column, crisp edges, past hours `--faint`,
    the hour in progress `--text`, future hours as a floor stub, axis
    `00 06 12 18 24`.
  - `Split`: two columns, 24 px gutter, one hairline.
  - `Empty`: one sentence; backticks become a mono chip.
- **Gallery:** 32 surfaces, adding the variant panels (`panel/docker/unavailable`,
  `panel/comfyui/offline`, `panel/tailscale/unavailable`, `panel/claude/empty`).
  The idle dial specimen now comes from a real idle variant.
- **Pages:**
  - Rail: right-click on a dial opens its menu; right-click on the rail body
    opens the rail menu.
  - Toast page: draws the shell's stack.
  - Panel page: fade on close; arrival animation on `panel-reveal`;
    tallest-page height above a horizontal rail.
  - Transport: carries the core's link state.
  - Store: `coreDown()` builds the rail's own dial while the core is not
    answering.

### Shell (`app/src-tauri`)

- **`toast.rs`** — the toast window is made once (GTK hint `Notification`).
  The shell owns the queue:
  - one widget's stack at a time, beside its dial on the panel side;
  - newest nearest the dial (column-reverse above a horizontal rail);
  - at most 3; 5 s each, then a 160 ms fade;
  - while any panel is open, toasts wait and the open widget's own are
    dropped;
  - click-through on Linux, only after the first show;
  - dismiss on hover on Windows (`toast_dismiss`).
- **`menu.rs`**:
  - Dial menu: Refresh now; the widget's items, looked up in its last
    snapshot and never taken from the page (a `url` opens with the opener,
    https/http only; a `copy` goes to the clipboard with the toast
    `Copied  <text>`; anything else is an action for the core); a
    separator; Settings…; Quit NoClickDock.
  - Rail menu: Orientation, Density, Readouts (check items), then
    Settings…, Quit.
  - Tray: Show/Hide dock, Settings…, Quit.
  - Settings opens a stub window on demand and is destroyed on close.
  - On Windows, the menu gives the foreground back after it closes
    (`win::popup_menu_keep_foreground`).
- **`dock.rs`** — position:
  - Saved as monitor, edge and place along the edge (x/y as fallback).
  - Restored from that: the named monitor, else the one under x/y, else the
    primary.
  - A resize keeps the rail on its edge.
  - GDK monitor, work-area and scale notifications (coalesced 150 ms), and
    `ScaleFactorChanged`, put the rail back.
  - The shell's own moves are not mistaken for drags.
  - Hide/show dock from the tray.
- **`hover.rs`** — the M1 design r2 work lives here too:
  - a 100 ms close fade before the window hides;
  - prepared during the dwell;
  - header anchoring on resize;
  - 400 ms of hover quiet after a menu pops.
- **`core.rs`**:
  - link state (`core-link` event, `core_link` command);
  - a watchdog: no hello within 8 s of starting is reported;
  - the core's last stderr line is kept, so the reason it stopped reaches
    the core-down panel;
  - toast relay into the queue.
- **`overlay.rs`**: on Linux the overlays are mapped once and then parked
  (opacity 0, click-through) instead of unmapped; see the M1 design r2
  latency note.
- Plugins `tauri-plugin-opener` and `tauri-plugin-clipboard-manager`, used
  from Rust only; the capabilities grant the pages events only.

## How it was verified

```bash
cd core && gofmt -l . && go vet ./... && GOOS=windows go vet ./... && go test -race -count=1 ./...   # 7 packages ok
cd app && pnpm check                                                                              # 0 errors, 0 warnings
cd app/src-tauri && cargo test --lib                                                              # 6 placement tests ok
PATH=<llvm-rc shim>:$PATH cargo check --target x86_64-pc-windows-msvc                           # ok, no warnings
cargo tauri build --no-bundle                                                                     # ok
ncd-core --mock --port 47810 & pnpm dev   # then:
node docs/rewrite/design/shoot.mjs http://127.0.0.1:1420 "$SCRATCH/M2-final" --audit
# 32 surfaces, 0 findings (clip-in-flex included), 0 console errors, both fonts loaded
```

**Real desktop (X11 :0)**, before X11 was handed to design. Every pointer
move targeted windows resolved from our own pid, and the pointer was
restored after each run.

- Toasts: live-mock toasts ("1 crashed today", "Generation complete",
  "Generating…") appeared beside their dials on the panel side, 6 CSS px
  from the rail, then faded.
- Right-click on the Docker dial: native menu with Refresh now, Open
  Portainer, separator, Settings…, Quit NoClickDock. The panel no longer
  reopens under it.
- `_NET_ACTIVE_WINDOW` was unchanged throughout.
- The saved position read back from the instance's config:
  `{"monitor":"DP-2","edge":"right","along":0.5,…}`. The next launch
  restored it.

**Private Xvfb display (:97)**, which holds only our windows:

- A stand-in `ncd-core` that exits 3: the rail still stands, with a
  NoClickDock dial reading `off`. Its panel says "Nothing here can be
  measured: ncd-core stopped with exit code 3 (cannot read its config:
  permission denied). NoClickDock keeps trying to start it."
- A horizontal rail at the bottom edge (seeded through the scratch config):
  the Docker panel opens above it and keeps top 437 / height 567 through
  Now → Stopped → System → Now, sitting 6 CSS px above the rail. The index
  tick faces up.

## Contract notes for main

1. New block kind `empty {text}` (backticks mark a command). Needs its line
   in the ARCHITECTURE table.
2. `GET /v1/variants` (dev API only) and `--mock-live`.
3. Action results are also broadcast as toasts (tone ok/crit); the shell
   drops them for the open widget.
4. The toast policy shows **one widget's stack at a time**. A second
   widget's toasts wait until the first stack has faded, which is the
   reading of "beside the dial that raised them" for a single toast
   window.
5. Codex mock: a broken component is listed in `Degraded` and left out of
   the Services chips (otherwise the chips renderer would pull it out a
   second time). This is a writer rule for M4.
6. Widget id `ncd` is reserved for the shell's own core-down dial.

## Known gaps

- **Tray:** built and error-free, but not observed at runtime on the real
  desktop (research proved the AppIndicator registration). Note: an
  instance started on Xvfb still registers its tray icon with the session's
  StatusNotifier watcher, so it briefly appears in the real top bar while
  it runs.
- **Windows:** runtime behaviour (toasts dismiss on hover, menu foreground,
  ScaleFactorChanged re-placement) is compiled only. That is M7.
- **Settings** is a stub window (M6), and so is legacy density `s/m/l`
  migration.
- **Latency:** the open-latency figure (~140–150 ms) comes from shell
  traces plus one compositor frame. With parked overlays, map-state polling
  no longer shows visibility; a frame-accurate capture would need a
  wall-clock-aligned recorder.
- **The core does not yet reject duplicate action ids within a block.** The
  UI no longer breaks on them.
