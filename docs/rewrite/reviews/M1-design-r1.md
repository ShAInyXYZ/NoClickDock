# M1 — design review, round 1

Reviewer: design supervisor, 2026-09-26. Reviewed 5856a2a and 8466166 against
DESIGN.md (as of 7542561 and 276ef19), the reference prototype, and M1-exec.md.
M1 scope: dial states, rail, panel chrome, and the section, row, meter, note,
stats and alert renderers. `limit chips table donut bars split`, toasts, the
context menu and the tray are M2. They are noted at the end and did not count
against this verdict.

## How it was checked

- **Browser:** `ncd-core --mock --port 47811` (built from HEAD) and
  `VITE_NCD_CORE=http://127.0.0.1:47811 pnpm dev --port 1430`.
  `shoot.mjs --audit` at 1× and 2× covered 28 surfaces with 0 findings,
  0 console errors, and Manrope + IBM Plex Mono loaded.
- **Geometry and computed styles:** measured in the DOM against DESIGN.md.
- **Interactions** (Playwright): tab dwell, arming, focus, reduced motion.
- **Maximum content:** the gallery pointed at a scratch stand-in core (via
  `?core=`) serving the mock state with names, subs, meta, bodies, stats,
  labels and glance text pushed past their budgets.
- **Real windows:** `cargo tauri build --no-bundle` at HEAD into a scratch
  `CARGO_TARGET_DIR`, then `ncd --mock --instance review` on X11 `DISPLAY=:0`
  (two 3840×2160 monitors, text scale 1.5). The rail was moved to the upper
  monitor through the instance's own scratch config, so it stayed clear of
  the research agent's instance.
  - Hover was driven with `xdotool`, and map state and geometry polled with
    `xwininfo`.
  - `_NET_ACTIVE_WINDOW` was sampled before, during and after each step.
  - Stale was forced by `SIGSTOP`ing my own sidecar for about 70 s.
  - The pointer was restored and every process was killed afterwards.
- **Where the evidence is:** real-desktop captures stay in the design
  scratchpad (`…/scratchpad/design/m1/x11/`) and are **not committed**.
  Browser evidence (invented data only) is in `shots/M1/`.

## What passes

- **Tokens:** `tokens.css` is byte-for-byte the design tokens, followed by the NCD block.
  The focus override, tone map, density geometry and the tabular-nums
  invariant come last. No off-role font size anywhere (the DOM holds only
  9.5, 11.5, 13, 15 and 21 px).
- **Rail:** 52 wide, padding `6 6 10`, slot pitch 64.
- **Dial:** 40px. The lamp sits exactly on the A5 formula (6.56 px from top
  and right) with its gasket. The crit ring breathes 2.4 s. The progress arc
  starts at 12 o'clock and the spinner turns at 1.2 s. The open state has the
  darker recess, lit mark and cherry index tick on the correct rail wall.
  Idle and stale marks go `--dim`.
- **Readout:** Plex 9.5/12 `--muted`.
- **Motion:** a healthy dial has 0 running animations.
- **Panel:** 440 wide from the snapshot, `--elev-2` edge with no border. The
  header is 48 (title 15/600, state 13/600 in tone, `ok` in `--muted`). The
  Segmented strip is 28 with 12 below and a raised selected key. Sections are
  32, 0.22em, `--dim` or tone. Rows are 32/40 with the value in Plex 11.5
  `--muted` and the sub in 11.5 `--dim`. The footer is 28, `--dim` / `--faint`.
- **Alerts:** 56 = 8 + 24 + 16 + 8, with the lamp centred on the title.
- **Meters:** crisp integer SVG, neutral unless warn or crit, and one grid per
  run.
- **Pills:** ghosts at rest (transparent, `--line`, `--dim`), 24 tall.
  Arming keeps the pill width **and** the row value's x unchanged. Armed is
  `--accent` on `--accent-soft` with `--accent-line`, and it disarms after
  4 s.
- **Focus:** 2px `--accent` offset 2 on pills; 2px `--text` on dials.
- **Reduced motion:** the crit ring is static at full opacity and the spinner
  stops. The panel reveal is a 140 ms fade plus a 3px slide from the rail,
  and none at all under reduced motion.
- **Collector error footer:** `--err`, with `showing <time>`.
- **Stale:** works in the browser and in the real shell. After the sidecar was
  paused, every dial dropped its ring, its lamp went idle and its readout went
  `--faint`, and all of it recovered within 4 s of resuming.
