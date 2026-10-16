# M2 — design review, round 1, part B (real windows)

Reviewer: design supervisor, 2026-09-26. Part A (browser) is
M2-design-r1.md.

**Build:** a clean `git archive` of `82c8f12`.

**Desktop:** real X11 `DISPLAY=:0`, GNOME 42.9 / Mutter, two 3840×2160
monitors at 60 Hz, text scale 1.5. Only my instances ran: `--instance
design`, plus `--instance down` for core-down.

**Evidence:** captures of the real desktop stay in
`…/scratchpad/design/m2b/x11/` and are **not committed**.

**Cleanup:** only my PIDs were killed, and pointer and focus were restored
after every step.

## What passes

- **Toast placement.** Beside the dial that raised it, on the panel side
  (left of a right-edge rail), vertically centred on the dial (ComfyUI dial
  centre 3168.5, toast centre 3168.5), 6 CSS px from the rail. Above a
  horizontal rail, toasts sit above it.
- **Toast waits while a panel is open.** The :00 toast held behind an open
  panel appeared about 0.8 s after the close, in 3 of 3 runs. Toasts that
  arrive while a native menu is open also wait, then live their full 5 s.
- **Toast click-through.** With a visible toast over my Tk probe, X reports
  the probe under the pointer, and the probe received `Enter`, `Motion`,
  `Button-1` and `ButtonRelease-1`.
- **Parked panels after use.** Both panel windows, parked at their last
  geometry, pass the pointer and clicks to the application beneath.
- **Tray menu.** Hide dock · Settings… · Quit NoClickDock, with a separator.
- **Rail menu.**
  - Its structure is right: Orientation ▸ (Vertical / Horizontal radio),
    Density ▸, Readouts (check), Settings…, Quit.
  - Horizontal, Compact and Large each applied, persisted to the config,
    and rendered correctly on screen. Rings and lamps scale; the panel opens
    above a horizontal rail with its index tick above the dial and clamped
    at the screen edge.
  - Toggling Readouts works.
- **Position restore.** A drag by the grip to the upper monitor snapped to
  its right edge and saved `{monitor: "DP-0", edge: "right", along: 0.575}`.
  After a restart the rail came back at 3759,884 on DP-0.
- **Core-down rail.** A copy of the app next to a stand-in `ncd-core` that
  exits 3: the rail stands, with one NoClickDock dial reading `off`, and its
  panel opens on hover. The shell restarts the core with backoff (7 exits
  logged).
- **Focus.** Focus stayed on the user's application throughout, apart from
  the moments I activated my own probe. Right-click and tray menus return
  focus when dismissed.

## MUST-FIX

1. **Never-shown panel windows swallow clicks in the top-left corner of the
   screen.**
   - Both panel windows are mapped "parked" at start at **0,0, 200×200**
     (top-left of the upper monitor) and stay there until they are first
     used: `panel` until the first open, `panel2` until the first
     size-changing switch.
   - In that state they **take input**. X reports `NoClickDock panel2` under
     the pointer at 40,90, 150,150 and 190,195, and my raised Tk probe
     beneath got **no** events there. At 260,260, just outside, it got
     them all.
   - The user's own window at that corner (the ChatGPT app's sidebar
     toggle and menu bar) is dead to clicks from every launch until two
     different panels have been opened.
   - Parked windows **after** use pass the same test, so the empty input
     shape is applied on park but not at the initial map.
   - **Fix:** apply the empty input shape (and opacity 0) before the first
     map. Don't park at 0,0: park initial windows under the rail, or in a
     1×1 geometry, until their first placement.
   - **Acceptance:** the same probe test at the initial parking rectangle
     receives every event.

2. **Right-click does not shut the panel.**
   - The contract (and dock-core §4) says right-click opens the menu and the
     panel stays shut under it.
   - Measured:
     - Panel already open: right-click on Codex leaves its panel open under
       the menu.
     - Mid-dwell (right-click 50 ms after arriving): the Docker panel
       **opened under the menu** and was still open 2 s later. The pending
       dwell fired even though a menu had popped. On Codex the same timing
       stayed shut, so it's a race.
   - **Fix:**
     - A right-click on a dial cancels any pending dwell (bump the hover
       generation) and closes an open panel before the menu pops.
     - The 400 ms hover quiet then applies from that moment.
   - **Acceptance:** right-click at 0, 50 ms and 600 ms after arrival → the
     panel is shut under the menu and stays shut after Escape until the
     pointer re-enters a dial.

