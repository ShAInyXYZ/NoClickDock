# M2 — research freeze soak

This asks whether the shell freezes under long, heavy use, as design saw
once after ~27 minutes at `82c8f12`
([M2-design-r1b.md](M2-design-r1b.md), "Open risk").

## Result

| build | run | outcome |
|---|---|---|
| `9a9b2e9` | **50 min (3,000 s), completed** | **No freeze.** 80 liveness probes passed; 15 of 15 right-clicks under a foreign keyboard grab left the dock working; **0 `WATCHDOG` lines**; no panic, xcb error or off-main-thread line. |
| `778b58a` | 6.5 min, stopped on purpose | No freeze in that window. That driver did not yet right-click under a grab. The freeze at this build is proven deterministically instead: one right-click while another client holds the keyboard ([M2-research-r3.md](M2-research-r3.md) must-fix 2, fixed in `9a9b2e9`). |

What the `9a9b2e9` run did in 50 minutes:
- 1,323 dial moves at human speed;
- 97 rapid sweeps (10–30 dials, 10–50 ms each);
- 229 tab hovers inside panels, and 39 pill clicks (arm and confirm, mock
  actions, including the refused `remove legacy-cron`);
- 85 native menus, dial or rail, open 0.4–4 s while live-mock toasts
  arrived. 19 of them had an item chosen (`Refresh now`); the rest were
  dismissed with Escape;
- 15 right-clicks while a helper client held the keyboard grab. Each logged
  `the menu could not open`, and each was followed by a probe in which
  dwell and hide both ran;
- rests of 1–8 s away from the dock.

**Liveness probe**, every 20–40 s: from rest, hover a dial, then leave.
A pass needs `dwell done` and then `hide` in the shell's `NCD_TRACE`. Both
go through a tokio timer and `run_on_main_thread`, the path the freeze had
starved. Three failures in a row would have counted as a freeze, as would
a `WATCHDOG … has not run` line not followed within 10 s by `moving again`.

**Shell RSS** (the `ncd` process only; the web process is separate):
- 174 MB at start;
- 188–191 MB from 5 to 30 min;
- about 120 MB from 35 min to the end (118 MB at 50 min).

The drop is a one-off release, stable afterwards. There is no growth
trend.

At the end, SIGINT under `gdb` finds the main thread in tao's own
`gtk_main_iteration_do`, the normal idle state. Thread states: 34 in
`futex_do_wait` (parked tokio workers), 9 polling, 2 blocking reads on the
core's pipes.

## Conditions

- Private `Xvfb :98` (1600×1000), with **bare Mutter 42** as the X11
  compositing window manager (recipe below).
- `ncd --mock --instance research-soak` (the live mock: data and toasts
  move with the clock), on a nonexistent session bus, so no tray reaches
  the real session.
- The shell was launched as a **child of `gdb`**, so a stack can be taken
  despite `ptrace_scope=1`:
  `gdb -batch -ex run -ex 'thread apply all bt' --args ncd …`. The build
  was an unstripped copy of the same tree, with line tables only.
- The pointer only ever went to our own windows (rail, panels, our menus)
  or empty desktop.

## Mutter on Xvfb

A real Mutter, the same compositor as the GNOME desktop, on a private
display, with no `:0` needed. There is no `mutter` binary on Pop!_OS
22.04, but libmutter-10 and its typelibs are installed (gnome-shell uses
them). So a 25-line gjs script drives it:

```js
imports.gi.versions.Meta = '10';
imports.gi.versions.Clutter = '10';
const { GObject, Meta } = imports.gi;
const Plugin = GObject.registerClass(class Plugin extends Meta.Plugin {
    vfunc_start() { Meta.get_stage_for_display(this.get_display()).show(); }
    vfunc_map(actor) { this.map_completed(actor); }
    vfunc_destroy(actor) { this.destroy_completed(actor); }
    vfunc_minimize(actor) { this.minimize_completed(actor); }
    vfunc_unminimize(actor) { this.unminimize_completed(actor); }
    vfunc_size_change(actor) { this.size_change_completed(actor); }
    vfunc_switch_workspace() { this.switch_workspace_completed(); }
    vfunc_plugin_info() { return null; }
});
const ctx = Meta.create_context('research-mutter');
ctx.configure(null);             // an argv array crashes gjs here
ctx.set_plugin_gtype(Plugin.$gtype);
ctx.setup();
ctx.start();
ctx.run_main_loop();
```

```sh
Xvfb :98 -screen 0 1600x1000x24 -nolisten tcp &
export DISPLAY=:98
# Mutter gets a private bus and in-memory settings: it never touches the
# user's session bus or dconf.
eval $(dbus-daemon --session --fork --print-address=1 --print-pid=1 | awk 'NR==1{print "BUS="$0} NR==2{print "BUSPID="$0}')
DBUS_SESSION_BUS_ADDRESS=$BUS GSETTINGS_BACKEND=memory XDG_SESSION_TYPE=x11 \
  GI_TYPELIB_PATH=/usr/lib/x86_64-linux-gnu/mutter-10 \
  LD_LIBRARY_PATH=/usr/lib/x86_64-linux-gnu/mutter-10 \
  gjs mutter.js &
# the app under test keeps a dead bus
DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent ncd --mock --instance <unique> &
```

Things to know:
- **Put a desktop-type window behind the dock**, e.g. a Tk window with
  `attributes('-type', 'desktop')` over the whole screen.
  - Without it, XGetImage returns the windows' own contents and ignores
    `_NET_WM_WINDOW_OPACITY`: an `xclock` at opacity 0 still shows, and
    parked panels look drawn.
  - With it, captures show what Mutter composites.
  - Do not use an override-redirect window for this: Mutter stacks those
    above everything, including the rail, and it eats the pointer.
- **Test clicks under a window manager.** On bare Xvfb, X focus is
  PointerRoot. A click leaves DOM focus on a dial, whose `onfocus`/`onblur`
  are wired as hover, and that produces stale, duplicated hover events
  that do not happen on a desktop.
- It reproduces design's real-desktop measurements. At `778b58a`: 3 of 12
  size-changing switches blinked one frame, median 74 ms; design measured
  4–5 of 12 and 78 ms. At `9a9b2e9`: 0 of 12.
- Grab a small `bbox` for frame sampling (about 3 ms a sample). A
  full-screen grab is too slow to see one frame.

## Scripts (scratch, not committed)

- `soak.sh` / `soak.py`: the driver above. Parameters are `WM`, `BIN`,
  `DURATION` and `HIDE_TRACE`.
- `kbgrab.py`: holds the keyboard grab from a second client for *n*
  seconds.
- `grabfail.sh`: the deterministic freeze repro, with a gdb stack.
- `frames.sh`: empty-frame sampling during switches.
- `inshape.py`: an overlay's input-shape rectangles (`XShapeGetRectangles`).

A soak of the next build (web-process crash recovery, approach prediction,
shell-driven toast fade) should reuse this setup. It should add a
`kill -9` of the web process mid-run and check that the dock comes back.
