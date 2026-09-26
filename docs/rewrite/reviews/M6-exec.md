# M6 — Settings and lifecycle · executor report

Branch `rewrite/tauri-go-svelte`. Commits:
- `c482456` — core: widgets on/off and order, field-level config errors,
  asks, legacy migration
- `8dbc033` — the Settings page and its gallery surface
- `63488d2` — shell: the Settings window, one instance, launch at login,
  `--settings`
- `7dd2612` — small items: A20-a Tailscale tones, transparent rail after a
  crash reload, the minimal rail after the sixth crash

## Settings

- **Window:** made on demand, never at start, and destroyed on close.
  - 560 × 680, undecorated, with our Cerveau title bar: drag region,
    minimise, close.
  - It runs in the rail's web process, so it costs no extra process.
  - Opened from the tray, the rail's and the dials' menus, and
    `ncd --settings`.
  - Its capability allows only events, dragging, minimise and close; it
    changes the config only through `core_send`.
- **Sections:** Widgets, Dock, Start, then Tailscale, Power and ComfyUI.
  - **Widgets:** each widget with its mark, an up/down order and an on/off
    switch.
  - **Dock:**
    - orientation;
    - size;
    - readouts (disabled in compact, which has none);
    - edge: right, left, top, bottom, or where I leave it. Choosing one
      moves the rail there, as far along as it was.
  - **Start:** launch at login.
  - **Tailscale:** watch the peers seen online, or only a list.
    - The list takes peers chosen from the tailnet (the core's `ask peers`)
      or typed by name.
    - Chips remove a peer.
  - **Power:** a flat or time-of-use tariff.
    - The 24 country presets, ported verbatim; one applies all five values
      (the Python applied two).
    - Rates, peak hours and the weekend rule.
    - Estimates: board and USB.
    - Poll and log intervals.
  - **ComfyUI:** host and port.
    - **Test** asks the core to probe that endpoint before anything is
      saved.
    - It reports the address actually asked and what answered there, e.g.
      `http://127.0.0.1:8188 answered · ComfyUI 0.3.60 · RTX 4090 · queue
      empty`.
    - On failure, why: connection refused, no answer within 5 s, no such
      host, or `HTTP 404 on /queue — is it ComfyUI?`.
- **Controls:** styled only. A bits-ui select (no native `<select>`),
  switches, a segmented radio group, 38 px fields, pill buttons.
- **Saving:**
  - Nothing applies until Save, and only what changed is sent.
  - The core validates and answers `config.result` with the field it
    refused, in a sentence, shown under that field. This covers type
    errors and unknown keys too.
  - A number that does not read blocks Save.
  - A config changed underneath (from the rail's menu) replaces a clean
    draft, never an edited one.
  - Closing with unsaved changes asks first.
- **Gallery:** `?surface=settings` draws it on the mock core's config. Save
  there changes nothing; Test and the peer list ask the mock, which
  answers from its invention and never touches the network.

## Core

- `widgets.<id>.enabled/order` (defaults on, 10…70) and
  `start.launchAtLogin` (default on).
  - The hub skips a disabled widget's collector and takes the rail order
    from the config.
  - A widget whose section changed is polled at once.
  - Power's `poll_interval` now applies live (the M5 gap).
- `ask` requests: ComfyUI `probe`, Tailscale `peers`. The shell allows the
  new message type.
- **Legacy migration,** read-only and once, on the real first start (no
  flags, Linux):
  - `status-widgets/bar.conf`: orientation, density (s/m/l → compact,
    normal, large) and position;
  - `status-widgets/tailscale.json`: the watch list;
  - `power-monitor/config.json`: key by key, so a refused value (a
    malformed `peak_hours`) does not lose the rest;
  - the autostart entries decide which widgets run. A disabled or hidden
    entry counts as not installed, and when none exists every widget
    runs. ComfyUI's `--host`/`--port` also come from its entry.
  - Tested against an invented legacy tree; every file is byte-identical
    afterwards.
- The power report says NoClickDock, not ShinyWidgets.

## Shell

- **One instance:** `tauri-plugin-single-instance`, registered only for
  the real app.
  - A second plain `ncd` hands its argv to the running one and exits.
    `--settings` opens its Settings; anything else reveals the dock.
  - Named instances and mocks stay separate.
  - Tested with a private session bus and a scratch HOME: `ncd --settings`
    and a plain `ncd` both exit 0, and the first instance opens Settings.
- **Launch at login:** `tauri-plugin-autostart`, entry name "NoClickDock".
  - It follows `start.launchAtLogin`, only in a release build of the real
    app.
  - A development build never touches `~/.config/autostart`; Settings says
    so there.
- **Safety:** a named instance on the real collectors now gets a scratch
  config, so no test run writes the real config or PM-Log.

## Small items (research M2 r5, M4 r2; A20-a)

- **Tailscale tones:** configuration is info and never raises the widget:
  - tailnet serve, services, advertised routes;
  - an offered or used exit node;
  - also a relayed path, an update available and a dormant peer's
    expired key. **These go beyond the list, by its principle** — in
    FOLLOWUPS for design to confirm.
  - A public funnel is warn: `public funnel :443`, toasted once, never
    crit.
- **Black corners after a crash:** the reloaded rail's first paint was
  opaque. `app.html` now marks the overlay pages transparent before
  anything paints.
- **Sixth crash:** a minimal rail (`dock?restarting`) shows the
  NoClickDock dial alone, crit, reading "interface restarting".
  - Everything is retried after a minute.
  - Only if even that crashes is the dock hidden until the retry.
  - Each step is logged in a banner.
  - Seven kills in a minute under Mutter on Xvfb: five reloads, then the
    minimal rail, then hidden, and the full rail back a minute later.

## Checks

- **Mutter on Xvfb** (`:97`, private gnome-shell, dead bus except the
  single-instance test):
  - turning Docker off removes its dial;
  - horizontal, the left edge and Tailscale moved up are all followed by
    the rail;
  - port 0 shows "The port must be between 1 and 65535, not 0." under the
    field;
  - Test answers;
  - the title bar's close destroys the window.
- **Automated:** `go test -race ./...`, `go vet` (Linux and Windows),
  cargo test and clippy (Linux, release, msvc) and svelte-check all pass.
  The gallery audit of `settings` has 0 findings.
- **Tests added:**
  - config field errors;
  - the full legacy migration;
  - disabled widgets and config order;
  - config results and asks in the hub;
  - the ComfyUI probe (answer, refused, not ComfyUI, bad input);
  - Tailscale peers and A20-a.

## Known gaps

In `docs/rewrite/FOLLOWUPS.md`.
