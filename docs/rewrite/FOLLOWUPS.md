# Follow-ups

Not must-fix: noted so nothing is lost. One line each, with its source.

- Settings: `poll_interval` changes the hub's cadence at the next poll, but a collector's own background cadences (Claude CLI 5 min, scans 2 min) are fixed. (M6-exec)
- Settings: country presets are 2026 approximations ported verbatim; a newer table belongs in a data file. (M6-exec, specs/power.md)
- Settings: the Edge choice has no monitor choice; a multi-monitor user moves the rail by dragging. (M6-exec)
- Tailscale: a relayed path, an update available and a dormant peer's expired key are `info` (decided in A20-a); a watched peer's expired key stays `warn`. (M6-exec, ARCHITECTURE A20-a)
- Crash recovery: after the first crash each view runs its own web process until the next start (~+130 MB). (M2-exec r4)
- Dev server: vite-plugin-svelte sometimes caches a component's CSS as the raw .svelte source when several windows load at once on a cold server; restart vite. Production is unaffected. (M2-exec r4, M6-exec)
- Power: legacy migration of the country preset the user picked is not possible (the Python stored only the rates). (M6-exec)
- Switch latency on the real desktop (design M1 r5: glide median ~118 ms, worst ~260 vs ≤ 70/100). One cheap attempt made (`see M7-exec`): parked windows now sit at compositor opacity 1/255 instead of 0 so Mutter keeps painting them (parked panels change 0 pixels over a white desktop on Mutter-on-Xvfb), and the swap is only the two properties in a server grab (no geometry query or move). On Mutter-on-Xvfb: panel_layout → painted median 23 ms, hover → show 0.2 ms median when predicted, 0 empty frames in a 360-frame glide. Not re-measured on the real desktop: that check happens with the user at install time. Not done: a fixed-size parked window (would need an input shape trimmed to the panel). (design M1 r5)
- Crash recovery: after a reload the rail window's transparent corners turn opaque dark (a square box behind the rounded rail) for 4–10 s. (M2-design-r3b)
- Real desktop, owed with the next `:0` session: a GNOME-recorder integrity pass of the switch and one look at the toast fade (both verified on Mutter-on-Xvfb only). (M1-design-r5, M2-design-r3b)
- Mutter-on-Xvfb no longer reproduces real-desktop switch latency (34 ms there vs 114–150 on `:0`): latency is judged on `:0` only. (M1-design-r5)
- Dock: a horizontal rail dragged onto a side edge could snap to the nearest top or bottom edge. (M2-design-r1b, r2b)
- Power `no-readings` variant: say it in one sentence, not a table of dashes, `Board ~0` and an empty right column. (M5-design-r1)
- Power report copy: "There are 1 gaps". (M5-design-r1)
- Note: the crash-reload dark corners above were fixed in 7dd2612 (app.html marks overlay pages transparent before the first paint). (M7-exec)
- Windows: never run — first run owed on a real machine (tray, placement, the panel swap while WebView2 may not paint a hidden window, toasts on hover, install.ps1 end to end). (M7-exec)
- Windows: web-process crash recovery (WebView2 ProcessFailed) not wired. (M7-exec)
- Windows: the tray's dark variant for light taskbars is not wired. (M7-exec)
- Windows: NoClickDock 1's %APPDATA%\NoClickDock\bar.conf is not migrated (migration is Linux-only). (M7-exec)
- Windows Power: displays, USB and drives are not read; CPU package power needs LibreHardwareMonitor. (M7-exec, specs/power.md)
- README hero: the rail (`height=420`, 0.88 × CSS size) and the Docker panel (0.67 ×) sit side by side at different scales, so the rail looks oversized; show them at one scale (e.g. rail `height=322` beside the panel at 420). (M8-design-r1)
- README: the Settings image (0.75 ×) and the toast image (1.27 ×) sit side by side at different scales; give the toast `width=130`. (M8-design-r1)
- README widget table: Power's 642 px panel squeezed into a 320 px cell is drawn at half size and its text is unreadable; give Power its own full-width row, or link the image. On a phone every panel in the table is ~140 px wide. (M8-design-r1)
- README copy: "Drag it anywhere; it snaps to the nearest edge" — it snaps only when dropped within 24 px of an edge; say "snaps to an edge when you drop it near one". (M8-design-r1)
- README: "Needs … a compositing desktop" understates the shell, which falls back to one unmapped panel window without a compositor; say "best with a compositor". (M8-design-r1)
- `docs/rewrite/design/readme-shots/` duplicates `assets/screenshots/` byte for byte (~1 MB); delete it now that `assets/` is canonical. (M8-design-r1)
- [research M7] Windows `-RestoreLegacy` lets NoClickDock 1 delete NoClickDock 2's `config.json` (NCD 1 prunes `*.json` without a live pid in `%APPDATA%\NoClickDock`) — move NCD 2's config to a subfolder or refuse restore while NCD 2 is installed.
- [research M8] `scripts/install.sh` without `--replace-legacy` leaves NoClickDock 1 running beside NCD 2, so two power loggers write PM-Log — make the installer refuse (or require an explicit flag) while NCD 1 is running.
- [research secret scan] main's older history still contains real hostnames from before the scrub; only a history rewrite of main with a force push removes them.
