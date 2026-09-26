# NoClickDock — interface contract

NCD 2 wears **Cerveau's identity**. Visual authority, in order:
`../Cerveau/panel/src/tokens.css` (palette, type, spacing, radius, depth,
motion), Cerveau's kit (`kit/Dot.svelte` square LEDs, `kit/Meter.svelte`
segmented level meters, `kit/Segmented.svelte`, `kit/Panel.svelte` printed
header strips, `kit/Button.svelte` pills), DeadWhispr / HubVoice's **inset
circular dial** (`../DeadWhispr2`, `../Cerveau-Hub/HubVoice`), then this file.
Where they disagree, this file wins for NCD.

The **reference prototype** — `docs/rewrite/design/reference.html` +
`reference.css`, shots in `docs/rewrite/reviews/shots/reference-*.png` — is
this contract drawn. Its CSS is organised by the Svelte component names and
its geometry is binding; lift it rather than re-derive it. The rationale for
each rule marked (A*n*) is in `docs/rewrite/design/amendments.md`.

The aesthetic is Cerveau's: TP-7 / Japanese old-school-modern, minimalist
industrial. Monochrome surfaces, one signal accent, hairlines, printed mono
labels, mechanical precision, generous negative space.

## The rule NCD adds

**If you have to click to find out, the widget has failed.** Everything a
person checks is readable from the dock or from a hover. Clicks exist only to
change the world (stop a container) and are always two-step.

**Colour is signal, never decoration.** A healthy dock is monochrome and
still. The only things that move are problems and work in progress. Green
lives on lamps only — never on bars or text (A6).

## Tokens

Copy `tokens.css` from Cerveau verbatim into `app/src/lib/tokens.css`, keeping
its comments; NCD adds only the tokens below. Never retype a hex value in a
component.

| token | value | use |
|---|---|---|
| `--bg` `--s1` `--s2` `--s3` | #161616 #1C1C1C #242424 #2B2B2B | surface ladder |
| `--recess-open` | #111111 | NCD: the open dial's recess (A4) |
| `--line` / `--line2` | #2E2E2E / #3D3D3D | structure / control edge |
| `--text` `--muted` `--dim` `--faint` | #FAFAFA #A1A1AA #71717A #52525B | ink |
| `--accent` | #E54866 cherry | interaction only: focus rings (not on dials), the open dial's index tick, armed buttons |
| `--ok` `--warn` `--err` `--info` | #4bb894 #cf9f2e #e6533f #5aa0d6 | state; `--info` = work in progress (busy) |
| `--r-panel` 14px · `--r-control` 999px · `--r` 3px · `--r-toast` 10px | | panels · pills · fields and LEDs · toasts |
| `--elev-1` / `--elev-2` | ring + top-light + bottom shade | the only depth; no cast shadows, no glow |
| `--dial` 40px (compact 32, large 48) | NCD | dock dial diameter |

**Ink (A3).** Anything a person reads is at least `--dim` (section titles,
section meta, stats keys, reset times, row subs, footer text). `--faint` is
only for what nobody needs to read: grip dots, `·` separators, the footer
freshness stamp, unlit or idle lamps.

