# M8 — Ship · executor report

Branch `rewrite/tauri-go-svelte`. This commit carries the Linux installer,
the README, the screenshots and the removal of the Python generation. The
power report restyle is `4c8f927`.

## scripts/install.sh (Linux)

- **Binaries:** `--build` compiles them (pnpm, Go, Rust through
  `pnpm tauri build --no-bundle`). `--from DIR` uses ready ones. Without
  either, it uses the last build in `app/src-tauri/target/release`.
- **Install:** into `~/.local/share/noclickdock2` (`NCD2_HOME`). Each
  binary is written beside its target, then renamed over it, so no half
  binary is ever left.
  - `ldd` finds missing libraries, and the script names the packages. A
    missing AppIndicator is said, but does not stop the install.
  - A menu entry with a Settings action, and an icon.
  - A login entry named `NoClickDock.desktop`, the same file the app's own
    launch-at-login manages, so the two never duplicate.
- **First install:** runs `ncd-core --migrate` (new flag) to bring
  NoClickDock 1's settings over (read only, once) *before* anything
  disables the legacy login entries, which say which widgets were in use.
- **`--replace-legacy`:**
  - It stops the Python widgets, but only processes running from NCD 1's
    prefix (`~/.local/share/noclickdock/`).
  - It disables their login entries the XDG way (`Hidden=true`), after
    keeping a `.ncd1-backup` copy. Nothing is deleted.
  - `--restore-legacy` copies the backups back.
- **Other modes:** `--status`, `--uninstall` (settings kept), `--no-start`.
  Without a desktop session it installs but does not start.
- **Tested with a scratch HOME only** (never this machine's session), on
  the release build:
  - invented legacy entries, a `bar.conf`, and a stand-in Python widget
    process;
  - install with `--replace-legacy`: settings migrated (dock, which
    widgets run), the stand-in stopped, three entries disabled with
    backups, the NCD 2 login entry written;
  - `--uninstall` and `--restore-legacy` both behaved as described;
  - the user's real NoClickDock 1 processes (9) were untouched throughout.

## Windows installer

- `install.ps1` moved to `scripts/install.ps1`, with its search path
  adjusted. `scripts/build-windows.sh` copies it into `dist/windows`.
- It still parses with PowerShell 7.

## README

- Rewritten for NoClickDock 2:
  - what it is, and the no-click rule;
  - each widget as it now behaves;
  - install on Linux (and from NoClickDock 1), and on Windows (honest: it
    has not been run there);
  - the optional VRAM junction tool, Settings, the power log and report;
  - how it is built, and development.
- **Screenshots:** design's gallery shots from `--mock` (invented data,
  2×), copied into `assets/screenshots/`. The hero is the rail beside the
  Docker panel. The old Python mockups are gone, including the
  now-unreferenced `panel-docker-stopped.png`.
- It replaces the user's uncommitted scrub of the old README: the old
  example hostnames are gone with it.

## The Python generation, removed

- **Removed:** `core/shinywidget.py`, `dock/`, `widgets/` (every widget's
  script, README and images, plus the report generator), and
  `tools/make-mockups.py`. The old `install.sh` is replaced by
  `scripts/install.sh`. Git keeps their history.
- **Kept:**
  - `tools/gddr6-oneshot.c`, moved from `widgets/power/tools/`: Power still
    uses the tool it builds.
  - `assets/logos/`, used by the README.
- **The user's uncommitted scrub** of `widgets/tailscale/README.md` and
  `tailscale-status-checker.py` was not discarded.
  - Those two files are removed from the index only and stay on disk,
    untracked, with the user's edits.
  - A copy of the diff is in the scratchpad (`user-scrub.patch`).
  - Main or the user can delete them once the swap is done.

## Checks

- `go vet`, `go test ./...` and cargo clippy pass.
- The Linux release and the Windows cross-build both build.
- No personal paths, hostnames or addresses in the new files.
- Research's branch-wide secret scan is still to come.
