//! Every window, monitor and GTK call belongs to the main thread. Xlib is not
//! thread-safe here (XInitThreads is never called): a monitor's work area
//! read from the sidecar's reader thread aborted the app in xcb. Other
//! threads only parse and forward; anything that touches geometry hops over
//! with `run`, and debug builds panic at the first call that does not.
use std::sync::OnceLock;
use std::thread::ThreadId;

use tauri::AppHandle;

static MAIN: OnceLock<ThreadId> = OnceLock::new();

/// Remembers the main thread. Called from `setup`, which runs on it.
pub fn init() {
    let _ = MAIN.set(std::thread::current().id());
}

/// Debug builds: panics when called off the main thread, so a regression
/// shows in testing instead of as a rare xcb abort. Unit tests never call
/// `init`, so pure geometry stays testable.
#[track_caller]
pub fn assert() {
    #[cfg(debug_assertions)]
    if let Some(main) = MAIN.get() {
        assert_eq!(
            std::thread::current().id(),
            *main,
            "a window, monitor or GTK call off the main thread ({:?})",
            std::thread::current().name()
        );
    }
}

/// Runs `f` on the main thread: at once when already on it (Tauri runs a
/// main-thread task inline), else queued to the event loop.
pub fn run(app: &AppHandle, f: impl FnOnce(&AppHandle) + Send + 'static) {
    let main = app.clone();
    if let Err(error) = app.run_on_main_thread(move || f(&main)) {
        eprintln!("ncd: could not reach the main thread: {error}");
    }
}

#[cfg(test)]
mod tests {
    #[test]
    #[cfg(debug_assertions)]
    fn a_geometry_call_off_the_main_thread_panics() {
        super::init(); // this test's thread stands in for the main thread
        super::assert();
        let elsewhere = std::thread::spawn(super::assert).join();
        assert!(elsewhere.is_err(), "the reader thread must not reach GTK");
    }
}
