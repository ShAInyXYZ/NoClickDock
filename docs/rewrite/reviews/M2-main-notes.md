# M2 — orchestrator's notes for the design review

From a headless capture of the mock gallery (Chromium, invented data), before
design's own M2 review. Design decides; these are observations, not verdicts.

1. **Row names switch voice inside one panel.** `row.mono` renders the *name*
   in Plex Mono 11.5/500 (`Row.svelte:66`). Tailscale mixes `address`,
   `magicdns` (mono) with `version`, `key expires` (Manrope) in the same
   section; `old-vm` is mono while the dormant chips beside it are Manrope.
   ComfyUI's `endpoint` is mono while `VRAM` is Manrope. Labels are not
   identifiers. Proposed rule: a row name is Manrope unless it is an
   identifier (hostname, container, mount path), and then every row of that
   section uses the same voice.
2. **Claude's header state is Title Case** ("Minor Service Outage") because it
   copies the status page's indicator text; every other widget's state label
   is sentence case ("fileserver offline"). Normalise in the writer.
3. **Claude's model split row** (`opus 5 1.1M  sonnet 5 96K  haiku 4.5 12K`)
   reads as a headerless table with wide gaps; compare against the
   reference's `stats` treatment.
