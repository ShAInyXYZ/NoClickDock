# M4 — research review, round 1

Reviewed: `e5958c6`, `7b081b6` and `0a2fcd5` (Claude limits without a
transcript), on a clean `git archive 0a2fcd5`, against
[specs/claude.md](../specs/claude.md) and
[specs/codex.md](../specs/codex.md). The report is
[M4-exec.md](M4-exec.md).

Regression checks: `gofmt` clean; `go vet` on linux and windows;
`go test -race ./...` ok (11 packages, `statuspage`, `claude` and `codex`
included).

On this machine (all read-only):
- `--once claude` and `--once codex` ran.
- Counting scripts read the local transcripts and rollouts. They printed
  only counts: no content, names or values left the scratchpad.

## Checked and fine

- **No secrets, prompts or names in what was added.** I scanned all 3,764
  added lines (code, fixtures, tests, the report) for:
  - this machine's user and host names;
  - every project name found under `~/.claude/projects` and in Codex
    rollout `cwd`s;
  - `sk-` keys, bearer tokens, JWTs, e-mail addresses and UUIDs.

  The only matches were generic words ("status", "report", "widgets",
  "noclickdock", "comfyui", "research"). Transcripts and rollouts in the
  tests are built from invented content. The CLI fixture's numbers and
  zone are invented.
- **No transcript content reaches errors or logs.** The scanners swallow
  read and parse errors. The one `log.Printf` names only the refused flag.
  Nothing from the CLI's stderr reaches a snapshot.
- **statuspage:**
  - public Statuspage v2 endpoints only, no credentials;
  - 10 s per request, a 4 MB body cap, a neutral User-Agent
    (`noclickdock/2 (+status)`);
  - a partial fetch keeps what was read;
  - group containers and resolved or postmortem incidents dropped.

  Incident bodies are untrusted text. The interface has no `{@html}`
  anywhere, so they are rendered as text.
- **State labels** name the fact in sentence case: `Console degraded`, `2
  services down`, `1 active incident`, `operational`, `status page
  unreachable`. Problems come first in both panels: degraded rows, then up
  to three incident alerts (meta ≤ 24), then Usage, then healthy services
  as chips.
- **`--no-session-persistence` works.** I listed every `*.jsonl` under
  `~/.claude/projects` (6,946 files) before a reading and again after the
  `claude -p /usage --no-session-persistence` child had finished. **0 new
  files.** The flag exists in the installed CLI's `--help`. `--bare` is
  never passed.

  The user's Stop/SessionEnd auto-log hook returns early when the
  transcript does not exist, and skips sessions with no real prompt, so
  readings add nothing to the memory store either way.
- **Token dedupe:**
  - Over 7 days here there are 25,546 usage lines but 11,963 distinct
    messages (id + request id). Per-line counting over-counts **2.13×**,
    which confirms the report's 2.1×.
  - 156 messages sit in two or more files. Their copies' timestamps
    differ by 1 s at the median (38 s at most), so it does not matter
    which copy wins.
- **Subagent transcripts** are read, since the walk goes to any depth.
- **Incremental re-parse:** cached by size and mtime; aged-out files are
  forgotten; dedupe is redone over the cache every scan.
- **`/usage`-only sessions count for nothing.** `<local-command-…>`
  output and tool results are not prompts; any other prompt (including
  `/compact`) makes the session count.
- **Codex:**
  - **Replay and fork dropping.** Of the 204 rollouts of the last 7 days
    here, 190 are forks.
    - In non-forks, the same-timestamp rule drops nothing: there is no
      duplicate-timestamp group at all.
    - In forks, every dropped turn (2,197) belongs to a same-timestamp
      group inside the fork's first second. The fork rule drops no lone
      turn, and 3,269 turns are kept.
  - **Newest reading** by parsed instant (`.9Z` < `.15Z`).
  - **Windows whose reset has passed** read 0 with `reset <age> ago` and
    are left out of the glance.
  - **A bucket that never moves** is one line of words.
- **Claude limits** are parsed from the CLI's own wording, in the zone it
  names. An empty or failed answer keeps the rows it had, and "Refresh now"
  keeps both limits and usage (tested). Limits run on a guarded goroutine
  (5 min, 45 s timeout). A UI refresh asks again only if the last reading is
  older than 10 s.
- **Windows shim:** `cmd.exe /d /c <path> -p /usage
  --no-session-persistence`. The argv is fixed and `/d` skips AutoRun. The
  only variable is the resolved path, which comes from the user's own PATH
  or `%APPDATA%`. No third party can inject into it. See SHOULD-FIX 3 for
  robustness.

## SHOULD-FIX

1. **State labels over the 28-character budget for long component names**
   (`statuspage.StateLabel`: `short(name) + " " + Label(status)`, no cut).
   - From today's public component lists, on the OpenAI page and after the
     Codex short-name table, 3 of 33 names still exceed 28 with "partial
     outage": `Image Generation partial outage` (31), `Connectors/Apps
     partial outage` (30), `Compliance API partial outage` (29). One,
     Image Generation, also exceeds it with "major outage". The Codex
     widget takes its state from the whole page.
   - On the Claude page, `Claude for Government partial outage` (36).
   - Fall back to a shorter form when it does not fit (`<name> outage`,
     or the count form `1 service partial outage`). Test with the longest
     names.
2. **The token glance can be 6 characters.** `sys.FmtTokens` keeps one
   decimal up to 999.9 (`185.2M`, `123.5B`). When there are no limits
   (Claude before its first CLI reading or without the CLI; Codex without
   a reading), the glance is the day's tokens (`claude/render.go:111`,
   `codex/render.go:99`). The dial's budget is 5 (dock-core §11). The last
   hour on this machine alone read `185.2M`. Drop the decimal at ≥ 100
   (`185M`) for the glance, or for all token counts.
3. **The Windows shim breaks on paths with cmd metacharacters.**
   - Go quotes an argument only for spaces, tabs and quotes, and `cmd.exe
     /c` then interprets `&`, `^`, `%`, `(`, `)` in the path. Windows user
     names may contain all of them, e.g. `C:\Users\A&B\AppData\Roaming\npm\claude.cmd`
     splits at `&`, and `%X%` expands.
   - Not an injection by anyone but the user, but the limits would silently
     never read.
   - Prefer running the shim's target directly: `node` plus the
     `cli.js` next to it, as npm's shim does. Otherwise build
     `SysProcAttr.CmdLine` yourself with the path quoted, and refuse a path
     containing `"` or `%`.
   - M7 item.
4. **The CLI outlives the core.**
   - `sys.Run` puts each child in its own process group, so it survives a
     core that is killed or exits.
   - `--once claude` returned in 1.1 s and left `claude -p /usage
     --no-session-persistence` running on its own until it finished.
   - A shell that SIGKILLs its core on quit or restart leaves the same.
     `Pdeathsig: SIGKILL` (with the forking thread locked) or killing the
     groups on shutdown would tie children to the core.
   - Related nit: `--once claude` never waits for the limits it starts, so
     its snapshot has no Usage limits.
5. **Nits:**
   - Reset zones need `time/tzdata` on Windows. `LoadLocation("Europe/Paris")`
     fails there without Go's zoneinfo and falls back to Local, which is
     right only when the CLI's zone is the system zone. Importing
     `time/tzdata` in `ncd-core` costs ~450 KB.
   - A resumed Codex session that replays exactly one turn is not caught
     by the same-timestamp rule, so that turn counts twice. This is rare,
     and rollouts carry no ids to dedupe by.

VERDICT: PASS