Fonts: **Manrope Variable** (self-hosted woff2 + OFL licence, copied from
HubVoice's `static/fonts`) for all prose and names; **IBM Plex Mono** for
every number, readout, label and tag — self-host it too (OFL), do not rely on
a system install. Type sizes are Cerveau's five roles only: **9.5 / 11.5 /
13 / 15 / 21 px** — no 12 or 12.5 anywhere (A1). `tabular-nums` on every
number.

**CSS traps (A16).** `tokens.css` ships global utilities `.label`, `.tag`,
`.mono`, `.elev-1/2`, `.blink` — never name a component part after them. The
`font:` shorthand resets `font-variant-numeric`; end the global stylesheet
with `:is(body, body *) { font-variant-numeric: tabular-nums !important }`.
Override tokens' `:focus-visible` (1px, offset 1) with this contract's 2px,
offset 2.

## The dock — "the rail"

A floating capsule, vertical by default, horizontal on request.

- **Body**: `--s1`, radius 999px, `--elev-2`. Padding 6px (10px at the
  trailing end of a vertical rail with readouts). No title, no wordmark.
- **Grip**: the leading 14px of the rail: three `--faint` 2px dots, engraved.
  Drag anywhere on the rail body that is not a dial to move it. Snaps to the
  nearest screen edge within 24px.
- **Slot** = dial + 4px + readout line (12px), 8px between slots (A13).
- **Dial** (one per widget), after DeadWhispr's microphone: an outer
  machined rim (4px, `--s2`, `--elev-1`) and an inset recess (32px at 40,
  `--bg`, `inset 0 2px 4px rgba(0,0,0,.5)`), the widget's **mark in
  monochrome** (18px box at 40; `--muted`, `--text` on hover; `--dim` when
  idle (A15)). Marks are never brand coloured: shape identifies the widget,
  colour is kept for state.
- **Lamp** (A5): a square `Dot`, 6px (5px at compact 32), radius 1px, centred
  on the 45° radius and pulled in until its outer corner is 1px inside the
  dial outline (`r = dial/2 − 1 − led·0.7071`), with a 1px `rgba(0,0,0,.55)`
  gasket so it reads as set into the bezel — never a badge poking out.
  `ok` → `--ok` at 70 % opacity; `warn` → `--warn`; `crit` → `--err`;
  `idle`/unknown → `--faint`; `busy` → `--info`.
- **Rim ring**: only for `crit` (2px `--err` ring on the outer edge of the rim
  that breathes 2.4 s — the only looping motion a dock may have while nothing
  is working) and `busy` (a 2px `--info` progress arc from 12 o'clock,
  clockwise, `progress` of the way round; an indeterminate 90° arc rotating
  1.2 s when `progress` is null).
- **Readout**: under each dial, `glance.text` in Plex Mono 9.5px, **`--muted`**
  (`--dim` when idle), tone colour only when `warn`/`crit` (A3). At most 5
  characters (`412W`, `86%`, `12/14`, `q 3`). Compact density hides
  readouts; that is its whole difference besides dial size.
- **Open dial** (A4): while its panel is open the recess is `--recess-open`,
  the mark `--text`, and a 2 × 10px `--accent` index tick sits **on the rail
  wall facing the panel, 4px off the rim** — not on the rim, where it would
  merge with a crit ring. This is the dock's only use of the accent.
- **Focus** (A17): a dial's focus ring is 2px `--text`, offset 2 — never
  cherry, which reads as the crit ring at 40px.
- **Stale** (A11): when a widget has sent no snapshot for **3 × pollSecs**,
  the UI marks it stale: ring and arc removed, lamp idle, mark `--dim`,
  readout `--faint`. A status dock must never keep showing an old green.
- Order follows `order` in the snapshot, reorderable in Settings. A widget
  disabled in Settings is not on the rail.

## Hover choreography

- Pointer enters a dial → panel opens after **120 ms** dwell (sweeping across
  the rail does not flash panels).
- While any panel is open, entering another dial switches the panel
  **immediately**, no close/open animation — the menu-bar model. Acceptance:
  no frame ever shows an empty panel or one widget's content in another's
  geometry; pointer-to-drawn median ≤ 70 ms, worst ≤ 100 ms on the real
  Mutter desktop at 60 Hz (measured from a 120 fps capture; the pointer's own
  path to the app and one compositor frame are inside that budget). The shell
  prepares the dial the pointer is approaching so the swap is ready on arrival.
- Leaving the dial and the panel → panel closes after **280 ms** grace;
  re-entering either cancels it.
- The panel opens perpendicular to the rail, on the side with more room,
  8px from the rail, centred on the dial and clamped to the monitor's work
  area. It never covers another dial.
- Opening never takes keyboard focus from the user's application. Escape,
  when the panel does have focus, closes it.
- Open and close are **instant** — no fade, no slide (amended 2026-09-26: on
  Mutter, window-level fades composite unreliably and fight the two-window
  swap). The 120 ms dwell is the only delay, the menu-bar model. The panel
  appears fully painted in one frame and disappears in one frame.
- Page tabs switch after 180 ms hover dwell, or on click.

## The panel

One surface, no nested cards.

- `--s1`, `--r-panel`, `--elev-2` — the elevation ring **is** the edge, no
  extra border (A8); windows keep 1px of transparent margin so the ring is
  never clipped. Width from the snapshot (`360` default). Max height = work
  area − 32px; the body scrolls, the header and footer do not. Body inset
  **16px** (A13).
- **Header**, one row, 48px: 18px mark in `--text` · title (Manrope 15/600)
  · flexible space · `LED + stateLabel` (Manrope **13**/600, tone colour;
  the `ok` label is `--muted`, not green — healthy is quiet). Nothing else:
  freshness lives in the footer (A10). No coloured banner blocks.
- **Pages** (two or more): a Segmented strip (28px) under the header,
  left-aligned at the body inset, 12px below it, then a `--line` hairline.
  The selected segment is a **raised key** — `--s3` fill, `--text` ink,
  `0 1px 0 var(--lift) inset, 0 -1px 0 var(--shade) inset, 0 0 0 1px
  var(--ring)` — not a cherry fill (A2). Unselected `--dim` → `--text` on
  hover. A page with problems carries a tone LED and its badge count.
- **Section**: a printed strip, 32px tall with 8px above its `--line` hairline
  (no hairline on the first) — mono 9.5px uppercase, `letter-spacing .22em`,
  `--dim` (tone colour when the section is an alert) on the left; its `meta`
  as mono 9.5 `--dim` on the right. One line. Never a stacked eyebrow over a
  title.
- **Row**: min-height **32px**, 40px with a `sub` line (A13); optional LED
  aligned to the name line; name in Manrope 13 `--text`; `sub` under it in
  11.5 `--dim`; value right-aligned in Plex Mono **11.5** `--muted` (tone
  colour only for `warn`/`crit`).
- **Actions** (A7): ghost pills at rest (transparent body, 1px `--line`
  outline, `--dim` ink, 24px tall, mono 11.5 lowercase); on row hover or
  `:focus-within` they take the Cerveau pill body (`--s2`, `--line2`,
  `--muted`). A pill's width is the larger of `label` and `confirm` (both in
  one grid cell, the inactive one `visibility:hidden`), so arming never moves
  the row. Armed = confirm label, `--accent-soft` fill, `--accent` ink
  (`--err` for `danger`), no countdown ornament.
