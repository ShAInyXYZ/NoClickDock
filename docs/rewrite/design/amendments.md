# DESIGN.md — proposed amendments

Design supervisor, 2026-09-26. These are proposals; main decides what enters
DESIGN.md. Each amendment is already applied in the reference prototype
(`reference.html` + `reference.css`), so "what it looks like" is the screenshot
set in `../reviews/shots/reference-*.png`. Where an A/B was needed, the
evidence is in `evidence/`.

Priority: **P1** means DESIGN.md is contradictory or leaves out something the
executor must decide, and a wrong guess would look cheap. **P2** is a
refinement.

---

### A1 · P1 · Stay inside the five type roles
DESIGN.md sets "five roles only: 9.5 / 11.5 / 13 / 15 / 21", then
itself uses 12.5 (header state label, toast text) and 12 (row value).
**Change:** header state label → Manrope **13**/600; toast text → Manrope
**13**/500; row value → Plex Mono **11.5**; alert meta stays mono 11.5.
**Why:** the file breaks its own rule three times, and each odd size
invites more. The reference uses only the five roles, and at 11.5 mono the
values still read as the data column next to 13px names.

### A2 · P1 · Selected segment is a raised key, not a cherry block
DESIGN.md lists "selected segment" as an accent use (the original kit's
`Segmented` fills it with `--accent`).
**Change:** selected segment = `--s3` fill, `--text` ink, the elev language
as insets (`0 1px 0 var(--lift) inset, 0 -1px 0 var(--shade) inset, 0 0 0 1px
var(--ring)`). Unselected stays `--dim` → `--text` on hover. Remove "selected
segment" from the accent's list.
**Why:** a page tab carries a tone LED and a badge (DESIGN.md "Pages").
On a cherry fill, a crit lamp and a red `2` disappear
(`evidence/segment-kit-accent-fill.png` vs `evidence/segment-raised-key.png`).
Cherry `#E54866` and `--err` `#e6533f` are about 20° apart in hue, so a
filled cherry block in the header of every multi-page panel reads as an
alarm. The kit was built for a mode selector. Here the tabs are hover
navigation, and a quiet raised key suits that better.

### A3 · P1 · Readable ink, never disabled ink, for anything a person reads
DESIGN.md puts section titles, stats keys and `resets 14:00` in `--faint`,
and the dock readout in `--dim`.
**Change:** minimum ink for read text is `--dim` (section titles, section
meta, stats keys, reset times, row subs, footer text). The **dock readout is
`--muted`** (`--dim` only for an idle readout). `--faint` is only for what
nobody needs to read: grip dots, `·` separators, the footer freshness
stamp, unlit or idle lamps.
**Why:** `--faint` on `--s1` measures 2.2:1 and `--dim` 3.5:1. The readout
is the number the dock exists to show, and "if you have to click to find
out, the widget has failed" also rules out squinting: keep essential labels
and state descriptions legible rather than using the palette's disabled
ink.

### A4 · P1 · Open dial: give "one step darker" a token and move the index tick off the rim
"The recess darkens one step" has no step below `--bg`, and the index tick
"on the rim" sits against the crit ring.
**Change:** add NCD token `--recess-open: #111111`. The open dial's mark is
`--text`. The index tick is 2 × 10 `--accent`, placed **on the rail wall
facing the panel, 4px off the rim** (inside the rail's 6px padding, flush
with the capsule edge). It is not on the rim.
**Why:** on the rim, a cherry tick 2px from a breathing `--err` ring reads
as part of the ring (r0). Moved 4px out onto the capsule edge, it points at
the panel.

### A5 · P1 · Lamp geometry that fits the rim
A 6px square cannot sit "on the rim" of a 4px rim: centred on the rim, its
corner pokes 2px outside the dial outline, and it reads as an app-icon
notification badge, which is exactly the Python bubble being replaced.
**Change:** the lamp is centred on the 45° radius, pulled in until its outer
corner is 1px inside the outline
(`r = dial/2 − 1 − led·0.7071`), with a 1px `rgba(0,0,0,.55)` gasket so it
reads as set into the bezel. It is 6px at 40 and 48 and **5px at compact 32**.
CSS is in `reference.css` → Dial.

