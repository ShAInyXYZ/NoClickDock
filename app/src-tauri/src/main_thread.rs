//! Every window, monitor and GTK call belongs to the main thread. Xlib is not
//! thread-safe here (XInitThreads is never called): a monitor's work area
//! read from the sidecar's reader thread aborted the app in xcb. Other
//! threads only parse and forward; anything that touches geometry hops over
//! with `run`, and debug builds panic at the first call that does not.
use std::sync::OnceLock;
use std::thread::ThreadId;

use tauri::AppHandle;

static MAIN: OnceLock<ThreadId> = OnceLock::new();

/// Makes Xlib safe to call from more than one thread, before anything opens
/// a display — defence in depth behind keeping every X call on the main
/// thread (the clipboard and the work-area watcher hold connections of
/// their own). Loaded at run time: without libX11 there is nothing to guard.
pub fn init_xlib() {
    #[cfg(target_os = "linux")]
    // SAFETY: libX11 is the library GDK itself uses; XInitThreads takes no
    // arguments and must only come before any other Xlib call, which it does.
    unsafe {
        if let Ok(lib) = libloading::Library::new("libX11.so.6") {
            if let Ok(init) = lib.get::<unsafe extern "C" fn() -> std::os::raw::c_int>(b"XInitThreads\0") {
                init();
            }
            std::mem::forget(lib); // Xlib stays loaded for the life of the app
        }
    }
}

/// Remembers the main thread. Called from `setup`, which runs on it.
pub fn init() {
    let _ = MAIN.set(std::thread::current().id());
}

/// Debug builds: panics when called off the main thread, so a regression
/// shows in testing instead of as a rare xcb abort. Release builds say so
/// once on stderr. Unit tests never call `init`, so pure geometry stays
/// testable.
#[track_caller]
pub fn assert() {
    let Some(main) = MAIN.get() else { return };
    if std::thread::current().id() == *main {
        return;
    }
    let at = std::panic::Location::caller();
    #[cfg(debug_assertions)]
    panic!("a window, monitor or GTK call off the main thread ({:?}) at {at}", std::thread::current().name());
    #[cfg(not(debug_assertions))]
    {
        use std::sync::atomic::{AtomicBool, Ordering};
        static SAID: AtomicBool = AtomicBool::new(false);
        if !SAID.swap(true, Ordering::Relaxed) {
            eprintln!("ncd: a window, monitor or GTK call off the main thread at {at}");
        }
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
