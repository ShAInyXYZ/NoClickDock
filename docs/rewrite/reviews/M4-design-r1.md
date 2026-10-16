# M4 — design review, round 1 (Claude and Codex writers)

Reviewer: design supervisor, 2026-09-26.

**Tree:** a clean `git archive` of `1b1a85b`. The real Claude and Codex
writers now drive the mock too.

**Checked against:** DESIGN.md's "What the panel says" and the widget specs.

**Sources:**
- the `--mock` gallery (committable, `shots/M2r2/claude@2x.png`,
  `codex@2x.png`);
- one real `ncd-core --once claude` / `--once codex` on this machine,
  rendered through a scratch stand-in core. Those snapshots and captures
  contain the user's real usage and stay in
  `…/scratchpad/design/m2a2/real/`, **not committed**;
- a throwaway Go test in the scratch export (not in the repo), driving
  `usageBlocks` with a stale reset and an unparsed reset.

## What is good

**Claude**
- Problems first (*Degraded*, then *Active incidents*, then *Usage*).
- A sentence-case state naming the fact (`Console degraded`).
- The degraded row's lamp and value agree (warn and warn).
- Limits with `resets HH:MM` / `resets Ddd HH:MM`.
- Two stats lines: totals, then per model.
- Services as chips.
- The empty ("No Claude Code sessions on this machine in the last 7 days.")
  and unreachable ("status.claude.com did not answer") variants say what is
  true.

**Codex**
- The same order and voice.
- Buckets folded into the limit labels (`codex 5h`, `codex week`).
- A dead bucket as words (`codex-mini · never reports usage`), sunk below
  the live ones.
- A lapsed window drawn as `0%` with `reset 12m ago`, which is true: a
  window reset since the last recorded turn has had no usage.
- `limits as of 12m ago` in the footer, and "24 other OpenAI services
  operational" condensing the rest on real data.

**Real data** rendered cleanly: long model names wrap on the stats line with
no leading separator, and there were no overflow findings.

## MUST-FIX

1. **Claude: a window whose reset has passed still shows its old percentage,
   and a past time as if it were ahead.**
   - Claude's limits are read from `claude -p /usage` every 5 minutes, and a
     failed read keeps the rows it had. So a reading routinely outlives its
     reset. Probe: `Session 46%` whose reset was 20 min ago renders
     **`Session 46% · resets 05:56`** at 06:16.
   - That breaks the time rule (a past time presented as a future one) and
     unknown-never-zero: the current use of a reset window is **unknown**
     until the next reading. It is neither 46 % nor 0 %.
   - Codex handles its own case, where 0 % is true because every turn is
     logged. Claude's case is different.
   - **Fix:** when `Resets < now`, draw the limit without a percentage or
     meter, in words: `Session · reset 20m ago · not read since` (dim). Leave
     it out of the glance until a fresh reading arrives.
   - **Acceptance:** the same probe renders words, no `46%`, and no future
     tense.

2. **Claude: unparsed reset text is printed verbatim.**
   - When `parseReset` cannot read the CLI's words, `render.go` uses
     `l.Raw`. Probe: **`resets Sep 5 at 3:59 in the afternoon
     (Europe/Paris)`**: the CLI's own format with a zone, where the rule is
     `HH:MM` / `Ddd HH:MM`. The column also overflows.
   - **Fix:** if the reset cannot be parsed, show no reset text (the
     percentage alone is still true). Log the raw string once for the
     parser's tests.

3. **The dial and the panel contradict themselves when a limit is high but
   the service is fine.**
   - Real Codex on this machine: `state ok` (green lamp, header
     `operational`) while its glance is **`90%` in crit red**, and the Week
     meter is red at 90 %. The dock shows a quiet green lamp over a red
     number, and the panel's header says all is well above a red bar.
   - The specs derive state from the status page alone and give the glance
     the limit's tone, so this is a contract gap, not a coding slip.
   - "Colour is signal": a person about to be cut off needs the lamp to say
     so.
   - **Proposed contract line (A20, main decides):** a widget's state is the
     worst of its signals. A warn/crit limit raises Claude or Codex to
     warn/crit, and the state label names it (`week 90%`). A19 then puts the
     limit first. The alternative, capping the glance tone at the state,
     hides the most useful fact.
   - Apply it in both writers either way.

## SHOULD-FIX

1. **Claude's first 5 minutes show no limits** (they are read in the
   background), with no word about it. The Usage section could say
   `limits · reading…` in dim until the first `/usage` answers, so an empty
   slot isn't mistaken for "no plan limits".
2. **`Claude for Government` ellipsizes** in the three-column chip grid (see
   M2-design-r2).

VERDICT: FAIL
