# M2 — design review, round 2, part A (browser)

Reviewer: design supervisor, 2026-09-26.

**Tree:** a clean `git archive` of `1b1a85b` (HEAD, compiles: `go vet`
passes, svelte-check 0 errors and 0 warnings). It contains the part-A fixes
`5c83d5b`, `38209da` and `dab3002`.

**Setup:** core `--mock` on 47811, Vite on 1430.

**Contract:** DESIGN.md, including "What the panel says" (A18, A19, time
format, unknown-never-zero, nothing twice) and ARCHITECTURE's table
`group?/meta?` rows.

**Evidence:** mock data only, in `shots/M2r2/`.

## How it was checked

- **Audit:** `shoot.mjs --audit`: 34 surfaces (now including
  `panel/ncd/core-down` and `panel/claude/unreachable`), **0 findings, 0
  console errors**, Manrope and Plex Mono loaded.
- **DOM sweep over every panel:**
  - which names render in Plex Mono;
  - Title Case in header states;
  - zeros for unmeasured values;
  - 12-hour or `Mon 5,`-style times;
  - empty dial glances.
- **Visual pass:** every panel at 2×.

## Part-A must-fix — all ten closed

| # | r1 must-fix | result |
|---|---|---|
| 1 | Docker actions wrap | **fixed.** Every row stays on one line (`4/4 · 1.2G · 6%`; `up` dropped from stack rows); the value ellipsizes before anything wraps. |
| 2 | one voice (A18) | **fixed.** Across all panels the only Plex Mono names are Disk's paths and its pool dataset (`/`, `/home`, `tank/media`, `/mnt/offsite`), with rows and meter labels in the same 400/11.5 voice. Labels are sentence case (`Address`, `MagicDNS`, `Key expires`, `Endpoint`, `Images`, `Build cache`); values are lowercase (`running`). |
| 3 | Title Case states | **fixed.** `Console degraded`, `VS Code partial outage`, `operational`, `status page unreachable`. None found by the sweep. |
| 4 | one time format | **fixed.** `resets 18:30`, `resets Mon 09:00`, `resets Tue 14:41`. No 12-hour or dated strings on any mock surface (but see M4-design-r1 for a Claude writer path that can still print one). |
| 5 | model split as a table | **fixed.** A second stats line (`opus 5 1.1M · sonnet 5 96K · haiku 4.5 12K`), which wraps without a leading separator on real data. |
| 6 | unknown drawn as zero | **fixed.** `codex-mini · never reports usage` as words, no meters. ComfyUI offline is its sentence alone. |
| 7 | the same item twice | **fixed.** ComfyUI offline shows the endpoint once, as the command chip. The core-down footer says `retrying every 10 s · since 03:27:52`. Docker System no longer repeats the engine versions (the counts moved into the subs). |
| 8 | Power's tables misaligned | **fixed.** One table with row groups (Draw in the header row; Storage, Displays and USB as group strips with meta). Names start on one x and W / Core / VRAM right-align on one set of columns. The 1.2-part name column gives the table room. |
| 9 | lamp contradicts words | **fixed.** Online peers are green. `build-agent`'s lamp and value are both warn. |
| 10 | problems first (A19) | **fixed.** Tailscale opens with *Watched · offline*; Claude and Codex open with *Degraded* and *Active incidents*, then usage. |

**Should-fixes taken** (all verified):
- alert title takes the free width;
- Docker writer: no OOM repeat, `5 stacks · 10 containers`, metrics value
  toned warn;
- Power: USB as one row plus a list line, `MEM` → `VRAM`, `27″`;
- Codex bucket folded into the limit label (`codex 5h`);
- glances never empty (`—`);
- the toast specimen is one widget's stack;
- service names use the status pages' own spelling.

**Tracked elsewhere, not re-counted here:** the header-label descender clip
is M2-design-r2b must-fix 2, already queued. It also shows in the browser
(`generating`, `unhealthy`).

## SHOULD-FIX

1. **A long service name loses its distinguishing tail.** In three columns
   at 440 px, `Claude for Government` becomes `Claude for Govern…`. Use two
   columns when a chip label would ellipsize, or have the writer use the
   short name the status page offers.

VERDICT: PASS
