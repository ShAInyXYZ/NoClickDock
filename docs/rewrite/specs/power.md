# Spec — power

Parity reference: `widgets/power/power-monitor.py` 1.0.2 — a **standalone** GTK
program — plus the report generator `widgets/power/report/generate_report.py`
and `report_template.html` (identical to the copies the running install uses).
Shared rules: [dock-core.md](dock-core.md). Legend: **P** Python, **N** NCD 2,
**BUG** do not carry, **Q** decision for main. Hardware names below are
invented.

## Identity

id `power` · title `Power` (P `POWER MONITOR`) · order 60 · poll
`config.power.poll_interval` (default **2 s**) · width N 640 CSS px (two
columns) · mark: bolt `M13 2L3 14h9l-1 8 10-12h-9l1-8z` (viewBox 24) —
monochrome.

## Config (`widgets.power`, migrated from `~/.config/power-monitor/config.json`)

| key | default | meaning |
|---|---|---|
| `price_per_kwh` | 0.30 | flat all-in rate |
| `currency` | `EUR` | free text ≤ 5 chars |
| `overhead_watts` | 30 | board/fans/PSU losses, added to every total ("Board") |
| `peripheral_watts` | 5 | added **once** when any USB peripheral exists (not per device) |
| `poll_interval` | 2 | seconds |
| `log_interval` | `hourly` | `off hourly daily weekly monthly` |
| `tou` | false | time-of-use tariff on |
| `peak_rate` / `off_peak_rate` | null | fall back to `price_per_kwh` when null/0 |
| `peak_hours` | `6-13,15-22` | `a-b` ranges, hour `a ≤ h < b`, comma-separated |
| `tou_weekend_offpeak` | true | Saturday/Sunday entirely off-peak |

Ignore the obsolete `monitor_watts_each`. P also accepted `--price` /
`--currency` CLI overrides and created the config file with defaults on first
run (N: never write the legacy file).

Country presets: the 24-entry `COUNTRY_PRESETS` table in P
(`power-monitor.py:125`, all-in marginal household rates with sources in the
comment) — port verbatim as data. **BUG**: P's settings dialog applies only
`price_per_kwh` and `currency` from a preset, ignoring its `tou`, `peak` and
`off_peak`; N applies all five. P's dialog has no TOU fields at all (file
only); N Settings → Power exposes every key above.

Settings changes apply live (**BUG**: P's logger captures `log_interval` at
start; a change only took effect after a restart).

## Tariff functions

- `in_peak(t)` = tou on, not (weekend-offpeak and Sat/Sun), and `a ≤ t.hour <
  b` for some range.
- `rate_now()` = flat → `(price, none, none)`; tou → `(peak_rate or price |
  off_peak_rate or price, "peak"|"off-peak", next change)` where next change =
  first whole hour within the next 168 h whose peak-ness differs.
- `blended_rate()` (projections) = flat → price; tou → `(peak_h·peak +
  (168 − peak_h)·off)/168` with `peak_h = days · Σ(b−a)`, days = 5 if
  weekend-offpeak else 7.
- `cost_monthly(W) = W · 24 · 30 / 1000 · blended_rate()`.
- Rate display: `≥ 10` → no decimals (`31`), else 2 decimals unless the rate
  needs 3 (`1.30`, `0.29`, `0.185`).

## Data sources — Linux

