# Spec — dock and shared runtime

Parity reference: `dock/ncd-dock.py` 1.0.1 (the rail) and
`core/shinywidget.py` 1.3.0 (the per-widget runtime used by codex, disk,
docker, tailscale). claude, comfyui and power predate the core and carry their
own copy of the same runtime (differences noted in their specs). Contracts:
[ARCHITECTURE.md](../ARCHITECTURE.md), [DESIGN.md](../../../DESIGN.md), windowing
facts in [research/tauri-windows.md](../research/tauri-windows.md). Where this
spec and DESIGN.md disagree on visuals, DESIGN.md wins; this spec wins on
behaviour and data.

Legend: **P** = what the Python does today (parity), **N** = what NCD 2 must do,
**BUG** = Python behaviour not to carry over, **Q** = open decision for main.

---

## 1. What changes shape

P: eight processes. Each widget is its own GTK process with its own dot
window, panel, toasts and poll thread; the dock is a ninth process that
publishes slot coordinates in `bar.json`, which every dot polls every 50 ms and
moves itself to; dots register themselves as `status-widgets/<name>.json`
(`{pid, name, xid}`) and the dock discovers them by listing that directory and
pruning dead pids.

N: one Go core polls everything, one Tauri shell draws one rail. None of the
file-based IPC survives: no `bar.json`, no `corner.json`, no registration
files, no X11 foreign-window moves, no `~` corner cycling (it needed keyboard
focus on the dot, which NCD 2 never takes). The per-widget "Quit X widget"
menu item becomes "disable in Settings".

---

## 2. The rail

| aspect | P | N |
|---|---|---|
| orientation | `vertical` / `horizontal`, radio items in the rail's right-click menu; saved in `bar.conf` | same two values; set in Settings → Dock (and in the rail's right-click menu for parity) |
| density | `s`/`m`/`l` = "Compact"/"Cozy"/"Roomy": gap 0/4/10 px, end pad 10/12/14, cross pad 4/5/7; dots are always 44 px | DESIGN.md: dial 32 / 40 / 48 CSS px; compact also hides readouts. Map legacy `s→compact`, `m→normal`, `l→large`. |
| grip | leading 14 px, three dim dots | DESIGN.md grip (14 px, three `--faint` dots) |
| layout | length = `2·pad_end + GRIP + n·44 + (n−1)·gap`; cross = `44 + 2·pad_cross` | computed by the page in CSS px; Rust sizes the window with `fit()` to `css × ui_scale` |
| order | alphabetical by widget name (the dots sort their registration files): claude, codex, comfyui, disk, docker, power, tailscale | `order` in the snapshot; defaults 10,20,…,70 in that alphabetical order; reorderable in Settings |
| membership | whatever widget processes are alive | every widget enabled in config; a disabled widget has no dial and its collector does not run |
| drag | press on the pill (not a dot) → manual `move()` on motion; on release publish + save | `startDragging()` from the grip/body (not a dial). Proven to work on the non-focusable rail. |
| snap | **none** | DESIGN.md: snap to the nearest work-area edge within 24 CSS px after the drag ends |
| clamp | at layout time, clamp the rail inside the **monitor geometry** (not work area) of the monitor at its top-left point | clamp inside the **work area** of the monitor holding the rail's centre, on start, after drag, on orientation/density/membership change, and on work-area change |
| stacking | UTILITY type, sinks below the dots (`lower()`) | one window; UTILITY type (Mutter stacks DOCK above every keep-above window — `ncd-dock.py:102`) |
| close | "Close bar (release widgets)" | no equivalent (Quit from the menu/tray quits everything) |

### Persistence

P `status-widgets/bar.conf` = `{"orientation","size","x","y"}` (x,y = GDK
logical px of the rail's top-left; GDK scale is 1 on this machine, so they are
physical px). Saved on drag release and on every menu change. Loaded at start
with defaults `vertical`, `m`, `60,60`.

N `config.json → dock`:

```jsonc
"dock": {
  "orientation": "vertical",            // vertical | horizontal
  "density": "normal",                  // compact | normal | large
  "readouts": true,                     // glance text under dials (forced off in compact)
  "position": {"monitor": "DP-2", "edge": "left", "along": 0.5, "x": 60, "y": 60}
}
```

