# M1 — design review, round 2

Reviewer: design supervisor, 2026-09-26.

**Scope:** the r1 must-fix commit `6c7deed`, plus the M2-progress commits
before it (`7383736`, `aa0bbca`, `d5ca493`). The verdict covers M1 surfaces
only.

**Build on the real desktop:** HEAD `fde7211`, since it contains the fix
for a crash I reproduced on `6c7deed` (below). It was built from a clean
`git archive` export, because the executor's working tree holds untracked
Docker files that do not compile. `f97655d` (between them) only adds unit
tests.

## How it was checked

**Browser** (core `--mock --port 47811`, Vite on 1430)
- `shoot.mjs --audit` at 1× and 2×: 32 surfaces, 0 findings, 0 console
  errors, Manrope and Plex Mono loaded. The harness now re-resolves each
  surface at capture time, because the live gallery can replace one
  mid-shot.
- DOM measurements for every r1 must-fix.
- A scratch stand-in core served maximum-length content.
- Playwright interaction checks: tab dwell, arming, focus, reduced motion.

**Real windows**, X11 `DISPLAY=:0`, two 3840×2160 monitors at 60 Hz, text
scale 1.5
- Instances: `ncd --mock --instance design` (and `review` earlier).
- Pointer: driven with `xdotool`.
- Timing: measured from the screen, not from the app's trace. I recorded
  with ffmpeg `x11grab` at 120 fps with the cursor drawn in, so pointer
  arrival and panel pixels share one clock. x11grab tears on a composited
  desktop (a torn frame shows the uncomposited root, with no rail), so torn
  frames are detected by the rail's grip dots and discarded.
- Flash checks: recorded with GNOME Shell's own screencaster, which captures
  the composited output the user sees and never tears.
- Parking: tested with a throwaway Tk window of mine placed under the parked
  panel and toast.
- Evidence stays in the design scratchpad (`…/scratchpad/design/m1r2/`);
  real-desktop captures are **not committed**.
- Cleanup: every process I started is killed, the pointer and focus are
  restored, and the executor's :47810 core is untouched.

## r1 must-fix — all seven closed

| r1 | check | result |
|---|---|---|
| 1 tab hover closed the panel | X11: dial → panel → NOW → STOPPED → SYSTEM → NOW → SYSTEM → STOPPED | **fixed.** The top held at y 2901 through heights 549 / 831 / 921; the panel stayed open; the tab under the pointer never moved |
| 2 near-white ring | X11 pixels over the dark desktop | **fixed.** Rail edge 56 and panel edge 56 over a 28 body and 31 desktop (r1: 147–189) |
| 3 row lamp low | DOM, all rows with a lamp across the gallery | **fixed.** 0.0 px from the name line in 26 of 26 rows |
| 4 flex hard-clip | max content, plus the new `clip-in-flex` audit | **fixed.** Meter labels, values, the header state, section titles and tabs all ellipsize; 0 findings |
| 5 wrapped stats `·` | max content | **fixed.** The wrapped line starts `build cache 22G` |
| 6 amber path/commands | DOM tones | **fixed.** `/var/lib/docker` `--muted`, prune note `--dim`; only 38G and 22G stay `--warn` |
| 7 bars at 3 x | DOM | **fixed.** One bar start per page; Disk starts at 112 across all three sections, and ends align |

The r1 should-fixes taken also check out: ring stroke 2px at every
density, readout guard, section and tab truncation, a floor for the row
name, and `i:id` action keys (my duplicate-id test no longer blanks the
panel).

## The new parking mechanism (Linux)

Parked means mapped, invisible, click-through. The parked panel was
3087,2901, 663×921, and the parked toast 3538,3332, 212×57 (inside the
panel's rectangle). My Tk probe (flat #808080 with a 1px black grid,
logging every event) was placed under both. The stacking order confirmed
panel, rail and toast above the probe.

- **Pointer and clicks reach the window underneath: pass.**
  - At 3300,3100 (under the parked panel only) and 3640,3360 (under the
    parked toast and panel), `xdotool getmouselocation` reports **my probe**
    as the window under the pointer.
  - The probe logged `<Enter>`, `<Motion>`, `<Button-1>` and
    `<ButtonRelease-1>` at both points.
  - NCD's trace logged no hover.
- **No ghost, no shadow, no tint: pass.** A capture of the probe under both
  parked windows contains **exactly two colours**, the probe's grey
  (528,989 px) and its grid black (58,429 px), and nothing else.
- **Not in alt-tab or the overview: pass.**
  - All three windows are `_NET_WM_STATE_SKIP_TASKBAR`, `SKIP_PAGER`,
    `ABOVE` and `STICKY`, typed `UTILITY` (rail, panel) or `NOTIFICATION`
    (toast).
  - Seen on screen: the Alt+Tab switcher listed my probe (the control) and
    the user's apps, with no NoClickDock, while a toast was visible and the
    panel parked. The Workspaces overview (Super+D on this Pop!_OS desktop)
    showed thumbnails for every normal window including my probe, and no
    NoClickDock.
- **Park and reveal do not flash: pass.**
  - A first reveal from parked lands directly on the right widget's content
    (Claude, then later Disk), with no earlier widget's content and no blank
    or white frame.
  - Closing fades and parks with nothing reappearing. Toasts that waited
    appear after the close, as the contract says.
- **A dial switch flashes when the panel changes size: fail** (must-fix 1).

## Latency, measured from the screen

