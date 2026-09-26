# M1 — research review, round 2

Reviewed: `dabc926` (the fixes for [M1-research-r1.md](M1-research-r1.md)),
tested on a clean `git archive dabc926` tree in my scratchpad (the working
tree already carries later M2 work). `dabc926` sits on top of the M2 commits
`7383736`/`aa0bbca`; their hub/transport/config changes were only checked for
regressions against M1 behaviour here, not reviewed as M2.

## Must-fix from r1

| # | r1 finding | verified |
|---|---|---|
| 1 | Concurrent collects after a timeout | **Fixed.** `hub.go` keeps the running collect in `entry.inflight`; a due poll or refresh while it has not returned re-emits "still collecting…" (snapshot once, ticks after) and never calls `Collect`; the loop applies the late answer the moment it lands. My r1 repro (collector ignoring ctx, `collectTimeout` shortened to 300 ms, 5 s run) re-run against the new code: **1 call, peak 1** (was 4 / 4). A ctx-honouring slow collector: 4 calls in 3.5 s, peak 1, each attempt reported "still collecting…" then "gave up after 300ms". Executor's two new tests pass 3/3 under `-race`. |
| 2 | `build-core.sh` built the host core for every target | **Fixed.** `triple=${1:-${TAURI_ENV_TARGET_TRIPLE:-host}}`. Checked in the archived tree: no env → `x86_64-unknown-linux-gnu` ELF; `TAURI_ENV_TARGET_TRIPLE=x86_64-pc-windows-msvc` → PE32+ GUI `.exe`; an explicit argument wins over the env; an unsupported triple fails the build (exit 1) instead of shipping a stale file. |

## Should-fix from r1

| # | item | verified |
|---|---|---|
| 1 | `WS_EX_TOOLWINDOW` lost at first show | Done: `WM_STYLECHANGING` guard in the subclass, as in the corrected research doc. `cargo check --target x86_64-pc-windows-msvc` of the archived shell passes (forced recheck of `win.rs`). Runtime remains M7. |
| 2 | Timed probe on every mount | Done: local/pool 5 s, network 2 s; a hung mount becomes a "not responding" row + `noresp:` issue; a share that errors is not responding. See SHOULD-FIX 1 below. |
| 3 | Kill the process group on timeout | Done (`Setpgid` + `Cancel` kills `-pid`); new test proves the backgrounded grandchild is gone. Windows note (Job object in M7) is recorded in `exec_windows.go`. |
| 4 | `in_panel` reset on hide | Done (`hover.rs:231`). |
| 5 | Scratch configs out of `/tmp` | Done: core `os.UserCacheDir()/noclickdock-dev/core-<port>`, shell `$XDG_CACHE_HOME`/`~/.cache` (`%LOCALAPPDATA%` on Windows) `/noclickdock-dev/mock-<instance>`; created 0700/0600 (observed). |
| 6 | Rejected `config.patch` silent | Done: the unchanged config is re-broadcast; seen as one `config` SSE event after a rejected `density: "huge"` patch. |
| 7 | "still collecting…" unreachable for 30 s widgets | Done: patience = `min(2 × interval, collectTimeout/2)`. |

## Regression checks (dabc926 tree)

- `gofmt -l` clean; `go vet` linux + `GOOS=windows` clean; `go test -race
  -count=1 ./...` all 7 packages ok.
- `GOOS=windows go build -ldflags -H=windowsgui` ok; shell `cargo check`
  native and msvc ok.
- `ncd-core --once disk`: same real snapshot as r1 (1.2 s, `/ 90% full`,
  Filesystems / Network / Docker).
- `--mock` HTTP guards unchanged, including the new `GET /v1/variants`
  (evil Origin → 403, evil Host → 403, `text/plain` POST → 415).
- stdio: `hello` first, stdin EOF → exit 0 in 1 ms.
- X11, the dabc926 shell built in scratch (`--mock --instance research2`, no
  clicks): rail UTILITY + input False; a 50 ms pass over a dial opens nothing,
  600 ms opens the panel; the panel stays while the pointer is in it, closes
  600 ms after leaving it; after leaving a dial directly the panel is still up
  at 150 ms (grace) and gone at 650 ms; `_NET_ACTIVE_WINDOW` unchanged
  throughout; SIGKILL of the shell took `ncd-core` with it. The scratch config
  it wrote was removed afterwards.

## SHOULD-FIX (new, non-blocking)

1. **Any error from a network `statfs` now reads "not responding"**
   (`core/internal/widgets/disk/disk_linux.go:37`, `err != nil && m.kind ==
   Network`). My r1 wording asked for exactly that, but it over-reaches:
   `EACCES`/`EPERM` (a share mounted for another user under a directory this
   user cannot search) or `ENOENT` would raise a permanent `warn` and a toast
   for something that is not a failure — P skipped those silently. Treat as
   not responding only the transport errors (`EHOSTDOWN`, `EHOSTUNREACH`,
   `ESTALE`, `EIO`, `ENOTCONN`, `ETIMEDOUT`, `ECONNREFUSED`,
   `ECONNRESET`); drop the rest as P did. Add one case to the disk tests.

VERDICT: PASS