### A6 · P1 · Meters are neutral until they are a problem
DESIGN.md: "lit segments in the tone colour", so every healthy CPU, RAM and
limit meter lights green.
**Change:** lit segments are `--muted` when the tone is `ok` or absent, and
`--warn` or `--err` only for warn or crit. Unlit segments stay `--line2`.
**Why:** the Refused list includes "a healthy colour shouted in green", and
"a healthy dock is monochrome". A panel of green bars is decoration. Green
stays on the lamps, where it is a state.
**Also:** a run of consecutive `meter` (or `limit`) blocks renders as **one
grid**, so the label column is as wide as its widest label and every bar
starts on the same x (Disk mount paths such as `/mnt/archive` otherwise clip
in a fixed column). Segments are drawn as an SVG of integer rects with
`shape-rendering: crispEdges`: spans at a fractional x snap to 3 or 4 device
pixels and the meter looks uneven at 1×.

### A7 · P1 · Row actions: ghost at rest, width never changes when armed
DESIGN.md: actions "appear at full opacity only on row hover".
**Change:** at rest a row or alert action is a **ghost pill**: transparent
body, 1px `--line` outline, `--dim` ink. On row hover or `:focus-within` it
takes the pill body (`--s2`, `--line2`, `--muted`). A pill's width
is the larger of `label` and `confirm`: both labels sit in the same grid
cell and the inactive one is `visibility: hidden`. Armed state is the
confirm label plus accent (or `--err` for `danger`), with no countdown
ornament.
**Why:** in r0, dimmed or bodiless pills read as more words in the value
column ("12d · 384M restart stop"), and arming `stop` → `stop?` pushed that
row's value 10px left (`evidence/r0-docker@2x.png`). A 1px drain line under
the armed label read as a link underline, so it was removed.

### A8 · P2 · Panel edge is the elevation ring, like the rail
DESIGN.md gives the panel `--elev-2` **and** a 1px `--line2` edge. The rail
has `--elev-2` only.
**Change:** the panel drops the `--line2` border; the `--elev-2` ring is its
edge. Tauri windows keep 1px of transparent margin so the outer ring is
never clipped.
**Why:** with both, the panel reads as a bordered card next to a borderless
capsule (`evidence/panel-edge-border.png` vs
`evidence/panel-edge-ring-only.png`). Rail and panel should look like one
material.

### A9 · P2 · 24-hour bars fill their column
DESIGN.md: "24 SVG bars 3px wide".
**Change:** bar width = `floor((w − 23·2) / 24)`, 2px gap (the meter's
rhythm), 40px tall, `crispEdges`. Past hours are `--faint`, the current hour
is `--text`, and future hours are a 1px `--line2` floor stub. The axis shows
`00 06 12 18 24` in mono 9.5 `--dim`.
**Why:** 3px bars make a 118px sparkline floating in a 330px column (r0).
You cannot tell which hour was expensive, and the chart looked like an
afterthought.

