# NoClickDock — interface contract

NCD 2 wears **Cerveau's identity**. Visual authority, in order:
`../Cerveau/panel/src/tokens.css` (palette, type, spacing, radius, depth,
motion), Cerveau's kit (`kit/Dot.svelte` square LEDs, `kit/Meter.svelte`
segmented level meters, `kit/Segmented.svelte`, `kit/Panel.svelte` printed
header strips, `kit/Button.svelte` pills), DeadWhispr / HubVoice's **inset
circular dial** (`../DeadWhispr2`, `../Cerveau-Hub/HubVoice`), then this file.
Where they disagree, this file wins for NCD.

The aesthetic is Cerveau's: TP-7 / Japanese old-school-modern, minimalist
industrial. Monochrome surfaces, one signal accent, hairlines, printed mono
labels, mechanical precision, generous negative space.

## The rule NCD adds

**If you have to click to find out, the widget has failed.** Everything a
person checks is readable from the dock or from a hover. Clicks exist only to
change the world (stop a container) and are always two-step.

**Colour is signal, never decoration.** A healthy dock is monochrome and
still. The only things that move are problems and work in progress.

## Tokens

Copy `tokens.css` from Cerveau verbatim into `app/src/lib/tokens.css`, keeping
its comments; NCD adds only the tokens below. Never retype a hex value in a
component.

| token | value | use |
|---|---|---|
| `--bg` `--s1` `--s2` `--s3` | #161616 #1C1C1C #242424 #2B2B2B | surface ladder |
| `--line` / `--line2` | #2E2E2E / #3D3D3D | structure / control edge |
| `--text` `--muted` `--dim` `--faint` | #FAFAFA #A1A1AA #71717A #52525B | ink |
| `--accent` | #E54866 cherry | interaction only: focus, the open dial's index, armed buttons, selected segment |
| `--ok` `--warn` `--err` `--info` | #4bb894 #cf9f2e #e6533f #5aa0d6 | state; `--info` = work in progress (busy) |
| `--r-panel` 14px · `--r-control` 999px · `--r` 3px | | panels · pills · fields and LEDs |
| `--elev-1` / `--elev-2` | ring + top-light + bottom shade | the only depth; no cast shadows, no glow |
| `--dial` 40px (compact 32, large 48) | NCD | dock dial diameter |

