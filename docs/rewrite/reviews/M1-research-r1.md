# M1 — research review, round 1

Reviewed: `5856a2a`, `8466166` (executor), `276ef19`, `fde33f4` (contract), against
ARCHITECTURE.md, specs/dock-core.md, specs/disk.md and research/tauri-windows.md.
Scope as briefed: disk is the only collector at parity; toast window, context
menu, tray, single instance, settings and legacy migration are M2/M6 and not
judged here.

## What I ran

| check | result |
|---|---|
| `gofmt -l .` · `go vet ./...` · `GOOS=windows go vet ./...` | clean |
| `go test -race -count=1 ./...` | all 6 packages ok |
| `GOOS=windows go build -ldflags -H=windowsgui ./cmd/ncd-core` | ok (PE32+ GUI) |
| `ncd-core --once disk` (this machine) | correct snapshot in 1.3 s: sections Filesystems / Network, `/` at 90 % → `warn`, label `/ 90% full`, glance `90%`; values match `df` (used/total incl. root reserve, as P); Docker section landed on the second collect |
| `ncd-core --mock --port 47812` | hello + 7 snapshots, ticks after unchanged polls; config in `/tmp/ncd-core-47812` only |
| `ncd-core --dev --port 47812` | real disk over HTTP; a `config.patch` wrote only the scratch config (0600, dir 0700); `~/.config/noclickdock` untouched |
| dev HTTP guards (curl) | Host ≠ `127.0.0.1:port`/`localhost:port` → 403; Origin evil/`null`/`tauri://localhost` → 403; allowed Vite origin → 200 + ACAO echo; preflight from evil origin → 403; POST `text/plain` / form → 415; JSON POST with evil Origin → 403; 2 MB body → 413; unknown message type / bad config / unknown config key → 400 with a reason; listening on 127.0.0.1 only |
| stdio protocol | first line `hello`; stdin EOF → exit 0 within 1 ms, in `--mock --stdio` and in production mode |
| death pact through the shell | `ncd --mock --instance research` killed with SIGKILL → its `ncd-core` gone within 1.5 s |
| X11, built `target/release/ncd --mock` | rail 81×717 physical (54 CSS × 1.5) flush right on the primary work area; `_NET_WM_STATE` SKIP_PAGER, SKIP_TASKBAR, ABOVE, STICKY; type UTILITY; WM_HINTS input False on rail and panel; hovering a dial mapped the panel ~150 ms later (dwell 120 ms), beside the rail, centred on the dial; it stayed while the pointer was in it and unmapped after leaving; `_NET_ACTIVE_WINDOW` unchanged by show, hover and panel |
| `cargo check --target x86_64-pc-windows-msvc` (own target dir, `llvm-rc` on PATH) | ok |

## MUST-FIX

1. **The hub runs a collector concurrently with itself after a timeout**
   (`core/internal/hub/hub.go:154-200`, contract at `core/internal/hub/collector.go:21-22`).
   `poll` returns on `cctx.Done()` while the `Collect` goroutine is still
   running; the next `loop` iteration starts another `Collect`. A collector
   that does not return (a `statfs` blocked in the kernel ignores `ctx`) gets
   a new stuck goroutine, and a new OS thread, every `collectTimeout +
   interval` for as long as the mount stays dead — and collectors with state
   (docker's log cache, tailscale's rate map in M3) would race.
   Reproduced in a scratch copy with `collectTimeout` shortened to 300 ms and a
   collector that sleeps 10 s ignoring ctx: 4 calls, **peak 4 running at
   once** in 5 s. The disk collector is exposed today: only `Network` mounts
   go through the prober (`disk_linux.go:34`); a Local-kind FUSE mount whose
   device starts with `/` (mergerfs, gocryptfs over a dead share) blocks
   `measure` at `disk_linux.go:41` with no timeout.
   Correct: keep the in-flight collect on the entry; while it has not
   returned, a due poll does not call `Collect` again — it re-emits the last
   snapshot with `error: "still collecting…"` (so the UI does not go stale),
   and when the stuck call finally returns its result is applied and normal
   polling resumes. Add a hub test with a ctx-ignoring collector asserting
   peak concurrency 1.