### A10 · P1 · Header and footer: say freshness once
The header has a poll tag (`20s`), and the footer says `updated 12:41:07 ·
every 20s`. ARCHITECTURE also sends `snapshot.footer` ("engine 27.3 ·
compose 2.29") with no slot for it.
**Change:** header = mark · title · flexible space · LED + state label, and
nothing else. Footer = `snapshot.footer` on the left (`--dim`) and
`12:41:07 · 20s` on the right (`--faint`). A collector error turns the
footer `--err`: the error on the left, `showing <last good time>` on the
right.

### A11 · P1 · Stale is a state the UI owns
Nothing in DESIGN.md covers a core that stops sending (sidecar crash,
hang). The dock would keep showing its last, possibly green, picture.
**Change:** when no snapshot has arrived for **3 × pollSecs**, the UI marks
the widget stale. Ring and arc are removed, the lamp goes to idle, the mark
steps back to `--dim` and the readout to `--faint`. The panel header state
reads `stale`, and the footer turns `--warn` with `no reading since 12:38:02`.
**Why:** it is the one failure a status dock must never hide.

### A12 · P1 · Alert: meta budget and grid-true height
**Change:** alert meta is **≤ 24 characters** (exit code, flag, age).
Everything else goes in the body, which is 2 lines max (mono for a log
line) and then ellipsis. Actions sit at the right of the title line, and the
title line is 24px tall (the pill height), so an alert is always
`8 + 24 + 16·n + 8` px. Padding is 8px with a −8px horizontal bleed, so the
alert's LED and text stay on the panel's 16px inset like every row.
**Why:** in 440px with two actions, the title line has about 270px. Longer
meta truncated exactly the words that matter (`unhealthy · 3 checks fa…`).
The 10px padding in DESIGN.md is off the tokens' spacing scale, and without
the fixed line the alert height was 53, 69 or 74px. This is a rule for the
core's snapshot writers (research and executor) as much as for the renderer.

### A13 · P1 · Pin the geometry DESIGN.md leaves open
All on the tokens' spacing scale and the 4px grid, and all in `reference.css`:
- panel body inset **16px**; header 48; tab zone = Segmented 28 + 12 below,
  then a `--line` hairline; footer 28.
- row min-height **32** (not 30, which is off-grid), 40 with a sub line;
  the row LED aligns to the **name line**, not the middle of two lines.
- section strip **32** with 8px above its hairline; the table header row is
  also 32 so strips align across a `split`.
- `split`: two equal columns, 24px gutter, one 1px `--line` divider.
- slot = dial + 4 + readout (12 line) with 8px between slots; rail padding
  6 (10 at the trailing end of a vertical rail with readouts).
- toast radius 10 as token `--r-toast`, padding 9/14/9/12, gap 6 between
  toasts.
- chips: Manrope 11.5/500 `--muted`, 24px tall, 3 columns at 400px,
  ellipsis.
- empty state: one sentence in Manrope 13/500 `--muted`, with a command in
  a mono chip (`--s2`, 1px `--line2` inset, `--r`).

### A14 · P1 · Toasts: plain sentences, and they yield to an open panel
**Change:** a toast is one plain sentence with no inline mono fragments.
While any panel is open, toasts from **other** dials wait and appear when
the panel closes (the panel already suppresses its own widget's toasts).
**Why:** mixed Manrope and mono text wrapped into a ragged second line
(r0). And toasts sit "beside the dial, on the panel side", which is where an
open panel already is, so they would render under it.

### A15 · P2 · An idle dial's mark steps back
**Change:** `idle` (not running or unknown) draws the mark in `--dim`, and
`--muted` on hover. The readout says so in words (`off`).
**Why:** "not running" becomes readable at a glance, without adding colour.

### A16 · P1 · Two CSS traps to write into the contract
- `tokens.css` ships global utilities `.label`, `.tag`, `.mono`, `.elev-1/2`
  and `.blink`. A component part named `label` picked up uppercase, 0.22em
  tracking inside a pill (r1). **Never name component parts after them.**
- The `font:` shorthand resets `font-variant-numeric`, so `tabular-nums` on
  `body` does not survive a component that sets `font:`. Re-assert it after
  every component rule: `:is(body, body *) { font-variant-numeric:
  tabular-nums !important }`, last in the global stylesheet. Tokens'
  `:focus-visible` (1px, offset 1) must also be overridden by DESIGN.md's
  2px, offset 2.

### A17 · P2 · A dial's focus ring is `--text`, not cherry
**Change:** every control keeps DESIGN.md's 2px / offset-2 accent focus
ring, except the dial, whose ring is `--text`.
**Why:** a 2px cherry ring around a 40px dial is the same shape, size and
near-same hue as the 2px `--err` crit ring. In the specimen row, "focus" and
"crit" read as the same state
(`../reviews/shots/reference-rail@2x.png`). Focus on a dial is keyboard-only
and rare, but it must not look like an alarm.
