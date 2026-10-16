# M2 — design review, round 1, part A (browser)

Reviewer: design supervisor, 2026-09-26.

**Tree:** a clean `git archive` of `5290a29`, the latest commit. It
compiles: `go vet` passes and svelte-check reports 0 errors and 0 warnings.
The executor's working tree has uncommitted work in progress and was not
used.

**Setup:** core `--mock` (still) on 47811, Vite on 1430.

**Contract:** DESIGN.md as amended, ARCHITECTURE's new rows (`empty`,
`/v1/variants`, M2-a..d), and main's notes (M2-main-notes.md).

**Part B**, the real X11 windows (toasts, menus, tray, fade, core-down on
screen), follows separately.

## How it was checked

- **Audit:** `shoot.mjs --audit` at 1× and 2× covered 32 surfaces: all
  seven panels, every page, every variant (Docker unavailable, ComfyUI
  offline, Tailscale unavailable, Claude empty, Disk collector error, Docker
  stale), all dial states, the rails and the toast stack. 0 findings, 0
  console errors, Manrope and Plex Mono loaded.
- **DOM measurements:** voices, column positions, row wrapping, alert
  widths.
- **Core-down:** the `ncd` dial and panel are not in the gallery, and a dead
  core in a plain browser does not trigger them. I served `coreDown()`'s
  exact snapshot from a scratch stand-in core, so the gallery's own Dial and
  Panel drew them.
- **Evidence:** invented data only, in `shots/M2/`.

## What is good

- **Renderers:** all seven new ones follow the reference.
  - Limit shares its grid.
  - Chips pull a problem out as a row with words (Claude and Codex
    *Degraded*; Tailscale `old-vm`).
  - Table carries per-cell tone (GPU0 MEM 92 °C).
  - Donut is monochrome, largest first.
  - Bars have past, now and future and the `00…24` axis.
  - Split has one hairline.
  - Empty renders backticks as command chips (Docker unavailable,
    Tailscale unavailable).
- **Variant panels:** short and truthful; each says what is wrong and what
  to type. The stale and collector-error footers read as designed.
- **Docker:** the Now page still reads problems first. The Stopped page's
  three groups are clear. The System page's prune rows have ghost pills and
  honest subs.
- **Power:** the right column (cost, today, utilisation) is exactly the
  reference.

## MUST-FIX

1. **Docker's row actions wrap onto a line of their own.** *(Row.svelte:
   `.Row { flex-wrap: wrap }`)*
   - When a row's value is long (`4/4 up · 1.2G · 6%`), its pills drop
     32 px below the name, flush left under the lamp column (shopfront,
     metrics). The next rows keep them inline, so one section has two row
     layouts.
   - **Fix:**
     - `.Row { flex-wrap: nowrap }`.
     - `.Row-value { min-width: 0; overflow: hidden; text-overflow: ellipsis }`.
     - Keep the 30 % name floor, so the value ellipsizes before anything
       wraps.
   - **Writer:** drop the redundant `up` (`4/4 · 1.2G · 6%`, as the
     reference).
   - **Evidence:** `shots/M2/docker_now@2x.png`.

2. **One voice per role: row names switch between Manrope and Plex Mono,
   and label case switches, inside one panel** (main's note 1, confirmed in
   the DOM).
   - **Tailscale:** peers are Plex Mono 11.5 under *Online* and *Watched ·
     offline*, but Manrope 13 under *Dormant* (row and chips), on one page.
   - **ComfyUI:** the `endpoint` label is mono-bold while `VRAM` and `RAM`
     are Manrope.
   - **Disk *Network*:** `/mnt/offsite` (row, mono 500, `--text`) sits under
     `/mnt/nas` (meter label, mono 400).
   - **Docker System:** `Containers`, `Images` (Engine) against `containers`,
     `images` (Disk · reclaimable).
   - **Values:** Tailscale's `Running` is capitalised among lowercase
     values.
   - **Rule** (proposed for DESIGN.md as A18):
     - A row or chip name is **Manrope 13/500**. Hostnames, container and
       peer names are names, so they use Manrope too (as Docker already
       does).
     - Filesystem **paths** are the one exception: Plex Mono 11.5/400 as the
       meter labels, for rows too.
     - **Labels are sentence case** (`Address`, `MagicDNS`, `Key expires`,
       `Endpoint`, `Images`, `Build cache`). Identifiers keep their own case.
       Values are lowercase words (`running`, `degraded performance`).
     - `row.mono` means "the name is a path"; nothing else sets it.

