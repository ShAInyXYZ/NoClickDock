//! The overlay windows — rail, panel, toast — and the three things every one of
//! them needs: exact sizing, placement before reveal, and the scale between
//! the page's CSS pixels and the desktop's physical ones.
//! Facts behind each choice: docs/rewrite/research/tauri-windows.md.
use std::collections::HashSet;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Mutex;

use tauri::{Manager, PhysicalPosition, WebviewUrl, WebviewWindow, WebviewWindowBuilder};

/// Built once at startup and reused. On Linux every overlay after the first
/// is related to it, so the four share one WebKit web process instead of
/// one each (design M2 r1b: 370 MB PSS with four).
pub fn build(app: &tauri::App, label: &str, route: &str, related: Option<&WebviewWindow>) -> tauri::Result<WebviewWindow> {
    #[allow(unused_mut)]
    let mut builder = WebviewWindowBuilder::new(app, label, WebviewUrl::App(route.into()));
    #[cfg(target_os = "linux")]
    if let Some(view) = related.and_then(webkit_view) {
        builder = builder.with_related_view(view);
    }
    #[cfg(not(target_os = "linux"))]
    let _ = related;
    let window = builder
        .title(format!("NoClickDock {label}"))
        .inner_size(64.0, 64.0) // the real size comes from fit()
        .transparent(true)
        .decorations(false)
        .shadow(false)
        .always_on_top(true)
        .skip_taskbar(true)
        .visible_on_all_workspaces(true) // the dock is always there
        .resizable(false)
        .visible(false)
        // Never takes keyboard focus, not even on a click: hovering the dock
        // must not interrupt what the user is typing.
        .focused(false)
        .focusable(false)
        .build()?;
    window.set_background_color(Some(tauri::webview::Color(0, 0, 0, 0)))?;
    #[cfg(target_os = "linux")]
    if label == "toast" {
        pass_through(&window, true)?; // for good, from the first map
    }
    #[cfg(target_os = "linux")]
    {
        use gtk::prelude::GtkWindowExt;
        // Before the first show: the hint is read at map time. UTILITY, not
        // DOCK, which Mutter stacks above every keep-above window; toasts are
        // NOTIFICATIONs.
        let hint = if label == "toast" {
            gtk::gdk::WindowTypeHint::Notification
        } else {
            gtk::gdk::WindowTypeHint::Utility
        };
        window.gtk_window()?.set_type_hint(hint);
    }
    #[cfg(windows)]
    crate::win::harden(&window)?;
    Ok(window)
}

/// The WebKit view inside an overlay's GTK window.
#[cfg(target_os = "linux")]
pub fn webkit_view(window: &WebviewWindow) -> Option<webkit2gtk::WebView> {
    use gtk::prelude::*;
    fn find(widget: &gtk::Widget) -> Option<webkit2gtk::WebView> {
        if let Ok(view) = widget.clone().downcast::<webkit2gtk::WebView>() {
            return Some(view);
        }
        widget.clone().downcast::<gtk::Container>().ok()?.children().iter().find_map(find)
    }
    find(window.gtk_window().ok()?.upcast_ref())
}

/// Sizes a window exactly, in physical pixels. On Linux `set_size` cannot
/// shrink a transparent WebKitGTK window below its webview's natural size,
/// so the GTK window is pinned to the size instead.
pub fn fit(window: &WebviewWindow, width: i32, height: i32) -> tauri::Result<()> {
    crate::main_thread::assert();
    #[cfg(target_os = "linux")]
    {
        window.with_webview(move |view| {
            use gtk::prelude::*;
            let webview = view.inner();
            let gtk_scale = webview.scale_factor().max(1);
            let (w, h) = ((width + gtk_scale - 1) / gtk_scale, (height + gtk_scale - 1) / gtk_scale);
            webview.set_size_request(1, 1); // drop the webview's natural minimum
            if let Some(top) = webview.toplevel().and_then(|t| t.downcast::<gtk::Window>().ok()) {
                top.set_size_request(w, h);
                top.set_geometry_hints(
                    None::<&gtk::Widget>,
                    Some(&gtk::gdk::Geometry::new(w, h, w, h, 0, 0, 1, 1, 0.0, 0.0, gtk::gdk::Gravity::NorthWest)),
                    gtk::gdk::WindowHints::MIN_SIZE | gtk::gdk::WindowHints::MAX_SIZE,
                );
                top.set_resizable(true); // min == max: still not resizable by hand
                top.resize(w, h);
            }
        })
    }
    #[cfg(not(target_os = "linux"))]
    window.set_size(tauri::PhysicalSize::new(width.max(1) as u32, height.max(1) as u32))
}

