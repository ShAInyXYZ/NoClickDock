# M7 — Windows · executor report

Branch `rewrite/tauri-go-svelte`. Commits: `12f1163` (installer, paths,
GPU sensor naming) and this report's commit (`scripts/build-windows.sh`).
The latency attempt from design M1 r5 is in `db7ba66`.

**Status: compile-verified, not run on Windows.** There is no Windows
machine or VM here. Everything below builds for `x86_64-pc-windows-msvc`,
and every Go package passes `go vet` for `GOOS=windows`. Nothing has
started on Windows yet.

## Build

- `scripts/build-windows.sh` is the proven cargo-xwin recipe
  (research/tauri-windows.md). It finds LLVM 14's `llvm-rc` by itself and
  writes `dist/windows/` (git-ignored): `ncd.exe`, `ncd-core.exe` and
  `install.ps1`.
- **ncd.exe:** 6.9 MB, PE32+ GUI. It imports only system DLLs and the
  Universal CRT api-sets: no `WebView2Loader.dll`, no `VCRUNTIME140.dll`.
- **ncd-core.exe:** 10.0 MB, GUI subsystem (`-H=windowsgui`), so no console
  window flashes. It embeds `time/tzdata`.

## install.ps1 (rewritten for NoClickDock 2)

- **NoClickDock 1 first:** its Python widgets are stopped (matched by their
  script names), and their `NCD *.lnk` Startup shortcuts are moved aside to
  `%LOCALAPPDATA%\NoClickDock\legacy-startup`, never deleted. While those
  widgets run they delete every `*.json` in `%APPDATA%\NoClickDock`,
  config.json included. `-RestoreLegacy` puts the shortcuts back.
- **WebView2 runtime:** checked in the EdgeUpdate client keys. When it is
  missing, the script says where to get it and stops.
- **Install:**
  - `ncd.exe` and `ncd-core.exe` are copied to
    `%LOCALAPPDATA%\NoClickDock\bin` and run through `Unblock-File`.
  - The `HKCU\…\Run\NoClickDock` entry is written, the same name the app's
    own launch-at-login uses, so the two never duplicate.
  - It then starts the app, unless `-NoStart`.
- **Other modes:** `-Status` is a small table. `-Uninstall` stops the app
  and removes the binaries and the Run entry; settings stay in
  `%APPDATA%\NoClickDock`.
- Parsed without errors by PowerShell 7.4 (a portable copy in the
  scratchpad). It has not been run.

## Windows paths, widget by widget

| widget | Windows path | limits |
|---|---|---|
| Claude | `%USERPROFILE%\.claude`, `claude.exe` or the npm shim. The shim's target is now run directly (node + `cli.js`); cmd.exe is only a fallback and never gets a path it would misread (`&`, `^`, `%`, parentheses). | — |
| Codex | `%USERPROFILE%\.codex` (or `CODEX_HOME`) | — |
| ComfyUI | HTTP and WS as on Linux; `nvidia-smi.exe`; CPU load from `GetSystemTimes` | CPU temperature says it needs a sensor driver |
| Disk | drive letters (fixed and network); Docker Desktop's CLI off PATH too | — |
| Docker | `docker` on PATH or Docker Desktop's `resources\bin` | — |
| Power | `nvidia-smi.exe`; CPU package power and temperature from LibreHardwareMonitor's WMI (go-ole) when present; CPU model and board from the registry; the log in the Documents known folder (OneDrive-safe) | Without LibreHardwareMonitor the CPU is estimated and the panel says so. Displays, USB and drives are not read. No VRAM junction temperatures. |
| Tailscale | `tailscale` on PATH or `%ProgramFiles%\Tailscale` | — |

## The shell on Windows (from M1–M6, unchanged here)

- **Windows:** `win.rs` hardens the overlays (no activation, tool window).
  Show and hide go through it: there is no compositor opacity on Windows.
- **Toasts:** they dismiss when pointed at, because click-through is
  unreliable there. They have no fade: the shell fade is Linux-only.
- **Menus:** muda's native menus.
- **One instance:** the plugin's Windows implementation.
- **Launch at login:** the Run key, release build only.

## Owed on a real Windows machine

- The whole first run: tray, rail placement, the panel swap. **Risk:**
  WebView2 may not paint a hidden window, so a panel drawn ahead in the
  hidden one may show late or blank for a frame. The page's 100 ms
  fallback keeps the swap from hanging.
- Toasts on hover, and `install.ps1` end to end, including
  `-RestoreLegacy`.
- A web-process crash: nothing recovers yet on Windows (WebView2's
  `ProcessFailed` is not wired).
- The tray's dark variant for light taskbars is not wired: the light glyph
  is always used.
- NoClickDock 1's Windows settings (`%APPDATA%\NoClickDock\bar.conf`) are
  not migrated; migration is Linux-only.

These are in FOLLOWUPS.md.
