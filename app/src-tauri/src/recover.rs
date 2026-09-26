//! A crashed web process (research M2 r4). The four views share one WebKit
//! web process; when it dies — the OOM killer, a driver crash — every view
//! stops drawing at once, and the dock would look alive with its last frame
//! while nothing updates and no hover opens anything. Each view's
//! `web-process-terminated` brings them all back: the pages are reloaded,
//! fetch the core's current state from the relay cache as on any start, and
//! the panel and toast windows start over empty (still parked, still
//! click-through); a panel page that was asked to draw while it reloaded
//! is asked again once it listens (`panel_ready`).
//!
//! Bounded: five reloads in a minute, backing off 0.25 s → 4 s. A sixth
//! crash hides the dock rather than leave a frozen one on screen, and it is
//! tried again every minute.
//!
//! Linux only for now: WebView2's equivalent (`ProcessFailed`) is M7.
#![cfg_attr(not(target_os = "linux"), allow(dead_code))]
use std::collections::VecDeque;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use tauri::{AppHandle, Manager};

const LABELS: [&str; 4] = ["rail", crate::hover::PANELS[0], crate::hover::PANELS[1], "toast"];
const WINDOW: Duration = Duration::from_secs(60);
const ATTEMPTS: usize = 5;
const GIVE_UP_RETRY: Duration = Duration::from_secs(60);
/// The rail's page is loading by then.
const RELATED_AFTER: Duration = Duration::from_millis(400);

#[derive(Default)]
pub struct Recovery(Mutex<State>);

#[derive(Default)]
struct State {
    crashes: VecDeque<Instant>,
    /// A reload is scheduled: the other views' signals for the same crash
    /// are the same event.
    pending: bool,
    gave_up: bool,
}

/// Watches every view. Main thread, once the windows exist.
#[cfg(target_os = "linux")]
pub fn watch(app: &tauri::App) {
    use webkit2gtk::WebViewExt;
    for label in LABELS {
        let Some(window) = app.get_webview_window(label) else { continue };
        let handle = app.handle().clone();
        let _ = window.with_webview(move |view| {
            view.inner().connect_web_process_terminated(move |_, reason| {
                let why = match reason {
                    webkit2gtk::WebProcessTerminationReason::ExceededMemoryLimit => "exceeded its memory limit",
                    webkit2gtk::WebProcessTerminationReason::TerminatedByApi => "was terminated",
                    _ => "crashed",
                };
                terminated(&handle, label, why);
            });
        });
    }
}

#[cfg(not(target_os = "linux"))]
pub fn watch(_app: &tauri::App) {}

fn terminated(app: &AppHandle, label: &str, why: &str) {
    crate::main_thread::assert();
    crate::watchdog::note(format!("web process {why} ({label})"));
    let recovery = app.state::<Recovery>();
    let Ok(mut st) = recovery.0.lock() else { return };
    if st.pending {
        return; // one crash, four views: already on its way back
    }
    let now = Instant::now();
    st.crashes.push_back(now);
    while st.crashes.front().is_some_and(|t| now.duration_since(*t) > WINDOW) {
        st.crashes.pop_front();
    }
    st.pending = true;
    let n = st.crashes.len();
    if n <= ATTEMPTS {
        let delay = Duration::from_millis(250 << (n - 1));
        eprintln!("ncd: the interface's web process {why}; reloading in {} ms ({n} of {ATTEMPTS} this minute)", delay.as_millis());
        later(app, delay, false);
    } else {
        st.gave_up = true;
        eprintln!("ncd: the interface's web process {why} {n} times in a minute; the dock is hidden, trying again in a minute");
        drop(st);
        crate::dock::set_hidden(app, true);
        later(app, GIVE_UP_RETRY, true);
    }
}

fn later(app: &AppHandle, delay: Duration, show: bool) {
    #[cfg(target_os = "linux")]
    {
        let app = app.clone();
        gtk::glib::timeout_add_local_once(delay, move || reload(&app, show));
    }
    #[cfg(not(target_os = "linux"))]
    let _ = (app, delay, show);
}

/// Reloads every view and starts the panels and toasts over.
fn reload(app: &AppHandle, show: bool) {
    crate::main_thread::assert();
    {
        let recovery = app.state::<Recovery>();
        let Ok(mut st) = recovery.0.lock() else { return };
        st.pending = false;
        if show {
            st.gave_up = false;
        }
    }
    crate::hover::reset(app);
    crate::toast::reset(app);
    // The rail first, so the dock is back before anything else loads. The
    // views no longer share one process after this (WebKit gives each its
    // own on reload); memory is back to ~one process per view until the
    // next start — a price paid only after a crash.
    #[cfg(target_os = "linux")]
    {
        use webkit2gtk::WebViewExt;
        let reload_view = |app: &AppHandle, label: &str| {
            if let Some(window) = app.get_webview_window(label) {
                let _ = window.with_webview(|view| view.inner().reload());
            }
        };
        reload_view(app, LABELS[0]);
        let app2 = app.clone();
        gtk::glib::timeout_add_local_once(RELATED_AFTER, move || {
            for label in &LABELS[1..] {
                reload_view(&app2, label);
            }
        });
    }
    crate::watchdog::note("interface reloaded");
    eprintln!("ncd: interface reloaded");
    if show {
        crate::dock::set_hidden(app, false);
    }
}

/// The user asked for the dock back after recovery gave up: try now.
pub fn retry(app: &AppHandle) {
    let gave_up = app.state::<Recovery>().0.lock().map(|st| st.gave_up && !st.pending).unwrap_or(false);
    if gave_up {
        reload(app, false);
        if let Ok(mut st) = app.state::<Recovery>().0.lock() {
            st.gave_up = false;
        }
    }
}
