# M8 — research review (single pass)

Reviewed: `508a809` (scripts/install.sh, README, the Python generation
removed from the tree). The installer was run **only in a scratch HOME**:
- `env -i HOME=<scratch>`, with no DISPLAY, so it never started anything;
- the binaries from a clean export;
- invented NoClickDock 1 login entries, plus an unrelated one;
- two fake NoClickDock 1 processes whose argv names the scratch legacy
  prefix, plus an unrelated sleeper.

This machine's real session, `~/.config/autostart` and `~/.local/share`
were compared before and after, and were unchanged.

## Results

| step | result |
|---|---|
| `--replace-legacy --no-start` | Migration runs **first** (dock, which widgets). Then only the 2 prefix-matched processes are stopped; the unrelated sleeper survives. The 8 legacy entries get `Hidden=true`, each with a `.ncd1-backup`, and the unrelated entry is untouched. **0 files deleted.** Result: binaries, menu entry and login entry in place. |
| second run (upgrade) | Migration skipped (`config.json` exists). Still one `Hidden=true` per entry, not two. |
| `--status` | Correct counts, nothing changed. |
| `--restore-legacy` | 8/8 entries byte-identical to the originals. |
| `--uninstall` | Binaries, menu entry, icon and login entry removed. Settings kept. The legacy prefix is intact. |

## MUST-FIX

1. **The default install leaves NoClickDock 1 running beside NoClickDock
   2, so two power loggers write PM-Log.**
   - `scripts/install.sh --build`, the README's first command, only
     replaces NoClickDock 1 when `--replace-legacy` is added.
   - Scratch run with a fake NoClickDock 1 power monitor running:
     - the install adds NoClickDock 2's login entry;
     - it leaves the old process running and its login entry enabled;
     - `--status` then reads `NoClickDock 1 running 1 process(es)`, with
       no warning.
   - Without `--no-start` it also starts NoClickDock 2 right away.
   - From then on both append hourly rows to the same
     `~/Documents/PM-Log/<month>/<date>.csv`, and both reports double the
     kWh and cost for every hour both ran.
   - Fix, either:
     - when NoClickDock 1 is detected (processes or live login entries),
       replace it by default, since it is fully reversible (backups,
       `--restore-legacy`);
     - or stop with a clear message unless `--replace-legacy` or an
       explicit `--keep-legacy` is given.
   - The logger-side guard ([M5-research-r1.md](M5-research-r1.md)
     must-fix 1) protects the log whichever way NoClickDock 2 is started.

Follow-ups are in [FOLLOWUPS.md](../FOLLOWUPS.md).

VERDICT: FAIL (1 PM-Log safety must-fix)
