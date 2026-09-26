# Spec — comfyui

Parity reference: `widgets/comfyui/comfyui-status-checker.py` 1.0.2 — a
**standalone** GTK program (own runtime copy). Shared rules:
[dock-core.md](dock-core.md). Legend: **P** Python, **N** NCD 2, **BUG** do not
carry, **Q** decision for main.

## Identity

id `comfyui` · title `ComfyUI` (P `COMFYUI STATUS`) · order 30 · poll **2 s**
· width N 400 CSS px · mark: comfy.org favicon mark (P path, **viewBox 48**,
not 24) — monochrome.

## Endpoint

P: `--host` (default `127.0.0.1`) and `--port` (default `8188`) command-line
arguments; a dialog opened by clicking the panel's Endpoint row can change
them for the session only (never saved). N: `widgets.comfyui.host` /
`port` in config, edited in Settings → ComfyUI; migrate from the legacy
autostart `Exec` args (dock-core §8). `base_url = http://<host>:<port>`
(IPv6 hosts must be bracketed — P does not; **BUG**).

## Data sources

### HTTP (every 2 s)

| url | timeout | used |
|---|---|---|
| `GET /queue` | 5 s | `len(queue_running)`, `len(queue_pending)` |
| `GET /system_stats` | 5 s | `system.{comfyui_version, pytorch_version, python_version, os, ram_total, ram_free}`, `devices[]` |

Any failure of either request → **offline** (all defaults). Header
`User-Agent: comfyui-status-checker/1.0`.

Verified against a live ComfyUI here: `system` also carries `argv`,
`embedded_python`, `comfy_package_versions`, … (ignored); each device has
`index, name, torch_vram_free, torch_vram_total, type, vram_free, vram_total`.

Device normalisation (`_device_info`), everything derived from the response:
- raw name like `cuda:0 NVIDIA GeForce RTX 4090 : cudaMallocAsync` → split the
  allocator off after the last ` : `; if the first token contains `:` (torch
  device string) → `label = "cuda:0"`, `name = rest`; else label = name.
- `index` = `dev.index` or the position in the list.
- `vram_used = max(0, vram_total − vram_free)`; `torch_vram_used` likewise.
- `python_version` cut at the first `(`; `pytorch_version` shown up to `+`.
- short GPU name: strip leading `NVIDIA GeForce `, `NVIDIA `, `AMD Radeon `,
  `AMD `, `Intel(R) `.

### Local sensors (only when the endpoint is this machine; every 5 s, cached)

`endpoint is local` = host ∈ {`127.0.0.1`, `::1`, `localhost`, `0.0.0.0`}, or
it resolves to 127.0.0.1/::1, or to one of the addresses of this host's own
hostname. Otherwise no sensors at all — never mix two machines in one panel.

- GPUs: `nvidia-smi --query-gpu=index,temperature.gpu,utilization.gpu,power.draw,power.limit,fan.speed --format=csv,noheader,nounits`
  (timeout 4 s); `[N/A]` → none; merged into devices **by index**. Caveat:
  torch's CUDA index order (`CUDA_DEVICE_ORDER`, default fastest-first) can
  differ from nvidia-smi's PCI order on multi-GPU machines — P ignores this;
  N: when both lists have the same length and the names differ at an index,
  match by name instead, else skip telemetry for that device.
- CPU temp (Linux): first match among hwmon devices named `k10temp`,
  `zenpower`, `coretemp` whose input label is `Tdie`, `Tctl`, `Package id 0` or
  unlabelled; else any hwmon input whose label starts with `Package`/`Tdie`/
  `Tctl`. Value `temp*_input / 1000`.
- CPU utilisation: `/proc/stat` first line delta (idle = idle + iowait) between
  successive samples; first sample yields none.
- Windows: CPU utilisation via `GetSystemTimes` deltas; CPU temperature is not
  readable without a sensor driver → omit (say so once in the CPU section:
  "CPU temperature needs a sensor driver on Windows"). `nvidia-smi.exe`
  (PATH, then `%ProgramFiles%\NVIDIA Corporation\NVSMI\nvidia-smi.exe`,
  `%SystemRoot%\System32\nvidia-smi.exe`) with `CREATE_NO_WINDOW`.

### WebSocket progress (best effort)

P connects to `ws://<host>:<port>/ws?clientId=<random uuid>` (own minimal
client, reconnect every 3 s), and on text frames:
- `{"type":"progress","data":{"value":v,"max":m}}` → progress toast `Step
  v/m  (pct%)` (6 s, updated in place);
- `{"type":"executing","data":{"node":null}}` → execution finished → clear.

**Verified in ComfyUI's source** (`server.py send_json`, `execution.py`):
`executing`, `progress`, `executed` are sent **only to the socket whose
clientId queued the prompt** (`server.client_id` from the prompt's
`extra_data.client_id`); only messages with `sid=None` (e.g. `status`) are
broadcast. The browser UI always sends its own client_id, so a monitor with a
random id receives progress **only for prompts queued without a client_id**
(API scripts). P's step toasts therefore almost never fire for UI-queued work.

