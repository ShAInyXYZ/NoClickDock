//! A crashed web process (research M2 r4). The four views share one WebKit
//! web process; when it dies — the OOM killer, a driver crash — every view
//! stops drawing at once, and the dock would look alive with its last frame
//! while nothing updates and no hover opens anything. Each view's
//! `web-process-terminated` brings them all back: the pages are reloaded
//! (their transparent background set again, or the rail's corners come
//! back black), fetch the core's current state from the relay cache as on
//! any start, and the panel and toast windows start over empty (still
//! parked, still click-through); a panel page that was asked to draw while
//! it reloaded is asked again once it listens (`panel_ready`).
//!
//! Bounded: five reloads in a minute, backing off 0.25 s → 4 s. A sixth
//! crash leaves a minimal rail — the NoClickDock dial alone, reading
//! "interface restarting" — and everything is tried again a minute later.
//! Only if even that crashes is the dock hidden until then. Each step is
//! logged, loudly.
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
const RETRY: Duration = Duration::from_secs(60);
/// The rail's page is loading by then.
const RELATED_AFTER: Duration = Duration::from_millis(400);

#[derive(Default)]
pub struct Recovery(Mutex<State>);

#[derive(Default, Clone, Copy, PartialEq)]
enum Mode {
    #[default]
    Normal,
    /// The rail shows the NoClickDock dial alone, until the retry.
    Minimal,
    /// Even the minimal rail crashed: the dock is hidden until the retry.
    Hidden,
}

