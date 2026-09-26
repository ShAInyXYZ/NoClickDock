# Power page (the HTML the core writes to PM-Log): port to the Cerveau identity

This is a single pass over `core/internal/widgets/power/report_template.html`,
and the layout stays as it is.

- Result on the invented 25-day log: `evidence/report-cerveau.png`.
- Before: `evidence/report-before.png`.

All 30 edits below were applied to a generated page in scratch. That leaves
no old colour literal, no tile and no eyebrow, and gives 0 console errors,
with both fonts loaded.

## 1. Style

- Replace everything inside `<style>…</style>` with `report.css`.
- Embed its three fonts in the binary (`go:embed`):
  - `app/static/fonts/manrope-latin-wght-normal.woff2`;
  - `docs/rewrite/design/fonts/ibm-plex-mono-latin-{400,500}-normal.woff2`.
- When writing the page, turn each `url("fonts/<file>")` into
  `url("data:font/woff2;base64,<base64>")`, passed as `template.CSS`. The
  page is self-contained (about 100 KB) and never falls back to DejaVu.

## 2. Markup

| find | replace |
|---|---|
| `<div class="k">NoClickDock · Power</div>` (the eyebrow over the title) | delete the line |
| `<div class="tiles" id="tiles">` | `<div class="stats" id="tiles">` |
| `<div class="tiles" id="recentTiles">` | `<div class="stats" id="recentTiles">` |
| `<section class="card" style="margin-top:8px">` | `<section class="card">` |
| legend `background:#4a4640` | `background:var(--ch-base)` |
| legend `background:var(--amber)` | `background:var(--ch-hi)` |
| legend `background:var(--fg);height:2px` | `background:var(--ch-top);height:2px` |
| legend `background:var(--gpu)` / `var(--cpu)` / `var(--other)` | `var(--ch-hi)` / `var(--ch-mid)` / `var(--ch-base)` |
| legend `background:#3a3631` | `background:var(--ch-back)` |
| legend `background:var(--fg)` (typical-day average) | `background:var(--ch-hi)` |
| legend `background:rgba(232,135,58,.18)` | `background:var(--ch-band);box-shadow:inset 0 0 0 1px var(--line2)` |
| "the amber cells are the hottest tenth of all hours" | "the white cells are the hottest tenth of all hours" |

The cards and the findings' left border need no markup change: `report.css`
draws `.card` as a hairline-topped section and `.findings` as plain
sentences.

## 3. Script

Add, after `const $ = …`:

```js
const V = n => getComputedStyle(document.documentElement).getPropertyValue(n).trim();
```

| find | replace |
|---|---|
| `fill: partial ? "transparent" : "#4a4640",` | `fill: partial ? "transparent" : V("--ch-base"),` |
| `stroke: partial ? "#5a5650" : "none"` | `stroke: partial ? V("--dim") : "none"` |
| `fill: "#e8873a", opacity: .85}` | `fill: V("--ch-hi")}` |
| `stroke: "#efece6", "stroke-width": 1.5}` | `stroke: V("--ch-top"), "stroke-width": 1.5}` |
| `[["gpu", "#6da46a"], ["cpu", "#7b9bd1"], ["other", "#6f6a62"]]` | `[["gpu", V("--ch-hi")], ["cpu", V("--ch-mid")], ["other", V("--ch-base")]]` |
| `fill: "rgba(232,135,58,.18)"}` | `fill: V("--ch-band")}` |
| `fill: "#3a3631"}` | `fill: V("--ch-back")}` |
| typical day `fill: "#efece6"}` | `fill: V("--ch-hi")}` |
| `fill: i === hi ? "#efece6" : "#4a4640"}` | `fill: i === hi ? V("--ch-top") : V("--ch-base")}` |
| heat: `v === null ? "#161411" : t >= 0.9 ? "#e8873a" : ` + the `rgba(239,236,230,…)` ramp | `v === null ? V("--ch-empty") : t >= 0.9 ? V("--ch-top") : ` + `rgba(250,250,250,${(0.04 + 0.5 * t).toFixed(2)})` |
| `[["gpuT", "gpuTmax", "#6da46a", 0.14], ["cpuT", "cpuTmax", "#7b9bd1", 0.52]]` | the same with `V("--ch-hi")` and `V("--ch-mid")` |
| `fill: "none", stroke: "#6f6a62"}` | `fill: "none", stroke: V("--dim")}` |
| "the amber part of each bar was used in peak-tariff hours" | "the lighter part of each bar was used in peak-tariff hours" |

**Headline tiles become one stats line**, in both places:

```js
$("#tiles").innerHTML = tiles.map(([l, v]) => `<span>${l.toLowerCase()}<b>${v}</b></span>`).join("<i>·</i>");
// recentTiles: the same .map(([l, v]) => …).join("<i>·</i>") instead of the tile markup
```

**Optional, same pass:** write "1 gap" rather than "1 gaps" in
`#qualitySub`.
