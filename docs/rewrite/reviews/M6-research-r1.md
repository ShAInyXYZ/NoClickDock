# M6 — research review (single pass)

Reviewed: `c482456` (core: widgets on/off and order, field errors, asks,
migration), `8dbc033` (Settings UI) and `63488d2` (shell: Settings window,
one instance, launch at login, `--settings`), on a clean `git archive
63488d2`, built release as shipped.

Regression checks:
- UI: `svelte-check`, 0 errors and 0 warnings.
- Shell: `cargo test --lib`, 11 passed; `cargo check --target
  x86_64-pc-windows-msvc` ok.
- Core: `go vet` on linux and windows; `go test -race ./...` ok.

## What I ran

**The real app in a fake home.** I ran a plain `ncd` (production: no
`--instance`, no `--mock`) under `env -i`, with:
- `HOME` in the scratchpad and no XDG overrides;
- a private `dbus-daemon`;
- Xvfb `:95` with bare Mutter.

The fake home held an invented Python-generation tree:
- `bar.conf`: horizontal, size `l`;
- `tailscale.json`: a watch list;
- `power-monitor/config.json`: a time-of-use tariff, `poll_interval: 2.0`
  and the obsolete `monitor_watts_each`;
- three autostart entries, ComfyUI's with `--host 192.0.2.10 --port 8190`.

| check | result |
|---|---|
| migration | One log line names everything brought over. `config.json` holds: horizontal/large; the watch list; the 8 known Power keys (`poll_interval` as 2, `monitor_watts_each` left behind); ComfyUI 192.0.2.10:8190; the three widgets with an autostart entry enabled and the other four disabled. |
| legacy files | Byte-identical afterwards. |
| once | After changing `bar.conf` and restarting, the config still says horizontal/large. |
| launch at login | `NoClickDock.desktop` written to the fake home's `~/.config/autostart` (on by default). Only a release build of the real app registers the plugin; named instances and mocks never do. |
| one instance | A second plain `ncd` exits 0 at once (0.02 s; the very first handover on a fresh private bus took 10 s, which is D-Bus activation on that bus, not the app), and the first stays. `ncd --settings` exits 0, and the first instance opens "NoClickDock Settings". |
| real home | `~/.config/autostart`, `~/.config/status-widgets`, `~/.config/power-monitor` and `~/.config/noclickdock` (still absent) unchanged. |
| field errors | Six patches over stdio to a mock core. Each invalid one is answered `ok:false` with the field and a sentence: `widgets.power.peak_hours`, `…price_per_kwh`, `widgets.comfyui.host`, `dock.orientation`, `widgets.tailscale.watch`. The valid one gives `ok:true` and a new config. |
| security | The Settings capability allows only event listen/unlisten and its own window's drag, minimise and close. Config changes go through `core_send`, which the core validates. `{@html}`, `innerHTML` and `eval` appear nowhere in the interface. Of a second instance's argv, only `--settings` is read. |

## MUST-FIX

None of M6's own.

The cutover risk it opens, both generations starting at login, lands on
PM-Log. It is recorded as
[M5-research-r1.md](M5-research-r1.md) must-fix 1:
- launch at login is on by default;
- migration reads the legacy autostart entries but leaves them in place;
- so from the next login the Python dock and NoClickDock 2 both start.

Follow-ups are in [FOLLOWUPS.md](../FOLLOWUPS.md).

VERDICT: PASS