| reading | source | notes |
|---|---|---|
| CPU package power | `/sys/class/powercap/intel-rapl:0/energy_uj` (P reads `intel-rapl/intel-rapl:0/…`) twice 0.5 s apart → µJ/0.5 s → W | also present on AMD Zen (`package-0`). Root-only on stock kernels ≥ 5.10 (PLATYPUS mitigation); readable here (0444) thanks to local setup. Unreadable → **estimate** (below). N: keep the previous reading and use the delta between polls (no 0.5 s sleep); handle wrap using `max_energy_range_uj`. |
| GPUs (NVIDIA) | `nvidia-smi --query-gpu=index,power.draw,power.limit,temperature.gpu,memory.used,memory.total,name,utilization.gpu,fan.speed,pci.bus_id --format=csv,noheader,nounits` (5 s) | one line per card; `[N/A]` → none (power/limit/vram → 0 in P); bus `00000000:11:00.0` → `11:00.0` |
| GPUs (AMD fallback) | `rocm-smi --showpower --showtemp --showmeminfo vram --json` (5 s) | keys `Average Graphics Package Power (W)`, `Max Graphics Package Power (W)`, `Temperature (Sensor edge) (C)`, `VRAM Total Used Memory (B)`, `VRAM Total Memory (B)`, `Card series`; util/fan none |
| VRAM junction temps | `sudo -n /usr/local/bin/gddr6-oneshot` (5 s), only if the file exists | lines `11:00.0 62` (`%02x:%02x.%x %d`, from `widgets/power/tools/gddr6-oneshot.c`); matched to GPUs by bus id; any failure → back off **60 s**; cards it does not support are simply absent |
| CPU temp | first hwmon named `k10temp`/`coretemp`/`zenpower`, `temp1_input / 1000` | (comfyui's finder is better — reuse it: Tctl/Tdie/Package) |
| CPU usage | `/proc/stat` delta (P sleeps 0.5 s between two reads; N: delta vs the previous poll) | idle = idle + iowait |
| RAM | `/proc/meminfo`: used = MemTotal − MemAvailable; GB = kB/1048576 | |
| NVMe | hwmon devices named `nvme`: model from `device/model`, temp `temp1_input` | flat **5 W** each |
| static: CPU model | `/proc/cpuinfo` first `model name` | read at start |
| static: board | `/sys/devices/virtual/dmi/id/board_name` (P via `cat`), fallback `sudo -n dmidecode -t baseboard` → `Product Name` | |
| static: RAM description | `sudo -n dmidecode -t memory` → `<n>x <size> · <type> · <speed> · <maker>` | usually fails without NOPASSWD — then absent |
| static: displays | `xrandr --query` connected outputs **with an active mode** (`WxH+X+Y`) → port + resolution; EDID from `/sys/class/drm/*/edid` of connected connectors (fallback `sudo -n cat`) → maker (PNP id bytes 8–9), model (descriptor `0xFC`), size (bytes 21–22 cm → inches), **est W = round((inches·1.1 + 2)·1.5)** | P pairs xrandr outputs with EDIDs **by order** — **BUG**: DRM connector order and xrandr order differ (and the NVIDIA driver often exposes no sysfs EDID). N: take the EDID per output from `xrandr --verbose --prop` (`EDID:` hex block) — exact pairing, no sudo. |
| static: USB | `lsusb` → names; skip lines containing `root hub`, `hub`, `virtual`, `smart card reader` (case-insensitive) | only used for the list and the flat peripheral watts |

P reads static info once at start; N: re-read every 5 min (hot-plugged
monitors and USB devices).

## Data sources — Windows (M7)

| reading | source |
|---|---|
| GPUs | `nvidia-smi.exe` same query (PATH, `%ProgramFiles%\NVIDIA Corporation\NVSMI\`, `%SystemRoot%\System32\`), `CREATE_NO_WINDOW` |
| CPU package power, CPU temp, NVMe temps | LibreHardwareMonitor WMI (`root\LibreHardwareMonitor`, class `Sensor`, `SensorType='Power'`, Name `CPU Package`/`Package`; `SensorType='Temperature'`) — only when that namespace exists (LHM running with WMI publishing; OpenHardwareMonitor: `root\OpenHardwareMonitor`). Query natively (go-ole/WMI), not by spawning PowerShell every 2 s. Absent → CPU power **estimated** and the panel says "CPU package power needs LibreHardwareMonitor on Windows"; temps omitted. |
| CPU usage | `GetSystemTimes` deltas |
| RAM | `GlobalMemoryStatusEx` |
| CPU model / board | registry `HKLM\HARDWARE\DESCRIPTION\System\CentralProcessor\0\ProcessorNameString`, `…\System\BIOS\BaseBoardManufacturer/BaseBoardProduct` |
| RAM description | WMI `Win32_PhysicalMemory` (Capacity, ConfiguredClockSpeed, Manufacturer, SMBIOSMemoryType) — no admin needed |
| drives (the "NVMe" rows) | fixed physical disks (`MSFT_PhysicalDisk` BusType NVMe=17, or `IOCTL_STORAGE_QUERY_PROPERTY`) → model, flat 5 W; temps only via LHM |
| displays | `EnumDisplayDevices` + `EnumDisplaySettings` (active outputs, resolution); EDID from `HKLM\SYSTEM\CurrentControlSet\Enum\DISPLAY\<id>\<instance>\Device Parameters\EDID` → same parsing and estimate |
| USB | `SetupDiGetClassDevs` / WMI `Win32_PnPEntity` with `DeviceID` starting `USB\`, excluding hubs/composite/root; friendly names |
| VRAM junction | not available (say nothing) |
| log dir | `FOLDERID_Documents\PM-Log` |

## Derived values

- `gpu_w` = Σ card power; `nvme_w` = 5 × drives; `periph_w` =
  `peripheral_watts` if any USB peripheral else 0; `overhead` =
  `overhead_watts`.
- `total_w` (PC through its own PSU; monitors excluded — they have their own):
  - CPU measured: `cpu + gpu + overhead + nvme + periph`
  - CPU unmeasured, usage known: `(10 + usage/100·190) + gpu + overhead + nvme + periph` (**estimate**)
  - neither: `gpu + overhead + nvme + periph`
- `mon_est` = Σ display estimates (shown, not in total).
- level: `< 100 low` (`idle`), `< 250 moderate` (`normal`), `< 500 high`
  (`high load`), else `extreme` (`full send`); unknown when no total.
- `avg_w` = mean of the last **1800** totals (≈ 1 h at 2 s; P averages samples,
  not time — fine).
- Donut: per-component averages over **60 s** windows (`cpu, gpu, nvme,
  periph, total`), published once a minute; the live values until the first
  minute closes (so it is never empty and does not twitch every 2 s).
- Thermal alerts (per poll): per GPU tag `GPU<index> <short name>`:
  VRAM junction `≥ 90 °C` → `…: VRAM 92°C`, else core `≥ 80 °C` → `…: core
  81°C`; CPU `≥ 85 °C` → `CPU 86°C`.

## State (N)

P: the dot colour is the load level (green/blue/yellow/red); nothing breathes
(`if False:`). Load is not health. N:

| state | condition | stateLabel |
|---|---|---|
| `warn` | any thermal alert active | the first alert text (`GPU0 VRAM 92°C`) |
| `idle` | no reading at all (no RAPL, no usage, no GPU) — practically never | `no readings` |
| `ok` | otherwise | `<level> · <W>W` (`high load · 412W`) — P badge `HIGH LOAD — 412W` |

## Glance

`<total>W` (`412W`, `1234W`); tone `warn` while any thermal alert is active.

## Toasts

- Thermal: once per component per excursion (tag set diff): texts above,
  tone warn (P red). These are the hub's alert ids (`tag`), messages the texts.
- Level change: P toasts **every** level change: `Idle 86W`, `Normal 180W`,
  `High load 312W`, `Full send 612W` (label + two spaces + watts). No
  hysteresis — at a threshold it flaps every 2 s (**BUG**). N: keep as the
  transition message but only after the new level has held for 60 s
  (**Q**: or drop level toasts entirely — DESIGN.md "a healthy dock is still").
  Tone neutral/info, never crit.
- Report: `Power report opened` · `No report yet — logging builds it`.

## Panel (single page, N `split` of two columns)

Header: P badge `LEVEL — W` → stateLabel. P title-right cluster → first body
block, a `stats` line: `avg 402W · 0.54 PLN/h · 1.30 PLN/kWh · peak until
13:00` (TOU: `peak|off-peak until HH:MM`, or `until Mon 06:00` when not today;
flat tariff: no window). cost/h = live total W /1000 × `rate_now`.

**Left column**

1. `section "Power draw"` + `table` columns `["", model, "W / cap", "core",
   "mem"?]` — the `mem` column only when any GPU reports a VRAM junction temp
   (then every row gets the column so core temps stay aligned). Rows:
   - `CPU` · cpu model · `<W>W` (or `—`; **N: `~<W>W est.` when estimated —
     P shows `0W` in the table and no CPU slice while the total contains the
     estimate: BUG**) · CPU temp.
   - per GPU: `GPU` (single) / `GPU<i>` · short name · `8/280W` (draw/cap) ·
     core temp · VRAM temp.
   - `Board` · board name or `—` · overhead W.
   Temperature tones: core/CPU `> 85 crit`, `> 70 warn`; VRAM `> 94 crit`,
   `> 84 warn`. **Block gap:** `table` rows have a row tone but no **per-cell
   tone** — temperatures need it. Proposed contract change: a cell may be a
   string or `{text, tone}`.
2. `section "Storage"` (when drives): rows `NVMe` · model · `5W` · temp.
3. `section "Displays (<n>) — own PSU"` (DESIGN: meta `own PSU`): rows port ·
   `<name> <size>" <res>` · `~<est>W`.
4. `section "USB (<n>)"`: rows with the device name only.

Model strings are cut to 26 chars + `…` in P; N lets the renderer ellipsise.

**Right column**

5. `donut {segments: [{label: "CPU", value: W, display: "42 PLN"}, GPU,
   Board, NVMe, USB] (only > 0), center: "<monthly cost of total>", unit:
   "<CUR>/mo"}` + legend rows (DESIGN: legend is rows) `CPU  42 PLN`; note
   `<kWh>/mo` = live total ×24×30/1000 (P) — N: use the donut's minute
   average for consistency.
6. Today (when logging produced rows or the current hour has samples): `stats`
   `today 4.1 kWh · 5.32 PLN` then `bars {values: 24 hourly average W,
   highlight: current hour}` (P sparkline: the last 24 logged rows plus none
   for the hour in progress; N per DESIGN A9: 24 **clock-hour** slots, past
   hours from the log, current hour from in-memory samples, future stub).
7. `section "Utilization"`: `meter` CPU `%`, RAM `%` with value `41/125G`,
   per GPU VRAM (`VRAM` / `VRAM<i>`) with `9.2/24G`. P colours: > 80 red, > 50
   yellow, else green → N tones **Q**: `warn ≥ 90`, `crit ≥ 97` for RAM/VRAM,
   none for CPU (load is not a fault); no green (DESIGN A6).

Footer: P `HH:MM:SS · logging hourly · Right-click: menu` → N
`snapshot.footer = "logging hourly"` / `"logging off"`, plus `CPU estimated`
when RAPL/LHM is absent.

## Logging (production only — never in `--dev`/`--mock`)

P `DataLogger`: every poll appends a sample `{total_w, cpu_w (0 when
unmeasured), gpu_w, cpu_temp, gpu_temp (max over cards), cpu_pct, ram_pct}`;
when `now − last_log ≥ interval` (3600/86400/604800/2592000 s) write one row
and clear. First row one interval after start (not aligned to the clock).

CSV (keep byte-compatible — the report and existing history depend on it):
`<LOG_DIR>/<YYYY-MM>/<YYYY-MM-DD>.csv`, header written when the file is new,
columns in this order:
`timestamp,avg_total_w,avg_cpu_w,avg_gpu_w,avg_cpu_temp,avg_gpu_temp,avg_cpu_pct,avg_ram_pct,samples,kwh_period,cost_period`
- `timestamp` = local time of the write, `%Y-%m-%d %H:%M:%S`;
- averages `%.2f` (temps/pcts averaged over samples where the value is truthy);
- `samples` integer;
- `kwh_period = avg_total_w · interval_secs / 3 600 000` (nominal interval);
- `cost_period = cost_monthly(avg_total_w)/720 · interval_h` (blended rate —
  **the report ignores it** and re-prices each row by the rate at `ts −
  30 min`).

N improvements (compatible): align periods to clock hours (first period
partial, kWh from the actual elapsed time — P charges a full nominal hour
even after a suspend), price `cost_period` by the rate in effect (not the
blend), fsync+append atomically. `LOG_DIR` = `~/Documents/PM-Log` (Windows
Documents known folder). ARCHITECTURE safety rail: dev/mock log to a scratch
directory.

"Today" (P `_today_from_logs`): rows of yesterday's and today's files; today's
kWh = Σ `kwh_period` of today's rows + in-memory samples `avg W/1000 ×
elapsed h`; cost = Σ `cost_period` + partial × `rate_now` (mixes blended and
live rates — **BUG**; N re-prices every row by its hour, like the report);
spark = rows of the last 24 h.

## Report

P `generate_report.py` (repo copy = running copy): reads every
`LOG_DIR/20*/*.csv`, re-prices each row at `ts − 30 min` with the current
tariff, splits kWh into CPU/GPU/other by the row's measured shares, computes
daily/monthly/rolling/hourly/weekday/heatmap/coverage/gaps/trend/drift/context
figures (full days = ≥ 20 rows), writes `report_data.json` and
`power-consumption-report.html` (template `report_template.html` with the
`/*__DATA__*/` placeholder replaced) into `LOG_DIR`. P re-runs it after every
log write and on "Open power report", then `xdg-open`s the HTML.

N: port the generator to Go (same `report_data.json` schema, same numbers —
unit-test against the Python on a fixture log), embed the template (visual
refresh is a design task), write both files to `LOG_DIR`; open with the
opener plugin. Report generation runs off the poll path.

## Menu

P right-click: `Settings`, `Open power report`, separator, `Quit`;
left-click (≤ 4 px movement) opens the report. N: `menu = [{id:"open-report",
label:"Open power report"}]` (core regenerates, then Rust opens the file —
dock-core §5.6) and Settings is already global.

## Subtle behaviours to keep

- Monitors are estimated and shown but excluded from the PC total (own PSU).
- Every GPU is a row (`GPU` alone when one card, `GPU0/GPU1…` when several);
  sensors a card lacks stay blank, never zero.
- VRAM junction column appears only when some card reports it.
- gddr6 probe back-off after any failure (transient PCI/sudo hiccups must not
  blind the widget until restart).
- Donut holds a minute average; the rest of the panel is live.
- Tariff visible on the panel (every cost figure derives from it).

## Bugs not to carry

Collected from above: CPU estimate missing from table/donut; display↔EDID
pairing by order; preset ignores TOU fields; log interval needs restart;
level-toast flapping; `today` cost mixing rates; CSV cost by blended rate;
nominal-hour kWh after suspend; 1 s of sleeps inside every poll; static info
never refreshed.

## Tests (M5)

Fixtures: nvidia-smi CSV with 2 cards and `[N/A]`, rocm-smi JSON,
gddr6-oneshot output, RAPL counter wrap, `/proc/stat` pair, meminfo, hwmon
tree, `xrandr --verbose` with EDID blocks (invented monitor), EDID bytes →
name/size/estimate, lsusb list with hubs. Tariff: peak/off-peak boundaries,
weekend, next-change search across a weekend, blended rate. Logger: row
maths and CSV text; report: Go vs Python numbers on a 3-day fixture log.
