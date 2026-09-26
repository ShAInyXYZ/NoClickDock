//! The overlay windows — rail, panel, toast — and the three things every one of
//! them needs: exact sizing, placement before reveal, and the scale between
//! the page's CSS pixels and the desktop's physical ones.
//! Facts behind each choice: docs/rewrite/research/tauri-windows.md.
use std::collections::HashSet;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Mutex;

use tauri::{Manager, PhysicalPosition, WebviewUrl, WebviewWindow, WebviewWindowBuilder};

/// Built once at startup and reused: each webview is a WebKit process.
pub fn build(app: &tauri::App, label: &str, route: &str) -> tauri::Result<WebviewWindow> {
    let window = WebviewWindowBuilder::new(app, label, WebviewUrl::App(route.into()))
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
        // Insurance, as HubVoice learnt: the window manager may still move a
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

pub fn conceal(window: &WebviewWindow) -> tauri::Result<()> {
    crate::main_thread::assert();
    #[cfg(windows)]
    return crate::win::hide(window);
    #[cfg(target_os = "linux")]
    return park(window);
    #[cfg(not(any(windows, target_os = "linux")))]
    window.hide()
}

/// The toast window is click-through for good; the others take the pointer
/// while they show.
#[cfg(target_os = "linux")]
fn clickable(window: &WebviewWindow) -> bool {
    window.label() != "toast"
}

#[cfg(target_os = "linux")]
fn park(window: &WebviewWindow) -> tauri::Result<()> {
    use gtk::prelude::WidgetExt;
    let shown = window.app_handle().state::<Shown>();
    if !shown.0.lock().map(|set| set.contains(window.label())).unwrap_or(false) {
        return Ok(()); // never shown: nothing to hide (and no GdkWindow yet)
    }
    window.gtk_window()?.set_opacity(0.0);
    if clickable(window) {
        window.set_ignore_cursor_events(true)?;
    }
    Ok(())
}

#[cfg(target_os = "linux")]
fn unpark(window: &WebviewWindow) -> tauri::Result<()> {
    use gtk::prelude::WidgetExt;
    if clickable(window) {
        window.set_ignore_cursor_events(false)?;
    }
    window.gtk_window()?.set_opacity(1.0);
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
