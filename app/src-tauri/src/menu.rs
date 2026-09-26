//! The native menus (DESIGN.md "Right-click and tray"): a dial's
//! right-click, the rail's own right-click, and the tray. They are secondary
//! surfaces, so they are the system's menus, not Cerveau's.
//!
//! Menu ids are namespaced: `refresh:<widget>`, `item:<widget>:<id>`,
//! `dock:<setting>:<value>`, `app:<verb>`. A widget item is looked up in its
//! last snapshot when chosen, never trusted from the page: one with a `url`
//! is opened, one with `copy` goes to the clipboard, anything else is an
//! action for the core.
use std::sync::Mutex;

use serde_json::{json, Value};
use tauri::menu::{CheckMenuItem, IsMenuItem, Menu, MenuItem, PredefinedMenuItem, Submenu};
use tauri::tray::TrayIconBuilder;
use tauri::{AppHandle, Manager, WebviewUrl, WebviewWindow, WebviewWindowBuilder, Wry};
use tauri_plugin_clipboard_manager::ClipboardExt;
use tauri_plugin_opener::OpenerExt;

use crate::{core, dock, hover, toast};

/// The tray's show/hide item, whose label follows the dock.
#[derive(Default)]
pub struct Tray(Mutex<Option<MenuItem<Wry>>>);

/// Right-click on a dial: refresh it, its own items, then the app's.
#[tauri::command]
pub fn dial_menu(window: WebviewWindow, widget: String) -> Result<(), String> {
    let app = window.app_handle();
    hover::close_for_menu(app);
    let e = |error: tauri::Error| error.to_string();
    let mut items: Vec<Box<dyn IsMenuItem<Wry>>> =
        vec![Box::new(MenuItem::with_id(app, format!("refresh:{widget}"), "Refresh now", true, None::<&str>).map_err(e)?)];
    for item in app.state::<core::Core>().menu_of(&widget) {
        let (Some(id), Some(label)) = (item.get("id").and_then(Value::as_str), item.get("label").and_then(Value::as_str))
        else {
            continue;
        };
        items.push(Box::new(MenuItem::with_id(app, format!("item:{widget}:{id}"), label, true, None::<&str>).map_err(e)?));
    }
    items.push(Box::new(PredefinedMenuItem::separator(app).map_err(e)?));
    items.extend(app_items(app).map_err(e)?);
    popup(&window, items)
}

/// Right-click on the rail itself: how the rail looks, then the app's items.
#[tauri::command]
pub fn rail_menu(window: WebviewWindow) -> Result<(), String> {
    let app = window.app_handle();
    hover::close_for_menu(app);
    let e = |error: tauri::Error| error.to_string();
    let dock = app.state::<core::Core>().dock_config().unwrap_or(Value::Null);
    let is = |key: &str, value: &str| dock.get(key).and_then(Value::as_str) == Some(value);
    let check = |id: &str, label: &str, on: bool| CheckMenuItem::with_id(app, id, label, true, on, None::<&str>);

    let orientation = Submenu::with_items(
        app,
        "Orientation",
        true,
        &[
            &check("dock:orientation:vertical", "Vertical", is("orientation", "vertical")).map_err(e)?,
            &check("dock:orientation:horizontal", "Horizontal", is("orientation", "horizontal")).map_err(e)?,
        ],
    )
    .map_err(e)?;
    let density = Submenu::with_items(
        app,
        "Density",
        true,
        &[
            &check("dock:density:compact", "Compact", is("density", "compact")).map_err(e)?,
            &check("dock:density:normal", "Normal", is("density", "normal")).map_err(e)?,
            &check("dock:density:large", "Large", is("density", "large")).map_err(e)?,
        ],
    )
    .map_err(e)?;
    let readouts = dock.get("readouts").and_then(Value::as_bool).unwrap_or(true);
    let mut items: Vec<Box<dyn IsMenuItem<Wry>>> = vec![
        Box::new(orientation),
        Box::new(density),
        Box::new(check("dock:readouts:toggle", "Readouts", readouts).map_err(e)?),
        Box::new(PredefinedMenuItem::separator(app).map_err(e)?),
    ];
    items.extend(app_items(app).map_err(e)?);
    popup(&window, items)
}

