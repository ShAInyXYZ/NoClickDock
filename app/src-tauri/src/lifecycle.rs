//! The app's life beyond its windows: one instance, launch at login, and
//! what a config change asks of the shell.
//!
//! - A second plain `ncd` does not start another dock: it reveals the
//!   running one (`--settings` opens its Settings). Named instances
//!   (`--instance`, `--mock`) are development runs and stay separate.
//! - Launch at login follows config.json's start.launchAtLogin — only in a
//!   release build running as the real app. A development build never
//!   touches the login items (on Linux, ~/.config/autostart).
use serde_json::Value;
#[cfg(not(debug_assertions))]
use tauri::Manager;
use tauri::AppHandle;

use crate::{dock, main_thread, menu};

/// Another `ncd` was started while this one runs; this is its argv. Called
/// off the main thread (the single-instance plugin's bus).
pub fn another(app: &AppHandle, argv: Vec<String>) {
    main_thread::run(app, move |app| {
        if argv.iter().any(|a| a == "--settings") {
            menu::settings(app);
        } else {
            dock::set_hidden(app, false);
        }
    });
}

fn edge(config: Option<&Value>) -> String {
    config.and_then(|c| c.pointer("/dock/position/edge")).and_then(Value::as_str).unwrap_or_default().to_owned()
}

/// The core announced a config (a hello, or after a patch). From the
/// relay's thread.
pub fn config(app: &AppHandle, previous: Option<&Value>, config: &Value) {
    // Settings chose an edge for the rail. (The shell's own saves after a
    // drag name the edge the rail is already on: following them moves
    // nothing.)
    if previous.is_some() && edge(previous) != edge(Some(config)) {
        main_thread::run(app, dock::follow_edge);
    }
    if let Some(on) = config.pointer("/start/launchAtLogin").and_then(Value::as_bool) {
        let app = app.clone();
        main_thread::run(&app, move |app| login(app, on));
    }
}

/// Makes the login item agree with the config — where allowed.
fn login(app: &AppHandle, on: bool) {
    #[cfg(not(debug_assertions))]
    if let Some(manager) = app.try_state::<tauri_plugin_autostart::AutoLaunchManager>() {
        let now = manager.is_enabled().unwrap_or(!on);
        if now != on {
            let done = if on { manager.enable() } else { manager.disable() };
            match done {
                Ok(()) => eprintln!("ncd: launch at login {}", if on { "on" } else { "off" }),
                Err(error) => eprintln!("ncd: could not turn launch at login {}: {error}", if on { "on" } else { "off" }),
            }
        }
        return;
    }
    let _ = (app, on); // a development build, or a named instance: left alone
}
