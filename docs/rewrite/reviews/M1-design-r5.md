# M1 — design review, round 5 (switch latency with approach prediction)

Reviewer: design supervisor, 2026-09-26.

**Scope:** the size-changing dial switch, against DESIGN.md (amended):
- no frame ever shows an empty panel or one widget's content in another's
  geometry (mandatory);
- pointer-to-drawn **median ≤ 70 ms, worst ≤ 100 ms on the real Mutter
  desktop at 60 Hz**, from a 120 fps capture, with the shell preparing the
  dial the pointer is approaching.

**Build:** a clean `git archive` of `cbf7c57` (approach prediction and
"each window keeps what it last drew", both from `d881cb2`).

## Where each measurement comes from

| source | what it measured | committed? |
|---|---|---|
| **Real desktop `:0`**: GNOME 42.9 / Mutter, two 3840×2160 monitors at 60 Hz, scale 1.5, `--instance design` | all latency runs, the stage breakdown, a same-session control run of the previous build | no; captures stay in `…/scratchpad/design/r5/x11/` |
| **Private Xvfb `:91` + bare Mutter 42** (research's recipe from `M2-research-soak.md`: dead session bus, a desktop-type window behind the dock) | integrity (empty or mixed frames); latency for comparison only | no screenshots needed |

The user came back to the desk during the session. My guard caught their
pointer paths twice, and main then stopped all pointer driving on `:0`. The
latency runs had already finished clean, with 0 foreign pointer events in
every counted run. The GNOME-recorder integrity run on `:0` was not done, so
integrity comes from Xvfb + Mutter this round.

## Method

**Motion.** Two kinds of pointer movement, driven by XTest from one process
that logs every move with the wall clock:
- **Glide:** the pointer moves along the rail's centre line with a
  minimum-jerk profile (a human reach), 200 ms per adjacent dial.
- **Warp:** the pointer jumps, so there is nothing to predict.

**Choreography.** The first open is on Docker, then 12 switches:
- Power, Tailscale, Power, Docker, Disk;
- Codex (passing ComfyUI), Claude, Codex, ComfyUI;
- Docker (passing Disk), Power, Disk (passing Docker);
- a 0.7 s rest after each.

**Arrival** is the logged move that puts the pointer inside the target
dial's circle (r = 30 px).

**Drawn** is the first captured frame, after arrival, that matches the
target panel's settled reference.

**Capture.** ffmpeg x11grab at 120 fps. The frames carry wall-clock
timestamps (`-copyts`, passthrough), so capture and pointer log share one
clock without cursor detection.

**Calibration** on the same desktop and the same capture: flipping the
`_NET_WM_WINDOW_OPACITY` of a plain X window reaches the capture in
**14.8 ms median** (max 26.9). Changing its colour takes 21.2 ms median. So
the capture itself adds about one frame.

## Results: latency (real desktop `:0`)

| run | build | motion | median | worst | values (ms) |
|---|---|---|---|---|---|
| glide 1 | `cbf7c57` | 200 ms / dial | **118.4** | **262.1** | 134 117 19 126 100 216 102 68 117 213 120 262 |
| glide 2 | `cbf7c57` | 200 ms / dial | **114.2** | **218.2** | 119 132 58 68 110 218 99 80 100 184 133 175 |
| fast glide | `cbf7c57` | 120 ms / dial | 139.0 | 255.8 | 158 117 59 135 118 256 203 36 119 241 143 170 |
| warp 1 | `cbf7c57` | jump | 137.8 | 167.9 | 122 155 64 133 166 139 168 63 154 130 137 164 |
| warp 2 (+ property spy) | `cbf7c57` | jump | 150.4 | 167.9 | 136 161 52 164 167 150 168 68 151 130 167 133 |
| control | `9a9b2e9` | jump | 121.3 | 173.0 | same session, same method |
| control | `9a9b2e9` | 200 ms / dial | 182.7 | 208.6 | same session, same method |

**Against the acceptance:** glides miss by about 45 ms at the median and
120–160 ms at the worst.

**Prediction helps glides compared with the previous build** (116 vs 183 ms
median), **but not enough on this compositor.**

**Environment.** The same `9a9b2e9` build measured 66.5 ms median at
round 4 (warp) and 121 ms today. This session's desktop was about 50 ms
slower for both builds: a 4K editor window under the panel area, swap full.
Even taking that away, the glides would still miss (≈ 65–70 median,
≈ 170–210 worst).

### Where the time goes (glides, `cbf7c57`)

Sources: the shell's `NCD_TRACE`, and a second X client timestamping every
`_NET_WM_WINDOW_OPACITY` change on the two panel windows (warp 2).

| stage | median | worst |
|---|---|---|
| pointer enters the dial → `hover_dial` in the shell | 17.5–19.3 ms | 49–66 |
| `hover_dial` → `show` | 41–49 ms | 119–143 |
| `show` → the opacity change reaches the X server | **11–18 ms** | 18 |
| X server → on screen, window drawn ~100 ms before | **33–46 ms** | 77 |
| X server → on screen, window drawn long before (a switch back) | 5–19 ms | — |

**Finding 1: the prediction almost never has the panel ready on arrival.**
- A parked window's `panel_layout` → `panel painted` takes a median of
  **109–117 ms on the real desktop** (max 256).
- The same step takes **21 ms on Xvfb + Mutter**.
- At human speed the approach lead is 34–84 ms. The paint is still pending
  when the pointer arrives, and the swap waits for it.
- Only **4 of 20** predicted switches in the two glide runs found the panel
  ready. The 4 switch-backs, where a window still held the widget, were
  ready too.
- The likely cause is Mutter itself. It does not paint a window at opacity 0,
  and for a window it does not paint it sends `_NET_WM_FRAME_DRAWN` only on
  a fallback timer of a few frames. GTK's frame clock, and with it the
  shell's "painted after the next frame" check, therefore run at about
  10 Hz for a parked window. Xvfb has no real frame clock, which is why the
  executor's 0.2 ms did not carry over.

**Finding 2: even a ready swap costs 50–77 ms to reach the screen.** In the
four ready cases, `show` came less than 1 ms after hover, yet the pointer to
drawn totals were 68, 100, 100 and 110 ms.
- **11–18 ms pass before the two properties reach the server.** Something
  on the main thread runs between the `show` trace and `set_visible`. The
  suspects are the incoming window's position query and `set_position`
  inside `overlay::swap`.
- **Mutter then takes 33–46 ms** for a window resized or redrawn about
  100 ms earlier, against 5–19 ms for one drawn long ago and 15 ms for a
  plain X window. That is two or three extra compositor frames, consistent
  with Mutter holding a recently resized, frame-synced window until the
  client completes a frame.

**Xvfb + Mutter no longer reproduces real-desktop latency for this build:**
glide median 33.7 ms (worst 36.9) and warp 61.0 (worst 79.4), against
114–150 ms on `:0`. Both findings depend on the real frame clock.

## Results: integrity

**Xvfb `:91` + Mutter: pass.**
- Every frame was classified against the settled references, with an
  "empty" test on a strip that lies inside every panel.
- **Zero** empty frames and **zero** mixed or wrong-geometry frames:
  - 12 glide switches;
  - 12 warp switches;
  - two full-rail glides at 50 ms per dial (0 → 6 → 3, twice), 24 panel
    changes.
- Intermediate dials passed on the way show their own panel, as the
  menu-bar model says they should.

**Real desktop: not judged this round.** x11grab on `:0` showed a few frames
with block-shaped holes (desktop showing through 16 px tiles of a panel).
x11grab reads back the composited root and tears here, as it did in rounds
3–4. Judging needs GNOME's recorder, which needs pointer time on `:0`.

## MUST-FIX

1. **Switch latency on the real desktop: glide median 114–118 ms, worst
   218–262 ms** (acceptance ≤ 70 / ≤ 100). Integrity holds; the budget goes
   on the two findings above. In order of value:
   - **Let the parked window paint at the frame rate.** Keep a parked panel
     composited: for example, park at opacity 1/255 rather than 0. Over any
     background that changes a pixel by at most one level, which the eye
     cannot see, but Mutter then paints the window and sends frame-drawn
     every frame. Check on `:0` that a parked window's
     `panel_layout → panel painted` drops from ~110 ms toward ~20 ms. If
     1/255 is visible anywhere, try another way to keep the window drawn.
     Do not drop the "painted after the next frame" check: it is what fixed
     the half-drawn panel in round 3.
   - **Do the positioning when the window is drawn, not in the swap.** A
     predicted window is already placed. `overlay::swap` should be only the
     grab and the two properties, so `show` → server drops from 11–18 ms to
     about 1 ms.
   - **Avoid resizing right before showing.** Two options:
     - give both panel windows one fixed size (the largest panel, with
       transparent space and an input shape trimmed to the panel), so a
       parked window never resizes;
     - or resize at approach time and keep the last size when the next
       widget fits.
   - **Acceptance:** the glide choreography above (200 ms per dial,
     minimum-jerk, 12 switches), twice, on the real desktop at 120 fps with
     wall-clock timestamps: median ≤ 70, worst ≤ 100. Also one
     GNOME-recorder integrity run: zero empty or mixed frames. This needs
     real-desktop time arranged with the user, since Xvfb cannot show
     either finding.

## Notes

- A switch back is still fast: 19–80 ms, since the window kept its drawing.
- The rail reports approaches as intended. The trace shows `approach <next>`
  34–84 ms before arrival at human speed, and a wrong guess cost nothing
  visible on Xvfb.
- Research's recipe also served M2-design-r3b (toast fade, crash recovery).

VERDICT: FAIL
