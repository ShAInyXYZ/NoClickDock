# M3 — research review, round 3

Reviewed: `1f80a65` (after [M3-research-r2.md](M3-research-r2.md)), on a
clean `git archive` of the branch head (`0a2fcd5`, which contains it).

Regression checks: `gofmt` clean; `go vet` on linux and windows;
`go test -race ./...` ok (11 packages).

## r2 items

| r2 | item | verified |
|---|---|---|
| MUST 1 | pins matched only the MagicDNS label | **Fixed.** `p.Watched = pins[p.name] \|\| pins[p.hostname]`. My r2 probe: peer `HostName "Office PC"`, label `office-pc`, offline, legacy pin `"office pc"` → **warn "office-pc offline"**. The label pin `"office-pc"` gives the same. |
| S1 | a refused duplicate showed as the running action failing | **Fixed.** The refusal is broadcast with only the request id. Both pages ignore a `result` with no widget or action, so `running` stays until the real result, and nothing is marked failed. The sender's promise resolves; `press` no longer reads it. No toast. |
| S2 | unknown shapes inside a foreground session | **Fixed.** `{"Foreground":{"s1":{"Future":…}}}` → `Serve / funnel · unknown` (idle), no longer "nothing exposed". |
| S3 | nits | **Fixed.** Foreground exposure gets rows of its own (`Funnel · session`, `Serve · session`), and a background serve beside them keeps a plain `Serve` row. Funnel ports sort as numbers (`:443, :10000`). `<host> offline` is cut to 28 characters (`a-very-long-build-ho offline`). |

The r1 foreground-funnel probe still gives `Funnel · session | :443 · public
internet` (crit).

VERDICT: PASS
