# M2 — research review, round 5

Reviewed: `d881cb2` (crash recovery, approach prediction, shell-driven toast
fade, descenders, hashed tray icon, pending-act settle, ungrab guard), and
the shell parts of `26e9dd8` and `cbf7c57`. Clean `git archive cbf7c57`,
built release as shipped, plus an unstripped copy for `gdb`.

Runs used private Xvfb `:96` (the soak had `:98`) with the bare Mutter 42
from [M2-research-soak.md](M2-research-soak.md), a desktop-type window
behind the dock, a dead session bus for the app, `--instance research-m2`,
and only my own PIDs.

Regression checks at `cbf7c57`:
- UI: `svelte-check`, 0 errors and 0 warnings.
- Shell: `cargo test --lib`, 11 passed; `cargo check --target
  x86_64-pc-windows-msvc` ok.
- Core: `go vet` on linux and windows, `go test -race ./...` ok.
- `scripts/check-web-process-crash.py`: interface back in 0.32 s, and a
  hover opens a panel.

## r4 items

| r4 | item | verified |
|---|---|---|
| MUST 1 | a web-process crash leaves a dead dock that looks alive | **Fixed.** Details below. |
| S1 | stale "did not answer" 150 s after a core restart | **Fixed.** Held result, sidecar SIGKILLed mid-action, core restarted. **163 s** after the press, the pressing window's pill reads `remove`, not failed. Pending actions settle quietly on a link drop or `hello`, and each settle clears its own timer. |
| S2 | `set_visible` could skip `ungrab_server` | **Fixed.** Grab only for a batch; ungrab and flush on every path. |

**MUST 1 in detail:**
- **One `kill -9` of the web process:**
  - a new web process after **0.4 s**;
  - the rail redraws with live data (12 of 12 captures over 12 s differ,
    readouts moving);
  - a hover opens a panel on two different dials.
  - The views no longer share a process after this: tree PSS is 305 MB,
    about r3's four processes. This is documented as the price paid only
    after a crash.
- **Six kills within a minute,** each after the previous reload:
  - reloads back off 0.25 → 0.5 → 1 → 2 → 4 s, and the dock returns each
    time;
  - the sixth crash parks the rail: 0 drawn pixels, empty input shape;
  - 60 s later it is back, and a hover opens a panel.
- A crash in any view, including a hidden panel, brings all four back. The
  soak (below) kills a random one of the web processes every 3–5 minutes,
  mid-use. At the time of writing that had happened 3 times, and the dock
  came back every time.

## Approach prediction

I read the code for races with the swap:
- `hover_approach` draws only while the panel shown is the one asked for
  (`settled`). So it never takes the back window while that window is
  still drawing the widget the pointer is on.
- `choose` never draws in the window showing, and never draws a widget
  that either window already holds, so a widget lives in at most one
  window.
- Measurements and paints are matched per window by `seq`, so a
  superseded guess's reports are dropped.
- `show` happens only for the widget `wanted()`.
- A parked window is resized and moved only while parked.

I found no race.

Measured:

| test | r4 (`9a9b2e9`) | r5 (`cbf7c57`) |
|---|---|---|
| 18.6 s smooth glide over all dials, panel open: hover → show (NCD_TRACE) | median 37.6 ms | **median 0.1 ms**; 72 of 73 switches were predicted |
| same glide: strip samples with no panel (every ~3 ms) | 0 / 5,404 | 0 / 5,079 |
| same glide: CPU of shell + WebKit | 405–467 ticks | 417–485 ticks (+3–4 %) |
| same glide: drawings the pages were asked for (`panel_layout`) | 135 | 124 (each window keeping what it drew offsets the guesses) |
| 80 randomised fast glides (steps 2–13 px, 4–30 ms apart, 30 % overshoot and return), check at rest | — | **0** wrong or missing panels; never two panels taking input |

Wasted rendering is small. A wrong guess costs one hidden redraw, and each
dial passed in a sweep asks for at most one.

## SHOULD-FIX

1. **After a crash reload the rail's rounded corners are opaque black.**
   - Before a crash the rail sits on the desktop with transparent corners.
     After a reload, all four corners and edges of its window rectangle
     are black. Its non-background pixel count goes 24,242 → 25,812 and
     stays there through five reloads.
   - After the give-up path (dock hidden, then shown again) it is
     transparent again (24,242), so it is the reload, not the web process.
   - Panels were unaffected.
   - This is a visible degradation that lasts until the next start. Look at
     what `reload()` does not redo for the rail that the show path does:
     the transparent background, or the RGBA visual after the view's new
     process.
2. **After the sixth crash the dock vanishes for a minute with no word.**
   - Hiding is the right call, since no view can draw.
   - But on a desktop with no tray (vanilla GNOME) nothing tells the user
     why the dock is gone. The reason goes to stderr only.
   - A desktop notification, or the tray item where there is one, would
     say it.

## Freeze soak of `cbf7c57`

Reported separately in [M2-research-soak.md](M2-research-soak.md) when the
run ends (≥ 45 min under Mutter, gdb as parent, periodic `kill -9` of the
web process, right-clicks under a foreign keyboard grab).

VERDICT: PASS