`monitor` = Tauri monitor name, `edge` = the snapped edge or `null`, `along`
= centre position along that edge as a fraction of the work area, `x/y` =
physical top-left as a fallback. Restore: named monitor present → edge+along
(or x,y) inside its work area; missing → primary monitor, clamped. Save
debounced (≥ 500 ms) after a drag ends, never during.

---

## 3. Panel placement

P (`_dock_panel_pos`, docked case; `shinywidget.py:166`):

- horizontal rail: panel below the rail (`rail.bottom + 8`), flipped above
  (`rail.top − ph − 8`) if it would pass the monitor bottom; x centred on the
  dot;
- vertical rail: panel right of the rail (`rail.right + 8`), flipped left if it
  would pass the monitor's right edge; y = **the dot's top** (not centred);
- clamp to the monitor geometry with a 4 px margin.
- Undocked dot: right of the dot `+6`, flipped left, y = dot top, clamped.
- The panel is re-placed after every data refresh while open ("a grown panel
  would otherwise run off-screen").
- Body max height: `max(200, min(panel_max_height (640), 70 % of monitor
  height))`, then it scrolls (header and footer stay).

N (DESIGN.md wins where it differs): perpendicular to the rail on the side
with more room (not "right unless it does not fit"), 8 CSS px gap, vertically
(or horizontally) **centred on the dial**, clamped to the **work area** of the
monitor holding the dial; never covers another dial (a panel clamped back over
the rail must be pushed to the other side instead). Max height = work area −
32 CSS px; body scrolls. Re-fit and re-place on every snapshot while open.
Order of native calls: `fit` → `set_position` → show (research doc).

---

## 4. Hover choreography

| | P | N (DESIGN.md) |
|---|---|---|
| open | on `enter-notify` of the dot, immediately (unless dragging) | 120 ms dwell on a dial |
| switch | n/a (each dot has its own panel; leaving one closes it after 200 ms) | immediate while any panel is open |
| close | 200 ms after leaving dot or panel, check the pointer is outside both (±4 px tolerance) | 280 ms grace after leaving dial and panel; re-entering either cancels |
| pages | hover a tab 160 ms (and still over the strip) → switch; click switches at once | 180 ms hover dwell, click works |
| drag | pressing the dot closes its panel | pressing the rail/grip closes the panel |
| right-click | closes the panel, opens the menu | same |
| action armed | survives polls | same, and the panel must not close while a button is armed and the pointer is inside |

Selected page per widget persists across polls and across close/open (P keeps
`app.page` on the widget object). If the page vanishes (docker becomes
unavailable → no pages) fall back to the first page. Scroll position is kept
across a data refresh of the same page and reset to the top on a page switch
(P `update_data` keys the view on `(page, app)`).

The hover state machine lives in one place fed by both windows (rail and
panel are separate X windows — see research (a)); recommended in Rust.

---

## 5. Shared runtime (Go hub)

### 5.1 Poll loop

P: one thread per widget: `data = fetch()` (exception → `{"error": True}`),
hand to GTK, `sleep(poll_secs)` — so the real period is fetch time + poll.
Some widgets refresh heavy sub-sources on their own slower cadence inside
`fetch()` (claude/codex usage 120 s, claude limits 300 s on a separate
thread, disk docker-df 300 s, docker system page 300 s, comfyui sensors 5 s,
power donut 60 s averaging).

N: one goroutine per enabled widget. Each iteration: `ctx` with the widget's
collect timeout → collect → build snapshot → hub diff → emit → sleep until
`start + poll` (never overlap; if collect overran, start the next one after a
short minimum gap of 1 s). Slow sub-sources keep their own cadence and cache
inside the collector, and **a refresh or a normal poll must never replace a
slow sub-source with "missing"** (claude **BUG**: "Refresh now" blanks usage and
limits until the next poll because `_fetch()` returns `usage: None` and the
merge only fills absent keys — `claude-status-checker.py:1088`).

Poll intervals (s): claude 30, codex 30, comfyui 2, disk 30, docker 20,
power `config.power.poll_interval` (default 2), tailscale 30.

### 5.2 Refresh

P: "Refresh now" runs an extra `fetch()` on a new thread (concurrent with the
poll loop; results can arrive out of order).

N: `{"type":"refresh","widget":…}` triggers an immediate collect of that widget
(or all). If a collect is in flight, coalesce (mark "refresh pending", run once
more after it finishes). Refresh forces the slow sub-sources to re-read only if
older than 10 s (so hammering refresh cannot spawn `claude -p /usage` or
`docker system df` repeatedly).

### 5.3 Collector failure vs. observed outage

Two different things:

- **Observed state** (daemon down, CLI missing, status page unreachable, no
  filesystems): a normal snapshot whose state/label/blocks say what is true
  (`idle` or `crit` as each spec defines). Transitions and toasts apply.
- **Collector failure** (panic, timeout of the whole collect, parse bug): P
  turns it into `{"error": True}`, which each widget's `state()` then maps to
  some unrelated state (codex: `ok`, disk: "NO FILESYSTEMS", docker:
  "DAEMON UNREACHABLE") and toasts as a transition — **BUG**. N: keep the last
  good snapshot content, set `error` to the message (footer shows it in
  `--err`), state `idle`, and **do not** run transition/alert diffing for that
  poll (the previous state/alert sets stay as they were). Recover the goroutine
  (`recover()`), never let one collector kill the core.

### 5.4 Transitions, alerts, toasts

P (`DotWindow._apply`, `shinywidget.py:841`):

```
state = app.state(data)
if prev_state is not None and state != prev_state:
    msg = app.transition_message(prev, state, data)   # default: state_label(new)
    if msg: toast(msg, colour_of(state))
alerts = set(app.alert_ids(data))
if prev_alerts:                                        # <- non-empty, not "armed"
    for a in alerts - prev_alerts: toast(app.alert_message(a, data), CRIT)
prev_alerts = alerts; prev_state = state
```

- First poll never toasts (prev_state is None) — keep.
- **Quirk**: new alerts only toast when the previous alert set was non-empty.
  So healthy → one problem produces only the generic transition toast ("1
  container unhealthy"), and a second problem while the state stays `crit`
  produces "db: UNHEALTHY". claude has the same shape for incidents
  (`if self._prev_incident_ids:`).
- One toast window per widget; a new toast destroys the previous one.
- Toast: 4000 ms, then fade ≈ 500 ms (0.06 opacity / 30 ms); placed right of
  the dot, vertically centred on it.

N (recommended rule — keeps the good part of the quirk, fixes the missing
case):

1. `armed` = the widget has had one successful collect. Nothing toasts before.
2. Per poll compute `transition` (if state changed: `{tone: new state tone,
   text: transitionMessage}`) and `newAlerts` = alert ids not in the previous
   set, each `{tone, text}` from the collector.
3. If `newAlerts` is non-empty, emit those (at most 3, most severe first) and
   **drop** the transition toast (specific beats generic); else emit the
   transition toast if its text is non-empty.
4. Optional per-widget `clearedAlerts` messages (only claude uses them:
   component recoveries, see claude spec).
5. Failed collects (5.3) neither update nor clear the previous sets.

Toast wire message: `{"type":"toast","toast":{"widget","tone","text","at"}}`.
Presentation (DESIGN.md, A14): beside the raising dial, on the panel side,
newest nearest, max 3, 5 s + 160 ms fade, click-through (on Windows: dismiss
on hover — research (b)); while **any** panel is open toasts wait and appear
when it closes; a toast for the widget whose panel is open is dropped. Toast
text is one plain sentence (no mono fragments) — collectors write toast texts
as sentences (`api-gateway is unhealthy` rather than `api-gateway: UNHEALTHY`
is acceptable; exact P wordings are listed per widget and may be softened to
sentence case, never changed in meaning).

Toast tones: transition → the new state's tone (`ok` recovery toasts are
normal); alert → the alert's own tone (P used CRIT for every alert; N lets the
collector say warn vs crit); action result → `ok` / `crit`; copy → `ok`.

