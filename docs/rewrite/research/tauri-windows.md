# Tauri 2.11 windowing, sidecar and platform research

Research supervisor, milestone M0. Everything marked **proven** was run on this
machine (Pop!_OS X11 GNOME/Mutter, compositor on, NVIDIA, two 3840×2160
monitors stacked, webkit2gtk-4.1 2.50.4, tauri 2.11.5 / tao 0.35.3 / wry 0.55.1
/ tauri-plugin-shell 2.3.6) with a scratch app (not in the repo). Everything
marked **docs** comes from source or documentation; Windows behaviour could
not be run here and is flagged for verification on Windows hardware in M7.

## Decisions (read these first)

1. **Overlay windows** (rail, panel, toast) are built in Rust with
   `focused(false) + focusable(false) + transparent + decorations(false) +
   shadow(false) + always_on_top + skip_taskbar + visible(false)`, plus GTK
   type hint `Utility` (rail, panel) / `Notification` (toast) set before the
   first show. **Proven on X11**: `_NET_ACTIVE_WINDOW` did not change through
   show, hide + re-show, left click, wheel, right-click menu and Escape;
   `mouseenter`/`mouseleave`/`click`/`wheel`/`contextmenu` all reach the page.
2. **Geometry is physical pixels owned by Rust.** `set_size()` cannot shrink a
   transparent WebKitGTK window below the webview's natural size (proven:
   64 px asked, 211 px got); use the `fit()` helper below for every resize.
   **Set the position before every `show()`** — Mutter re-places a re-mapped
   window (proven: a panel hidden at 600,400 came back at 150,2358 on the
   other monitor). **CSS px ≠ Tauri logical px on this machine**: WebKitGTK
   lays out at Xft.dpi/96 = 1.5 physical px per CSS px while
   `devicePixelRatio`, `scale_factor()` and `zoom_level()` all say 1 — measure
   the ratio, never assume it.
3. **Work area**: `Monitor::work_area()` exists in 2.11 and is per-monitor on
   both platforms (Linux: GDK workarea = Mutter's `_GTK_WORKAREAS_Dn`;
   Windows: `GetMonitorInfoW().rcWork`). Never use `_NET_WORKAREA` (here it is
   the whole 3840×4320 desktop). Call monitor APIs on the main thread.
4. **Sidecar** through `tauri-plugin-shell` from Rust only (no `shell:*`
   permission in any capability). Rust supervises: respawn with backoff,
   `kill()` on `RunEvent::Exit` (the plugin only kills children spawned from
   JS). **Death pact: ncd-core exits on stdin EOF** — proven: `kill -9` of the
   shell made the core exit within a second. Windows: the plugin already sets
   `CREATE_NO_WINDOW`; ncd-core is `-H=windowsgui` and must pass
   `CREATE_NO_WINDOW` to every child it runs.
5. **Windows needs three extra calls** (from docs/source; compile-checked, not
   run): show/hide through `ShowWindow(SW_SHOWNOACTIVATE / SW_HIDE)` because
   tao's `show()` uses `SW_SHOW` after the first map (activates); subclass
   `WM_MOUSEACTIVATE → MA_NOACTIVATE`; add `WS_EX_TOOLWINDOW` (Alt+Tab) and
   keep it through tao's `GWL_EXSTYLE` rewrites (first `show()` included) by
   handling `WM_STYLECHANGING` in the same subclass.
6. **Windows build**: `cargo tauri build --runner cargo-xwin --target
   x86_64-pc-windows-msvc --no-bundle` works from this box with `llvm-rc` on
   `PATH` → one 6.1 MB GUI exe, no `WebView2Loader.dll`, no VC runtime DLL.
   The Go core cross-compiles with `-H=windowsgui`. See "Cross-building".

Also proven: `set_ignore_cursor_events(true)` **aborts the app on Linux if
called before the window was ever shown** (tao unwraps a missing GdkWindow);
after the first show it gives true click-through. `window.popup_menu()` works
on the non-focusable rail. The tray registers through ayatana-appindicator.
`start_dragging()` moves a non-focusable window. Three webviews cost ≈ 290 MB
PSS in total (≈ 64 MB per WebKitWebProcess) — create rail, panel and toast
once and reuse them; create Settings on demand and destroy it on close.

---

## (a) No-focus overlay windows on X11 — proven

### Builder

```rust
use tauri::{WebviewUrl, WebviewWindowBuilder};

fn overlay(app: &tauri::App, label: &str, route: &str) -> tauri::Result<tauri::WebviewWindow> {
    let w = WebviewWindowBuilder::new(app, label, WebviewUrl::App(route.into()))
        .title(format!("NoClickDock {label}"))   // WM_NAME; handy for xdotool
        .inner_size(64.0, 64.0)                  // real size comes from fit()
        .transparent(true)
        .decorations(false)
        .shadow(false)
        .always_on_top(true)
        .skip_taskbar(true)                      // also sets SKIP_PAGER on X11
        .resizable(false)
        .visible(false)
        .focused(false)
        .focusable(false)
        .build()?;
    w.set_background_color(Some(tauri::webview::Color(0, 0, 0, 0)))?;
    #[cfg(target_os = "linux")]
    {
        use gtk::prelude::GtkWindowExt;
        use gtk::gdk::WindowTypeHint;
        // Before the first show: type hints are read at map time.
        // UTILITY, not DOCK: Mutter stacks DOCK above every keep-above window
        // (the Python dock learnt this the hard way, dock/ncd-dock.py:102).
        let hint = if label == "toast" { WindowTypeHint::Notification } else { WindowTypeHint::Utility };
        w.gtk_window()?.set_type_hint(hint);
    }
    Ok(w)
}
```