2. **`build-core.sh` builds the host's core for every target**
   (`scripts/build-core.sh:12`, used by `app/src-tauri/tauri.conf.json:7,9`).
   `cargo tauri build --target x86_64-pc-windows-msvc` runs the hook with no
   argument, so it rebuilds `ncd-core-x86_64-unknown-linux-gnu` and Tauri
   bundles whatever `binaries/ncd-core-x86_64-pc-windows-msvc.exe` already
   exists — right now a copy from 02:28, older than `8466166` (the disk sync),
   i.e. a stale core would ship silently. Tauri exports the target to the hook
   (measured: `TAURI_ENV_TARGET_TRIPLE=x86_64-pc-windows-msvc`).
   Correct: `triple=${1:-${TAURI_ENV_TARGET_TRIPLE:-$(rustc --print host-tuple)}}`
   — then `cargo tauri build [--target …]` is self-contained for every target,
   as M1-exec.md claims.

## SHOULD-FIX

1. **Windows: `WS_EX_TOOLWINDOW` is lost at the first show**
   (`app/src-tauri/src/overlay.rs:38`, `win.rs`). `harden` runs before the
   first `window.show()`, and tao's show (a VISIBLE flag change) rewrites
   `GWL_EXSTYLE` from its own flags (`tao …/window_state.rs:438-440`), so the
   rail and panel would be listed in Alt+Tab. This is my research doc's fault
   ("call harden after the last flag change" was not enough); corrected there:
   handle `WM_STYLECHANGING` for `GWL_EXSTYLE` in the existing subclass and OR
   `WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE` into `styleNew` (compile-checked for
   msvc). Blocking for M7, cheap now.
2. **Probe every mount, not only network ones** (`disk_linux.go:33-42`): run
   Local and Pool `measure` through the prober too (e.g. 5 s), so one hung
   mount becomes a "not responding" row instead of freezing the whole disk
   widget (with MUST-FIX 1 in place it would otherwise sit at "still
   collecting…" forever). Same for network mounts that answer with an error
   (`EHOSTDOWN`, `ESTALE`): today they vanish from the panel
   (`disk_linux.go:43`); show them as not responding.
3. **Timeouts do not kill grandchildren on Linux** (`core/internal/sys/exec_unix.go:10`):
   `exec.CommandContext` kills only the direct child; `WaitDelay` frees
   `Wait` but leaves e.g. a `docker compose` plugin process running. Put the
   child in its own process group (`Setpgid: true`) and set `cmd.Cancel` to
   kill `-pid` (research doc, "Go sidecar"; proven there). Needed before M3's
   docker actions.
4. **`hover::hide` leaves `in_panel` set** (`app/src-tauri/src/hover.rs:223-231`):
   if the panel is ever hidden while the pointer is in it, WebKit may not send
   `mouseleave`, and the next panel will never enter Grace from the dial
   (`hover.rs` `(None, Phase::Open(w)) if !st.in_panel`), staying open. Reset
   `in_panel = false` in `hide()`.
5. **Predictable scratch config paths in world-writable `/tmp`**
   (`core/cmd/ncd-core/main.go:97`, `app/src-tauri/src/lib.rs:51`): another
   local user can pre-create `/tmp/ncd-core-47810` and make saving fail or
   plant a config. Dev-only, so low risk; prefer `os.UserCacheDir()/noclickdock-dev/<port>`
   (and the Rust equivalent) or create with `MkdirTemp`.
6. **A rejected `config.patch` is silent to the UI** (`hub.go:345`): over
   stdio the error only reaches the core's stderr. When Settings arrives (M6)
   the UI needs either a `result` for the patch or a re-broadcast of the
   unchanged `config` so it can revert.
7. **"still collecting…" is unreachable for 30 s widgets**: `patience = 2 ×
   interval` (`hub.go:168`) equals `collectTimeout` (60 s) for every
   `pollSecs ≥ 30`, so disk/claude/codex/tailscale go straight to "gave up".
   With MUST-FIX 1 this matters less; consider `patience = min(2 × interval,
   collectTimeout/2)`.

## Parity notes on disk (no action)

- Classification, skip lists (whole path components), `\040` decoding,
  `st_dev` de-duplication, `shutil.disk_usage` arithmetic, thresholds
  0.85/0.92, the OK label's smallest-free rule, issue ids and texts, Q6
  sections (Network with share device, Pools by dataset, docker/podman graph
  datasets skipped), 2 s share probe, decimal Docker sizes, Windows drive
  letters with volume labels and remote drives on the network probe: all as
  specified.
- The Docker block is `row + stats + note` instead of the spec's `chips`
  (exec note 5, following the reference plate). Same data and tones; fine.
- Like P, btrfs subvolumes mounted separately appear twice (each subvolume
  has its own anonymous `st_dev`). Parity, not a regression.

## Process note

During my first X11 pass a scripted click aimed at the panel landed on a
desktop window underneath (my script had picked a stale window id), which
changed the active window once. No product code was involved; the second pass
matched windows by pid and is the one reported above.

VERDICT: FAIL