fn app_items(app: &AppHandle) -> tauri::Result<Vec<Box<dyn IsMenuItem<Wry>>>> {
    Ok(vec![
        Box::new(MenuItem::with_id(app, "app:settings", "Settings…", true, None::<&str>)?),
        Box::new(MenuItem::with_id(app, "app:quit", "Quit NoClickDock", true, None::<&str>)?),
    ])
}

fn popup(window: &WebviewWindow, items: Vec<Box<dyn IsMenuItem<Wry>>>) -> Result<(), String> {
    let refs: Vec<&dyn IsMenuItem<Wry>> = items.iter().map(|item| item.as_ref()).collect();
    let menu = Menu::with_items(window.app_handle(), &refs).map_err(|e| e.to_string())?;
    #[cfg(windows)]
    let shown = crate::win::popup_menu_keep_foreground(window, &menu);
    #[cfg(not(windows))]
    let shown = window.popup_menu(&menu);
    shown.map_err(|e| e.to_string())
}

/// The tray: show or hide the dock, Settings, Quit. On a GNOME without an
/// AppIndicator host, or without the AppIndicator library, there is no tray
/// at all — the rail's menu has the same items, so nothing depends on it.
pub fn tray(app: &tauri::App) -> Result<(), String> {
    #[cfg(target_os = "linux")]
    if !indicator_library() {
        return Err("neither libayatana-appindicator3 nor libappindicator3 is installed".into());
    }
    build_tray(app).map_err(|e| e.to_string())
}

/// libappindicator-sys loads one of these on first use and panics when
/// neither is there — an abort in a release build, which no `Result` can
/// catch. Fedora, Arch and openSUSE do not install either by default.
#[cfg(target_os = "linux")]
fn indicator_library() -> bool {
    ["libayatana-appindicator3.so.1", "libappindicator3.so.1"]
        .iter()
        // SAFETY: loading a system library runs its initialisers; these are
        // the libraries the tray is about to load anyway.
        .any(|name| unsafe { libloading::Library::new(name) }.is_ok())
}

fn build_tray(app: &tauri::App) -> tauri::Result<()> {
    let toggle = MenuItem::with_id(app, "app:toggle", "Hide dock", true, None::<&str>)?;
    let menu = Menu::with_items(
        app,
        &[
            &toggle,
            &MenuItem::with_id(app, "app:settings", "Settings…", true, None::<&str>)?,
            &PredefinedMenuItem::separator(app)?,
            &MenuItem::with_id(app, "app:quit", "Quit NoClickDock", true, None::<&str>)?,
        ],
    )?;
    let mut builder = TrayIconBuilder::with_id("ncd").tooltip("NoClickDock").menu(&menu).show_menu_on_left_click(true);
    if let Some(icon) = app.default_window_icon() {
        builder = builder.icon(icon.clone());
    }
    builder.build(app)?;
    if let Ok(mut slot) = app.state::<Tray>().0.lock() {
        *slot = Some(toggle);
    }
    Ok(())
}

/// Every menu choice, from any menu.
pub fn chosen(app: &AppHandle, id: &str) {
    let mut parts = id.splitn(3, ':');
    match (parts.next(), parts.next(), parts.next()) {
        (Some("refresh"), Some(widget), None) => send(app, json!({"type": "refresh", "widget": widget})),
        (Some("item"), Some(widget), Some(item)) => widget_item(app, widget, item),
        (Some("dock"), Some("readouts"), _) => {
            let on = app
                .state::<core::Core>()
                .dock_config()
                .and_then(|d| d.get("readouts").and_then(Value::as_bool))
                .unwrap_or(true);
            send(app, json!({"type": "config.patch", "patch": {"dock": {"readouts": !on}}}));
        }
        (Some("dock"), Some(key @ ("orientation" | "density")), Some(value)) => {
            let mut dock = serde_json::Map::new();
            dock.insert(key.to_owned(), json!(value));
            send(app, json!({"type": "config.patch", "patch": {"dock": dock}}));
        }
        (Some("app"), Some("settings"), None) => settings(app),
        (Some("app"), Some("toggle"), None) => {
            let hidden = !app.state::<dock::Dock>().is_hidden();
            dock::set_hidden(app, hidden);
            if let Ok(slot) = app.state::<Tray>().0.lock() {
                if let Some(item) = slot.as_ref() {
                    let _ = item.set_text(if hidden { "Show dock" } else { "Hide dock" });
                }
            }
        }
        (Some("app"), Some("quit"), None) => app.exit(0),
        _ => eprintln!("ncd: unknown menu item {id}"),
    }
}