Create the windows in `setup()`, not in `tauri.conf.json` (`"windows": []`),
so every option above is in one place. Cargo: `tauri = { version = "2.11",
features = ["tray-icon"] }`, and on Linux `gtk = "0.18"` (plus
`webkit2gtk = "2.0"` only if you need `WebViewExt`).

### What the flags do on X11 (source + xprop)

| call | X11 effect (measured with `xprop`) |
|---|---|
| `focusable(false)` | `gtk_window_set_accept_focus(FALSE)` → `WM_HINTS` "Client accepts input or input focus: False" for the life of the window |
| `focused(false)` alone | accept-focus false **only until the first draw**, then tao sets it back to true (tao `platform_impl/linux/window.rs:215`). Not a guarantee for later shows. |
| `always_on_top` | `_NET_WM_STATE_ABOVE` |
| `skip_taskbar` | `_NET_WM_STATE_SKIP_TASKBAR` + `_NET_WM_STATE_SKIP_PAGER` (excluded from Alt+Tab and the overview) |
| `transparent` | 32-bit ARGB visual (Depth 32) — the desktop shows through the rounded corners; no black box |
| `shadow(false)` / `decorations(false)` | no `_GTK_FRAME_EXTENTS`, no CSD shadow margin |
| type hint | `_NET_WM_WINDOW_TYPE_UTILITY` / `_NOTIFICATION` (default is `_NORMAL`) |
| `visible_on_all_workspaces(true)` | `_NET_WM_STATE_STICKY`. The Python dock did not stick; recommended for the rail anyway (it is "always on"), decision for main. |

### Measured focus behaviour

`xprop -root _NET_ACTIVE_WINDOW` was sampled before and after each step; the
user's window stayed active in every case, with `focusable(false)`:

- first `show()` of rail, panel, toast; `hide()` + `show()` of the panel;
- pointer enter, wheel, **left click** on the rail (the page got `click`);
- right-click → native GTK menu open → Escape.

With `focusable(true) + focused(false)` Mutter (focus-new-windows = smart)
also did not move focus on show, but that relies on Mutter's user-time
heuristics, and a click would focus the window. Use `focusable(false)`.

Consequences: the overlay windows never get keyboard input, so DESIGN.md's
"Escape closes the panel when it has focus" never triggers — the panel simply
never has focus. Anything that needs typing lives in Settings (a normal,
focusable, decorated-by-us window).

### Sizing: `fit()` — mandatory on Linux

`set_size()` on a transparent, undecorated WebKitGTK window is clamped to the
webview's natural size. Measured: rail asked 64×260 → got 211×260; toast
asked 260×60 → 260×366; panel asked 360×300 → 360×366. `set_size()`
afterwards changes nothing. The same fix as the sibling apps works
(measured: exact sizes, grow and shrink, survives hide/show):

```rust
/// Exact size in GTK logical px (= physical px / GDK scale; GDK scale is 1 here).
pub fn fit(window: &tauri::WebviewWindow, width: i32, height: i32) -> tauri::Result<()> {
    #[cfg(target_os = "linux")]
    {
        window.with_webview(move |view| {
            use gtk::prelude::*;
            let webview = view.inner();
            webview.set_size_request(1, 1);              // drop the webview's natural minimum
            if let Some(top) = webview.toplevel().and_then(|w| w.downcast::<gtk::Window>().ok()) {
                top.set_size_request(width, height);
                top.set_geometry_hints(
                    None::<&gtk::Widget>,
                    Some(&gtk::gdk::Geometry::new(width, height, width, height, 0, 0, 1, 1, 0.0, 0.0,
                                                  gtk::gdk::Gravity::NorthWest)),
                    gtk::gdk::WindowHints::MIN_SIZE | gtk::gdk::WindowHints::MAX_SIZE,
                );
                top.set_resizable(true);                 // min == max: still not user-resizable, not tileable
                top.resize(width, height);
            }
        })?;
        Ok(())
    }
    #[cfg(not(target_os = "linux"))]
    window.set_size(tauri::PhysicalSize::new(width as u32, height as u32))
}
```

A sibling app also notes that WebKitGTK can leave stale pixels on the
transparent surface when DOM content is removed: always shrink the window to
the content rather than leaving transparent slack.

### Positioning and show order

Measured: a panel placed at (600,400), hidden, then shown again without a
move reappeared at (150,2358) — Mutter placed it as a new window. Both of
these held their position (checked again 300–400 ms later):

- `set_position()` while hidden, then `show()`;
- `show()`, then `set_position()` immediately.

Rule for every reveal: `fit()` → `set_position(PhysicalPosition)` → `show()`
(Windows: `win::show`, below). The sibling app additionally re-applies the placement
60 ms after the map (`gtk::glib::timeout_add_local_once`); cheap insurance,
recommended for the panel.

### Pointer events across windows

Rail, panel and toast are separate X windows, so leaving a dial toward the
panel fires `mouseleave` on the rail page and `mouseenter` on the panel page.
The hover choreography (120 ms dwell, 280 ms grace, immediate switch) must
therefore live in one place fed by both pages. Recommendation: in Rust (one
`HoverState` behind a `Mutex`, `invoke('hover', {surface, dial|null})` from
each page, timers with `tauri::async_runtime::spawn` + a generation counter),
because Rust also owns show/hide/position and a webview reload cannot lose it.