/// Windows shown at least once. On Windows a later show must bypass tao,
/// whose `show()` activates the window.
#[derive(Default)]
pub struct Shown(Mutex<HashSet<String>>);

/// Windows parked right now (Linux): mapped, invisible at the compositor,
/// taking no input.
#[derive(Default)]
pub struct Parked(#[cfg_attr(not(target_os = "linux"), allow(dead_code))] Mutex<HashSet<String>>);

#[cfg(target_os = "linux")]
fn set_parked(window: &WebviewWindow, parked: bool) {
    let app = window.app_handle();
    let state = app.state::<Parked>();
    if let Ok(mut set) = state.0.lock() {
        if parked {
            set.insert(window.label().to_owned());
        } else {
            set.remove(window.label());
        }
    };
}

/// Places, then shows: Mutter re-places a re-mapped window that was not
/// moved first. Never focuses.
///
/// On Linux a window is mapped once and never unmapped again: mapping a
/// window costs Mutter ~60 ms (it waits for WebKit's first frame), which
/// would sit between the hover dwell and the panel appearing. Hiding parks
/// it instead — invisible and click-through, still mapped — and showing it
/// again is immediate.
pub fn reveal(window: &WebviewWindow, position: PhysicalPosition<i32>) -> tauri::Result<()> {
    crate::main_thread::assert();
    window.set_position(position)?;
    let shown = window.app_handle().state::<Shown>();
    let first = shown.0.lock().map(|mut set| set.insert(window.label().to_owned())).unwrap_or(true);
    #[cfg(windows)]
    if !first {
        return crate::win::show(window);
    }
    #[cfg(target_os = "linux")]
    if !first {
        return unpark(window);
    }
    let _ = first;
    window.show()?;
    #[cfg(target_os = "linux")]
    {
        // Insurance: the window manager may still move a
        // freshly mapped window; put it back once it is on screen.
        let window = window.clone();
        gtk::glib::timeout_add_local_once(std::time::Duration::from_millis(60), move || {
            if window.outer_position().ok() != Some(position) {
                let _ = window.set_position(position);
            }
        });
    }
    Ok(())
}

/// Maps a window once, parked: WebKit runs no rendering updates (no
/// ResizeObserver, no animation frames) in a window never shown, and a
/// panel page must draw, measure and paint while still unseen. Parked
/// before it is ever mapped — invisible, taking no input, 1×1 — so it is
/// never an inert rectangle on screen that swallows clicks (design M2 r1b).
pub fn prime(window: &WebviewWindow) -> tauri::Result<()> {
    crate::main_thread::assert();
    #[cfg(target_os = "linux")]
    if composited() {
        pass_through(window, true)?;
        if let Some(id) = crate::x11::xid(window) {
            crate::x11::set_visible(&[(id, false)]);
        }
        fit(window, 1, 1)?;
        set_parked(window, true);
        window.show()?;
        let app = window.app_handle();
        let shown = app.state::<Shown>();
        if let Ok(mut set) = shown.0.lock() {
            set.insert(window.label().to_owned());
        };
    }
    #[cfg(not(target_os = "linux"))]
    let _ = window;
    Ok(())
}

/// Runs `f` once GTK has drawn a frame of this window from now on (at most
/// 50 ms later, whatever happens): what the page painted is then in the
/// window's buffer, not only in WebKit's.
pub fn after_next_frame(window: &WebviewWindow, f: impl FnOnce() + 'static) {
    crate::main_thread::assert();
    #[cfg(target_os = "linux")]
    {
        use gtk::glib;
        use gtk::prelude::*;
        use std::cell::RefCell;
        use std::rc::Rc;
        type Job = Rc<RefCell<Option<Box<dyn FnOnce()>>>>;
        let job: Job = Rc::new(RefCell::new(Some(Box::new(f))));
        let run = |job: &Job| {
            let f = job.borrow_mut().take();
            if let Some(f) = f {
                f();
            }
        };
        if let Some((gtk_window, clock)) = window.gtk_window().ok().and_then(|w| w.frame_clock().map(|c| (w, c))) {
            let handler: Rc<RefCell<Option<glib::SignalHandlerId>>> = Rc::new(RefCell::new(None));
            let (j, h, c) = (job.clone(), handler.clone(), clock.clone());
            let id = clock.connect_after_paint(move |_| {
                run(&j);
                if let Some(id) = h.borrow_mut().take() {
                    let c = c.clone();
                    glib::idle_add_local_once(move || c.disconnect(id));
                }
            });
            *handler.borrow_mut() = Some(id);
            gtk_window.queue_draw();
            let j = job.clone();
            glib::timeout_add_local_once(std::time::Duration::from_millis(50), move || run(&j));
            return;
        }
        run(&job);
    }
    #[cfg(not(target_os = "linux"))]
    {
        let _ = window;
        f();
    }
}

/// Shows `incoming` where it already is and parks `outgoing`, as one change
/// at the compositor: no frame with neither, none with both. The incoming
/// window was sized and placed while it was drawn (hover::place): the swap
/// is only the two properties in a server grab — no geometry query, no
/// move (design M1 r5: those cost 11–18 ms before the flip reached the
/// server). `position` is for the fallback without a compositor.
pub fn swap(incoming: &WebviewWindow, outgoing: &WebviewWindow, position: PhysicalPosition<i32>) -> tauri::Result<()> {
    crate::main_thread::assert();
    #[cfg(target_os = "linux")]
    if composited() && is_parked(incoming) {
        if let (Some(a), Some(b)) = (crate::x11::xid(incoming), crate::x11::xid(outgoing)) {
            if crate::x11::set_visible(&[(a, true), (b, false)]) {
                if clickable(incoming) {
                    pass_through(incoming, false)?;
                }
                pass_through(outgoing, true)?;
                set_parked(incoming, false);
                set_parked(outgoing, true);
                return Ok(());
            }
        }
    }
    reveal(incoming, position)?;
    conceal(outgoing)
}

#[cfg(target_os = "linux")]
fn is_parked(window: &WebviewWindow) -> bool {
    let set = window.app_handle().state::<Parked>();
    let parked = set.0.lock().map(|set| set.contains(window.label())).unwrap_or(false);
    parked
}

pub fn conceal(window: &WebviewWindow) -> tauri::Result<()> {
    crate::main_thread::assert();
    #[cfg(windows)]
    return crate::win::hide(window);
    #[cfg(target_os = "linux")]
    return if composited() { park(window) } else { unmap(window) };
    #[cfg(not(any(windows, target_os = "linux")))]
    window.hide()
}

/// Whether a compositor draws the desktop. Parking needs one: without it a
/// window at opacity 0 is an opaque black rectangle (research M2 r2), so
/// windows are unmapped instead, and the panel uses one window.
pub fn composited() -> bool {
    #[cfg(target_os = "linux")]
    return gtk::gdk::Screen::default().is_some_and(|s| s.is_composited());
    #[cfg(not(target_os = "linux"))]
    true
}

/// Without a compositor: unmap, and forget it was shown, so the next reveal
/// maps it again (placed first).
#[cfg(target_os = "linux")]
fn unmap(window: &WebviewWindow) -> tauri::Result<()> {
    use gtk::prelude::WidgetExt;
    if let Some(id) = crate::x11::xid(window) {
        crate::x11::set_visible(&[(id, true)]); // a mapped window is never left invisible
    }
    let shown = window.app_handle().state::<Shown>();
    if let Ok(mut set) = shown.0.lock() {
        set.remove(window.label());
    }
    window.gtk_window()?.set_opacity(1.0);
    window.hide()
}

/// The compositor went away: every parked window (invisible, still mapped)
/// would now be a black rectangle — unmap them.
#[cfg(target_os = "linux")]
pub fn watch_compositor(app: &tauri::App) {
    let Some(screen) = gtk::gdk::Screen::default() else { return };
    let app = app.handle().clone();
    screen.connect_composited_changed(move |screen| {
        if screen.is_composited() {
            return;
        }
        for window in app.webview_windows().values() {
            if is_parked(window) {
                set_parked(window, false);
                let _ = unmap(window);
            }
        }
    });
}

#[cfg(not(target_os = "linux"))]
pub fn watch_compositor(_app: &tauri::App) {}

/// The toast window is click-through for good; the others take the pointer
/// while they show.
#[cfg(target_os = "linux")]
fn clickable(window: &WebviewWindow) -> bool {
    window.label() != "toast"
}

#[cfg(target_os = "linux")]
fn park(window: &WebviewWindow) -> tauri::Result<()> {
    let shown = window.app_handle().state::<Shown>();
    if !shown.0.lock().map(|set| set.contains(window.label())).unwrap_or(false) {
        return Ok(()); // never shown: nothing to hide
    }
    pass_through(window, true)?;
    if let Some(id) = crate::x11::xid(window) {
        crate::x11::set_visible(&[(id, false)]);
    }
    set_parked(window, true);
    Ok(())
}

#[cfg(target_os = "linux")]
fn unpark(window: &WebviewWindow) -> tauri::Result<()> {
    if let Some(id) = crate::x11::xid(window) {
        crate::x11::set_visible(&[(id, true)]);
    }
    if clickable(window) {
        pass_through(window, false)?;
    }
    set_parked(window, false);
    Ok(())
}

/// Lets every pointer event through the window (an empty input shape), or
/// takes them again. Not tao's `set_ignore_cursor_events`: that keeps a 1×1
/// input region at the window's corner, and a parked window stays mapped
/// and on top — that pixel would swallow a click meant for what is below.
/// Set on the GTK widget, which applies it at every realize and map — set
/// only on the X window, the first map reset it (design M2 r1b: the
/// never-shown panels swallowed clicks at 0,0).
#[cfg(target_os = "linux")]
pub fn pass_through(window: &WebviewWindow, through: bool) -> tauri::Result<()> {
    use gtk::prelude::WidgetExt;
    crate::main_thread::assert();
    let gtk_window = window.gtk_window()?;
    if through {
        gtk_window.input_shape_combine_region(Some(&gtk::cairo::Region::create()));
    } else {
        gtk_window.input_shape_combine_region(None);
    }
    Ok(())
}

/// Physical pixels per CSS pixel. WebKitGTK lays pages out at the desktop's
/// text scale (Xft.dpi / 96) while Tauri's scale factor says 1, so the scale
/// is read from GTK and confirmed against what the rail page measures.
pub struct Scale(AtomicU64);

impl Default for Scale {
    fn default() -> Self {
        Self(AtomicU64::new(1.0_f64.to_bits()))
    }
}

impl Scale {
    pub fn get(&self) -> f64 {
        f64::from_bits(self.0.load(Ordering::Acquire))
    }

    pub fn set(&self, scale: f64) {
        if scale.is_finite() && (0.5..=8.0).contains(&scale) {
            self.0.store(scale.to_bits(), Ordering::Release);
        }
    }
}

/// Reads the initial scale. Main thread only (GTK).
pub fn init_scale(app: &tauri::App) {
    crate::main_thread::assert();
    #[cfg(target_os = "linux")]
    {
        use gtk::prelude::GtkSettingsExt;
        if let Some(dpi) = gtk::Settings::default().map(|s| s.gtk_xft_dpi()).filter(|dpi| *dpi > 0) {
            app.state::<Scale>().set(f64::from(dpi) / 1024.0 / 96.0);
        }
    }
    #[cfg(not(target_os = "linux"))]
    let _ = app;
}

/// The scale for a window: WebView2 follows the monitor's DPI (CSS px =
/// logical px); WebKitGTK follows the text scale measured above.
pub fn scale(window: &WebviewWindow) -> f64 {
    #[cfg(windows)]
    return window.scale_factor().unwrap_or(1.0);
    #[cfg(not(windows))]
    window.app_handle().state::<Scale>().get()
}