### 5.5 Actions and arming

P (`Action`, `_action_button`, `WidgetApp.arm`): a button with `confirm` arms
on first click (label → confirm text, amber, or red when `danger`); a second
click within `ARM_SECS = 4` runs it; the arm expires by timer only (clicking
another button does not disarm the first). Arm state lives on the widget
object keyed by `action.key or label+"\0"+confirm`, so it survives the panel
rebuild on every poll. Buttons are not focusable. `run_action(fn, done)` runs
`fn` off-thread, `done(ok,msg)` clears pending markers, the message becomes a
toast (`OK`/`CRIT`), then an immediate refresh.

N: arm state in the UI keyed by `widget/actionId/canonical(args)` (ARCHITECTURE),
4 s. DESIGN.md says every world-changing click is two-step — so actions that
P ran on one click (docker `start`, `resume`) get a confirm too (decided,
Q1). Core side:

- `action` messages are validated against the **current snapshot**: the
  `(widget, id, args)` must correspond to an action the core itself put in the
  last snapshot (or menu). Unknown → `result ok:false "unknown action"`. This
  is the injection guard: the UI never supplies a command, a container name
  that does not exist, or a flag.
- Execution is async; the collector publishes a pending marker immediately
  (e.g. docker row value `stopping…`, tone `busy`), the core replies
  `{"type":"result","id","ok","message"}` and raises the result toast, then
  refreshes the widget.
