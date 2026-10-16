# M4 — design review, round 2 (Claude and Codex writers)

Reviewer: design supervisor, 2026-09-26. **Tree:** a clean `git archive` of
`cbf7c57`. Browser only.

**How it was checked:**
- the `--mock` gallery, where the harness audit is clean: 34 surfaces, 0
  findings, 0 console errors;
- the r1 edge cases, rendered by the **real writers** in a throwaway scratch
  test (not in the repo), with invented data, `RaiseToWorst` applied as the
  hub does, and served to the gallery by a stand-in core.

| r1 item | result |
|---|---|
| must-fix 1: a passed reset shows its old % as if the reset were ahead | **fixed.** Session read 35 min ago, reset 20 min ago: the row reads **`Session · reset 20m ago · no reading since`** in dim words, with no percentage and no meter. It is left out of the glance, which shows Week 90 %. With every window lapsed, the glance falls back to the day's tokens (`1.2M`). `shots/M4r2/claude-passed-reset@2x.png`, `claude-scenarios.png` |
| must-fix 2: raw CLI reset text printed | **fixed.** `resets Sep 5 at 3:59 in the afternoon (Europe/Paris)` gives `Session 46%` with no reset text. |
| must-fix 3 / A20: state is the worst signal | **fixed, all widgets.** A Week limit at 90 % with the page fine gives: crit, header `week 90%`, crit ring and lamp, glance `90%` in crit, for both Claude and Codex (Codex at 80 % gives warn, at 50 % ok). A worse page keeps its words (`Console major outage`). On the full mock, every widget's lamp, header and glance agree with its worst signal. ComfyUI stays busy with no warn signal. `shots/M4r2/codex-week-90@2x.png`, `dials-scenarios@2x.png` |
| should-fix 1: first minutes show no limits | **fixed.** `Plan limits · reading…` |
| should-fix 2: long chips clip | **fixed.** "Claude for Government" takes two columns. `shots/M4r2/claude-mock@2x.png` |

VERDICT: PASS
