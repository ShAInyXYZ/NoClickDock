# M2 — design review, round 2, part B (real windows)

Reviewer: design supervisor, 2026-09-26. Re-review of M2-design-r1b's six
must-fixes, plus memory and the freeze trigger.

**Build:** a clean `git archive` of `9a9b2e9`.

**Desktop:** real X11 `DISPLAY=:0`, GNOME 42.9 / Mutter, two 3840×2160
monitors at 60 Hz. I had it alone; instances `--instance design` and
`--instance down`.

**Evidence:** real-desktop captures stay in `…/scratchpad/design/r4/x11/`
and are **not committed**.

**Cleanup:** only my PIDs were killed, and pointer and focus were restored.

## r1b must-fix — status

| # | r1b must-fix | result |
|---|---|---|
| 1 | never-shown panels swallow clicks at 0,0 | **fixed.** My probe was mapped and raised at the upper-left **before** launch. From launch to +6.5 s, the pointer sampled at every overlay's corner and centre (the rail excepted) never landed on a NoClickDock window. All 5 clicks (at +0.5, 1, 2, 4, 5.5 s) reached the probe. Panels park mapped as 1×1 windows with no input. |
| 2 | right-click leaves the panel open | **fixed.** Right-click at 0, 50 and 600 ms after arriving (Docker, Codex): both panel windows have `_NET_WM_WINDOW_OPACITY = 0` under the menu and still 1 s later. After Escape with the pointer still on the dial, X sends a fresh enter and the panel opens after the dwell. That is ordinary hover behaviour; I dropped the "stays shut until re-entry" clause from my r1b acceptance. |
| 3a | panel fades don't render | **closed by the amended DESIGN.md (instant).** GNOME's composited recording of three open/close cycles: each title goes 30 → 255 and back **in one frame**. |
| 3b | toast half-fades then pops | **still failing** (must-fix 1 below) |
| 4 | orientation round trip leaves the rail off its edge | **fixed.** Horizontal sits flush on the right edge (3288,3212, 552×105). Back to vertical returns to 3759,2906. The config is saved at each step. |
| 5 | core-down names the wrong reason | **fixed.** With a stand-in core that exits 3: "Nothing here can be measured: ncd-core stopped with exit code 3 (cannot read its config: permission denied). NoClickDock keeps trying to start it." The footer reads `retrying every 10 s · since 06:03:50` in `--warn`, with no duplicate of the error. |
| 6 | tray icon nearly invisible | **fixed in the build.** The item publishes a 44 × 44 light monochrome glyph (mean RGB 250, the mark knocked out), identical to `icons/tray-light-44.png`. **On this session's top bar it still shows the old dark icon:** the file name `tray-icon-ncd-0.png` is reused and GNOME's AppIndicator extension caches the texture by path, so it stays stale until the next login. See should-fix 1. |

**Also verified:**
- **Toast queue:** one widget's stack at a time, each 5 s. In one minute:
  ComfyUI "Generation complete" from :01; then Docker "ledger-api is
  unhealthy", which waited its turn and appeared at :06.5; then "Generating…"
  from :13. Toasts arriving under an open menu or panel wait.
- **Memory on real Mutter:** **217 MB PSS** after using every panel: one
  shared WebKitWebProcess 114, shell 78, network 12, ncd-core 12. r1b
  measured 370; the Python NCD on the same desktop uses 166. Good.
- **Freeze trigger:**
  - Setup: I opened GNOME's calendar menu (the Shell holds the grab), then
    right-clicked the Docker dial.
  - The Shell consumed the click to close its own menu, as Shell menus do;
    no dial menu popped.
  - Everything kept working afterwards: dwell fired, `show`/`hide` as
    normal, and toasts appeared and expired on time for the next minute.
  - I could not recreate the exact "popup under another client's keyboard
    grab" by hand (the Shell ungrabs on click). Research proved that
    mechanism; with non-blocking menus and the heartbeat, no freeze was seen
    in this session.

## MUST-FIX

1. **The toast still doesn't fade on screen; it pops.**
   - The amended DESIGN.md: its content fades out over 160 ms **inside** the
     toast window, and the window parks only after the fade has finished.
   - GNOME's composited recording of the dismissal (made with an animated
     window of mine supplying damage, so every frame was recorded):
     - the toast body dims only **41.9 → 39.4** over three frames (about
       20 % of the way);
     - the text stays at **full brightness (255)**;
     - nothing changes for about 200 ms;
     - then the whole toast disappears **in one frame**.
   - So the page believes it drew the fade, but WebKit stops producing
     frames for this window after the first few. It is click-through, and
     WebKit likely throttles it as occluded or hidden.
   - **Fix:**
     - either drive this one fade from the shell (step the **toast window's**
       opacity over 160 ms at the frame clock; the toast takes no part in
       the panel swap, so the reason for banning window-level fades does
       not apply to it),
     - or make the toast page un-throttled for the fade (WebKit settings /
       visibility) and prove it on this recorder,
     - or amend DESIGN.md to an instant dismiss. Main decides; the current
       half-fade-and-pop is the one outcome that isn't acceptable.
   - **Acceptance:** a composited recording shows the toast's text and body
     dimming monotonically to the desktop over 140–180 ms, then nothing, or
     one clean frame from full to nothing if main amends.

2. **Header state labels clip their descenders.**
   - `.PanelHeader-label` truncates with `overflow: hidden`, and the header
     state uses `font: … /1`. A 13 px line box cannot hold Manrope's
     descenders, so the tails of `g`, `p`, `y` are cut: "core not runninq",
     "1 container unhealth"-y, "generatin"-g, "operational".
   - Seen on the real core-down panel. My reference had the same flaw (I
     introduced the label in M1 r1). It is **fixed there in this round**:
     `.PanelHeader-label { line-height: 20px }`. Checked: `scrollHeight` =
     `clientHeight` = 20 and the "g" of "degraded" is whole.
   - **Fix:** the same one line in `PanelHeader.svelte`. Check every
     overflow-hidden text element with a 1× line height (none of the others
     have lowercase descenders today, but `.Section-title` would if it were
     not uppercase).

## SHOULD-FIX

1. **Tray icon cache.** Write the tray PNG under a name that changes with
   its content, e.g. `tray-icon-ncd-<hash>.png` (the `tray-icon` crate's
   temp-dir path, or set the icon by theme name with a version suffix). GNOME
   then loads the light glyph at once, rather than keeping a cached dark
   texture until the next login, on upgrade and on any future icon change.
2. **A horizontal rail on a side edge** (unchanged from r1b): it is fine, but
   moving to the nearest top or bottom edge when turning horizontal would
   read more naturally.

VERDICT: FAIL
