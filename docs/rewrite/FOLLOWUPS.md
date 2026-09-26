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
