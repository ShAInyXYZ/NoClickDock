# M4 — collectors B · executor report

Branch `rewrite/tauri-go-svelte`. Commits:
- `e5958c6` — `sys.OutputIn`, `FmtTokens`, `Age`
- `7b081b6` — the Claude and Codex collectors, the shared status-page
  package, and the hub's notices and forced refresh

Both collectors follow specs/claude.md and specs/codex.md. The writer rules
of DESIGN.md ("What the panel says") apply to both, through shared helpers.
Between these commits the M2/M3 review fixes landed (see M2-exec.md and
M3-exec.md).

## Shared (`core/internal/statuspage/`)

- **Reader:** a Statuspage v2 reader for public pages. No credentials exist
  anywhere in this path.
  - status, components and incidents are read in order, 10 s each, behind a
    `User-Agent`;
  - a failure after `status.json` keeps what was already read;
  - group containers and resolved/postmortem incidents are left out (the
    incident.io mirror has no `unresolved.json`).
- **The panel's words:**
  - status words and tones per dock-core §7; maintenance is `idle`/info;
  - a **state-label normaliser** that names the fact in the label budget:
    `Console degraded` (a name keeps its case), `2 services degraded`,
    `2 services down`, `1 active incident`, `Batch maintenance`,
    `operational`, `status page unreachable`. Never the page's Title Case
    phrase.
  - issues `comp:<name>:<status>` and `inc:<id>`, in sentence form, tone
    from status or impact;
  - transitions: `All systems operational`, `Status page unreachable`, else
    the label as a sentence;
  - the problems-first blocks: *Degraded* rows (worst first) and up to three
    incident alerts, meta `<status> · <age>` within 24 characters, body the
    latest update cut to 160.
- **Hub:**
  - `Snapshot.Notices`: one-off toasts after any new issues, within the same
    three. Claude uses them for service recoveries (dock-core §5.4).
  - A UI refresh calls `Force()` on collectors with slow sources. These ask
    again only if older than 10 s (dock-core §5.2), and what was last read
    stays on screen.

## Claude (`core/internal/widgets/claude/`)

- **Limits:** from `claude -p /usage`, every 5 min, on its own goroutine,
  with a 45 s timeout.
  - The CLI runs from the user's home directory, never the core's working
    directory.
  - Windows: `claude.exe`, or the npm shim `%APPDATA%\npm\claude.cmd` run as
    `cmd.exe /d /c <path> -p /usage` with a fixed argv.
  - The output format was **re-verified against Claude Code 2.1.280** on
    this machine. Resets appear as `Sep 26, 6:30pm`, `Sep 28, 9am` and
    `3:59pm`, followed by `(Zone/Name)`.
  - Resets are read in the zone the CLI names and shown in the one time
    format (`resets 18:30`, `resets Mon 09:00`). Unreadable words are kept
    as they are.
  - An empty answer keeps the rows it had.
  - Labels: `Session`, `Week`, `Week · Fable`.
