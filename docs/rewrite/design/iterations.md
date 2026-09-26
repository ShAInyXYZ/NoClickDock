# Reference prototype — self-critique log

Each round: screenshot at 1× and 2× (Chromium via Playwright,
`prefers-reduced-motion: reduce` for deterministic frames), look at them as
if someone else had shipped them, list what is wrong, fix it, and shoot
again. Intermediate shots stay in the scratchpad. The final set is
`../reviews/shots/reference-*.png`, and the r0 Docker shot is kept as
`evidence/r0-docker@2x.png`.

## Round 0 → 1 (first render)

It read as calm and industrial at once, and the rail, recess and printed
labels were right. The defects:

1. **Resting row actions read as words.** Bodiless `--dim` pills made the
   value column say "12d · 384M restart stop". → Ghost pill at rest (1px
   `--line` outline); body on row hover or focus-within. *(A7)*
2. **The arm countdown looked like a link.** A 1px drain line under `stop?`
   read as an underline. → Removed. Armed = confirm label + accent. *(A7)*
3. **Arming moved the row.** `stop` → `stop?` widened the pill and pushed
   shopfront's value 10px out of the column. → Both labels share one grid
   cell, so width = max. *(A7)*
4. **The LED floated between the name and sub lines** of two-line rows. →
   Aligned to the name line.
5. **The empty actions column** left a stray 10px gap, and the Console
   row's value missed the edge. → Row is flex, not a four-column grid.
6. **`resets Mon 09:00` was clipped by the panel edge** (92px column). →
   112px, later max-content in a LimitGroup.
7. **Healthy meters shouted green** (Session, CPU, RAM). → Lit segments are
   `--muted` unless warn or crit. *(A6)*
8. **Power: problem hidden, names clipped, columns lopsided.** VRAM 94°
   showed only as one amber cell under a header saying "normal". `27″
   2560×1440 o…` and `keyboard · mou…` were clipped, and the left column
   ended half-way down. → Full-width warn Alert first, state `VRAM 94°`, `~`
   estimate marker plus a footnote, and shorter names.
9. **Section strip (32) and table header (28) misaligned** across the split.
   → `th` is 32. *(A13)*
10. **Toasts mixed Manrope with inline mono** and wrapped raggedly. → One
    plain sentence each. *(A14)*
11. **Specimen captions collided** (`BUSY 42%BUSY ?`). → One word each.
12. **The lamp poked 2px outside the dial outline** and read as an app-icon
    badge, the look being retired. → Pulled in until its corner is 1px
    inside the outline. *(A5)*
13. **The cherry index tick merged with the crit ring.** It sat 2px off the
    rim, and cherry and `--err` are about 20° apart. → Moved to the rail
    wall, 4px off the rim. *(A4)*
14. **An idle mark looked alive.** → Idle mark is `--dim`. *(A15)*
15. **A 6px lamp was heavy on a 32px compact dial.** → 5px. *(A5)*

Found while fixing: a pill part named `.label` inherited Cerveau's global
`.label` utility (uppercase, 0.22em) and every pill shouted
`RESTART STOP`. → Renamed the parts. *(A16)* Also, alert meta truncated
the words that matter (`unhealthy · 3 checks fa…`). → Meta budget
≤ 24 characters, with detail moved to the body. *(A12)*

## Round 1 → 2

1. **The Power panel was 830px tall,** clipped by a 660px work area, with
   columns of 520 vs 370. → Utilisation moved right and Tariff left:
   columns 450 / 444, panel 590px.
2. **The bars axis said `now 12:41` at the right end** while the current
   bar was at 12. → Axis `00 06 12 18 24`. The bars now fill the column;
   the 3px sparkline was unreadable. *(A9)*
3. **The anatomy tick overlapped the spec list.** → Wider gap.
4. **Panel edge A/B.** The `--line2` border plus `--elev-2` read as a
   bordered card beside the borderless rail. → Ring only
   (`evidence/panel-edge-*.png`). *(A8)*

## Round 2 → 3

1. **The reference showed only happy data.** There was no empty, error or
   stale state and no timings, yet those are what I will review against. →
   Plate 05: Tailscale not running (an empty state that names the command),
   Disk with a collector error footer (`df timed out after 10 s · showing
   12:38:02`), and a drawn, to-scale choreography timeline.
2. **Long mount paths would clip a fixed 64px meter label.** → Consecutive
   meters and limits render as one grid (MeterGroup / LimitGroup) with a
   max-content label column. *(A6)*
3. **Segmented A/B.** The kit's cherry-filled selected tab hides the crit
   lamp and badge (`evidence/segment-*.png`). → Raised key. *(A2)*

## Round 3 → 4

1. **At 1×, meter segments rendered 3 or 4px wide** unevenly (spans at a
   fractional x), and the fractional-width 24h bars shimmered. → SVG,
   integer rects, `crispEdges`. *(A6, A9)*
2. **Timeline gridlines ran through the annotations.** → Axis ticks and a
   hairline track per lane; every note sits under its bar.

## Round 4 → 5 (the harness audits the reference)

`shoot.mjs --audit` run against this page found:

1. **Digits in Manrope text were proportional everywhere.** Every
   component's `font:` shorthand resets `font-variant-numeric`, so
   `tabular-nums` on `body` never arrived. → The invariant is re-asserted
   last in the stylesheet. *(A16)*
2. **Alert heights were off the 4px grid** (53, 69 and 74px, depending on
   whether actions existed). → Title line fixed at 24, row gap 0, so every
   alert is `8 + 24 + 16·n + 8`. *(A12)*

After this round the audit reports 0 findings on every surface and 0
console errors.

## Round 5 → final

1. **Added focus and stale specimens.** Focus as a 2px cherry ring looked
   identical to crit on a 40px dial. → The dial's focus ring is `--text`.
   *(A17)* Stale: no ring, idle lamp, mark and readout step back. *(A11)*

## What I would still look at in the real app

- WebKitGTK (Tauri on Linux) renders Plex Mono and `color-mix()` slightly
  differently from Chromium. The final design review compares the real X11
  windows against these shots, not only the browser.
- The busy lamp sits on the progress arc about 1/8 of the way round. That
  is acceptable, but on a real ComfyUI run the arc passes under it every
  generation. Watch it live.
- The tailscale mark's hollow dots become a grey haze at 1×. It is the real
  mark, so leave it unless it reads as dirt on the actual display.

## Corrections found by the M1 review (the implementation inherited them)

Measuring the executor's build against the reference exposed defects in the
reference itself:

1. **The row lamp sat 4px below the name line in two-line rows** and 1px
   below in single-line rows. A fixed `margin-top` on a flex child cannot
   fit both heights. → The lamp moved inside `.Row-text`, absolutely placed
   at `top: (18 − led)/2`, and now measures 0.0px in every row.
2. **Ring stroke scaled with density** (1.6px at compact, 2.4px at large). →
   `vector-effect: non-scaling-stroke`.
3. **A wrapped stats line could start with `·`.** → Each item owns a 16px
   separator; the box is pulled into the inset and its first 16px are
   clipped with `clip-path`.
4. **The header state label and section title could not ellipsize.** → A
   text child (`.PanelHeader-label`) and `min-width: 0` on the section title.

The audit still reports 0 findings and 0 console errors. `shoot.mjs --audit`
now also reports text clipped inside a flex or grid container
(`clip-in-flex`), which is what hid item 4 in round 1.
