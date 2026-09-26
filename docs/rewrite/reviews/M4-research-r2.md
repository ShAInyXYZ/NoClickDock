# M4 — research review, round 2

Reviewed: `26e9dd8` (my r1 should-fixes) and `cbf7c57` (design M4 r1: A20,
time moves forward, chips), on a clean `git archive cbf7c57`, against
[M4-research-r1.md](M4-research-r1.md) and design's
[M4-design-r1.md](M4-design-r1.md) must-fixes.

Regression checks: `gofmt` clean; `go vet` on linux and windows;
`go test -race ./...` ok.

## r1 should-fixes

| r1 | item | verified |
|---|---|---|
| S1 | state labels over 28 characters | **Fixed.** Probed on real page names. The label falls back to a shorter word, then to a count, so nothing exceeds 28: `Claude for Government partial outage` → `1 service degraded`; `Image Generation partial outage` → `Image Generation degraded` (25); `Connectors/Apps major outage` (28). A long description becomes `service disruption`. |
| S2 | 6-character token glance | **Fixed.** No decimal from 100 up (`185M`), and the unit rolls over before `1000.0M`. |
| S3 | Windows shim and cmd metacharacters | Open, as agreed: an M7 item. |
| S4 | the CLI outlives the core | **Fixed, both ways.** Every command is tracked and killed on exit, and children get `Pdeathsig: SIGKILL`. Proven with a fake `claude` on PATH that sleeps 30 s: SIGKILL the `--dev` core (scratch config, port 47812) and the fake CLI is gone within 0.5 s. After `--once claude` exits, no `claude -p` is left. Transcript count before and after: 7,068 = 7,068. |
| S5 | nits | **Fixed.** `time/tzdata` is embedded on Windows only (+450 KB there). A single replayed Codex turn is now dropped too (any turn in a `session_meta` second). On this machine's 203 recent rollouts the new rule drops exactly the turns the old one did (1,798) and no others. |

## Design M4 r1 must-fixes

| design | item | verified |
|---|---|---|
| MUST 1 | Claude window whose reset passed keeps its old % | **Fixed.** Probe at 06:16: `Session 46%` that reset 20 min ago renders as a note, `Session · reset 20m ago · no reading since` (idle), with no percentage and no future tense. It leaves the glance: with Week at 91 % the glance is `91%`; with only the lapsed window it falls back to the day's tokens. The CLI is asked again as soon as a window has reset (no faster than a refresh may). |
| MUST 2 | unparsed reset text printed verbatim | **Fixed.** `resets Sep 5 at 3:59 in the afternoon (Europe/Paris)` gives the percentage with no reset text, and the phrase is logged once. It is CLI time words only, with no transcript content. |
| MUST 3 | green lamp under a red limit | **Fixed** by A20 (`model.RaiseToWorst`, applied by the hub to every snapshot). Week at 91 % → **crit "week 91%"**, and lamp, header and glance agree. The Claude and Codex writers stay quiet when a limit falls back, so no "All systems operational" toast appears for a page that was fine. |

## SHOULD-FIX

1. **A20 applied hub-wide turns Tailscale configuration into a permanent
   alarm.** This is for main (a contract decision), not an executor defect.
   - `RaiseToWorst` raises on any warn/crit row, as A20 says. Tailscale's
     spec tones exposure rows warn (serve) or crit (funnel), and `Exit
     node offered` and `Route advertised` are warn too.
   - Probe, with a healthy node and every peer up:
     - a tailnet-only serve → **warn "Serve :443, :2222, :8443"**;
     - a subnet router → **warn "Route advertised 10.9.0.0/24"**;
     - a deliberate funnel → **crit "Funnel :443 · public intern…"**, a
       breathing red ring for as long as it is up.
   - None of these is a problem. They are how the node is set up. The
     Python widget stayed green.
   - It hits this user: this machine has a tailnet serve, so its Tailscale
     lamp will never be green again once its current health warning
     clears.
   - Suggested: tone serve, route and exit rows `info` (they are facts),
     and decide whether a funnel is `warn` or stays `crit`. Alternatively,
     A20 could consider only the kinds it names (limit, meter, row) and
     skip rows marked as configuration.
   - The implementation also raises on alerts, chips, table cells and the
     glance, which is broader than A20's "limit, meter or row". Worth one
     sentence in DESIGN either way.

VERDICT: PASS