3. **Status-page Title Case in state labels** (main's note 2).
   - `Minor Service Outage`, `Partially Degraded Service` and
     `All Systems Operational` appear in the header and in toasts. Every
     other widget uses short sentence case (`1 container unhealthy`,
     `fileserver offline`).
   - **Fix (writer):** normalise to sentence case and prefer the specific
     fact: `console degraded`, `vs code partial outage`, `operational`.

4. **One time format.**
   - Codex resets read `resets Sep 29, 9:12am` (12-hour, comma) directly
     under `resets 16:40`. Claude uses `resets Mon 09:00`.
   - **Fix (writer):** everywhere, `HH:MM` within a day and `Ddd HH:MM`
     beyond it, 24-hour, as `clock()` already does for the footer.

5. **The model split is a headerless table** (main's note 3).
   - `opus 5 ··· 1.1M   sonnet 5 ··· 96K   haiku 4.5 ··· 12K` (Codex:
     `gpt-5-codex ··· 2.1M   gpt-5 ··· 180K`) is drawn as value-bearing
     chips without lamps. Each value is pushed to its cell's right edge, so
     the row reads as a spreadsheet.
   - **Fix:** a second `stats` line, as the reference draws it:
     `opus 5 1.1M · sonnet 5 96K · haiku 4.5 12K`. Chips stay for
     status-bearing items.

6. **Unknown is drawn as zero.**
   - Codex's `codex-mini` bucket "never reports usage", yet renders two 0 %
     meters with `resets —`.
   - ComfyUI offline shows `running 0 · pending 0`: the queue is unknown,
     not empty.
   - In both, the panel asserts something false. "Empty states say what is
     true."
   - **Fix (writer):**
     - A bucket with no data is one line of words
       (`codex-mini · never reports usage`) with no meters.
     - The ComfyUI offline page is **only** the empty sentence (see 7).
   - **Evidence:** `shots/M2/codex@2x.png`, `comfyui_offline@2x.png`.

7. **The same item twice on a page (M2-d).**
   - **ComfyUI offline:** the `endpoint` row repeats the URL that the empty
     sentence already carries as a chip.
   - **Core-down panel:** the error is in the sentence **and** again as the
     red footer, truncated there.
   - **Fix:**
     - ComfyUI offline: drop the stats and the endpoint row; the page is the
       sentence alone.
     - Core-down: keep the sentence. The footer's left side says what the
       shell is doing (`retrying every 10 s`) and the right side
       `since 03:27:52`, in `--warn`, never a second copy of the error. The
       same rule applies to any widget whose empty sentence already quotes
       its `error`.
   - **Evidence:** `shots/M2/comfyui_offline@2x.png`,
     `ncd-core-down@2x.png`.

8. **Power's left column is three tables that do not line up.**
   - Draw, Storage and Displays are separate tables with their own id
     widths (32, 26 and 39 px at 1×), so names start at 49, 43 and 56 px.
   - The W column ends at 226 in Draw but 269 in Storage and Displays.
   - The Draw table also stacks a column-header row under its section
     strip: two printed strips before the first number.
   - **Fix:**
     - One table with row groups, as the reference and the contract
       describe ("hairline between groups only").
     - The table model gains a group marker (e.g.
       `rows:[{group:"Storage", meta:"2"}, …]`), rendered as a 32 px strip
       with the hairline.
     - The header row carries the first group's title in its first cell
       (`DRAW · W / CAP · CORE · VRAM`).
     - If groups stay separate tables, the page shares one id width and one
       set of numeric column widths (the same mechanism as
       `--meter-cols`).
   - **Evidence:** `shots/M2/power@2x.png`.

9. **A lamp that contradicts its words.**
   - Tailscale's *Online* section lists `exit-fra`, `laptop` and `phone`
     with the idle lamp (`--faint`), which the contract defines as "not
     running / unknown". The section says they are online.
   - **Fix:** online peers take the `ok` lamp; a quiet one is still online.
     Drop the lamp if activity is the point, and say it in the value
     (`idle`).
   - **Related:** `build-agent` has an `ok` lamp and a fully amber value
     (`linux · relay fra · 340 KB/s`). A row's lamp and its value tone agree,
     or only the relayed part carries it.

10. **The reason for the amber dial is 570 px down the Tailscale panel.**
    - The header says `fileserver offline`, but *Watched · offline* is the
      fourth section (y 570 of a 795 px panel), after This node, Exposure
      and five healthy peers.
    - The product's own rule (README: "problems are listed first and in
      full") is not written into DESIGN.md, and it should be.
    - **Proposed A19:** when a widget is `warn` or `crit`, the block that
      explains its header comes first on its first page.
    - **Fix here:** *Watched · offline* first. The expired key (`old-vm`)
      stays in *Dormant*, where its warn row is already pulled out.

## SHOULD-FIX

1. **Alert title stops at 60 %.**
   - It stops even when there is room: Codex's incident is cut at 229 px
     with 34 px free.
   - Use `.Alert-title { flex: 0 1 auto; min-width: 0 }` and
     `.Alert-meta { flex: none }`. The ≤ 24-character meta budget
     guarantees the meta fits.
2. **Docker, writer:**
   - Thumbnailer's meta `OOM (137) · 2h` and body line 2 `OOM-killed (137) …`
     say the same thing twice. Keep the log line and the restart policy in
     the body; drop the repeat.
   - `RUNNING 10` counts containers over 5 stack rows. Use
     `5 stacks · 10 containers`.
   - `metrics` (2/3 up) has a warn lamp but a neutral value. Tone the value
     `warn`.
3. **Docker System's footer repeats the Engine rows** on the same page
   (`engine 27.3 · compose 2.29`). Drop the snapshot footer on that page, or
   omit the Docker and Compose rows.
4. **Power:**
   - USB is three bare name rows (96 px for no numbers). Make it one line
     (`keyboard · mouse receiver · audio interface`), or rows with watts.
   - Rename the `MEM` column to `VRAM`.
   - `27"` → `27″`.
5. **Codex usage:** the bucket lines (`codex · 12m ago`) are Manrope 13
   `--dim` sub-heads that are dimmer than their children. Fold the bucket
   into the limit label (`codex 5h`, `codex week`), or into the section
   meta when there is one bucket.
6. **Empty glance.** Claude with no sessions leaves an empty readout line on
   the rail (`dial/ok`). A glance is never empty; use `—`.
7. **The gallery's toast specimen contradicts M2-b.** It shows three
   widgets' toasts in one stack, worded `Claude: Minor Service Outage`.
   Draw one widget's stack from its transition texts (e.g. Docker's), so
   the specimen is something the shell can actually show.
8. **Service names:** capitalisation is mixed (`API`, `claude code`,
   `console`). Use the status page's own names (`Claude Code`, `Console`).
   This follows from rule 2.

## FYI

- ComfyUI's panel has no job block while busy. The header
  (`generating · 42%`) and the dial arc carry progress, which matches the
  spec. Fine, but a person hovering during a run sees devices first; a
  `Job` stats line (`step 12/28 · KSampler`) would earn its place if the
  websocket gives it.
- The NoClickDock mark (a vertical pill with three dots) reads a little
  like a traffic light. It is monochrome, so it is acceptable.
- Tailscale's mock MagicDNS suffix (`tail0c4fe`) appears in the mock and
  test fixtures. It is not the user's tailnet as it appears elsewhere, so
  it reads as invented; research owns the leak check. Its shot is not
  committed.

VERDICT: FAIL