| | measured (`fde7211`) | executor's claim | contract |
|---|---|---|---|
| open: pointer on dial → first panel pixel | **142–167 ms** (5 dials) | 140–150 | 120 ms dwell, then a 140 ms fade |
| open: → fully drawn | 142–192 ms | | |
| close: pointer off → panel gone | **333–383 ms** | | 280 ms grace + 100 ms fade ✓ |
| sweep, 60 ms per dial, all 7 | **0 frames** with a panel ✓ | | |
| switch, same width (Claude → Docker) | **116 ms**, no gap | 24–76 (≈ 50) | "immediately" |
| switch, width changes (Docker → Power, Power → Codex, Codex → Disk) | **83–166 ms, with a blank or stale gap** | | "immediately, no close/open" |

Open is now at spec: it is the dwell plus one to three frames. The switch
is not, and it is the one visible defect left in the choreography.

## MUST-FIX

1. **A dial switch that changes the panel's size blinks: the old widget
   vanishes, or shows in the new geometry, for about 100 ms before the new
   one appears.** *(hover.rs switch path; panel page)*
   - **120 fps recording, no torn frames in range, Docker → Power:**
     - +0 to +58 ms: Docker is still shown.
     - **+66 to +158 ms: no panel at all.**
     - +166 ms: Power appears.
   - **GNOME's recorder** (composited truth) caught the same moment as
     **Docker's content drawn inside Power's wider window, shifted and cut
     off**, on screen for about 100 ms (frames at 2616 and 2718 ms).
   - **Scope:** every switch between panels of different width or page
     height does this (three of four in my run). An equal-size switch
     (Claude → Docker) went straight from one to the other.
   - **Why it matters:** DESIGN.md says a switch is immediate with "no
     close/open animation — the menu-bar model". A 100 ms hole is a
     close/open blink, and stale content in the wrong place is a flash of
     the wrong widget.
   - **Fix:** never let a visible panel window change geometry before it
     has painted the new widget at that geometry.
     - The cheapest robust form is **two panel windows**, now that parking
       is free: draw the incoming widget into the parked one (the page
       measures, the window is sized and placed while parked), then unpark
       it and park the old one in the same main-loop turn.
     - The alternative is a single window that renders the new widget
       first and moves and resizes only on the page's next paint (for
       example the page answers `panel_layout` after a
       `requestAnimationFrame` pair, and the shell applies geometry and
       content together).
   - **Acceptance:** a 120 fps capture of Docker → Power → Codex → Disk
     switches shows no frame without a panel and no frame of the old
     content outside its own geometry, and each switch is fully drawn
     within 50 ms of the pointer arriving (60 ms tolerance, about 3–4
     frames).
   - **Evidence:** `…/m1r2/x11/switch5-sheet.png` and
     `switch-power.png` (scratchpad).

## Found and already fixed: X-thread abort (`6c7deed` → `fde7211`)

On the `6c7deed`/`f97655d` build my instances aborted three times in about
20 minutes, each time roughly 60–110 s after the last hover, with the panel
parked, while live snapshots and toasts were still flowing.

| when | instance | how it died |
|---|---|---|
| 03:27:52 | `review` | `Gdk-ERROR: BadImplementation … request_code 20 (X_GetProperty)`; kernel `trap int3 in libglib` |
| 03:42:01 | `design` | the same Gdk-ERROR and trap |
| 03:45:2x | `design`, run under gdb with `GDK_SYNCHRONIZE=1` | `[xcb] Unknown sequence number while processing queue … Most likely this is a multi-threaded client and XInitThreads has not been called` → `poll_for_event: Assertion '!xcb_xlib_threads_sequence_lost' failed`, from `XPending` in the GTK main loop, while another thread sat in `XNextEvent` |

In each case the dock vanished; the sidecar died with it and no orphan was
left. Xlib was being driven off the main thread.

`fde7211` ("every window and monitor call on the main thread") names this
exact cause: a work-area read from the sidecar's reader thread.

**Soak of `fde7211`:** 03:48:37 → 04:00:38 (721 s) on
the same desktop, across every test above, with toasts every 30 s. **0
aborts**, where the same routine aborted the old build within about 2
minutes.

(One more instance of mine vanished silently at about 03:34:20 while the
research agent ran another `--instance review` on :0. Main ruled that
unattributable, so it isn't counted.)

## SHOULD-FIX

1. **Belt and braces for the X threading class of bug.**
   - Call `XInitThreads()` once, before GTK initialises. In Rust:
     `x11::xlib::XInitThreads()` as the first line of `main` on Linux.
     Xlib then survives a stray off-thread call instead of aborting the
     dock.
   - Keep `main_thread::assert` as it is, so strays are still found in
     debug.
   - In release builds, a violation should log once to stderr rather than
     be silent.
2. **Make "the dock has died" visible.** When the shell aborts, nothing
   tells the user the status dock is gone, which is the worst failure for
   an at-a-glance tool.
   - The autostart entry or the tray could restart it once with backoff (an
     M6 concern), or at least the next launch can say "NoClickDock quit
     unexpectedly at 03:27".

## FYI (M2, not counted)

- Toasts queued behind an open panel appear after it closes, as a stack
  (two seen: "Generating…" over "Generation complete"). Check the stack
  order against "newest nearest the dial" in the M2 review.
- On `6c7deed`, once, a toast arriving while the Docker panel was open made
  the panel vanish, with the pointer still on the dial and the shell
  believing the panel was open. On `fde7211` the same hold across the :08
  toast kept the panel up and the toast waited. It was most likely a
  symptom of the threading bug; re-check in M2.

VERDICT: FAIL
