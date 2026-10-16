# M2 — design review, round 3, part B (real windows)

Reviewer: design supervisor, 2026-09-26. **Build:** a clean `git archive` of
`cbf7c57`.

**Where each check ran:**
- The toast fade, crash recovery and header descenders ran on a private
  **Xvfb `:91` + bare Mutter 42** (research's recipe, dead session bus,
  `--instance design`). The user was at the desk, so there was no pointer on
  `:0`.
- The tray icon was a **passive look at the real `:0` top bar**, with no
  pointer; that capture stays in the scratchpad.
- The header was also checked in the browser gallery.

| r2b item | result |
|---|---|
| must-fix 1: the toast pops instead of fading | **fixed.** The shell steps the window opacity 0.9 → 0 at 16 ms. The composited capture shows **9 frames dimming monotonically**, text and body together, then nothing: 155 ms from the first step to gone. No flash afterwards, even though the next toast shows 2 ms later elsewhere. `shots/M2r3/toast-fade-xvfb.png` |
| reduced motion | **instant.** With `gtk-enable-animations=false` the opacity goes straight from none to 0, and the toast goes from full to gone in one frame. `shots/M2r3/toast-reduced-motion-xvfb.png` |
| must-fix 2: header descenders | **fixed.** In the gallery DOM, no `.PanelHeader-label` clips (line box ≥ 1.3×, scroll height = client height). On real windows, "Console de**g**raded" and "**p**artial outa**g**e" are whole. `shots/M2r3/header-descenders-xvfb.png` |
| should-fix 1: tray icon cache | **fixed.** The item publishes `tray-icon-ncd-0fc5b2aa-0.png`, and GNOME's top bar shows the light rail glyph at once, with no re-login. It reads as a light pill with three dots, legible next to the other tray icons. `shots/M2r3/tray-icon-published.png` |
| crash recovery (`kill -9` of my instance's web process, idle and with a panel open) | **looks right.** The open panel parks at +260 ms, and the rail is back and live at +450 ms. All 7 panels open on hover afterwards, with the right content. The only on-screen cost is 1 frame without the rail plus about 50 ms of a dark square where it sits. `shots/M2r3/crash-rail-frames-xvfb.png` |

Two cosmetic crash leftovers are in `FOLLOWUPS.md`:
- square dark corners behind the rail for 4–10 s;
- four web processes instead of one afterwards.

The toast fade is verified on Mutter-on-Xvfb only. It is compositor-only,
with no WebKit painting involved, so it is low risk on `:0`; check it with
the M1 acceptance run.

VERDICT: PASS
