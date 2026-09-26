# docs/rewrite/design — the design supervisor's workbench

This folder holds what the NCD 2 interface is judged against, and the tool
that judges it. None of it ships. The executor lifts CSS from it; the
reviews cite it.

| file | what it is |
|---|---|
| `reference.html` + `reference.css` | High-fidelity static reference: the rail (every dial state, compact, horizontal, healthy), Docker, Power, Claude, Tailscale-empty and Disk-error panels, a toast stack, and the hover choreography drawn to scale. Invented data only. |
| `tokens.css` | Cerveau's `panel/src/tokens.css`, **byte-for-byte**. `reference.css` adds only the NCD tokens listed at its top. |
| `fonts/` | IBM Plex Mono 400/500/600 latin woff2 from `@fontsource/ibm-plex-mono` 5.3.0, with `IBMPlexMono-OFL.txt`. Manrope is referenced from `../Cerveau-Hub/HubVoice/static/fonts` (OFL; the app copies both into `app/static/fonts`). |
| `amendments.md` | Proposed changes to DESIGN.md (A1–A17), each with its reason and evidence. Main decides. |
| `iterations.md` | The prototype's self-critique rounds: what was wrong, and the fix. |
| `evidence/` | A/B crops the amendments cite. |
| `shoot.mjs` | The review harness (below). |

`reference.css` is organised by the Svelte component names (Led, Rail,
Dial, Readout, Panel, PanelHeader, Segmented, Section, Row, PillButton,
Alert, Meter/MeterGroup, Limit/LimitGroup, Stats, Chips, Table, Donut,
Bars, PanelFooter, Note, Empty, Toast, Split). Each block lifts into the
component's `<style>`, with the `data-*` attributes becoming props. The
canonical Dial markup is written out in the comment at the top of the
page's script.

## Viewing the reference

The Manrope URL points at the sibling `Cerveau-Hub` checkout, so serve the
GitHub directory, not this folder, and never over `file://` (Chromium
blocks `file://` fonts):

```bash
python3 -m http.server 1431 --bind 127.0.0.1 --directory ~/Documents/GitHub
# open http://127.0.0.1:1431/NoClickDock/docs/rewrite/design/reference.html
```

## How I run the app for a review

Ports are split so three agents never collide: executor 47810 / 1420,
**design 47811 / 1430**, research 47812 / 1440. The core's dev CORS
allowlist must therefore accept `http://127.0.0.1:{1420,1430,1440}` and
`http://localhost:{1420,1430,1440}`.

```bash
# 1. core with invented data, on my port (flag names follow the executor's M1 CLI)
go run ./core/cmd/ncd-core --mock --port 127.0.0.1:47811

# 2. the UI in a plain browser, on my port, pointed at my core
#    (env var name follows app/src/lib/transport.ts once it exists)
VITE_NCD_CORE=http://127.0.0.1:47811 pnpm -C app dev --port 1430 --strictPort

# 3. capture every surface at 1× and 2× into the scratchpad, with the audit
node docs/rewrite/design/shoot.mjs http://127.0.0.1:1430 "$SCRATCH/M1" --audit
node docs/rewrite/design/shoot.mjs http://127.0.0.1:1430 "$SCRATCH/M1-rm" --reduced-motion --only dial
```

When a milestone includes the Tauri shell, I also run the built windows on
X11 (`DISPLAY=:0`) briefly, capture them with `xwd`/`scrot` into the
scratchpad, and kill them. **Real-desktop captures are never committed**,
because they show the desktop around the dock. Two things this needs from
the shell:

- a way to start the real windows on invented data: `ncd --mock` passing
  `--mock` through to the sidecar;
- a way past single-instance, so my run does not hand off to the
  executor's running instance (an instance id or `NCD_INSTANCE=design`).

I never touch the running Python NCD, `~/.config/status-widgets`,
`~/.config/power-monitor`, `~/Documents/PM-Log` or autostart.

## The gallery contract `shoot.mjs` relies on

- `/gallery` renders the dock in every orientation and density, every dial
  state, every panel page of every widget from `ncd-core --mock`, the toast
  stack, and the empty, error and stale states.
- Each capturable surface has
  `data-surface="<kind>/<widget-or-variant>[/<page-or-state>]"`, for example
  `dock/v-normal`, `dial/crit`, `panel/docker/now`, `panel/docker/stopped`,
  `panel/tailscale/empty`, `toast/stack`.
- `<html>` or `<body>` gets `data-gallery-ready` once fonts are loaded and
  every surface has drawn its first snapshot.
- `/gallery?surface=<id>` renders that one surface alone on `--bg`, which
  is used by `--isolated`.
- Component roots carry their component name as a class (`.Row`,
  `.Section`, `.PanelHeader`, `.PillButton`, `.Dial`, `.Toast`, `.Alert`, …).
  The audit checks those heights against the 4px grid.

`reference.html` follows the same contract, so the harness can be tested
against it:

```bash
node docs/rewrite/design/shoot.mjs http://127.0.0.1:1431/NoClickDock/docs/rewrite/design \
  "$SCRATCH/selftest" --route /reference.html --reduced-motion --audit
```

This reports 0 findings on every surface and 0 console errors.

## shoot.mjs

`node shoot.mjs <baseUrl> [outDir] [--route /gallery] [--only <text>]…
[--isolated] [--scales 1,2] [--reduced-motion] [--viewport 1600x1000]
[--audit]`

- It writes `gallery@1x.png` (full page) and `<surface>@<n>x.png` per
  surface, then prints every path.
- Without `--reduced-motion`, animations are paused at a fixed frame so
  shots are comparable between rounds.
- `--audit` writes `audit.json`. It covers: required fonts actually loaded
  (Manrope, IBM Plex Mono); console errors; text clipped without an
  ellipsis; digits in a proportional face without tabular figures;
  component heights off the 4px grid; elements escaping their surface.
- Playwright comes from the project, then `$NCD_PLAYWRIGHT`, then
  `../Cerveau-Hub`. Chromium comes from Playwright, then `$NCD_CHROME`, then
  the newest `~/.cache/ms-playwright/chromium-*`.

The audit is a floor, not the review. Timings, focus, hover-to-switch, and
whether it looks like a Cerveau product are checked by hand against
`reference.html` and DESIGN.md.