- **Meter** (A6): label · Cerveau segmented level meter (3px segments, 2px
  gap, fills the column, drawn as an SVG of integer rects with
  `shape-rendering: crispEdges`) · value in mono. Lit segments are `--muted`
  when the tone is `ok` or absent, `--warn`/`--err` only for warn/crit;
  unlit `--line2`. All `meter` blocks on **one page** (and, separately, all
  `limit` blocks) share one label column, so every bar on the page starts on
  the same x even across sections. Thresholds belong to the core.
- **Limit**: label · segmented meter · `54%` · `resets 14:00` in `--dim`.
- **Stats**: one mono line, `key value · key value`, keys `--dim`, values
  `--text`. It replaces the Python "tiles"; there are no KPI tiles.
- **Chips** (A13): a grid of `LED + label`, no box around each item, Manrope
  11.5/500 `--muted`, 24px tall, 3 columns at 400px, ellipsis. A non-healthy
  item leaves the grid and becomes a full row with words above it.
- **Alert** (A12): LED · title (13/600) and meta (mono 11.5, tone colour, ≤ 24
  characters — exit code, flag, age) on a fixed 24px title line with the
  actions at its right · body under it in `--muted`, 2 lines max then
  ellipsis (Plex Mono when it is a log line). Background
  `color-mix(in srgb, var(--tone) 6%, var(--s1))`, `--r` corners, 8px
  padding with a −8px horizontal bleed so LED and text stay on the 16px
  inset. No coloured bar. Snapshot writers keep meta within budget; the rest
  goes in the body.
- **Table** (Power): mono numbers, right-aligned columns, header row 32px in
  the printed-strip style, hairline between groups only.