Fonts: **Manrope Variable** (self-hosted woff2 + OFL licence, copied from
HubVoice's `static/fonts`) for all prose and names; **IBM Plex Mono** for
every number, readout, label and tag — self-host it too (OFL), do not rely on
a system install. Type sizes are Cerveau's five roles only: 9.5 / 11.5 / 13 /
15 / 21 px. `font-variant-numeric: tabular-nums` on every number.

## The dock — "the rail"

A floating capsule, vertical by default, horizontal on request.

- **Body**: `--s1`, radius 999px, `--elev-2`. Padding 6px. No title, no
  wordmark on the rail itself.
- **Grip**: the leading 14px of the rail: three `--faint` 2px dots, engraved.
  Drag anywhere on the rail body that is not a dial to move it. Snaps to the
  nearest screen edge within 24px.
- **Dial** (one per widget), after DeadWhispr's microphone: an outer
  machined rim (`--s2`, `--elev-1`) and an inset recess (`--bg`,
  `inset 0 2px 4px rgba(0,0,0,.5)`), the widget's **mark in monochrome**
  (`--muted`, 45 % of the dial; `--text` on hover). Marks are not brand
  coloured: shape identifies the widget, colour is kept for state.
- **LED**: a square `Dot` (6px, radius 1px) sitting on the rim at 45°, like a
  channel lamp. `ok` → `--ok` at 70 % opacity; `warn` → `--warn`;
  `crit` → `--err`; `idle`/unknown → `--faint`; `busy` → `--info`.
- **Rim ring**: only for `crit` (2px `--err` ring that breathes 2.4 s, the
  only looping motion in a healthy-or-not dock) and `busy` (a 2px `--info`
  progress arc from 12 o'clock, clockwise, `progress` of the way round; an
  indeterminate 90° arc rotating when `progress` is null).
- **Readout**: under each dial, the snapshot's `glance.text` in Plex Mono
  9.5px, `--dim`, coloured with its tone only when `warn`/`crit`. At most 5
  characters (`412W`, `86%`, `12/14`, `q 3`). Compact density hides
  readouts; that is its whole difference besides dial size.
- **Open dial**: while its panel is open the dial's recess darkens one step
  and a 2px × 10px `--accent` index tick appears on the rim side that faces the
  panel. This is the dock's only use of the accent.
- Order follows `order` in the snapshot, reorderable in Settings. A widget
  disabled in Settings is not on the rail.

## Hover choreography

- Pointer enters a dial → panel opens after **120 ms** dwell (sweeping across
  the rail does not flash panels).
- While any panel is open, entering another dial switches the panel
  **immediately**, no close/open animation — the menu-bar model.
- Leaving the dial and the panel → panel closes after **280 ms** grace;
  re-entering either cancels it.
- The panel opens perpendicular to the rail, on the side with more room,
  8px from the rail, vertically centred on the dial and clamped to the
  monitor's work area. It never covers another dial.
- Opening never takes keyboard focus from the user's application. Escape,
  when the panel does have focus, closes it.
- Open: 140 ms fade + 3px slide from the rail (`--ease-out`). Close: 100 ms
  fade. Reduced motion: no slide, instant.

## The panel

One surface, no nested cards.

- `--s1`, `--r-panel`, `--elev-2`, 1px `--line2` edge. Width from the
  snapshot (`360` default). Max height = work area − 32px; the body scrolls,
  the header and footer do not.
- **Header**, one row, 48px: 18px mark in `--text` · title (Manrope 15/600)
  · flexible space · state as `LED + stateLabel` (Manrope 12.5/600, tone
  colour; `ok` state label is `--muted`, not green — healthy is quiet) · poll
  tag (`20s`, Plex Mono 9.5, `--faint`, 1px `--line2` box, `--r-sm`). No
  coloured banner blocks.
- **Pages** (two or more): Cerveau `Segmented` under the header, left-aligned
  at the body inset. Hovering a segment for 180 ms switches to it; click also
  works. A page with problems carries a tone LED and its badge count.
- **Section**: a printed strip — mono 9.5px uppercase, `letter-spacing
  .22em`, `--faint` (tone colour when the section is an alert) on the left;
  its `meta` as a mono tag on the right; a `--line` hairline above, except the
  first. One line. Never a stacked eyebrow over a title.
- **Row**: 30px min height; optional LED; name in Manrope 13 `--text`;
  optional `sub` under it in 11.5 `--dim`; value right-aligned in Plex Mono 12
  `--muted` (tone colour only for `warn`/`crit`). Actions sit after the value
  as small pills and appear at full opacity only on row hover (they stay
  keyboard-focusable).
- **Meter**: label · Cerveau segmented level meter (3px segments, 2px gap,
  fills the column, lit segments in the tone colour, unlit `--line2`) ·
  value in mono. Thresholds belong to the core, the meter only shows the tone.
- **Limit**: label · segmented meter · `54%` · `resets 14:00` in `--faint`.
- **Stats**: one mono line, `key value · key value`, keys `--faint`, values
  `--text`. It replaces the Python "tiles"; there are no KPI tiles.
- **Chips**: a grid of `LED + label` with no box around each item. Healthy
  items are `--muted`; a non-healthy item leaves the grid and becomes a full
  row with words above it.
- **Alert**: the problem block. A 3px tone bar is **not** used. Layout: LED ·
  title (13/600) and meta (mono 11.5, tone colour) on one line · body under it
  in `--muted` (Plex Mono when it is a log line) · actions right. Background
  `color-mix(in srgb, var(--tone) 6%, var(--s1))`, `--r` corners, 10px pad.
- **Table** (Power): mono numbers, right-aligned columns, header row in the
  printed-label style, hairline between groups only.
- **Donut** (Power cost split): SVG, 10px ring, segments on a **monochrome
  ramp** (`--text`, `--muted`, `--dim`, `--faint`, `--line2`), largest first,
  1px `--s1` gaps; centre value in Plex Mono 21, unit in 9.5. Legend is rows.
- **Bars** (Power 24 h): 24 SVG bars 3px wide, `--faint`; the current hour
  `--text`; hour ticks every 6 h in mono 9.5.
- **Buttons**: Cerveau pills, 24px tall inside panels, mono 11.5 lowercase
  (`restart`, `stop`). Armed: label becomes the confirm text and the pill
  fills `--accent-soft` with `--accent` ink (`--err` for `danger`).
- **Footer**, 28px, hairline above: `updated 12:41:07 · every 20s` in mono
  9.5 `--faint`; a collector error replaces it in `--err` with the error text.
- Empty states say what is true: "No containers", "tailscaled is not
  running — start it with `sudo systemctl start tailscaled`".

## Toasts

Beside the dial that raised them, on the panel side, stacked newest nearest
the dial, at most 3. `--s2`, 10px radius, `--elev-2`; LED + text (Manrope
12.5, max 2 lines). 5 s, then 160 ms fade. Pointer passes through them — they
never block the desktop. A toast for the widget whose panel is open is not
shown (the panel already says it).

## Settings

A separate, decorated-by-us window (Cerveau title bar with drag region,
minimise/close), 560 × 680, in the DeadWhispr 2 settings layout: section
headings in Manrope 15/600, fields 38px with `--r` corners, Cerveau
`Select`, pill buttons, Save / Discard footer. Sections: **Widgets** (on/off
switch and order per widget, one row each, with its mark), **Dock**
(orientation, density, readouts on/off, edge), **Start** (launch at login),
then one section per widget that has options (Tailscale watch list, Power
tariff and estimates, ComfyUI endpoint). Opened from the tray, from the rail's
right-click menu, or `ncd --settings`.

## Right-click and tray

Right-click on a dial opens a native context menu: `Refresh now`, the
widget's `menu` items, a separator, `Settings…`, `Quit NoClickDock`. The tray
menu: `Show dock` / `Hide dock`, `Settings…`, `Quit`. Native menus are
acceptable here; they are secondary surfaces.

## Accessibility

Visible focus (`outline: 2px solid var(--accent); outline-offset: 2px`) on
every control. Every LED has text next to it somewhere on the same surface
(the header state label, the row value) — state is never colour-only. Dial
buttons carry `aria-label="<title>: <stateLabel>"`. Reduced motion stops the
breathing ring and the indeterminate arc (the ring stays, static).

## Refused

Brand-coloured discs, KPI tiles, card grids, nested cards, coloured left
borders, eyebrow labels stacked above titles, gradients or glow, emoji,
generic icon packs standing in for real marks, fake data in production, a
"healthy" colour shouted in green text, and any motion in a healthy dock.
