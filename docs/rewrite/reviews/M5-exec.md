# M5 — collectors C · executor report

Branch `rewrite/tauri-go-svelte`. Commits:
- `ba80395` — ComfyUI, the shared `internal/sensors`, config for ComfyUI
  and Power, and bars that can say "not measured"
- `3052bca` — Power: draw, tariff, hourly log, report

Both follow specs/comfyui.md and specs/power.md, and DESIGN.md's writer
rules. Both mocks are drawn by the real render.

## ComfyUI (`core/internal/widgets/comfyui/`)

- **Endpoint:** `widgets.comfyui.host` / `port`, default 127.0.0.1:8188,
  validated. An IPv6 host is bracketed (the Python did not).
  - Changing it starts over: offline, "local" re-evaluated, sensors
    forgotten, websocket reconnected.
- **Every 2 s:** `/queue` and `/system_stats`, 5 s each, our own
  User-Agent. Any failure means offline.
- **Devices:** every device the server reports, in its order and naming.
  - The allocator goes in the section meta.
  - A missing `index` is the device's position.
  - Several devices: `CUDA:0 · RTX 4090`; one: `GPU · RTX 4090`.
  - VRAM meter tones: warn ≥ 85 %, crit ≥ 95 % (Q5).
  - Torch's share is a meter without tone: a full reservation is its
    normal state. Hidden when torch reports 0 (cudaMallocAsync).
- **Local sensors,** only when the endpoint is this machine, read every
  5 s:
  - "This machine" means loopback, unspecified, one of the interfaces'
    addresses, or what the hostname resolves to.
  - Card telemetry comes from nvidia-smi, merged by index. When torch and
    nvidia-smi order the cards differently, a card is matched by name if
    both lists are the same length; otherwise that device gets no
    telemetry.
  - CPU temperature (Tctl/Tdie/Package) and load (`/proc/stat` deltas);
    on Windows `GetSystemTimes`, plus "CPU temperature needs a sensor
    driver on Windows".
  - The CPU section has no RAM figure: the System section's meter already
    shows it (nothing twice).
