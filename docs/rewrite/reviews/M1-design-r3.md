# M1 — design review, round 3

Reviewer: design supervisor, 2026-09-26.

**Scope:** the one r2 must-fix, the dial switch between panels of different
sizes. It is now built on two panel windows: the incoming widget is drawn
and sized in the parked window, the page confirms two painted frames, and
the two windows swap in one main-loop turn.

**Build:** a clean `git archive` of `82c8f12`, built into a scratch target.

**Desktop:** real X11 `DISPLAY=:0`, GNOME Shell 42.9 (Mutter), two
3840×2160 monitors at 60 Hz, text scale 1.5. I was the only NoClickDock
there (research on :98, executor on :97). Instance `--instance design`,
rail on the primary monitor's right edge.

**The pass condition from r2:**
- no frame without a panel;
- no frame of old content in the new geometry;
- each switch fully drawn within 50 ms of the pointer arriving.

## Method

Two independent recorders over the same choreography: 13 dials, 12 switches
between widgets of different sizes (Docker, Power, Codex, Disk, Power,
Claude, Tailscale, ComfyUI, Power, Docker, Disk, Tailscale, Codex), then
two rapid sweeps at 30 ms per dial.

- **GNOME Shell's own screencaster** records Mutter's composited output
  (what the user sees). Region: the union of all seven panels,
  2780,2560 980×1600.
- Every frame is classified against a settled reference of each panel and
  of the empty desktop. Each frame is either one widget's panel, or the
  desktop, or a match for nothing.
- **Empty** means the strip at x 3600–3750, y 2984–3386 shows desktop. That
  strip lies inside **every** panel, so it is never empty while any panel
  is up.
- **ffmpeg x11grab at 120 fps** with the cursor drawn in: pointer arrival
  and panel pixels share one clock, which gives the latency. Torn frames,
  detected by the rail's grip dots, are discarded.
- The shell's own `NCD_TRACE` agrees with the video on latency: `show` comes
  70–80 ms after each move.

## Results

**GNOME recorder, composited truth.**
- 12 switches:
  - 4 have one fully **empty frame** between old and new: Claude →
    Tailscale, Power → Docker, Docker → Disk, Tailscale → Codex.
  - 1 has a **wrong-geometry frame**: Codex → Disk shows Disk's content at
    Codex's position, cut off at the left, for one frame.
- A second, independent run of 12 switches (the toast-arrival run below)
  had 4 empty or mixed frames: Codex → Power, ComfyUI → Docker,
  Power → Claude, Claude → Tailscale.
- The rapid sweeps add one more empty frame.
- Evidence: `…/m2b/x11/empty-events.png` and `switch-events.png`
  (scratchpad).

**x11grab at 120 fps, same 12 switches.**
- 7 of 12 show 1–2 empty 120 fps frames (8–17 ms) between old and new.
- **Drawn after the pointer lands: 50, 58, 75, 75, 75, 75, 75, 83, 83, 83,
  84, 100 ms** (median about 78 ms).

**A toast arriving mid-switch:** ComfyUI's :00 transition landed while I was
switching.
- It waited, as the contract says, and no switch frame was disturbed by it.
- After the close it appeared beside its dial, verified in 3 of 3 direct
  runs. One recorded run showed no toast after the close; that did not
  reproduce, so it is not counted.

This is much better than r2 (166 ms with a 90 ms hole every time): most
switches now go old → new directly. But on Mutter a third to a half still
blink for one frame, and none are drawn within 50 ms.

## MUST-FIX

1. **A size-changing switch still blinks on Mutter, and is not drawn within
   50 ms.**
   - **Measured:** one empty frame in 4–5 of 12 switches, and one frame of
     old content at the old geometry (Codex → Disk). Switches settle in
     50–100 ms (median about 78).
   - **Likely cause** (fits both artefacts): the swap is two separate
     opacity changes on two X windows. Mutter applies each from its own
     property event, and may composite one frame between them, or show the
     incoming window's texture before it has taken its new size.
   - **Fix direction** (the executor chooses):
     - Never make a window visible by toggling its opacity from 0 at the
       swap. Keep the incoming window at full opacity while it paints, but
       where it cannot be seen: **below the outgoing window** in the
       stacking order when it is no larger, otherwise off the work area.
     - Then swap by **stacking and position** (raise and move in one
       flush), which Mutter applies with the window's current texture.
     - Hide the outgoing window only after the incoming one has reported a
       frame on screen (`_NET_WM_FRAME_DRAWN`, or GDK's frame clock
       `after-paint` on the incoming window plus one vblank).
     - Start preparing the incoming widget on hover-enter of a dial while
       another panel is open (as the first open already does during the
       dwell), so the 70–80 ms the page needs to draw and confirm is spent
       before the swap, not after it.
   - **Acceptance** (unchanged): with GNOME's recorder, 12 or more
     size-changing switches and two rapid sweeps show **no** empty frame and
     **no** mixed frame, and 120 fps x11grab shows each switch drawn within
     50 ms of the pointer arriving.

## Notes

- **Recorder behaviour:** GNOME's screencaster writes nothing for short or
  low-damage recordings when its D-Bus connection closes (StopScreencast
  hangs on GNOME 42). Twice it left its helper (`gjs
  org.gnome.Shell.Screencast`, activated by my calls) recording and the
  orange recording indicator lit. I stopped only those helper processes
  after verifying they had been started by my calls; the indicator cleared.
- **The r2 abort does not reproduce** on this build. The X-thread fix and
  `XInitThreads` are in. But see the open risk in M2-design-r1b.md: one
  instance froze after about 27 minutes.

VERDICT: FAIL
