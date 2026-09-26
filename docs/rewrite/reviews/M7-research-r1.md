# M7 — research review (single pass)

Reviewed: `12f1163` (install.ps1, Windows paths, the Claude shim, Docker
Desktop CLI), `c273cba` (build-windows.sh) and `db7ba66`, as of `508a809`.
There is no Windows machine here, so this is a compile-level and script
review.

**Compile level:**
- `GOOS=windows go vet` and `go build` ok.
- `cargo check --target x86_64-pc-windows-msvc` ok.
- Linux `go test -race`, `cargo test --lib` (11) and `svelte-check` clean.

## Checked and fine

- **install.ps1:**
  - WebView2 is checked first, and the script refuses with a link when it
    is missing. Keys: HKLM (both views) and HKCU EdgeUpdate `pv`.
  - NoClickDock 1's widgets are stopped by command line
    (`status-checker.py | ncd-dock.py | power-monitor.py` under
    `python*`).
  - Their `NCD *.lnk` Startup shortcuts (the old installer's names) are
    **moved** to `%LOCALAPPDATA%\NoClickDock\legacy-startup`, never
    deleted.
  - `Unblock-File` runs on each copied exe.
  - The HKCU `Run` value holds a quoted path.
  - `-Status`, `-Uninstall` (settings kept) and `-RestoreLegacy` work as
    described.
- **Claude shim:**
  - `claude.cmd` is run as `node <dir>\node_modules\@anthropic-ai\claude-code\cli.js`,
    preferring a `node.exe` beside the shim.
  - A path containing `&|<>^%"()!` is never given to `cmd.exe`: there are
    simply no limits then. This closes M4 r1 S3.
- **Docker Desktop:** `%ProgramFiles%\Docker\Docker\resources\bin\docker.exe`
  is the fallback when docker is not on a GUI process's PATH.
- **PM-Log on Windows:** under the Documents known folder, which OneDrive
  may move. NoClickDock 1 had no Power widget on Windows, so there is no
  second writer there.
- **build-windows.sh:** checks cargo-xwin, the rustup target and llvm-rc,
  then copies ncd.exe, ncd-core.exe and install.ps1 to `dist/windows`.

## MUST-FIX

1. **`-RestoreLegacy` lets NoClickDock 1 delete NoClickDock 2's settings.**
   - NoClickDock 2 keeps `config.json` in `%APPDATA%\NoClickDock`,
     NoClickDock 1's own widget directory.
   - NoClickDock 1's `_get_active_widgets` (main: `core/shinywidget.py`
     lines 115–126) removes every `*.json` there without a live `pid`.
     `config.json` has none.
   - install.ps1's own header says so. The install path is safe because it
     stops the widgets first.
   - But `-RestoreLegacy` puts their shortcuts back while `config.json`
     stays (also after `-Uninstall`, which keeps settings). The next login
     deletes it silently.
   - Fix: before restoring, copy `config.json` aside (e.g.
     `%LOCALAPPDATA%\NoClickDock\config.json.bak`) and say where. Better
     still, move NoClickDock 2's settings out of NoClickDock 1's folder
     (a subfolder, migrated once).

Follow-ups are in [FOLLOWUPS.md](../FOLLOWUPS.md).

VERDICT: FAIL (1 data-loss must-fix; no re-review needed beyond it)