- **Websocket,** under a random client id of our own (never another
  client's), reconnecting every 3 s.
  - A `status` event polls at once.
  - `progress` sets the arc; `executing node=null` or an
    `execution_*` event clears it.
  - ComfyUI sends progress only to the client that queued the prompt, so
    for browser-queued work the arc stays **indeterminate**. /queue and
    history carry no step counts, so nothing honest can be derived from
    them.
  - A finished node (`value = max`) goes back to indeterminate: the next
    node may never report.
  - Preview images (binary frames) and pings are handled.
- **States, labels and glance:**
  - `generating · 37%` / `37%`;
  - `generating` / `run`;
  - `queued · 2 pending` / `q 2`;
  - `idle` / device 0's VRAM;
  - `offline` / `off`: the empty sentence alone, with the endpoint as a
    command chip.
- **Toasts:** "Generating…", "Generation complete", "Queued, 2 pending",
  "ComfyUI is offline" / "is online".
  - A queue that drains unseen says nothing.
  - Queued → generating (busy to busy) raises no toast.
- **Real machine:** ran against the local ComfyUI (five CUDA devices,
  telemetry matched). The output stayed in the scratchpad.

## Power (`core/internal/widgets/power/`)

- **Sources (Linux):**
  - RAPL `energy_uj`, as the delta between polls. A counter wrap uses
    `max_energy_range_uj`. A refresh within 0.2 s repeats the last value.
    Only the first poll waits 0.5 s.
  - When RAPL is unreadable, the CPU is estimated from its load and the
    panel says why (root-only on the kernel, or no counter).
  - GPUs through nvidia-smi, with rocm-smi as fallback.
  - VRAM junction temperatures through `sudo -n /usr/local/bin/gddr6-oneshot`,
    only if installed:
    - never a prompt;
    - off the poll path, every 5 s;
    - a minute's back-off after any failure;
    - matched by bus id.
  - NVMe hwmon (a flat 5 W each).
  - Board from sysfs, else `sudo -n dmidecode`; RAM from
    `sudo -n dmidecode -t memory`.
  - USB through lsusb, with hubs, readers and virtual devices left out.
  - The slow sources are re-read every 5 minutes, off the poll (monitors
    and USB are hot-plugged).
- **Displays (the pairing bug fixed):** each output's EDID comes from
  `xrandr --verbose --prop`, printed under that output.
  - An output xrandr shows no EDID for uses the DRM connector of the same
    name.
  - Never paired by order.
  - A BMC's virtual output has no active mode and is left out.
  - The estimate is P's: `round((in·1.1 + 2)·1.5)`.
  - The cell shows size and mode (`27″ 2560×1440`), as design drew it; the
    model name only when there is no size.
- **Tariff:** flat or time-of-use, with the peak window and its next
  change (`peak until 13:00`, `off-peak until Mon 06:00`), a blended rate
  for projections, and P's rate text.
  - One figure is never priced two ways. Today's hours are each priced at
    their own rate. The log's `cost_period` sums every poll's energy at
    the rate in effect.
- **Panel (the approved mock, now real):**
  - thermal alerts first;
  - a stats line: avg, cost per hour, rate, window;
  - one table: draw, storage, displays (`own PSU`), USB as one row, with
    the names in a note under it. Numbers carry no units and `~` marks an
    estimate: board, NVMe, USB, displays and an estimated CPU;
  - the donut: a minute's average, largest first, monthly cost per
    component, the estimated CPU included;
  - today: kWh and cost, then 24 clock-hour bars;
  - utilisation meters: RAM/VRAM warn 90 / crit 97, CPU without tone.
  - A card without a power reading shows `—`, not 0 W.
- **State:** a thermal alert, else `<level> · <W>W`, else `no readings`.
  - Temperature cell tones start at the alert thresholds (core 80, CPU 85,
    VRAM 90). Crit is above 85 / 95 / 94. **This departs from the spec's
    >70 warn:** under A20 every toned cell is a state, so the lamp, the
    label, the toast and the cell now agree.
  - A crit reading makes the widget crit, with the alert's words.
- **Level toasts:** `High load at 312W`, once a new level has held 60 s
  (Q3), tone info.
- **Hourly CSV (byte-compatible):** same columns, `%.2f`, `\r\n` line
  endings, `<dir>/<YYYY-MM>/<date>.csv`, header when new, appended in one
  synced write. Improvements:
  - Periods are aligned to the clock. The first is partial.
  - The timestamp is the end of the hour the row covers (the report
    prices a row by the hour before its timestamp).
  - kWh comes from the time elapsed. A gap over 60 s (a suspend) counts
    nothing; P charged a nominal hour.
  - `log_interval` applies live.
- **Where it writes:**
  - `~/Documents/PM-Log` **only in production**: plain `ncd-core` with no
    `--dev`, `--mock`, `--once` or `--config`.
  - Otherwise a scratch directory: beside the scratch config, or
    `~/.cache/noclickdock-dev/once/PM-Log`.
  - The shell now gives a named instance (`ncd --instance X`, real
    collectors) a scratch config too, so no review or test run can write
    PM-Log.
  - I never ran a production core here. `--once power` on the real
    collectors left PM-Log untouched (checked by mtime); the user's Python
    monitor was writing it throughout.
- **Report:** `generate_report.py` ported to Go.
  - `html/template`, with the repo's template embedded and its
    placeholder made an action. Written with `report_data.json` next to
    the log, through a temp file and a rename.
  - Rebuilt off the poll after each CSV row and on "Open power report".
  - It is opened by a core action (`xdg-open`; on Windows the URL
    handler), never a `file://` menu url. No rows: "No report yet —
    logging builds it".
  - **Same numbers:** `report_data.json` equals the Python's, field for
    field, on an invented 25-day log (a gap, a thin day, rows without a
    GPU temperature, a malformed row) under a time-of-use tariff.
  - Two ties first differed. Python 3.12's `sum()` uses Neumaier
    compensated summation and `statistics.mean` is exact, so both are
    ported as such.
- **Windows:**
  - GPUs through nvidia-smi.exe.
  - CPU package power and temperature from LibreHardwareMonitor's WMI
    namespace (or OpenHardwareMonitor's), queried natively through
    go-ole, with a minute's back-off when absent. Without it: estimated,
    and "CPU package power needs LibreHardwareMonitor on Windows".
  - CPU model and board from the registry.
  - Displays, USB and drives on Windows: M7. Compile-only here.

## Contract notes for ARCHITECTURE (main's file)

- `bars.values[]` may hold `null`: not measured, drawn as a floor stub.
- `config.json` gains `widgets.comfyui {host, port}` and `widgets.power`
  (the spec's keys, validated; peak hours refused if malformed).
- New dependencies: `github.com/coder/websocket` (no dependencies of its
  own), `github.com/go-ole/go-ole` and `golang.org/x/sys` (Windows only in
  use). x/sys is pinned at v0.30.0, which still builds with Go 1.23.

## Tests

`go test -race ./...`, `go vet` (Linux and Windows), cargo
test/clippy, svelte-check: all pass. The gallery audit over 35 surfaces
has 0 findings.

- **sensors:** nvidia-smi CSV with two cards and `[N/A]`, a `/proc/stat`
  pair, meminfo, a hwmon tree.
- **comfyui:**
  - `/queue` idle, running and pending;
  - `/system_stats` with one CUDA device (cudaMallocAsync), two devices, a
    CPU-only one and a missing index;
  - telemetry by index, by name, and refused;
  - local detection for localhost, 0.0.0.0, ::1, a LAN address of this
    host, and a remote name;
  - panels (generating with and without progress, queued, idle, offline,
    Windows);
  - collect with transitions against a local server;
  - websocket frames: binary, ping, status, progress, executing null, and
    a reconnect under the same id.
- **power:**
  - an EDID built in the test; `xrandr --verbose` with three outputs, a
    BMC and one without an EDID;
  - lsusb with hubs, dmidecode memory and baseboard, gddr6-oneshot,
    rocm-smi;
  - a RAPL wrap;
  - the tariff at its boundaries, a weekend, the next change across a
    weekend, the blend;
  - the logger's row and CSV text, a suspend, off;
  - today's pricing;
  - level hysteresis;
  - the panel line by line, a crit temperature, the estimates, no
    readings;
  - the report against the Python.

## Known gaps

- `poll_interval` is read at start (the hub's cadence is fixed at
  registration). `log_interval` and the tariff apply live.
- Legacy migration of `~/.config/power-monitor/config.json` and the
  ComfyUI autostart endpoint is M6, with the Settings window (the country
  presets live there too).
- A production core started while the Python monitor still runs would log
  alongside it. Q7 has the installer stop the legacy widgets first.
- The report page's footer still says "ShinyWidgets": its visual refresh
  is a design task.
