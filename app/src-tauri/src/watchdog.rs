//! A heartbeat for the main loop, and the last things that happened.
//!
//! Design saw one instance freeze after ~27 minutes: timers stopped, a toast
//! stuck, the rail stale, menu choices lost — while the GTK main loop still
//! answered the pages. Two heartbeats tell the causes apart the next time:
//! - `glib` ticks from a GTK timeout on the main thread itself;
//! - `tasks` ticks from a tokio task through `run_on_main_thread`, the path
//!   every timer, toast and relay emit takes (tokio → tao's event queue →
//!   main thread).
//!
//! A thread of its own checks both every 2 s; one that has not moved for 5 s
//! is reported loudly on stderr with the last 64 events, once per stall.
use std::collections::VecDeque;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Mutex;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use tauri::AppHandle;

static GLIB: AtomicU64 = AtomicU64::new(0);
static TASKS: AtomicU64 = AtomicU64::new(0);
static EVENTS: Mutex<VecDeque<(f64, String)>> = Mutex::new(VecDeque::new());
const KEEP: usize = 64;
const STALL: Duration = Duration::from_secs(5);

fn now_ms() -> f64 {
    SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_secs_f64() * 1000.0).unwrap_or_default()
}

/// Remembers one event (cheap; any thread).
pub fn note(what: impl Into<String>) {
    if let Ok(mut events) = EVENTS.try_lock() {
        if events.len() == KEEP {
            events.pop_front();
        }
        events.push_back((now_ms(), what.into()));
    }
}

/// Starts both heartbeats and the watching thread. Main thread, once.
pub fn start(app: &AppHandle) {
    crate::main_thread::assert();
    #[cfg(target_os = "linux")]
    gtk::glib::timeout_add_local(Duration::from_secs(1), || {
        GLIB.fetch_add(1, Ordering::Relaxed);
        gtk::glib::ControlFlow::Continue
    });
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        loop {
            tokio::time::sleep(Duration::from_secs(1)).await;
            crate::main_thread::run(&app, |_| {
                TASKS.fetch_add(1, Ordering::Relaxed);
            });
        }
    });
    let _ = std::thread::Builder::new().name("ncd-watchdog".into()).spawn(watch);
}

fn watch() {
    let mut last = [(GLIB.load(Ordering::Relaxed), Instant::now()), (TASKS.load(Ordering::Relaxed), Instant::now())];
    let mut stalled = [false, false];
    let names = ["the GTK main loop", "timers and messages to the main thread (tokio → tao)"];
    loop {
        std::thread::sleep(Duration::from_secs(2));
        for (i, counter) in [&GLIB, &TASKS].into_iter().enumerate() {
            let value = counter.load(Ordering::Relaxed);
            if value != last[i].0 {
                if stalled[i] {
                    eprintln!("ncd: WATCHDOG: {} is moving again after {:.0} s", names[i], last[i].1.elapsed().as_secs_f64());
                    stalled[i] = false;
                }
                last[i] = (value, Instant::now());
            } else if !stalled[i] && last[i].1.elapsed() > STALL {
                stalled[i] = true;
                eprintln!("ncd: WATCHDOG: {} has not run for {:.0} s — the dock is frozen. Last events:", names[i], last[i].1.elapsed().as_secs_f64());
                if let Ok(events) = EVENTS.lock() {
                    let now = now_ms();
                    for (at, what) in events.iter() {
                        eprintln!("ncd:   -{:>8.0} ms  {what}", now - at);
                    }
                }
            }
        }
    }
}