- **Token spend:** from `~/.claude/projects` (or `CLAUDE_CONFIG_DIR`, or
  `%USERPROFILE%\.claude`), every 2 min, in the background.
  - Files are read at any depth, so subagent transcripts are included.
  - Only files touched in the last 7 days are opened.
  - Results are cached per file by size and modification time.
  - Lines are streamed at any length, and a line without `"usage"` is
    skipped before any JSON is decoded.
  - **Each message counts once**, by message id + request id, keeping the
    line with the most output. Lines with no ids fall back to their uuid.
    The dedup works across files, since a resumed session copies history
    into its new file.
  - Measured on this machine: 24 156 usage lines are only 11 265 messages,
    so the Python widget counted 2.1× here (2.7× in research's sample).
  - `<synthetic>` counts in the totals, never in the model split.
  - Model names are shortened: `claude-opus-5-5` → `opus 5.5`,
    `claude-haiku-4-5-20251001` → `haiku 4.5`.
- **Panel:**
  - problems first: the degraded services and open incidents, then Usage
    (limits, a stats line of token spend, and the model split as a second
    stats line), then the healthy services as chips;
  - nothing this week → the empty sentence, no zeros;
  - unreachable → `status.claude.com did not answer`.
  - Footer: `limits as of 4m ago`.
  - Glance: the highest limit (warn at 75, crit at 90), else the day's
    tokens, else `—`.
- **Toasts:**
  - An unreachable poll keeps the issues it knew, so a recovery does not
    raise every open incident again.
  - A service back to `operational` is announced once as a notice.
- **"Refresh now"** never blanks the limits or the usage (tested).

## Codex (`core/internal/widgets/codex/`)

- **Source:** `~/.codex/sessions` and `archived_sessions` (or `CODEX_HOME`,
  or `%USERPROFILE%\.codex`), every 2 min in the background. The same
  per-file cache and streaming as Claude.
- **Limits:**
  - the newest reading per allowance is chosen **by instant**: `.9Z` is
    later than `.15Z`;
  - an allowance counts as live once it has been above 0 %;
  - the plan comes from the newest bucket, so it no longer depends on map
    order;
  - windows are sorted shortest first and labelled `5h` / `week` / `2d` /
    `45m` / `limit`.
- **Replayed history is dropped:**
  - Resumed sessions: the same timestamp appears on several turns (spec).
  - Forked rollouts: `session_meta` names a parent, and the parent's turns
    are replayed at the moment of the fork.
  - Checked on this machine: 174 of 654 recent rollouts are forks with
    replay groups, and every replay group falls in the second of its fork's
    `session_meta`. After the rule, no turn appears in two files.
- **Windows whose reset has passed** read `0` with `reset 1h ago`, and are
  left out of the glance. The Python widget kept showing the stale
  percentage.
- **A bucket that never moves** is one line of words
  (`codex-mini · never reports usage`), not 0 % meters.
- **Panel:**
  - problems first: every broken component on the whole OpenAI page, then
    open incidents;
  - Usage (`plus plan`), then a stats line, then the models;
  - then the Codex components as chips (a broken one is already a row
    above), and `<n> other OpenAI services operational` when all is well.
  - Footer: `limits as of 20m ago`.

## Tests

`go test -race ./...` and `go vet` (Linux and `GOOS=windows`) pass;
`gofmt` is clean.

- **statuspage:** the state-label cases, the incident alert budget, and a
  partial fetch.
- **claude:**
  - the CLI fixture (numbers and reset times invented, zone `Etc/UTC`,
    one line whose wording changed and must not match);
  - reset parsing (with and without minutes or a date, the year rolling
    over in December);
  - a transcript tree **built in the test** with invented content: a
    streamed message on three lines, a subagent file, a resumed copy, a
    line over 64 KB, an unparsable line, `<synthetic>`, a uuid fallback,
    and a file 8 days old;
  - de-duplicated totals, and only changed files parsed again;
  - collect against a local HTTP server: issues, an unreachable poll, and
    a recovery notice;
  - the panel in order;
  - the Windows shim argv;
  - refresh keeping values.
- **codex:**
  - a rollout tree built in the test: a model switch, two allowances (one
    idle), a resumed replay, a fork replay, `.9Z` against `.15Z`, windows
    of 300 / 10080 / 2880 / 45 / `null`, an archived file, and a file 8
    days old;
  - status fixtures with resolved, postmortem and open incidents, and a
    broken non-Codex component;
  - the panel line by line, including a lapsed window.
- **The real machine:** `--once claude` and `--once codex`, and `--dev`
  with the CLI answering (the limits read `59% / 95% / 80%`). The output
  stayed in the scratchpad.
- **Mocks:** both mocks are drawn by the real render. The gallery audit
  ran over 34 surfaces with 0 findings.

## Known gaps

- `claude -p /usage` is the only source of limits. If Anthropic changes its
  wording, the Usage section keeps its last rows. The regex fixture will
  catch a change in a test, not at runtime.
- Windows paths and the `.cmd` shim are unit-tested only (M7).
