//! NoClickDock's shell: it owns the windows and nothing else. Every number
//! the dock shows comes from ncd-core, a Go sidecar spoken to over stdio.
mod core;
mod dock;
mod hover;
mod main_thread;
mod menu;
mod overlay;
mod toast;
#[cfg(windows)]
mod win;

use tauri::{Manager, RunEvent, WebviewWindow, WindowEvent};

/// A page's uncaught error, written where the shell's own errors go: a
/// production webview has no console anyone can read.
#[tauri::command]
fn page_error(window: WebviewWindow, message: String) {
    let message: String = message.chars().take(2000).collect();
    eprintln!("ncd: {} page: {message}", window.label());
}

/// `ncd [--mock] [--instance <name>]`, or NCD_INSTANCE. `--mock` runs the
/// real windows on invented data that moves (a job's progress, a service
/// that falls ill and recovers, so toasts appear), with a scratch config, so
/// they can be reviewed without showing (or changing) anything about this
/// machine.
struct Options {
    mock: bool,
    instance: Option<String>,
}

impl Options {
    fn from_env() -> Self {
        let mut options = Self { mock: false, instance: std::env::var("NCD_INSTANCE").ok() };
        let mut args = std::env::args().skip(1);
        while let Some(arg) = args.next() {
            match arg.as_str() {
                "--mock" => options.mock = true,
                "--instance" => options.instance = args.next(),
                _ => eprintln!("ncd: ignoring unknown argument {arg}"),
            }
        }
        // The instance names a directory: keep it to a plain word.
        options.instance = options
            .instance
            .filter(|name| !name.is_empty() && name.chars().all(|c| c.is_ascii_alphanumeric() || c == '-' || c == '_'));
        options
    }

    fn core_args(&self) -> Vec<String> {
        if !self.mock {
            return Vec::new();
        }
        let name = format!("mock-{}", self.instance.as_deref().unwrap_or("default"));
        let config = user_cache_dir().join("noclickdock-dev").join(name);
        vec!["--mock".into(), "--mock-live".into(), "--stdio".into(), "--config".into(), config.to_string_lossy().into_owned()]
    }
}

/// The user's own cache directory: a scratch config never lives in the
/// world-writable temp dir, where another user could plant one.
fn user_cache_dir() -> std::path::PathBuf {
    #[cfg(windows)]
    let base = std::env::var_os("LOCALAPPDATA").map(std::path::PathBuf::from);
    #[cfg(not(windows))]
    let base = std::env::var_os("XDG_CACHE_HOME")
        .map(std::path::PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|home| std::path::PathBuf::from(home).join(".cache")));
    base.unwrap_or_else(std::env::temp_dir)
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    main_thread::init_xlib();
    let options = Options::from_env();
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_clipboard_manager::init())
        .manage(core::Core::new(options.core_args()))
        .manage(dock::Dock::default())
        .manage(hover::Hover::default())
        .manage(toast::Toasts::default())
        .manage(menu::Tray::default())
        .manage(overlay::Scale::default())
        .manage(overlay::Shown::default())
        .invoke_handler(tauri::generate_handler![
            core::core_state,
            core::core_send,
            core::core_link,
            dock::rail_layout,
            hover::hover_dial,
            hover::hover_panel,
            hover::panel_layout,
            hover::panel_painted,
            toast::toast_layout,
            toast::toast_dismiss,
            menu::dial_menu,
            menu::rail_menu,
            page_error,
        ])
        .setup(|app| {
            main_thread::init();
            overlay::init_scale(app);
            // Built once and reused: each webview is a WebKit process.
            overlay::build(app, "rail", "dock")?;
            for label in hover::PANELS {
                let panel = overlay::build(app, label, "panel")?;
                overlay::prime(&panel)?;
            }
            overlay::build(app, "toast", "toast")?;
            if let Err(error) = menu::tray(app) {
                // No tray host (vanilla GNOME): the rail's menu has everything.
                eprintln!("ncd: no tray: {error}");
            }
            dock::observe_monitors(app.handle());
            core::supervise(app.handle().clone());
            Ok(())
        })
        .on_menu_event(|app, event| menu::chosen(app, event.id().as_ref()))
        .on_window_event(|window, event| {
            if window.label() != "rail" {
                return;
            }
            match event {
                WindowEvent::Moved(_) => dock::moved(window.app_handle()),
                // A monitor with another scale: the rail's geometry changes.
                WindowEvent::ScaleFactorChanged { .. } => dock::replace(window.app_handle()),
                _ => {}
            }
        })
        .build(tauri::generate_context!())
        .expect("NoClickDock could not start")
        .run(|app, event| {
            if let RunEvent::Exit = event {
                core::shutdown(app);
            }
        });
}
