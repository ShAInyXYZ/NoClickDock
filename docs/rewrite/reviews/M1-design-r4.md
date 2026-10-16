# M1 — design review, round 4

Reviewer: design supervisor, 2026-09-26.

**Scope:** the size-changing dial switch, against DESIGN.md's acceptance
(`bf2622c`):
- no frame ever shows an empty panel or one widget's content in another's
  geometry (mandatory);
- pointer-to-drawn **median ≤ 50 ms, worst ≤ 70 ms on Mutter**.

**Build:** a clean `git archive` of `9a9b2e9`. The swap uses
`_NET_WM_WINDOW_OPACITY` on both panel windows inside one server grab,
after two painted frames.

**Desktop:** real X11 `DISPLAY=:0`, GNOME Shell 42.9 / Mutter, two
3840×2160 monitors at 60 Hz, text scale 1.5. I had it alone. Instance
`--instance design`, rail on the primary monitor's right edge.

## Method

Same as round 3:
- **Choreography:** 12 switches between widgets of different sizes (Docker
  → Power → Codex → Disk → Power → Claude → Tailscale → ComfyUI → Power →
  Docker → Disk → Tailscale → Codex), then two rapid sweeps at 30 ms per
  dial.
- **Integrity:** GNOME Shell's own screencaster (Mutter's composited output)
  over the union of all panels. Every frame is classified against settled
  references. "Empty" is tested on the strip x 3600–3750, y 3032–3372,
  which lies inside every panel at this build's sizes.
- **Latency:** ffmpeg x11grab at 120 fps with the cursor drawn in (pointer
  arrival and panel pixels on one clock); two runs. The shell's own
  `NCD_TRACE` gives hover → show.
- **Recorder helpers:** each helper my recordings started was stopped
  afterwards; none was left running and no recording indicator remained.

## Results

**Integrity: pass.**
- 227 composited frames: every one of the 12 switches went straight from the
  old panel to the new one.
- Both sweeps (60 frames): **zero** empty frames and **zero** mixed or
  wrong-geometry frames.
- The only frame matching no reference (8011 ms) is Docker with live mock
  content that differs from its reference. The switch went Power → Docker
  directly.
- This is the r3 defect fixed: r3 had 4–5 of 12.

**Latency: fail.**

| | median | worst | values (ms) |
|---|---|---|---|
| run 1, pointer → drawn on screen | **66.5** | **108** | 58 33 41 75 58 58 50 108 92 92 92 100 |
| run 2, pointer → drawn on screen | **83** | **133** | 67 67 75 133 83 83 58 92 108 92 84 |
| shell trace, hover event → `show` | 42 | 85 | 37 27 38 85 42 42 25 40 56 56 43 42 |

Where the time goes:
- About 10–15 ms from the pointer to the hover reaching the shell (WebKit
  `pointerenter` → command).
- About 42 ms (median) of page drawing, measuring and the two-painted-frame
  confirmation.
- One to two compositor frames for the opacity swap to reach the screen on a
  60 Hz dual-4K desktop.

The executor's 33–66 ms (median 50) was measured on Mutter-on-Xvfb. There is
no real vblank there, so it does not carry over. On this desktop the
acceptance is missed by about 17–33 ms at the median and 38–63 ms at the
worst.

## MUST-FIX

1. **Switch latency on real Mutter: median 66–83 ms, worst 108–133 ms**
   (acceptance: median ≤ 50, worst ≤ 70). Integrity is solved; the budget is
   now spent **after** the pointer arrives. Options, roughly in order of
   value:
   - **Prepare before arrival.** The rail page sees pointer motion. When the
     pointer is moving along the rail toward a dial (inside the 8 px gap, or
     in the last third of the current dial, heading that way), start drawing
     and sizing that widget in the parked window. By the time `pointerenter`
     fires, the swap only waits for the grab. Most switches are to a
     neighbour, and a wrong guess costs nothing but a redraw.
   - **One painted frame, not two,** if the server-grabbed opacity swap no
     longer needs the second as a safety margin. Recheck integrity with the
     same recorder method if you change it.
   - **Keep the last-shown widget drawn in the parked window.** A switch back
     (A → B → A, common when reading two panels) then needs no redraw.
   - If after this the real-Mutter numbers still miss by a frame, bring the
     measured distribution to main. The acceptance is main's number, and the
     compositor's vblank sets a floor this desktop can't beat.
   - **Acceptance** (unchanged): the same 12-switch run, twice, at 120 fps on
     this desktop.

## Notes

- **x11grab showed isolated single empty frames** in 2 switches. GNOME's
  composited recording of the same switches showed none. x11grab reads the
  root window and tears on a composited desktop (last round it also invented
  toast blinks that were not on screen), so integrity is judged on the
  compositor's recording.
- **No abort or freeze** in this session (see M2-design-r2b.md for the
  freeze trigger check).

VERDICT: FAIL