3. **Motion as specified does not render on the real windows.**
   - Animations are enabled on this desktop (`enable-animations true`), so
     WebKit reports no reduced motion.
   - **Panel open and close:** instant in both directions. The title
     brightness goes 30 → 252 and back in **one frame**, with no 140 ms
     fade-in or 3 px slide and no 100 ms fade-out. This was measured from
     GNOME's composited recording over three open/close cycles.
   - **Toast:** dims only to about 75 % in about 50 ms, holds there about
     90 ms, then **pops** off in one frame, instead of a 160 ms fade.
   - The CSS animations run inside WebKit, whose frames reach the screen
     late or not at all around a park or unpark.
   - **Fix, preferred:** animate **window opacity from the shell**
     (`gtk_widget_set_opacity` on the toplevel, stepped at the frame clock
     over 140, 100 and 160 ms). Mutter composites that smoothly, and it is
     the mechanism parking already uses. The 3 px slide can go: an
     opacity-only open is calm and honest.
   - **Alternative** (main decides): amend DESIGN.md to instant
     open/close/dismiss on Linux. Either way, the toast's partial fade and
     pop must go.

4. **Switching orientation back leaves the rail off its edge.**
   - Vertical → Horizontal kept the rail against the right edge. Back to
     Vertical, the rail landed at **x 3288, about 470 px into the screen**,
     over application content.
   - The saved position still said `{edge: "right", x: 3759}`, so the next
     launch would disagree with what is on screen.
   - **Fix:** a change of orientation or density re-places the rail from
     its saved `{monitor, edge, along}` (as a resize already does), never
     from the previous rectangle's corner.

5. **The core-down panel names the wrong reason.**
   - With a core that **exits** (code 3, stderr "cannot read its config:
     permission denied", restarted 7 times), the panel says
     "ncd-core **started but has not answered**", the watchdog's wording for
     a hung core. The header says `core not running`.
   - The one job of this panel is to say why. The executor's Xvfb run showed
     the right text, but on the real desktop the watchdog overrides the exit
     reason.
   - **Fix:**
     - While the core keeps exiting, show the last exit: "ncd-core stopped
       with exit code 3 (cannot read its config: permission denied)".
     - Use the watchdog wording only when the process is alive and silent.
     - Part A's rule still applies: the error is not repeated in a red
       footer; the footer says `retrying · since 05:07:12`.

6. **The tray icon is nearly invisible.**
   - It is the app's default window icon, a dark square with a dim pill,
     shown on GNOME's dark top bar beside white symbolic icons.
   - When the dock is hidden from the tray, the tray is the only way back,
     so it has to be findable.
   - **Fix:** a symbolic tray icon: the NoClickDock mark as a light
     monochrome glyph on transparent (`-symbolic` style, `--text` ink),
     22 px, the same visual weight as its neighbours.

## Open risk — the dock froze after about 27 minutes (not reproduced)

My first instance (started 04:28) stopped working at about 04:55, and
stayed like that until I killed it at about 05:03.

**What was broken:**
- **Timers stopped:** a toast stayed on screen for minutes instead of 5 s,
  and a hover reached the shell (`hover_dial Some("docker")`,
  `panel_layout`, `panel painted`) but `dwell done` never came, so no panel
  opened.
- **The rail went stale:** all lamps idle, readouts faint.
- **The core was not the problem:** it was alive and streaming (12 KB per
  5 s on stdout) and reading from stdin.
- **Rail-menu changes never reached the core:** its config stayed
  `vertical`.

**Thread states** (`/proc/*/wchan`):
- core: idle;
- shell: 32 tokio workers in `futex_do_wait`, two in `anon_pipe_read`, the
  main thread polling.
- `ptrace` of a non-child is not permitted here, so no stack dump.

**Circumstances:** it began around the moment a toast arrived while I had
a native menu open, after about 27 minutes of heavy use (recordings,
switches, sweeps, right-click, tray and rail menus).

**Replays that did not freeze:**
- a fresh instance under gdb with the same sequence (dial menus, tray menu,
  rail-menu keyboard change);
- a toast arriving under an open rail menu.

**Why it matters:** a status dock that silently stops updating while still
on screen is the worst failure it can have.

**Asks:**
- a soak of 30+ minutes with live mocks, menus and toasts under gdb, or
  with a periodic `NCD_TRACE` heartbeat from the tokio runtime, to catch it
  in the act;
- a watchdog that notices its own timers stopped and says so on the rail.

## SHOULD-FIX

1. **Memory.**
   - NoClickDock 2 uses **370 MB PSS**: four WebKitWebProcesses at 52–97 MB
     each, the shell 67 MB, WebKitNetworkProcess 11 MB, ncd-core 12 MB. The
     Python NCD's nine processes on the same desktop use **165 MB PSS**.
   - Each webview has its own web process; the second panel window alone
     costs about 60 MB.
   - Share one `WebKitWebContext` (one web process) across the four overlay
     webviews (wry `WebContext` reuse, or a shared data directory). They are
     one app, from one origin.
2. **A horizontal rail on a side edge** stays on that edge (a horizontal bar
   flush against the right edge, half-way down). Consider moving a rail to
   the nearest top or bottom edge when it turns horizontal, and back to
   left or right when it turns vertical.

VERDICT: FAIL
