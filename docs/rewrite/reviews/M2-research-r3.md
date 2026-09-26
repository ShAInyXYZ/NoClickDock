# M2 — research review, round 3

Reviewed: `778b58a` (after [M2-research-r2.md](M2-research-r2.md)). All
runs used a clean `git archive 778b58a` tree, built release as shipped. They
ran on a private Xvfb `:98` with `--instance research-m2` and
`DBUS_SESSION_BUS_ADDRESS` pointing at a nonexistent bus.

Two compositors were used:
- `picom --config /dev/null --backend xrender`, as in earlier rounds;
- a bare **Mutter 42** as the X11 compositing window manager, new this
  round. It is libmutter-10 driven from a 30-line gjs script, with its own
  private session bus and in-memory GSettings, so it never touches the
  user's session. See [Mutter on Xvfb](#mutter-on-xvfb).

For stacks, the shell ran as a child of `gdb`, from an unstripped build of
the same tree.

Regression checks at `778b58a`:
- Go: `gofmt` clean; `go vet` on linux and windows; `go test -race ./...`
  ok (9 packages).
- UI: `svelte-check`, 0 errors and 0 warnings.
- Shell: `cargo test --lib`, 9 passed; `cargo check --target
  x86_64-pc-windows-msvc` ok, once the Windows core was built for the
  bundle's resource path.

## r2 items

| r2 | item | verified |
|---|---|---|
| MUST 1 | tab and action state lived per window | **Fixed.** See the repros below. The shell keeps each widget's page (`panel_page`, sent with `panel-open`). The hub broadcasts `running` once an action passes the allow-list, and `result` names widget, action and args. Both pages derive running and failed from those broadcasts. Arming stays per window. |
| S1 | no compositor: the outgoing window covers the new one | **Fixed.** `composited()` → unmap instead of park, one panel window; `composited-changed` unmaps parked windows. |
| S2 | Windows: the hidden incoming window may not render | Open; stays an M7 item. |
| S3 | a snapshot mid-swap reveals a stale size | **Fixed.** `panel_painted(seq, w, h)` shows nothing unless the size matches the last `panel_layout`. |

### Repros

| scenario | result |
|---|---|
| Docker's Stopped tab, then Docker again after one and after two other dials (picom) | Stopped both times. After two dials it opened in the *other* window, the r2 failure case. |
| a refused action (the mock's `remove legacy-cron`), then two window switches, twice, within 10 s (picom) | `failed` and its note were shown in the other window at +3.6 s, back in the first at +5.5 s, and in the other again at +7.5 s. They were gone at +12.6 s, as specified. The action was sent once; `running` and `result` came from the real hub. |
| a running action pressed again from the other window | A stand-in wraps the real `ncd-core`, passes `running` at once and holds `result` for 6 s. The other window drew the pill as busy (`aria-busy`, dimmed). Two clicks there neither armed it nor sent anything: the stand-in logged exactly one `action` from the UI. When the result arrived, `failed` showed in that window. |
| bare Xvfb, no compositor, 20 switches | One window mapped at a time; 0 % black outside the rail and panel; 0 % unpainted; nothing mapped after leaving. |
| compositor lost after 8 switches (picom killed) | The parked window was unmapped at once; 12 more switches used a single window; no black anywhere. |
| compositor arriving after 6 switches | Two-window swap from then on, 0 % unpainted at rest, no errors. |
| snapshot storm (Docker re-emitted every 50 ms, height toggling), 20 switches | 1 of 20 had 8 % unpainted, on Docker, as in r2. **Resting** on Docker with no switch shows the same (6 of 30 samples at 8 %). So it is the visible window resizing under a storm, not the swap. |

## MUST-FIX

1. **Never-shown panel windows swallow clicks at startup.** This is design's
   M2 part B must-fix 1, reproduced independently. It has been present since
   `82c8f12`; I missed it in r2, where I only tested windows parked after
   use.
   - `overlay::prime` (`app/src-tauri/src/overlay.rs:122`, called from
     `lib.rs:112`) maps both panels at opacity 0 at their default
     geometry, 0,0 200×200. It then calls `pass_through(window, true)`
     (`overlay.rs:133`).
   - After startup, both windows report a full 200×200 input region
     (`XShapeGetRectangles`, ShapeInput). They get an empty region only
     after their first reveal and park.
   - Probe: an `xev` window under that corner receives **0** of the clicks
     at 100,100. A control click at 300,300 arrives. After two dial
     hovers, both panels show empty input shapes.
   - Under a real WM the 200×200 square lands wherever the WM places it,
     but it is always on top and always invisible.
   - Fix direction: apply the empty input shape once the window is realised
     and mapped (e.g. in `map-event`, or set it on the widget so GTK keeps
     it across realise). Or prime at a 1×1 geometry under the rail.
   - Acceptance: the `xev` probe at the primed rectangle gets every click,
     straight after launch.

2. **One dial right-click can freeze the dock for good.** This is the
   likely cause of design's 27-minute freeze. It reproduces every time,
   under picom and under Mutter.
   - Trigger: a right-click on a dial while another X client holds the
     keyboard grab. On a desktop that can be a shell menu or popover, the
     overview, a password prompt (pinentry, askpass), or a VM or
     remote-desktop window.
   - GTK 3 cannot take its grab, so it aborts the popup silently. No menu
     appears, and it emits neither `cancel` nor `selection-done`.
   - muda 0.19.3 (`src/platform_impl/gtk/mod.rs:1479-1496`) loops on
     `gtk::main_iteration()` until one of those signals arrives, so it
     loops forever.
   - It does so inside `menu.rs:42` `dial_menu` → `menu.rs:100`
     `window.popup_menu`, called from the IPC handler (tauri
     `ipc/protocol.rs:75`).
   - tao's Linux loop drains user events only in its outer loop (tao
     `event_loop.rs:1125`). From then on, nothing queued with
     `run_on_main_thread` runs: the dwell, grace and fade timers, toast
     placement and ticks, emits from the core reader thread, and menu
     events.
   - Measured: timer closures run +2 in a control probe. After the
     right-click they run +0, still +0 20 s after the grab is released.
     `gdb` at SIGINT shows the main thread still inside
     `show_context_menu` → `gtk_main_iteration` → `poll`, and every tokio
     worker parked.
   - This matches design's `/proc` picture: main thread polling, workers in
     `futex_do_wait`, core alive and streaming, rail stale, no `dwell done`.
   - `rail_menu` (`menu.rs:84`) takes the same path.
   - Fix direction (the executor chooses):
     - Pop the menu without a nested loop on Linux: `gtk::Menu` +
       `popup_at_pointer`, with items answered from their own `activate`
       signals.
     - Or check that the grab can be taken before popping, and stop waiting
       if the menu is not visible after `popup`.
   - A heartbeat watchdog would detect this but not cure it.
   - Acceptance: a right-click while another client holds the keyboard
     leaves the dock working (hover, close, toasts, rail data). The
     scratch `grabfail.sh` repro and a gdb stack show the main thread back
     in tao's loop.