N: keep the websocket (Go `nhooyr.io/websocket` or `gorilla`), use `progress`
when it arrives to set `snapshot.progress = value/max`, clear it on `executing
node=null` or when `queue_running` becomes empty; otherwise `progress = null`
(indeterminate arc). **Never** connect with another client's id (ComfyUI
drops the older socket with the same id — it would disconnect the user's
browser). Also listen to the broadcast `status` event
(`data.status.exec_info.queue_remaining`) to refresh the queue between polls.
No per-step toasts (DESIGN: the dial's arc shows progress).

## State

| P state | condition | N state | stateLabel |
|---|---|---|---|
| offline | any HTTP failure | `idle` | `offline` |
| generating | `running > 0` | `busy` (progress or null) | `generating` (+ ` · 37%` when known) |
| queued | `running == 0 && pending > 0` | `busy` (progress null) | `queued · <n> pending` |
| idle | otherwise | `ok` | `idle` |
| error | **never set** (dead code in P) | — | — |

## Glance

generating with progress → `37%`; generating without → `run`; pending > 0 →
`q <n>` (`q 3`); idle → device-0 VRAM used % (`14%`); offline → `off`. Tone
neutral (the LED and the busy arc carry state).

## Toasts (P `_apply_data`, after the first poll, on state change)

| from → to | text (P) |
|---|---|
| any → generating | `Generating...` |
| generating → idle | `Generation complete` |
| any → offline | `ComfyUI offline` |
| offline → idle | `ComfyUI online` |
| any → queued | `Queued (<n> pending)` |
| other changes (queued → idle, idle ↔ …) | none |

N: same table as `transitionMessage(old, new)`; sentence case allowed
(`Generation complete`, `ComfyUI is offline`). Tones: generating/queued busy,
complete ok, offline idle, online ok. No alert ids. Generation toasts are
frequent on a busy machine — they stay (they are P behaviour), but the
2-second poll means a job shorter than 2 s is never seen; that is fine.

## Panel (single page), top to bottom

1. **Queue**: P row `Running N    Pending N` → N `stats {items: [{key:
   "running", value}, {key: "pending", value}]}` directly under the header
   (no section needed; P had `QUEUE`).
2. Offline: `row {name: "endpoint", value: base_url, mono}` + empty-state
   sentence "ComfyUI is not answering at `http://127.0.0.1:8188`." and stop
   (P shows only queue zeros + endpoint row).
3. Per device (`section`): single device `GPU · <short name>`; several
   `<LABEL> · <short name>` (`CUDA:0 · RTX 4090`), meta = allocator when
   present (P shows allocator nowhere despite the comment — N puts it in the
   section meta):
   - `meter {label: "VRAM", frac, value: "9.2 GB / 24.0 GB (38%)"}` when
     `vram_total > 0`;
   - `meter {label: "Torch VRAM", frac, value: "6.1 GB / 8.0 GB"}` when
     `torch_vram_total > 0` (hidden with cudaMallocAsync, where torch reports 0);
   - telemetry `stats`: `load 87% · temp 71°C · power 312W · fan 45%`, each
     only when reported.
   Meter tones: P green < 0.60, yellow < 0.85, red ≥ 0.85 → N `warn` ≥ 0.85
   **Q** (P used 0.60 for yellow — too eager for VRAM, which ComfyUI fills by
   design); crit ≥ 0.95; else none.
4. **CPU** (only with local sensors): `stats` `load 23% · temp 54°C · ram 41%`.
5. **System**: `meter {label: "RAM", value: "51.2 GB / 125.7 GB"}`;
   `stats` `comfyui 0.3.x · torch 2.x · python 3.12.x`; row `endpoint` =
   base_url (mono) — P made it a button opening the endpoint dialog; N: plain
   row, editing lives in Settings.

`fmt_bytes` here is ComfyUI's own: binary, one decimal, with a space and a
two-letter unit (`9.2 GB`, `512.0 MB`); `—` for none. Percentages `%.0f`.

## Menu

P: `Refresh now`, `Open ComfyUI` (browser at base_url), separator, `Quit
ComfyUI widget`. N: `{id:"open-ui", label:"Open ComfyUI", url: base_url}`.

## Config

`widgets.comfyui.host` (string, default `127.0.0.1`), `port` (1–65535,
default 8188). Changing the endpoint resets state to offline, re-evaluates
"local", resets sensor caches and reconnects the websocket (P does all of this
in the dialog handler — keep).

## Windows

HTTP/WS identical. Sensors as above (no CPU temperature). ComfyUI Desktop on
Windows listens on port 8000 by default — Settings should make the port easy to
change (not auto-detected in P).

## Subtle behaviours to keep

- Sensors only for a local endpoint.
- Every device the server reports, in its order and naming (cuda, cpu, mps,
  xpu…); nothing assumed about count or vendor.
- Torch VRAM row omitted when the allocator hides it.
- 2 s poll for the queue, 5 s cadence for sensors.

## Bugs not to carry

- Unused `error` state; unreachable endpoint dialog (clickable row) replaced by
  Settings; session-only endpoint.
- Progress toasts per step (and their dependence on a client id the monitor
  cannot have).
- IPv6 host not bracketed in the URL.

## Tests (M5)

Fixtures: `/queue` idle/running/pending; `/system_stats` with one CUDA device
(cudaMallocAsync, torch totals 0), two devices, a CPU-only device, missing
`index`; nvidia-smi CSV with `[N/A]`; websocket frames `progress`,
`executing node=null`, `status`, a binary frame, a ping. Local-endpoint
detection for `localhost`, `0.0.0.0`, `::1`, a LAN IP of this host, a remote
name.