#[derive(Default)]
struct State {
    crashes: VecDeque<Instant>,
    /// A reload is scheduled: the other views' signals for the same crash
    /// are the same event.
    pending: bool,
    /// When the last crash was handled: the views' signals for one crash
    /// arrive together.
    last: Option<Instant>,
    mode: Mode,
    /// Each view's page, taken while it was alive: a view whose process
    /// died no longer knows its URL.
    urls: Vec<(&'static str, url::Url)>,
}

/// Signals closer than this are one crash seen by several views.
const SAME_CRASH: Duration = Duration::from_millis(1500);

/// Watches every view. Main thread, once the windows exist.
#[cfg(target_os = "linux")]
pub fn watch(app: &tauri::App) {
    use webkit2gtk::WebViewExt;
    for label in LABELS {
        let Some(window) = app.get_webview_window(label) else { continue };
        if let (Ok(url), Ok(mut st)) = (window.url(), app.state::<Recovery>().0.lock()) {
            st.urls.push((label, url));
        }
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
    let now = Instant::now();
    if st.pending || st.last.is_some_and(|t| now.duration_since(t) < SAME_CRASH) {
        return; // one crash, four views: already on its way back
    }
    st.last = Some(now);
    st.crashes.push_back(now);
    while st.crashes.front().is_some_and(|t| now.duration_since(*t) > WINDOW) {
        st.crashes.pop_front();
    }
    let n = st.crashes.len();
    match st.mode {
        Mode::Normal if n <= ATTEMPTS => {
            st.pending = true;
            let delay = Duration::from_millis(250 << (n - 1));
            eprintln!("ncd: the interface's web process {why}; reloading in {} ms ({n} of {ATTEMPTS} this minute)", delay.as_millis());
            drop(st);
            later(app, delay, Step::Reload);
        }
        Mode::Normal => {
            st.mode = Mode::Minimal;
            eprintln!("ncd: ================================================================");
            eprintln!("ncd: the interface's web process {why} {n} times in a minute.");
            eprintln!("ncd: the rail now shows only 'interface restarting'; retrying in a minute.");
            eprintln!("ncd: ================================================================");
            drop(st);
            crate::hover::reset(app);
            crate::toast::reset(app);
            // Not from inside the termination signal: WebKit ignores a load
            // asked for there.
            later(app, Duration::from_millis(250), Step::Minimal);
            later(app, RETRY, Step::Full);
        }
        Mode::Minimal => {
            // The minimal rail crashed too: nothing can be drawn now.
            st.mode = Mode::Hidden;
            eprintln!("ncd: ================================================================");
            eprintln!("ncd: even the minimal rail crashed ({why}): the dock is hidden; retrying in a minute.");
            eprintln!("ncd: ================================================================");
            drop(st);
            crate::dock::set_hidden(app, true);
        }
        Mode::Hidden => {}
    }
}

enum Step {
    /// Reload every view.
    Reload,
    /// The rail with the NoClickDock dial alone.
    Minimal,
    /// Back from the minimal rail (or hidden): every view, and the dock.
    Full,
}

fn later(app: &AppHandle, delay: Duration, step: Step) {
    #[cfg(target_os = "linux")]
    {
        let app = app.clone();
        gtk::glib::timeout_add_local_once(delay, move || match step {
            Step::Reload => reload(&app),
            Step::Minimal => minimal_rail(&app),
            Step::Full => full(&app),
        });
    }
    #[cfg(not(target_os = "linux"))]
    let _ = (app, delay, step);
}

/// Reloads every view and starts the panels and toasts over.
fn reload(app: &AppHandle) {
    crate::main_thread::assert();
    if let Ok(mut st) = app.state::<Recovery>().0.lock() {
        st.pending = false;
    }
    crate::hover::reset(app);
    crate::toast::reset(app);
    // The rail first, so the dock is back before anything else loads. The
    // views no longer share one process after this (WebKit gives each its
    // own on reload); memory is back to ~one process per view until the
    // next start — a price paid only after a crash.
    load(app, LABELS[0], false);
    #[cfg(target_os = "linux")]
    {
        let app2 = app.clone();
        gtk::glib::timeout_add_local_once(RELATED_AFTER, move || {
            for label in &LABELS[1..] {
                load(&app2, label, false);
            }
        });
    }
    crate::watchdog::note("interface reloaded");
    eprintln!("ncd: interface reloaded");
}

/// The minute is over: everything again, and the dock shown.
fn full(app: &AppHandle) {
    let was = app.state::<Recovery>().0.lock().map(|mut st| std::mem::take(&mut st.mode)).unwrap_or_default();
    reload(app);
    if was == Mode::Hidden {
        crate::dock::set_hidden(app, false);
    }
}

/// The rail with the NoClickDock dial alone (dock?restarting): the least
/// that can say something is wrong.
fn minimal_rail(app: &AppHandle) {
    load(app, LABELS[0], true);
}

/// (Re)loads a view at its own page — `?restarting` for the minimal rail —
/// with its background transparent. (The black corners research M2 r5 saw
/// after a reload came from the page's first paint being opaque: app.html
/// now makes an overlay page transparent before it paints anything.)
fn load(app: &AppHandle, label: &str, restarting: bool) {
    let Some(window) = app.get_webview_window(label) else { return };
    transparent(&window);
    let known = app.state::<Recovery>().0.lock().ok().and_then(|st| st.urls.iter().find(|(l, _)| *l == label).map(|(_, u)| u.clone()));
    if let Some(mut url) = known {
        url.set_query(if restarting { Some("restarting") } else { None });
        let _ = window.navigate(url);
    } else {
        #[cfg(target_os = "linux")]
        let _ = window.with_webview(|view| {
            use webkit2gtk::WebViewExt;
            view.inner().reload();
        });
    }
}

fn transparent(window: &tauri::WebviewWindow) {
    let _ = window.set_background_color(Some(tauri::webview::Color(0, 0, 0, 0)));
    #[cfg(target_os = "linux")]
    let _ = window.with_webview(|view| {
        use webkit2gtk::WebViewExt;
        view.inner().set_background_color(&gtk::gdk::RGBA::new(0.0, 0.0, 0.0, 0.0));
    });
}

/// The user asked for the dock back while recovery waits: try now.
pub fn retry(app: &AppHandle) {
    let waiting = app.state::<Recovery>().0.lock().map(|st| st.mode != Mode::Normal).unwrap_or(false);
    if waiting {
        full(app);
    }
}