- **Split**: two equal columns, 24px gutter, one 1px `--line` divider.
- **Donut** (Power cost split): SVG, 10px ring, segments on a **monochrome
  ramp** (`--text`, `--muted`, `--dim`, `--faint`, `--line2`), largest first,
  1px `--s1` gaps; centre value in Plex Mono 21, unit in 9.5. Legend is rows.
- **Bars** (Power 24 h, A9): 24 bars filling the column — width
  `floor((w − 23·2) / 24)`, 2px gap, 40px tall, `crispEdges`. Past hours
  `--faint`, current hour `--text`, future hours a 1px `--line2` floor stub.
  Axis `00 06 12 18 24` in mono 9.5 `--dim`.
- **Footer**, 28px, hairline above (A10): `snapshot.footer` on the left
  (`--dim`), `12:41:07 · 20s` on the right (`--faint`). A collector error
  turns it `--err`: the error on the left, `showing <last good time>` on the
  right. Stale: `--warn`, `no reading since 12:38:02`, and the header state
  reads `stale`.
- **Empty state** (A13): one sentence in Manrope 13/500 `--muted` that says
  what is true, with any command in a mono chip (`--s2`, 1px `--line2` inset,
  `--r`): "tailscaled is not running on this machine. Start it with
  `sudo systemctl start tailscaled`".

## What the panel says (rules for snapshot writers)

- **Problems first (A19).** When a widget is `warn` or `crit`, the block that
  explains its header state is the first thing on its first page. The
  healthy majority comes after, condensed.
- **One voice per role (A18).** A row or chip name is Manrope 13/500 —
  hostnames, container and peer names included. Filesystem **paths** are the
  one exception: Plex Mono 11.5/400, for meter labels and rows alike;
  `row.mono` means "the name is a path" and nothing else sets it. Labels are
  sentence case (`Address`, `MagicDNS`, `Key expires`, `Build cache`);
  identifiers keep their own case; values are lowercase words (`running`,
  `degraded performance`).
- **State labels** are short sentence case and name the specific fact
  (`console degraded`, `fileserver offline`, `2 containers unhealthy`) —
  never a status page's Title Case phrase.
- **One time format.** 24-hour `HH:MM` within the day, `Ddd HH:MM` beyond it
  (`resets 16:40`, `resets Mon 09:00`); durations as `12m`, `3h`, `2d`.
- **Unknown is never zero.** A value that was not measured is absent or said
  in words (`never reports usage`), never a `0`, `0 %` or empty meter. An
  unreachable service's page is its empty sentence alone.
- **Nothing twice.** An item appears once per page (ARCHITECTURE M2-d); a
  footer never repeats the error the empty sentence already states — it says
  what the app is doing about it (`retrying every 10 s · since 03:27:52`).
- A row's lamp and its value tone agree.

## Toasts

Beside the dial that raised them, on the panel side, stacked newest nearest
the dial, at most 3, 6px apart. `--s2`, `--r-toast`, `--elev-2`, padding
9/14/9/12; LED + **one plain sentence** (Manrope 13/500, max 2 lines, no mono
fragments). Appears instantly; after 5 s the **shell** fades the toast
window's compositor opacity (`_NET_WM_WINDOW_OPACITY`) to 0 over 160 ms and
then parks it — WebKit stops painting a click-through window, so the fade
cannot live in the page. Reduced motion: instant dismiss. Pointer passes through them on Linux
(on Windows, where click-through is unreliable, they dismiss on hover).
While **any** panel is open, toasts wait and appear when it closes; a toast
for the widget whose panel is open is dropped — the panel already says it
(A14).

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

Visible focus on every control: 2px `--accent`, offset 2 (dials: `--text`,
A17). Every LED has text next to it somewhere on the same surface (the
header state label, the row value) — state is never colour-only. Dial buttons
carry `aria-label="<title>: <stateLabel>"`. Reduced motion stops the
breathing ring and the indeterminate arc (the ring stays, static).

## Refused

Brand-coloured discs or marks, KPI tiles, card grids, nested cards, coloured
left borders, eyebrow labels stacked above titles, gradients or glow, emoji,
generic icon packs standing in for real marks, green bars or green text for
"healthy", a cherry-filled tab, fake data in production, and any motion in a
healthy, idle dock.
