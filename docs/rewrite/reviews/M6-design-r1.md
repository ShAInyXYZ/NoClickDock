# M6 — design review, round 1 (Settings window)

Reviewer: design supervisor, 2026-09-26. **Tree:** a clean `git archive` of
`7dd2612`.

**Where it was checked:**
- the gallery `settings` surface: the harness audit finds 0 overflow, grid,
  digit or font findings and 0 console errors;
- the real window on my Mutter-on-Xvfb (`ncd --mock --instance design
  --settings`, dead session bus).

Shots are in `shots/M6/`.

**Layout and styling:**
- The window is 560 × 680 with our own title bar (Mutter adds no frame:
  Motif decorations 0), with minimise and close.
- Sections in DESIGN order: Widgets (mark, name, reorder arrows, switch;
  the ends' arrows dimmed), Dock, Start, Tailscale, Power, ComfyUI.
- Headings are Manrope 15/600, and fields are 38 px with `--r` corners.
- The segmented controls are the raised key. Selects are Cerveau, not
  native. Numbers and hosts are in mono. Hints sit inline after labels
  (`where the rail sits`, `W, once for all`).
- The Save / Discard footer is tied to the state:
  - `Unsaved changes` enables both buttons, with Save filled;
  - Discard reverts and says `Changes discarded.`.
- Close hides the window and leaves the dock running.

**Nothing visibly broken, no contract violation.**

VERDICT: PASS