### Dragging the rail

`getCurrentWindow().startDragging()` from a `mousedown` on the grip moved the
non-focusable rail by exactly the pointer delta (200,300 → 360,420) and did
not move focus. It needs `"core:window:allow-start-dragging"` in the rail's
capability. During a WM move the page receives no `mouseup`; snap and persist
on `WindowEvent::Moved` with a ~200 ms debounce (Moved events were not
measured; this is the standard tao behaviour). The Python dock dragged by
hand (`move()` on motion); either is fine, `startDragging` is less code.

### Click-through toasts

After the toast window has been shown once, `set_ignore_cursor_events(true)`
makes it click-through (measured: the pointer over the toast hit the panel
underneath; the toast page got no `mouseenter`). tao implements it with a 1×1
input region at (0,0), so a single pixel remains clickable.

**Calling it before the first show aborts the whole app**
(`tao-0.35.3/src/platform_impl/linux/event_loop.rs:457`, `window().unwrap()`
on an unrealized window, inside a GLib callback → abort). Order: build →
`fit` → position → `show` → `set_ignore_cursor_events(true)`.

### UI scale (CSS px → physical px)

Measured with a 360×300 physical panel: `innerWidth × innerHeight` =
240×200, `devicePixelRatio` = 1, Tauri `scale_factor()` = 1, WebKit
`zoom_level()` = 1, `gtk-xft-dpi` = 147456 (= 144 × 1024). WebKitGTK applies
the GNOME text-scaling factor (1.5 here) to CSS layout. `set_zoom(1.0)` does
not undo it. `screen.width` still reports 3840 (device px) — do not mix
`screen.*` with CSS px in the page.

Decision: honour it (the user chose 1.5× text on 4K; the Python widgets'
Pango text scaled too). Define `ui_scale = physical px per CSS px` per window
and do all geometry in Rust as `css × ui_scale`:

- Linux initial value: `gtk-xft-dpi / 1024 / 96` (1.5 here), read with
  `gtk::Settings::default().unwrap().gtk_xft_dpi()` on the main thread.
- Confirm after the first `fit()`+`show()`: the page reports `innerWidth`,
  Rust divides the physical inner width by it (the sibling app's
  `src/lib/window.ts` does exactly this). Re-measure on scale/monitor change.
- Windows: `ui_scale = window.scale_factor()` (WebView2 follows per-monitor
  DPI; CSS px = logical px). **docs**
- Snapshot `width` (ARCHITECTURE.md) is therefore **CSS px**, not Tauri
  logical px; the panel window is `width × ui_scale` physical px wide.

### Renderer notes

No `WEBKIT_DISABLE_DMABUF_RENDERER` / `WEBKIT_DISABLE_COMPOSITING_MODE` was
needed on this NVIDIA machine (webkit2gtk 2.50.4). If another NVIDIA setup
shows blank or black windows, `WEBKIT_DISABLE_DMABUF_RENDERER=1` is the known
workaround; set it in `main()` before the builder, not globally.

---

## (b) Windows (WebView2) — docs and source, verify in M7

What tao/wry do (source read, `tao-0.35.3/src/platform_impl/windows/`):