## SHOULD-FIX

1. **A core that dies mid-action leaves the other window's pill busy
   forever.**
   - `running` is cleared only by a `result`.
   - The window that pressed gives up after 150 s (`RESULT_TIMEOUT_MS`).
     The other window never does, not even when the restarted core says
     `hello`.
   - Clear `running` on a link drop or a `hello`.
2. **Testing trap: Xvfb without a window manager fakes hover bugs.**
   - With no WM, X focus is PointerRoot. A left or right click on a dial
     leaves DOM focus on it, so its `onfocus`/`onblur` (wired as hover in
     `Dial.svelte`) fire on every entry to the rail.
   - The result is a duplicated `hover_dial None` that drops the grace
     timer, and a stale `Some(<clicked dial>)` after each real one. The
     panel follows the clicked dial and never closes.
   - It does **not** happen under Mutter (click-to-focus, rail not
     focusable), so it is not a product bug. Pointer tests that click
     should run under a WM, e.g. the Mutter below.

## Mutter on Xvfb

It reproduces design's real-desktop numbers without `:0`. Twelve
size-changing switches in design's order, with a strip inside every panel
sampled every ~3 ms:

| | switches with an empty stretch | new panel first seen after (median) |
|---|---|---|
| Mutter 42 on Xvfb | 3 of 12, each ~5 samples (one ~16 ms frame) | 74 ms (60–136) |
| picom xrender | 9 of 12, 1–4 samples each | 61 ms (46–127) |

Design measured 4–5 of 12 and a median of about 78 ms on real Mutter, so
this setup is a usable stand-in for M1 design r3 must-fix 1.

Recipe:
- `Meta.create_context()`, then `configure(null)` (an argv array crashes
  gjs), then `set_plugin_gtype()` of a `Meta.Plugin` subclass. The
  subclass completes map, destroy, minimize and size-change at once and
  shows the stage in `start`.
- Run it with `GI_TYPELIB_PATH` and `LD_LIBRARY_PATH` set to
  `/usr/lib/x86_64-linux-gnu/mutter-10`, `GSETTINGS_BACKEND=memory`,
  `XDG_SESSION_TYPE=x11`, and a private `dbus-daemon --session` for Mutter
  only. The app keeps the dead bus.
- Use a desktop-type background window: Mutter stacks override-redirect
  windows above everything.

## Process note

Every run used `:98` (or the soak's own instance name, below), a nonexistent
session bus for the app, and only my own PIDs. The other agent's `picom` was
left alone. The freeze soak design asked for (45+ min under Mutter, gdb as
parent) is running on `:98` and will be reported separately.

VERDICT: FAIL