- Every external command goes through `internal/sys.Run(ctx, name, args…)`:
  no shell, argv only, timeout, `CREATE_NO_WINDOW` on Windows, stdout/stderr
  captured separately, exit code returned (P's `run_cmd` returned stdout even
  on non-zero exit and dropped stderr — widgets must decide explicitly).

### 5.6 Menus

P right-click on a dot: `Refresh now`, the widget's `menu_items`, `Open <Title>
page` when `click_url`, separator, `Quit <Title> widget`. Left-click (no drag)
calls `on_click` = open `click_url` (claude, codex status pages; power: open
report). comfyui/claude/power build their own menus (see specs).

N (DESIGN.md): `Refresh now`, the snapshot's `menu` items, separator,
`Settings…`, `Quit NoClickDock`. Left-click on a dial does nothing but what
DESIGN.md gives it (it is not a link); the "open" targets move into `menu`.
**Q (contract)**: menu items and actions that only open a URL/file or copy
text should be handled in Rust without a core round-trip — propose optional
fields on menu items: `{"id","label","url"?,"copy"?}`. Rust opens `url` with
`tauri-plugin-opener` (allow only `https:`, `http:` and the power report file)
and copies `copy` with `tauri-plugin-clipboard-manager`, then shows the toast
`Copied  <text>` (P wording, two spaces). Otherwise the core would need
`xdg-open`/`xclip`/Win32 clipboard code for no benefit.

### 5.7 Footer

P: `Last checked: HH:MM:SS` (claude/comfyui append ` UTC`, printing the UTC
time) plus interaction hints (`Click: open · ~: move · Right-click: menu`,
docker adds `Hover a tab`). Codex prefixes `Limits as of <age>`.

N (DESIGN.md A10): `snapshot.footer` on the left (widget facts: codex
`limits as of 3h ago`, docker `engine 27.3 · compose 2.29`), `12:41:07 · 20s`
on the right from `updatedAt`/`pollSecs` in **local** time (the P UTC footer
is a **BUG** for everyone not in UTC); interaction hints dropped. A collector
error: the error text on the left in `--err`, `showing <last good time>` on
the right. Stale (A11, no snapshot for 3 × pollSecs): `no reading since
<time>`, header state `stale` — computed by the UI, not the core.

### 5.8 Snapshot emission

ARCHITECTURE.md (decided): every poll produces a message — the full
snapshot when anything but `updatedAt` changed, otherwise the lightweight
`{"type":"tick","widget","at"}`. Rationale as follows. Emit something after
**every** poll (not only on change): the footer's
time must be true, and DESIGN.md A11 marks a widget **stale** after 3 ×
pollSecs without a snapshot — a change-only emitter would turn every quiet,
healthy widget stale. At these sizes (≤ 10 KB, fastest cadence 2 s) change
detection buys nothing. A failed collect (5.3) still emits (last good content
+ `error`), so an erroring widget is shown as erroring, not as stale. Slow
widgets: a collect that has not finished after 2 × pollSecs must still emit
the last snapshot with `error: "still collecting…"` rather than fall silent. On (re)start of the sidecar, the
core's first lines are `hello` + every widget's last snapshot (UI state is
rebuilt from them after a respawn).

---

## 6. Glance readouts (≤ 5 chars)

| widget | text | tone |
|---|---|---|
| claude | highest plan-limit % (`54%`); no limits → 24 h tokens (`1.2M`); neither → empty | ≥ 90 crit, ≥ 75 warn, else neutral |
| codex | highest % among non-idle limit groups (`47%`); none → 24 h tokens; neither → empty | same |
| comfyui | generating with progress → `37%`; generating without → `run`; pending > 0 → `q 3`; idle → device-0 VRAM used % (`14%`); offline → `off` | neutral except offline (neutral) — the LED/ring carry state |
| disk | worst used % (`86%`) | ≥ 92 crit, ≥ 85 warn |
| docker | running containers (`12`); unavailable → `off` | the widget state's tone when warn/crit |
| power | total watts (`412W`, `1234W`) | crit while any thermal alert is active, else neutral |
| tailscale | watched up/total (`3/4`); key expiring → days (`6d`); not running → `off` | warn/crit with the widget state |

The ARCHITECTURE example `12/14` for docker is illustrative; running/total
would read as a problem forever for anyone with dormant containers.

---

## 7. Tone mapping from the Python colours

| P colour | meaning | N tone |
|---|---|---|
| `OK` #6da46a, "operational", comfy green | healthy | `ok` (LED only; text stays quiet) |
| `WARN` #d9a53d, "degraded", comfy "queued" yellow | degraded | `warn` |
| `BAD` #e8873a (partial outage, codex "major") | between | `warn` for partial, `crit` for major (see codex/claude specs) |
| `CRIT` #cf5347 | failed | `crit` |
| `IDLE` #6f6a62, "unknown", comfy "offline" grey | unknown/offline | `idle` |
| comfy "generating" blue | work in progress | `busy` |
| `AMBER` accent on values (exit node, exposure, top model) | "notice this" | `warn` when it is a risk (funnel = `crit`, serve/exit node/advertised route = `warn`); **drop** when decorative (top model highlight) |

---

## 8. Config (`internal/config`)

Path: Linux `~/.config/noclickdock/config.json` (`os.UserConfigDir()`),
Windows `%APPDATA%\NoClickDock\config.json`. Owned by the core; written
atomically (temp + rename in the same dir), mode 0600 on Linux. The UI only
sends `config.patch`; the core validates, writes, emits `config`.

Proposed shape (the widget sections are defined in each spec):

```jsonc
{
  "version": 1,
  "dock": { … },                                       // §2
  "start": {"launchAtLogin": true},
  "widgets": {
    "claude":    {"enabled": true, "order": 10},
    "codex":     {"enabled": true, "order": 20},
    "comfyui":   {"enabled": true, "order": 30, "host": "127.0.0.1", "port": 8188},
    "disk":      {"enabled": true, "order": 40},
    "docker":    {"enabled": true, "order": 50},
    "power":     {"enabled": true, "order": 60, …tariff…},
    "tailscale": {"enabled": true, "order": 70, "watch": null}
  }
}
```

### Legacy migration (first run only, when `config.json` is absent)

Read-only on the legacy files; never delete or rewrite them.

| source | N key | notes |
|---|---|---|
| `~/.config/status-widgets/bar.conf` (Win: `%APPDATA%\NoClickDock\bar.conf`) | `dock.orientation`, `dock.density` (`s/m/l`), `dock.position.x/y` | settings the user chose |
| `~/.config/status-widgets/tailscale.json` | `widgets.tailscale.watch` **only if** the file has a `"watch"` list | usually it does not — see tailscale spec **BUG** (the registry file overwrote it) |
| `~/.config/power-monitor/config.json` | `widgets.power.*` | keys listed in the power spec; `monitor_watts_each` is obsolete, ignore |
| `~/.config/autostart/<id>.desktop` presence (Win: `Startup\NCD <Label>.lnk`) | `widgets.<name>.enabled` | a widget the user never installed starts disabled; if no legacy install exists at all, enable everything that can run on the platform |
| `~/.config/autostart/comfyui-status-checker.desktop` `Exec=… --host H --port P` | `widgets.comfyui.host/port` | P only knew the endpoint from CLI args |

**Windows collision (MUST for M6/M7):** the Python widgets on Windows use
`%APPDATA%\NoClickDock` as their registry directory and **delete every
`*.json` there whose `pid` is not a live process** (`_get_active_widgets`,
which also runs on every `~` press and corner apply). A `config.json` in the
same folder has no `pid` and would be deleted by any still-running Python
widget. Either `install.ps1` stops every legacy widget and removes their
Startup shortcuts before `ncd.exe` first starts (and ncd-core recreates a
missing config from memory instead of reverting to defaults), or the config
lives one level down (`%APPDATA%\NoClickDock\ncd\config.json`). Linux is not
affected (`~/.config/noclickdock` vs `~/.config/status-widgets`).

---

## 9. Formatting helpers (Go `internal/sys` or `internal/format`)

Byte and time formats differ per widget; keep each widget's own (they are
specified in its spec). Shared ones:

- `fmt_bytes` (disk, shinywidget): binary units, one letter: `1.5K`, `83G`,
  `9.8G` — one decimal below 10, none from 10; `-` for invalid, `512B`.
- `fmt_count`: `1.2M`, `348K`, `912`.
- `fmt_tokens` (claude, codex): `1.2B`, `1.2M`, `348K`, `912`.
- `time_ago` (shinywidget/tailscale): `just now` < 60 s, `Nm ago`, `Nh ago`,
  `Nd ago`; empty for unknown. (claude's own copy is minute-based, same output.)
- `parse_iso`: accept `Z`, any number of fractional digits (Tailscale emits
  one), offsets; Go `time.Parse(time.RFC3339Nano, …)` already does — test it
  with `…:00.1Z`, `…:00.123456789Z`, `…+02:00`.

---

## 10. Not carried over (on purpose)

- `~` corner cycling and multi-monitor stacking of free dots; free-floating
  (undocked) dots at all.
- Per-widget processes, `bar.json`, `corner.json`, registration files.
- Brand-coloured discs, status "bubble", breathing ring for `warn`
  (DESIGN.md: ring only for `crit`, arc for `busy`).
- The Python per-widget "Quit X widget" (→ Settings) and click-to-open on the
  dot (→ menu items).
- Badge chips with the state label on a coloured background (→ header LED +
  label).

---

## 11. Snapshot-writer budgets (so the UI never has to truncate meaning)

- `glance.text` ≤ 5 characters.
- `alert.meta` ≤ 24 characters (DESIGN A12) — cause and age only; details go
  in `body` (2 lines max, then ellipsis).
- `stateLabel`: a few words (≤ 28 characters is comfortable at 360 px).
- Toast text: one plain sentence, ≤ 2 lines at 13 px (≈ 80 characters).
- Numbers are pre-formatted by the core (units included); the UI never does
  arithmetic on display strings.

## 12. Contract gaps and open decisions (collected)

Block/contract gaps found while mapping every panel:

1. `table` cells need an optional per-cell tone (power temperatures): a cell is
   `"text"` or `{"text","tone"}`.
2. Menu items (and optionally row actions) that only open a URL/file or copy
   text: `{"id","label","url"?,"copy"?}` handled in Rust (§5.6).
3. `section.meta` is used for counts and short phrases (docker "this week",
   power "own PSU") — already in the contract; no change.

Open decisions (**Q**) — **decided 2026-09-26, see ARCHITECTURE.md
"Decisions on the specs' open questions"**; all as recommended below except
**Q6** (disk now includes network filesystems and ZFS datasets — disk spec
updated) and **Q1** (confirmed: every Docker action two-step):

| # | question | recommendation |
|---|---|---|
| Q1 | docker `start`/`resume` one-click (P) vs two-step (DESIGN) | two-step |
| Q2 | codex/claude indicator `major` → `crit` (P codex: separate `bad`) | crit |
| Q3 | power level-change toasts: keep with 60 s hysteresis, or drop | keep with hysteresis |
| Q4 | power utilization tones (P yellow > 50 %, red > 80 %) | RAM/VRAM warn ≥ 90, crit ≥ 97; CPU none |
| Q5 | comfyui VRAM meter tones (P yellow ≥ 60 %) | warn ≥ 85, crit ≥ 95 |
| Q6 | disk: include network/ZFS mounts (P drops non-`/dev` devices) | keep P (drop) |
| Q7 | Windows config path collides with the legacy registry dir | installer stops legacy widgets first; recreate config if deleted |
| Q8 | new-alert toasts replace the generic transition toast in the same poll | yes (§5.4) |