| need | tao behaviour | NCD action |
|---|---|---|
| no activation on first show | `focused(false)` → `SW_SHOWNOACTIVATE`, then the marker is cleared (`window_state.rs:325`) | ok for the first show only |
| no activation on later shows | `show()` → `ShowWindow(SW_SHOW)`, which activates even with `WS_EX_NOACTIVATE` ([ShowWindow](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-showwindow)) | show/hide through `win::show` / `win::hide` below; never call tao `show()`/`hide()` on overlays after startup (tao's VISIBLE flag goes stale — do not rely on `is_visible()`) |
| no activation on click | `focusable(false)` → `WS_EX_NOACTIVATE` ([extended styles](https://learn.microsoft.com/en-us/windows/win32/winmsg/extended-window-styles)); tao does not handle `WM_MOUSEACTIVATE`; active-window-tracking bypasses `WS_EX_NOACTIVATE` ([Old New Thing 2024-09-19](https://devblogs.microsoft.com/oldnewthing/20240919-00/?p=110283)) | subclass `WM_MOUSEACTIVATE → MA_NOACTIVATE` (keeps pointer input: [Old New Thing 2016-09-12](https://devblogs.microsoft.com/oldnewthing/20160912-00/?p=94295)) |
| WebView2 grabbing focus on click | WebView2 moves focus into its child HWND on click; `Focusable=false` does not stop it ([WebView2Feedback #5049](https://github.com/MicrosoftEdge/WebView2Feedback/issues/5049), [#1526](https://github.com/MicrosoftEdge/WebView2Feedback/issues/1526), [#862](https://github.com/MicrosoftEdge/WebView2Feedback/issues/862)) | residual risk: a click on a pill may still activate NCD. Hover never does. Accept (clicks are deliberate, two-step) and verify in M7 |
| `focus: false` in config | ignored on Windows in early 2.x ([tauri #11566](https://github.com/tauri-apps/tauri/issues/11566), fixed by [#11569](https://github.com/tauri-apps/tauri/pull/11569)) | build windows in Rust with `.focused(false)` (we do) |
| not in taskbar / Alt+Tab | `skip_taskbar` → `ITaskbarList::DeleteTab` only; Alt+Tab still lists the window | add `WS_EX_TOOLWINDOW` ("does not appear in the taskbar or in ALT+TAB") |
| keep custom ex-styles | tao recomputes and `SetWindowLongW(GWL_EXSTYLE)` on **every** flag change (`window_state.rs:440`) — **including the first `show()`**, which sets tao's VISIBLE flag | guard the bits in the subclass: on `WM_STYLECHANGING` for `GWL_EXSTYLE`, OR `WS_EX_TOOLWINDOW \| WS_EX_NOACTIVATE` into `styleNew` (corrected in the M1 review: calling `harden` once before the first show is not enough) |
| transparency | `transparent(true)` → WebView2 `DefaultBackgroundColor` alpha 0; undecorated windows get a DWM shadow + 1 px border unless `shadow(false)` ([WindowConfig docs](https://docs.rs/tauri-utils/latest/tauri_utils/config/struct.WindowConfig.html)) | `shadow(false)` (we do) — also removes Windows 11 corner rounding |
| click-through toast | `set_ignore_cursor_events` → `WS_EX_TRANSPARENT \| WS_EX_LAYERED`; reported not to pass clicks through a WebView2 window ([tauri #11461](https://github.com/tauri-apps/tauri/issues/11461)); invisible alpha-0 WebView2 windows swallow input ([WebView2Feedback #5668](https://github.com/MicrosoftEdge/WebView2Feedback/issues/5668)) | size the toast window to the toast stack only, and dismiss the toasts (fade + `win::hide`) on `mouseenter`. Never leave an invisible overlay mapped. |
| context menu | muda calls `SetForegroundWindow(owner)` before `TrackPopupMenu` (`muda-0.19.3/src/platform_impl/windows/mod.rs:1038`) → NCD becomes foreground | remember `GetForegroundWindow()` before `popup_menu` and restore it after (it is modal) — `win::popup_menu_keep_foreground` |
| mouseenter/leave | WebView2 delivers pointer events to an inactive window | nothing |

`src-tauri/src/win.rs` — type-checked with `cargo check --target
x86_64-pc-windows-msvc` against tauri 2.11.5 (`windows = { version = "0.61",
features = ["Win32_Foundation", "Win32_UI_WindowsAndMessaging",
"Win32_UI_Shell"] }`, the same major as tao/wry):

```rust
use windows::Win32::Foundation::{HWND, LPARAM, LRESULT, WPARAM};
use windows::Win32::UI::Shell::{DefSubclassProc, SetWindowSubclass};
use windows::Win32::UI::WindowsAndMessaging::{
    GetForegroundWindow, GetWindowLongPtrW, SetForegroundWindow, SetWindowLongPtrW, SetWindowPos,
    ShowWindow, GWL_EXSTYLE, HWND_TOPMOST, MA_NOACTIVATE, STYLESTRUCT, SWP_NOACTIVATE, SWP_NOMOVE,
    SWP_NOSIZE, SWP_SHOWWINDOW, SW_HIDE, SW_SHOWNOACTIVATE, WM_MOUSEACTIVATE, WM_STYLECHANGING,
    WS_EX_NOACTIVATE, WS_EX_TOOLWINDOW,
};

unsafe extern "system" fn refuse_activation(
    hwnd: HWND, msg: u32, wparam: WPARAM, lparam: LPARAM, _id: usize, _data: usize,
) -> LRESULT {
    match msg {
        WM_MOUSEACTIVATE => return LRESULT(MA_NOACTIVATE as isize),
        // tao rewrites GWL_EXSTYLE from its own flags on every flag change
        // (the first show() included): keep our two bits in every rewrite.
        WM_STYLECHANGING if wparam.0 as i32 == GWL_EXSTYLE.0 => {
            let style = &mut *(lparam.0 as *mut STYLESTRUCT);
            style.styleNew |= WS_EX_TOOLWINDOW.0 | WS_EX_NOACTIVATE.0;
        }
        _ => {}
    }
    DefSubclassProc(hwnd, msg, wparam, lparam)
}

/// Main thread only (window.run_on_main_thread): SetWindowSubclass must run
/// on the window's own thread. Once is enough — the subclass keeps the bits.
pub fn harden(window: &tauri::WebviewWindow) -> tauri::Result<()> {
    let hwnd = window.hwnd()?;
    unsafe {
        let ex = GetWindowLongPtrW(hwnd, GWL_EXSTYLE);
        SetWindowLongPtrW(hwnd, GWL_EXSTYLE, ex | (WS_EX_TOOLWINDOW.0 | WS_EX_NOACTIVATE.0) as isize);
        let _ = SetWindowSubclass(hwnd, Some(refuse_activation), 0x4e4344, 0);
    }
    Ok(())
}

pub fn show(window: &tauri::WebviewWindow) -> tauri::Result<()> {
    let hwnd = window.hwnd()?;
    unsafe {
        let _ = ShowWindow(hwnd, SW_SHOWNOACTIVATE);
        let _ = SetWindowPos(hwnd, Some(HWND_TOPMOST), 0, 0, 0, 0,
                             SWP_NOACTIVATE | SWP_NOMOVE | SWP_NOSIZE | SWP_SHOWWINDOW);
    }
    Ok(())
}

pub fn hide(window: &tauri::WebviewWindow) -> tauri::Result<()> {
    unsafe { let _ = ShowWindow(window.hwnd()?, SW_HIDE); }
    Ok(())
}

pub fn popup_menu_keep_foreground<M: tauri::menu::ContextMenu>(
    window: &tauri::WebviewWindow, menu: &M,
) -> tauri::Result<()> {
    let previous = unsafe { GetForegroundWindow() };
    let result = window.popup_menu(menu);
    if !previous.is_invalid() { unsafe { let _ = SetForegroundWindow(previous); } }
    result
}
```

Other Windows facts:

- Origin of the app page is `http://tauri.localhost` (Linux: `tauri://localhost`, measured).
- The first `show()` of an overlay may use tao's `show()` (it honours
  `focused(false)`); after that use `win::show`/`win::hide` exclusively.
- Positions: `set_position(PhysicalPosition)`; tao is per-monitor-DPI-v2
  aware, so physical coordinates are real pixels on mixed-DPI setups.
- Single instance / autostart are plugin work (M6), not windowing:
  `tauri-plugin-single-instance`, `tauri-plugin-autostart` (Run key).

---

## (c) Sidecar over stdio — proven on Linux

### Naming and config

```jsonc
// src-tauri/tauri.conf.json
"bundle": { "externalBin": ["binaries/ncd-core"] }
```

Files that must exist at build time (relative to `src-tauri/`):

| target | file |
|---|---|
| Linux | `binaries/ncd-core-x86_64-unknown-linux-gnu` |
| Windows (msvc) | `binaries/ncd-core-x86_64-pc-windows-msvc.exe` |
| Windows (gnu) | `binaries/ncd-core-x86_64-pc-windows-gnu.exe` |

Triple: `rustc --print host-tuple`, or the `--target` given to
`cargo tauri build`. `tauri-build` copies the file next to the built exe
**without** the triple (`target/release/ncd-core`, measured; `.exe` kept on
Windows), and `app.shell().sidecar("ncd-core")` resolves it relative to
`current_exe()` ([Tauri sidecar docs](https://v2.tauri.app/develop/sidecar/)).
The portable Windows layout is therefore `ncd.exe` + `ncd-core.exe` in one
folder. `scripts/build-core.sh` builds Go straight to those names
(`-buildvcs=false` if the tree is not a git checkout; the scratch build failed
without it).

### Supervisor (proven: echo, 200 KB line, crash → respawn)

```rust
use std::sync::{atomic::{AtomicBool, Ordering}, Mutex};
use tauri::{Emitter, Manager};
use tauri_plugin_shell::{process::{CommandChild, CommandEvent}, ShellExt};

#[derive(Default)]
pub struct Core { child: Mutex<Option<CommandChild>>, quitting: AtomicBool }

pub fn supervise(app: tauri::AppHandle) {
    tauri::async_runtime::spawn(async move {
        let mut backoff = 250u64;
        while !app.state::<Core>().quitting.load(Ordering::Acquire) {
            let spawned = app.shell().sidecar("ncd-core").and_then(|c| c.spawn());
            let (mut rx, child) = match spawned {
                Ok(v) => v,
                Err(e) => { eprintln!("ncd-core: {e}"); sleep_ms(backoff).await; backoff = (backoff * 2).min(10_000); continue; }
            };
            *app.state::<Core>().child.lock().unwrap() = Some(child);
            let started = std::time::Instant::now();
            while let Some(ev) = rx.recv().await {
                match ev {
                    // One event per line; the trailing '\n' is INCLUDED. Trim it.
                    CommandEvent::Stdout(bytes) => {
                        let line = String::from_utf8_lossy(&bytes).trim_end().to_string();
                        let _ = app.emit("core", line);          // or parse + emit_to per window
                    }
                    CommandEvent::Stderr(bytes) => eprintln!("ncd-core: {}", String::from_utf8_lossy(&bytes).trim_end()),
                    CommandEvent::Terminated(p) => { eprintln!("ncd-core exited {:?} {:?}", p.code, p.signal); break; }
                    _ => {}
                }
            }
            app.state::<Core>().child.lock().unwrap().take();
            if started.elapsed().as_secs() > 30 { backoff = 250; }
            sleep_ms(backoff).await;
            backoff = (backoff * 2).min(10_000);
        }
    });
}

async fn sleep_ms(ms: u64) {
    let _ = tauri::async_runtime::spawn_blocking(move || std::thread::sleep(std::time::Duration::from_millis(ms))).await;
}

#[tauri::command]
fn core_send(app: tauri::AppHandle, line: String) -> Result<(), String> {
    // Validate: parse as JSON, allow only type ∈ {action, refresh, config.patch},
    // re-serialise (guarantees a single line), then write.
    let v: serde_json::Value = serde_json::from_str(&line).map_err(|_| "bad message")?;
    match v.get("type").and_then(|t| t.as_str()) {
        Some("action" | "refresh" | "config.patch") => {}
        _ => return Err("message type not allowed".into()),
    }
    let mut out = serde_json::to_vec(&v).map_err(|e| e.to_string())?;
    out.push(b'\n');
    let state = app.state::<Core>();
    let mut guard = state.child.lock().map_err(|_| "core lock")?;
    guard.as_mut().ok_or("core not running")?.write(&out).map_err(|e| e.to_string())
}

// in Builder::run(|app, event| …)
if let tauri::RunEvent::Exit = event {
    let state = app.state::<Core>();
    state.quitting.store(true, Ordering::Release);
    let child = state.child.lock().unwrap().take();   // bind first: borrow ends before `state`
    if let Some(child) = child { let _ = child.kill(); }
}
```

Facts measured / read:

- `CommandEvent::Stdout` is one line **including `\n`**; a 200 000-byte JSON
  line arrived as one event. Channel capacity is 1: a slow consumer
  back-pressures the core's stdout (fine; keep the handler non-blocking).
- `child.write()` goes straight to the pipe; add the `\n` yourself.
- Crash (exit 3) → `Terminated{code: Some(3)}` → respawn → new `hello` line.
  After a respawn the UI needs the full state again: the core's first lines
  must be `hello` + every snapshot (ARCHITECTURE.md already says so).
- The plugin's own exit hook kills only children spawned via the JS API
  (`tauri-plugin-shell-2.3.6/src/lib.rs:133`), not `spawn()` from Rust.
- **Death pact** (Go side, both OSes): read stdin in the main loop; on EOF,
  cancel everything and `os.Exit(0)`. Proven: SIGKILL of the shell → core
  gone within 1 s. Also covers `panic = "abort"` in the shell.
- Windows console: the plugin sets `CREATE_NO_WINDOW` on the sidecar
  (`process/mod.rs:173`). ncd-core is linked `-ldflags "-H=windowsgui"`; a GUI
  subsystem process still uses the inherited stdin/stdout pipe handles. Every
  process ncd-core starts (`docker.exe`, `nvidia-smi.exe`, `tailscale.exe`,
  `powershell.exe`) must use
  `cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}`
  — a console child of a console-less parent otherwise opens a new console
  window.
- Security: grant the webviews **no** `shell:*` permission. The webview can
  only `invoke('core_send')` (validated above) and listen to events. Set a
  production CSP so the page cannot reach any network origin:
  `"csp": "default-src 'self'; connect-src ipc: http://ipc.localhost; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self'"`.

---

## (d) Positioning, scale, work area, multi-monitor — proven on Linux

Measured here (`available_monitors()`, physical px):

| monitor | position | size | scale | work_area |
|---|---|---|---|---|
| DP-2 (primary, bottom) | (0, 2160) | 3840×2160 | 1 | (0, 2208) 3840×2112 — 48 px top bar |
| DP-0 (top) | (0, 0) | 3840×2160 | 1 | (0, 0) 3840×2160 |

`_NET_WORKAREA` = `0,0,3840,4320` (the whole desktop — useless per
monitor); Mutter publishes the real per-monitor areas in
`_GTK_WORKAREAS_D0`, which is what `GdkMonitor::workarea()` and therefore
Tauri's `work_area()` return. The page's `screen.availHeight` said 2112 on
one monitor and 2160 on the other, but do not use it (device px, and it is the
page's monitor only).

APIs (all in 2.11, physical units): `app.available_monitors()`,
`app.primary_monitor()`, `window.current_monitor()`,
`app.monitor_from_point(x, y)`, `app.cursor_position()`, `m.work_area()`,
`m.scale_factor()`.

Rules:

- All placement math in **physical px**, inputs in CSS px × `ui_scale`
  (section (a)). Snapshot `width` is CSS px.
- Run monitor queries on the main thread (`app.run_on_main_thread`): Tauri
  converts GDK monitor properties on the caller thread (the sibling app
  `desktop.rs:68`).
- A hot-plug can briefly report an empty work area — keep the last valid one
  when `work_area().size` is 0 (the sibling app's `desktop.rs:120`).
- Work area changes (panel autohide, monitor add/remove, scale change): watch
  `GdkMonitor` `workarea`/`geometry`/`scale-factor` notifies and
  `monitors-changed`, coalesced with a 150 ms one-shot timer, then re-clamp
  the rail and re-place any open panel (the sibling app's `observe_monitors`). On
  Windows, `WM_DISPLAYCHANGE`/`WM_SETTINGCHANGE(SPI_SETWORKAREA)` via a
  subclass, or simply re-clamp on `ScaleChanged` + every panel open.
- Persist the rail position as `{monitor name, edge, offset along edge}`
  (plus the raw physical x/y as a fallback) so it survives resolution and
  scale changes; on start, if the named monitor is missing, use the primary
  and clamp into its work area.
- Windows work area is `GetMonitorInfoW().rcWork` (taskbar excluded), already
  wrapped by `work_area()` (`tauri-runtime-wry-2.11.4/src/monitor/windows.rs`).
  `SystemParametersInfo(SPI_GETWORKAREA)` is primary-monitor only — do not use.

---

## (e) Context menu and tray

### Context menu on the rail — proven on X11

```rust
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};

#[tauri::command]
fn dial_menu(window: tauri::WebviewWindow, widget: String, items: Vec<(String, String)>) -> Result<(), String> {
    let app = window.app_handle();
    let e = |x: tauri::Error| x.to_string();
    let mut owned: Vec<MenuItem<tauri::Wry>> = vec![
        MenuItem::with_id(app, format!("{widget}::refresh"), "Refresh now", true, None::<&str>).map_err(e)?,
    ];
    for (id, label) in items {            // the snapshot's `menu` — ids are namespaced by widget
        owned.push(MenuItem::with_id(app, format!("{widget}::{id}"), label, true, None::<&str>).map_err(e)?);
    }
    let sep = PredefinedMenuItem::separator(app).map_err(e)?;
    let settings = MenuItem::with_id(app, "app::settings", "Settings…", true, None::<&str>).map_err(e)?;
    let quit = MenuItem::with_id(app, "app::quit", "Quit NoClickDock", true, None::<&str>).map_err(e)?;
    let mut refs: Vec<&dyn tauri::menu::IsMenuItem<tauri::Wry>> = owned.iter().map(|m| m as _).collect();
    refs.push(&sep);
    refs.push(&settings);
    refs.push(&quit);
    let menu = Menu::with_items(app, &refs).map_err(e)?;
    #[cfg(windows)]
    let shown = crate::win::popup_menu_keep_foreground(&window, &menu);
    #[cfg(not(windows))]
    let shown = window.popup_menu(&menu);
    shown.map_err(e)
}
// app.on_menu_event(|app, ev| match ev.id().as_ref() { … })  — one global handler
```

Page side: `contextmenu` → `preventDefault()` → `invoke('dial_menu', …)`.
Measured: GTK menu opens at the pointer on the non-focusable rail,
`_NET_ACTIVE_WINDOW` unchanged while open and after Escape; no menu event is
emitted on dismissal. The menu is themed by GTK (not Cerveau) — acceptable
per DESIGN.md.

### Tray

- Cargo feature `tray-icon`; `TrayIconBuilder::with_id("ncd").icon(app.default_window_icon().unwrap().clone()).menu(&menu).show_menu_on_left_click(false)` (the sibling app's `desktop.rs:tray`).
- **Linux, proven**: the icon registered with the StatusNotifierWatcher as
  `/org/ayatana/NotificationItem/tray_icon_tray_app_<id>`. Runtime needs
  `libayatana-appindicator3.so.1` (present here) and a host for
  StatusNotifier items — on this machine the `ubuntu-appindicators` GNOME
  extension (enabled). Vanilla GNOME without such an extension shows **no**
  tray: the rail's right-click menu must therefore offer everything the tray
  does (it does: Settings…, Quit). No `-dev` package is needed to build; the
  library is loaded at runtime.
- On Linux, tray clicks other than opening the menu are not delivered
  (appindicator); do not design left-click behaviour for Linux.
- **Windows (docs)**: native notification-area icon; left/right click events
  and the menu work; tray-icon also calls `SetForegroundWindow` before its
  menu (`tray-icon-0.24.2/src/platform_impl/windows/mod.rs:545`), which is the
  documented requirement for the menu to dismiss correctly.

---

## (f) Dev transport from a `tauri://` page — proven

From the built app (origin `tauri://localhost`), `fetch('http://127.0.0.1:47812/v1/state')`
returned 200 and `new EventSource('http://127.0.0.1:47812/v1/events')` received a
named `snapshot` event, with CSP `null` and the server answering
`Access-Control-Allow-Origin: <request Origin>`. The request carried
`Origin: tauri://localhost`. No mixed-content block for 127.0.0.1.

Production does not use this. For `--dev`/`--mock`, allow exactly the Vite
origins (`http://localhost:1420`, `:1430`, `:1440`, and the `127.0.0.1`
spellings); add `tauri://localhost` / `http://tauri.localhost` only if a built
app is ever pointed at a dev core. With the production CSP above, a built app
cannot reach the dev server at all, which is the intent.

---

## Cross-building the Windows exe from this Linux box — proven

Built the scratch probe (three overlay windows, tray, context menu, the
`win.rs` helpers wired in, `tauri-plugin-shell` sidecar with `externalBin`)
for Windows from Linux, both ways. Neither could be **run** here (no Windows,
no Wine); runtime behaviour on Windows remains an M7 verification item.

### Recommended: MSVC target with cargo-xwin (single portable exe)

One-time setup (no sudo; LLVM 14 is already on this machine):

```bash
rustup target add x86_64-pc-windows-msvc
cargo install cargo-xwin --locked          # 0.23.1; first build downloads the MSVC CRT + Windows SDK
                                           # into ~/.cache/cargo-xwin (1.1 GB; accepts the MS licence)
```

Build (from `app/src-tauri`, frontend already built into `frontendDist`):

```bash
PATH=/usr/lib/llvm-14/bin:$PATH \
  cargo tauri build --runner cargo-xwin --target x86_64-pc-windows-msvc --no-bundle
# → target/x86_64-pc-windows-msvc/release/ncd.exe  (+ ncd-core.exe copied from externalBin)
```

- `llvm-rc` must be on `PATH` — `tauri-winres` compiles the icon/manifest
  resource with it and panics with `NotAttempted("llvm-rc")` otherwise. Here
  it is `/usr/lib/llvm-14/bin/llvm-rc` (also `/usr/bin/llvm-rc-14`, which is
  not found under that name). The Tauri docs' `apt install lld llvm` is not
  needed: cargo-xwin puts its own `clang-cl` (→ clang 14) and `lld-link` (→
  the toolchain's `rust-lld`) shims in `~/.cache/cargo-xwin`.
- Result (probe, production profile `lto = true, opt-level = 3,
  codegen-units = 1, panic = "abort", strip = true`): **6.1 MB** `ncd.exe`,
  PE32+ **GUI** subsystem. Imports only system DLLs and the Universal CRT
  api-sets (`api-ms-win-crt-*`, present on Windows 10/11): **no
  `WebView2Loader.dll`** (webview2-com links `WebView2LoaderStatic.lib` on
  msvc) and **no `VCRUNTIME140.dll`** (the Tauri CLI sets
  `STATIC_VCRUNTIME=true`; a plain `cargo build` would not — always build
  through `cargo tauri build`).
- Build time: 1 m 10 s first time (incl. the SDK download), ~30 s
  incremental, 1 m 17 s for the production profile.
- `--no-bundle` gives the portable exe. `cargo tauri build … --bundles nsis`
  (needs `makensis`, not installed) would make a setup.exe; MSI cannot be
  cross-built ([Tauri docs](https://v2.tauri.app/distribute/windows-installer/)).
  ARCHITECTURE's plan (portable exe + `install.ps1`) needs neither.

### Alternative: GNU target with mingw (works, worse artefact)

```bash
rustup target add x86_64-pc-windows-gnu
cargo tauri build --target x86_64-pc-windows-gnu --no-bundle
```

Builds with the installed `x86_64-w64-mingw32-gcc` (30 s), but the exe is
28.5 MB unstripped, imports `msvcrt.dll`, and **needs `WebView2Loader.dll`
shipped next to it** (tauri-build copies the 160 KB DLL into the target
dir). The sidecar must then be named `…-x86_64-pc-windows-gnu.exe`. Use only
if cargo-xwin ever breaks.

### Go sidecar

```bash
cd core
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -buildvcs=false \
  -ldflags "-H=windowsgui -s -w" \
  -o ../app/src-tauri/binaries/ncd-core-x86_64-pc-windows-msvc.exe ./cmd/ncd-core
go build -trimpath -ldflags "-s -w" \
  -o ../app/src-tauri/binaries/ncd-core-x86_64-unknown-linux-gnu ./cmd/ncd-core
```

- Proven: the probe core cross-compiles to a 5.4 MB PE32+ **GUI** exe
  (`-H=windowsgui`), no cgo. `GOOS=windows go vet ./...` is the cheap check that
  Windows-tagged files compile.
- Tauri exports `TAURI_ENV_TARGET_TRIPLE` (and `TAURI_ENV_PLATFORM`,
  `TAURI_ENV_ARCH`) to `beforeBuildCommand`/`beforeDevCommand` — measured:
  `x86_64-pc-windows-msvc` for a `--target x86_64-pc-windows-msvc` build.
  A sidecar build hook must build for that triple, not for the host, or the
  bundle picks up whatever stale `ncd-core-<triple>.exe` is lying in
  `binaries/`.
- `-buildvcs=false` only matters outside a git checkout (the scratch build
  failed without it); harmless inside the repo.
- A GUI-subsystem Go process still reads/writes the stdin/stdout **pipes** it
  is given (the handles come from `STARTUPINFO`); with no pipes (double-click)
  writes are silently dropped — ncd-core must therefore treat stdin EOF at
  start as "no parent" and exit, not spin.
- Exec helper, proven on Linux (timeouts kill grandchildren: a `sh -c 'sleep
  30 &'` child was reaped after the 2 s timeout with no orphan left), and
  cross-compiled for Windows:

```go
// internal/sys/run.go
func Run(ctx context.Context, timeout time.Duration, name string, args ...string) (Result, error) {
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
    cmd := exec.CommandContext(ctx, name, args...)   // argv only, never a shell
    var out, errb bytes.Buffer
    cmd.Stdout, cmd.Stderr = &out, &errb
    platform(cmd)
    cmd.WaitDelay = 2 * time.Second                 // a grandchild holding the pipe cannot block Wait
    err := cmd.Run()
    res := Result{Stdout: out.String(), Stderr: errb.String(), Code: -1}
    if cmd.ProcessState != nil { res.Code = cmd.ProcessState.ExitCode() }
    return res, err
}

// run_windows.go
const createNoWindow = 0x08000000
func platform(cmd *exec.Cmd) {
    cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// run_unix.go (!windows)
func platform(cmd *exec.Cmd) {
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
    cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
```

  On Windows a timeout kills only the direct child; for tools that fork
  (`claude.cmd` → node, `docker compose`) assign the child to a Job object with
  `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` if orphans show up in M7 testing.

### WebView2 and the portable layout

- `ncd.exe` needs the **Evergreen WebView2 Runtime**. It ships with Windows
  11 and is on "the vast majority" of Windows 10 devices, not all
  ([Microsoft: distribution](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution)).
- `install.ps1` must detect it before first start: `pv` (REG_SZ, > 0.0.0.0)
  under
  `HKLM\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
  or
  `HKCU\Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`;
  if missing, download the Evergreen bootstrapper and run
  `MicrosoftEdgeWebview2Setup.exe /silent /install` (per-user when not
  elevated), or tell the user where to get it.
- Layout: `%LOCALAPPDATA%\NoClickDock\ncd.exe` + `ncd-core.exe` side by side
  (the shell plugin resolves the sidecar next to `current_exe()`), autostart via
  `tauri-plugin-autostart` (HKCU Run key). The installer must first stop the
  Python generation and remove its Startup shortcuts — see
  [specs/dock-core.md §8](../specs/dock-core.md) for why (it deletes
  unknown `*.json` files in `%APPDATA%\NoClickDock`).
- The exes are unsigned: a downloaded copy carries Mark-of-the-Web and
  SmartScreen will warn; `install.ps1` should `Unblock-File` what it copies.
  Unsigned `-H=windowsgui` Go binaries are a known source of antivirus false
  positives — keep the sidecar's name and location stable and document it.

---

## Scratch-app provenance

Every Rust snippet in this file (`overlay`, `fit`, `supervise`, `core_send`,
`dial_menu`, the exit hook, `win.rs`) was compiled with `cargo check` for
`x86_64-unknown-linux-gnu` and `x86_64-pc-windows-msvc` against tauri 2.11.5,
and `win.rs` is linked into the cross-built probe exe.

Probe sources (not committed): a Tauri app with three overlay windows driven
over a FIFO, a Go echo sidecar, and `xprop`/`xwininfo`/`xdotool` scripts that
sample `_NET_ACTIVE_WINDOW`, window state, geometry and the window under the
pointer. The pointer was moved over the probe's own windows only and restored
afterwards; all probe processes were killed after each run.