- **Tab dwell:** 186 ms, and a 90 ms pass over a tab does not switch.
- **Real shell, hover:**
  - A 60 ms-per-dial sweep over all seven dials opened nothing.
  - Grace to unmap measured 311 ms (280 plus about 30 ms of pipeline).
  - Re-entering at 150 ms cancels the close.
  - Travel from the dial into the panel keeps it open, tested at 6, 15 and
    40 px steps, by pointer warp, and while resting inside.
- **Real shell, placement:** the panel sits 8 CSS px from the rail and exactly
  centred on the dial (panel centre 1055.5 vs dial 1054.5, physical px). It
  never covers a dial, and the index tick faces the panel.
- **Focus is never taken:** `_NET_ACTIVE_WINDOW` stayed on the user's own
  application through every open, switch, in-panel and close step. The only
  change during the session was the user moving between two of their own
  apps.
- **Death pact:** killing `ncd` took its `ncd-core` with it.
- **WebKitGTK vs the Chromium reference:** the same Manrope and Plex Mono
  shapes and metrics, `color-mix()` alert tints identical, and consistent
  proportions at the 1.5 text scale. The rail measured 54 × 478 CSS as
  81 × 717 physical. One exception: must-fix 2.

## MUST-FIX

1. **Hovering a page tab can close the panel.** *(real shell; hover.rs `place`, panel resize)*
   - **What happens:** switching pages changes the panel's height, and the
     shell re-centres the panel on its dial, so the panel's top edge (header
     and tab strip) jumps. Docker Now → System is 723 → 537 px physical,
     which moves the top down 93 px.
   - **Reproduction:** with the pointer on the SYSTEM tab 54 CSS px below the
     panel's top edge, the page switched at 241 ms and the panel moved from
     y 694 to 787. That left the pointer 12 px above the new top edge, the
     panel page got a leave, and the panel **unmapped at 493 ms**.
   - **Also:** when the panel does stay open, the tab under the pointer jumps
     24–93 px on every switch.
   - **Fix:**
     - Once a panel is placed, a size change for the **same widget** never
       moves its header.
     - For left and right panels, keep the top edge and grow or shrink
       downward; if the bottom would leave the work area, shift up only by
       the overflow.
     - For a panel above a bottom rail (its bottom edge is pinned to the
       rail), give it a stable height: the tallest of its pages, measured
       once per open, clamped to work area − 32.
     - Re-centre on the dial only on open and on widget switch.
     - This also covers a snapshot that grows while the pointer is inside.
   - **Evidence:** `…/m1/x11/tabjump.py` timeline (scratchpad).

