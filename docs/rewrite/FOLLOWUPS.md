# Follow-ups

Not must-fix: noted so nothing is lost. One line each, with its source.

- Settings: `poll_interval` changes the hub's cadence at the next poll, but a collector's own background cadences (Claude CLI 5 min, scans 2 min) are fixed. (M6-exec)
- Settings: country presets are 2026 approximations ported verbatim; a newer table belongs in a data file. (M6-exec, specs/power.md)
- Settings: the Edge choice has no monitor choice; a multi-monitor user moves the rail by dragging. (M6-exec)
- Tailscale: a relayed path, an update available and a dormant peer's expired key were quieted to `info` beyond A20-a's list, by its principle — design to confirm. (M6-exec)
- Crash recovery: after the first crash each view runs its own web process until the next start (~+130 MB). (M2-exec r4)
- Dev server: vite-plugin-svelte sometimes caches a component's CSS as the raw .svelte source when several windows load at once on a cold server; restart vite. Production is unaffected. (M2-exec r4, M6-exec)
- Power report page: visual refresh to the NoClickDock design (a design task). (M5-exec)
- Power: legacy migration of the country preset the user picked is not possible (the Python stored only the rates). (M6-exec)