/// A widget's own item, as its last snapshot describes it.
fn widget_item(app: &AppHandle, widget: &str, id: &str) {
    let items = app.state::<core::Core>().menu_of(widget);
    let Some(item) = items.iter().find(|i| i.get("id").and_then(Value::as_str) == Some(id)) else { return };
    if let Some(url) = item.get("url").and_then(Value::as_str) {
        match web_page(url) {
            Some(url) => {
                if let Err(error) = app.opener().open_url(url.as_str(), None::<&str>) {
                    toast::push(app, widget.to_owned(), "crit".into(), format!("Could not open {url}: {error}"));
                }
            }
            None => eprintln!("ncd: {widget} offered {url:?}, which is not a web page; not opened"),
        }
    } else if let Some(text) = item.get("copy").and_then(Value::as_str) {
        match app.clipboard().write_text(text) {
            Ok(()) => toast::push(app, widget.to_owned(), "ok".into(), format!("Copied  {text}")),
            Err(error) => toast::push(app, widget.to_owned(), "crit".into(), format!("Could not copy: {error}")),
        }
    } else {
        let id_ = app.state::<core::Core>().action_id();
        send(app, json!({"type": "action", "id": id_, "widget": widget, "action": id}));
    }
}

/// Only web pages: a snapshot never gets to launch a program or open a file.
/// Parsed, not prefix-matched: http or https exactly, a host, no user or
/// password, nothing hidden in control characters or spaces.
fn web_page(raw: &str) -> Option<url::Url> {
    if raw.chars().any(|c| c.is_control() || c.is_whitespace()) {
        return None;
    }
    let url = url::Url::parse(raw).ok()?;
    let web = matches!(url.scheme(), "http" | "https");
    (web && url.host_str().is_some_and(|h| !h.is_empty()) && url.username().is_empty() && url.password().is_none()).then_some(url)
}

fn send(app: &AppHandle, message: Value) {
    if let Err(error) = core::send(app, &message) {
        eprintln!("ncd: {error}");
    }
}

/// Opens Settings — made on demand and destroyed on close, so its webview
/// costs nothing the rest of the time. The window itself arrives in M6.
pub fn settings(app: &AppHandle) {
    if let Some(window) = app.get_webview_window("settings") {
        let _ = window.set_focus();
        return;
    }
    let built = WebviewWindowBuilder::new(app, "settings", WebviewUrl::App("settings".into()))
        .title("NoClickDock Settings")
        .inner_size(560.0, 680.0)
        .resizable(true)
        .center()
        .build();
    if let Err(error) = built {
        eprintln!("ncd: could not open Settings: {error}");
    }
}

#[cfg(test)]
mod tests {
    use super::web_page;

    #[test]
    fn only_plain_web_pages_open() {
        for ok in ["https://localhost:9443", "http://127.0.0.1:8188/", "https://example.org/a?b=c#d"] {
            assert!(web_page(ok).is_some(), "{ok}");
        }
        for bad in [
            "file:///etc/passwd",
            "javascript:alert(1)",
            "https://user:pass@example.org/",
            "https://user@example.org/",
            "https://exa mple.org/",
            "https://example.org/\n",
            "https://example.org/\u{7}",
            "ftp://example.org/",
            "smb://server/share",
            "https://",
            "",
        ] {
            assert!(web_page(bad).is_none(), "{bad:?}");
        }
    }
}