2. **The elevation ring renders 3–4× too bright on the real windows.** *(real shell; Rail, Panel)*
   - **Measured:** the ring should be `0 0 0 1px rgba(255,255,255,.12)`,
     which measures ≈ 52 over the dark desktop (Chromium: ≈ 50).
   - On WebKitGTK the rail's ring measured **147–157 grey on every sample**,
     both sides and around the caps.
   - Three of four panel captures measured **152–189** (Codex, Claude, Disk).
     Only the Docker panel, sampled just after a tab interaction, measured 52.
   - **Effect:** on the desktop the dock and panels wear a light-grey
     outline, the brightest line in the UI. That is exactly the bordered-card
     look A8 removed, and the contract's "the elevation ring is the edge"
     becomes "a white border".
   - **Likely cause:** a semi-transparent pixel over the transparent window
     area is composited without premultiplication when WebKit is not in
     accelerated-compositing mode. The panel's 140 ms reveal animation
     briefly promotes it to a layer, which would explain the one correct
     sample.
   - **Fix (robust, and identical in look):** on overlay surfaces, draw the
     ring **inset**, over the opaque body, so no translucent pixel ever sits
     over the transparent window:
     `.Rail, .Panel { box-shadow: inset 0 0 0 1px var(--ring-strong), 0 1px 0 0 var(--lift-strong) inset, 0 -1px 0 0 var(--shade) inset; }`
     The edge then measures ≈ 55 over `--s1` everywhere. The 1px
     window margin is no longer needed; keep it or drop it, but keep the
     window and page sizes consistent.
   - **If you prefer to fix the root cause instead** (e.g. WebKit
     `hardware-acceleration-policy: always` on the overlay webviews), verify
     it the same way.
   - **Verify:** sample the physical pixels of the rail and panel edges over
     a dark area on X11. Both must read within ±6 of each other (≈ 52–55),
     and the value must not change after the reveal animation ends.
   - The reference prototype adopts whichever fix main accepts. Main to fold
     the inset form into DESIGN.md (A8) if chosen.
   - **Evidence:** `…/m1/x11/real-{claude,codex,disk}.png` and
     `rail-rest.png` (scratchpad; pixel dumps in this review's log).

3. **The row lamp is not on the name line.** *(Row.svelte)*
   - **Measured:** the lamp's centre is **4.0 px below** the name line in
     every two-line row (storefront, postgres, cache, metrics) and 1 px below
     in single-line rows.
   - **Cause:** `margin-top: 10px` on a `flex-start` child cannot be right
     for both row heights. The reference prototype had the same error. It is
     **corrected in this round** (`reference.css` → Row, reference shots
     refreshed), and the corrected reference measures 0.0 px.
   - **Fix:** move the lamp into `.Row-text` and centre it on the 18px name
     line:
     ```svelte
     <span class="Row-text" class:has-led={!!block.led}>
       {#if block.led}<Led tone={block.led} />{/if}
       <span class="Row-name">…</span>{#if block.sub}<span class="Row-sub">…</span>{/if}
     </span>
     ```
     ```css
     .Row-text { position: relative; }
     .Row-text.has-led { padding-left: 16px; }            /* 6px lamp + 10px gap */
     .Row-text > :global(.Led) { position: absolute; left: 0; top: calc((18px - var(--led)) / 2); }
     ```
     Then remove `.Row > .Led`.
   - **Evidence:** `shots/M1/docker-now@2x.png` (running rows).

4. **Truncation is defeated inside flex containers: the text is hard-clipped
   mid-word with no ellipsis.** *(Meter.svelte `.Meter-label`/`.Meter-value`; PanelHeader.svelte `.PanelHeader-state`)*
   - `text-overflow` does nothing on a flex container, because its text is an
     anonymous flex item.
   - At maximum content, a long mount path shows as
     `/mnt/archive/projects/20` (cut mid-segment, looking like a real,
     shorter path).
   - A long state label shows as `…right now and` with no ellipsis. Disk's
     header label is data-driven (`<mount> 88% full`), so this is reachable
     with real mounts.
   - **Fix:** put the text in its own child and ellipsize that:
     - `.Meter-label, .Meter-value { display:block; line-height:28px; }`
       (keep `height:28px`). The value can keep `text-align:right`.
     - In the header:
       `<span class="PanelHeader-label">{…}</span>` with
       `.PanelHeader-label { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap }`.
   - The reference is updated with `.PanelHeader-label`, and `shoot.mjs
     --audit` now flags `clip-in-flex`.
   - **Evidence:** `shots/M1/disk-max-content@2x.png`.

5. **A wrapped stats line starts with a separator** (`· build cache 22G`).
   *(Stats.svelte; visible in the real Disk panel with today's mock)*
   - **Fix:** each item owns a 16px separator in front, and the container
     hides the one that begins any line:
     ```css
     .Stats { margin-left: -16px; clip-path: inset(0 0 0 16px); }
     .Stats-item::before { content: '·'; display: inline-block; width: 16px; text-align: center; color: var(--faint); }
     ```
     Then remove `.Stats-item + .Stats-item::before`.
   - Verified in the corrected reference with a forced wrap: no separator at
     the start of a line, and the box never goes past the 16px inset.
   - **Evidence:** `shots/M1/disk@2x.png` (Docker section).

6. **Colour on things that are not problems.** *(disk widget snapshot)*
   - In the Disk panel's Docker section, the row value `/var/lib/docker` (a
     path) and the note `docker system prune -a · docker builder prune`
     (commands) are `--warn` amber.
   - "Colour is signal, never decoration": the signal is the size, and
     `images 38G` and `build cache 22G` already carry it in the stats line.
   - **Fix:** no tone on that row's value (`--muted`), and the prune note
     untoned mono (`--dim`). Keep the tone on the stats values over the
     threshold.
   - **Evidence:** `shots/M1/disk@2x.png`.

7. **Meter bars start at three different x positions in one panel.** *(Blocks.svelte)*
   - Each run of meters is its own grid, so the label column resizes per
     section. In Disk, bars start at **112, 98 and 84 CSS px**
     (FILESYSTEMS, POOLS, NETWORK), and their right ends differ by up to
     4 px because the value column also varies.
   - A6 exists "so every bar starts on the same x". In the one real M1
     widget, the panel is ragged down its length.
   - **Fix:** all meters on a page share their columns.
     - Blocks measures the widest meter label and value on the page after
       render (`max-content` widths).
     - It sets `--meter-label` and `--meter-value` on `.Panel-body`, and
       every MeterGroup uses
       `grid-template-columns: min(var(--meter-label), 45%) minmax(0,1fr) var(--meter-value)`.
     - Or, equivalently, render every meter on a page as rows of one CSS
       grid with `subgrid`.
   - **Contract note for main:** extend A6 from "consecutive" to "on one
     page".
   - **Evidence:** `shots/M1/disk@2x.png`.

## SHOULD-FIX

1. **Open and switch latency.**
   - Pointer-on-dial to panel-mapped measured **288 ms** (first open) and
     **204 / 206 ms** after that, against a 120 ms dwell. The rest is the
     emit → page draw → measure → `panel_layout` → place → show pipeline
     (and the 60 ms Linux re-position).
   - A switch between dials took **144 ms** to re-place ("immediately" in
     the contract).
   - **Suggestion:** emit `panel-open` at dwell **start**, so the page draws
     and measures during the 120 ms, then place and show when the dwell
     ends. Skip the second re-position when the first landed correctly.
   - **Target:** map within 150 ms of pointer-enter, and a switch within
     50 ms.
2. **The ring stroke scales with density.**
   - The ring's `viewBox` is fixed at 40, so the crit and busy rings are
     2.4 px at large (48) and 1.6 px at compact (32) instead of 2 px.
   - Add `vector-effect: non-scaling-stroke` to `.Dial-ring circle` (the
     reference had the same flaw and is corrected).
   - **Evidence:** `shots/M1/rail-large@2x.png`.
3. **Long readouts collide in a horizontal rail.**
   - A glance longer than 5 characters overlaps its neighbours
     (`shots/M1/rail-long-readouts@2x.png`).
   - The core should enforce the 5-character budget, and the UI should
     guard it too: `.Readout { max-width: calc(var(--dial) + var(--slot-gap)); overflow: hidden; text-overflow: clip; }`.
4. **Section strip at maximum content.** The title does not truncate, and the
   meta is pushed out past the inset. Use
   `.Section-title { min-width:0; overflow:hidden; text-overflow:ellipsis }`
   and `.Section-meta { flex:none }` (reference updated).
5. **Segmented can exceed the panel width** with long labels or badges. Use
   `.Segmented { max-width:100% }`, with buttons that `min-width:0` and
   ellipsize their label.
6. **Row identity is squeezed first.**
   - With a long value and three actions, the name collapses to `med…`
     while the value keeps its full width.
   - Give `.Row-text` a floor (`min-width: 30%`) and let `.Row-value`
     `min-width:0` ellipsize before the name disappears.
   - **Evidence:** `shots/M1/docker-max-content@2x.png`.
7. **One bad block blanks the whole panel.**
   - Two actions with the same `id` in one block throw `each_key_duplicate`,
     and the panel renders nothing.
   - Key the actions each by `${i}:${action.id}`. The core (research) should
     also reject duplicate action ids in a block.
8. **Long mount paths** would read better with a middle ellipsis that keeps
   the distinguishing tail (`/mnt/…/raw-footage-backup`). This is a
   nice-to-have on top of must-fix 4.

## M2 gaps noted (not counted)

- The empty-state renderer (A13's sentence plus a command chip) does not
  exist yet. Tailscale's "not running" will need it, or a `note` convention.
- The close fade (100 ms) is not implemented.
- There is no "core not running" rail state.
- Toast window, context menu, tray, and the
  `limit/chips/table/donut/bars/split` renderers. The gallery marks them
  `· not drawn yet`, which is the right behaviour until then.

## Changes made on the design side this round

The reference prototype (`docs/rewrite/design/reference.*`) had three of
these defects itself: the row lamp, the density-scaled ring stroke, and the
stats wrap. It also lacked `.PanelHeader-label` and section truncation. All
are corrected, the reference audit is still 0 findings and 0 console errors,
and `shots/reference-*.png` is refreshed. `shoot.mjs --audit` now reports
text clipped inside flex or grid containers (`clip-in-flex`).

VERDICT: FAIL
