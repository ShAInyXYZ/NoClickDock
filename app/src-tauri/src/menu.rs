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
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::TrayIconBuilder;
use tauri::{AppHandle, Manager, WebviewUrl, WebviewWindow, WebviewWindowBuilder, Wry};
use tauri_plugin_clipboard_manager::ClipboardExt;
use tauri_plugin_opener::OpenerExt;

use crate::{core, dock, hover, toast};

/// The tray's show/hide item, whose label follows the dock.
#[derive(Default)]
pub struct Tray(Mutex<Option<MenuItem<Wry>>>);

/// One entry of a context menu, whatever draws it.
enum Entry {
    Item(String, String),        // id, label
    Check(String, String, bool), // id, label, on
    Radio(String, String, bool), // one of a set: id, label, chosen
    Separator,
    Sub(String, Vec<Entry>),
}

fn item(id: impl Into<String>, label: impl Into<String>) -> Entry {
    Entry::Item(id.into(), label.into())
}

/// Right-click on a dial: refresh it, its own items, then the app's.
#[tauri::command]
pub fn dial_menu(window: WebviewWindow, widget: String) -> Result<(), String> {
    let app = window.app_handle();
    hover::close_for_menu(app, Some(widget.clone()));
    let mut entries = vec![item(format!("refresh:{widget}"), "Refresh now")];
    for it in app.state::<core::Core>().menu_of(&widget) {
        let (Some(id), Some(label)) = (it.get("id").and_then(Value::as_str), it.get("label").and_then(Value::as_str)) else {
            continue;
        };
        entries.push(item(format!("item:{widget}:{id}"), label));
    }
    entries.push(Entry::Separator);
    entries.extend(app_entries());
    popup(&window, entries)
}

/// Right-click on the rail itself: how the rail looks, then the app's items.
#[tauri::command]
pub fn rail_menu(window: WebviewWindow) -> Result<(), String> {
    let app = window.app_handle();
    hover::close_for_menu(app, None);
    let dock = app.state::<core::Core>().dock_config().unwrap_or(Value::Null);
    let is = |key: &str, value: &str| dock.get(key).and_then(Value::as_str) == Some(value);
    let check = |id: &str, label: &str, on: bool| Entry::Check(id.into(), label.into(), on);
    let radio = |id: &str, label: &str, on: bool| Entry::Radio(id.into(), label.into(), on);
    let readouts = dock.get("readouts").and_then(Value::as_bool).unwrap_or(true);
    let mut entries = vec![
        Entry::Sub(
            "Orientation".into(),
            vec![
                radio("dock:orientation:vertical", "Vertical", is("orientation", "vertical")),
                radio("dock:orientation:horizontal", "Horizontal", is("orientation", "horizontal")),
            ],
        ),
        Entry::Sub(
            "Density".into(),
            vec![
                radio("dock:density:compact", "Compact", is("density", "compact")),
                radio("dock:density:normal", "Normal", is("density", "normal")),
                radio("dock:density:large", "Large", is("density", "large")),
            ],
        ),
        check("dock:readouts:toggle", "Readouts", readouts),
        Entry::Separator,
    ];
    entries.extend(app_entries());
    popup(&window, entries)
}

fn app_entries() -> Vec<Entry> {
    vec![item("app:settings", "Settings…"), item("app:quit", "Quit NoClickDock")]
}

/// Linux: a GTK menu of our own, popped up without blocking. muda's
/// context menu spins a nested main loop until the menu reports done — and
/// GTK reports nothing when it cannot grab the keyboard (another client
/// holds it: a shell popover, the overview, a password prompt, a VM), so
/// the loop spun forever inside this IPC call and starved every timer,
/// toast and message to the main thread (the ~27-minute freeze; research).
/// Here the call returns at once; a choice arrives on the item's `activate`.
#[cfg(target_os = "linux")]
fn popup(window: &WebviewWindow, entries: Vec<Entry>) -> Result<(), String> {
    use gtk::prelude::*;
    thread_local! {
        // The menu showing (if any): kept alive until the next one.
        static SHOWING: std::cell::RefCell<Option<gtk::Menu>> = const { std::cell::RefCell::new(None) };
    }
    crate::main_thread::assert();
    fn build(app: &AppHandle, entries: Vec<Entry>) -> gtk::Menu {
        let menu = gtk::Menu::new();
        for entry in entries {
            let radio = matches!(entry, Entry::Radio(..));
            let widget: gtk::MenuItem = match entry {
                Entry::Item(id, label) => {
                    let it = gtk::MenuItem::with_label(&label);
                    let app = app.clone();
                    it.connect_activate(move |_| chosen(&app, &id));
                    it
                }
                Entry::Check(id, label, on) | Entry::Radio(id, label, on) => {
                    let it = gtk::CheckMenuItem::with_label(&label);
                    it.set_draw_as_radio(radio);
                    it.set_active(on);
                    let app = app.clone();
                    it.connect_activate(move |_| chosen(&app, &id));
                    it.upcast()
                }
                Entry::Separator => gtk::SeparatorMenuItem::new().upcast(),
                Entry::Sub(label, children) => {
                    let it = gtk::MenuItem::with_label(&label);
                    it.set_submenu(Some(&build(app, children)));
                    it
                }
            };
            menu.append(&widget);
        }
        menu
    }
    let app = window.app_handle();
    let menu = build(app, entries);
    menu.show_all();
    // At the pointer, over the window that asked. There is no GDK event
    // here (the request came through IPC), so the rectangle is the pointer
    // itself and GTK grabs with the seat's pointer.
    let gtk_window = window.gtk_window().map_err(|e| e.to_string())?;
    menu.set_attach_widget(Some(&gtk_window));
    let gdk_window = gtk_window.window().ok_or("the window is not realised")?;
    let pointer = gdk_window.display().default_seat().and_then(|seat| seat.pointer());
    let (x, y) = pointer.map(|p| {
        let (_, x, y, _) = gdk_window.device_position(&p);
        (x, y)
    })
    .unwrap_or((0, 0));
    menu.popup_at_rect(
        &gdk_window,
        &gtk::gdk::Rectangle::new(x, y, 1, 1),
        gtk::gdk::Gravity::SouthEast,
        gtk::gdk::Gravity::NorthWest,
        None,
    );
    if !menu.is_visible() {
        // Could not grab: another client holds the keyboard. Say so, never wait.
        eprintln!("ncd: the menu could not open (another application holds the keyboard)");
        crate::watchdog::note("menu: no grab");
    }
    SHOWING.with(|showing| {
        if let Some(old) = showing.borrow_mut().replace(menu) {
            old.detach();
        }
    });
    Ok(())
}

/// Elsewhere: the system menu (TrackPopupMenu on Windows returns when the
/// menu closes, or at once when it cannot open).
#[cfg(not(target_os = "linux"))]
fn popup(window: &WebviewWindow, entries: Vec<Entry>) -> Result<(), String> {
    use tauri::menu::{CheckMenuItem, IsMenuItem, Submenu};
    fn build(app: &AppHandle, entries: Vec<Entry>) -> tauri::Result<Vec<Box<dyn IsMenuItem<Wry>>>> {
        let mut out: Vec<Box<dyn IsMenuItem<Wry>>> = Vec::new();
        for entry in entries {
            out.push(match entry {
                Entry::Item(id, label) => Box::new(MenuItem::with_id(app, id, label, true, None::<&str>)?),
                Entry::Check(id, label, on) | Entry::Radio(id, label, on) => {
                    Box::new(CheckMenuItem::with_id(app, id, label, true, on, None::<&str>)?)
                }
                Entry::Separator => Box::new(PredefinedMenuItem::separator(app)?),
                Entry::Sub(label, children) => {
                    let items = build(app, children)?;
                    let refs: Vec<&dyn IsMenuItem<Wry>> = items.iter().map(|i| i.as_ref()).collect();
                    Box::new(Submenu::with_items(app, label, true, &refs)?)
                }
            });
        }
        Ok(out)
    }
    let items = build(window.app_handle(), entries).map_err(|e| e.to_string())?;
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
    // The mark as a light monochrome glyph on transparent, like the symbolic
    // icons beside it on a dark top bar (design M2 r1b: the app icon was a
    // dark square there). The dark variant is for light taskbars (M7).
    const PNG: &[u8] = include_bytes!("../icons/tray-light-44.png");
    let icon = tauri::image::Image::from_bytes(PNG)?;
    // On Linux the icon is written to a file named after the tray's id, and
    // GNOME's AppIndicator host caches the texture by that path until the
    // next login (design M2 r2b: the old dark icon stayed). An id that
    // changes with the picture gives every new icon a new path.
    let id = format!("ncd-{:08x}", fnv1a(PNG) as u32);
    let builder = TrayIconBuilder::with_id(id).tooltip("NoClickDock").menu(&menu).show_menu_on_left_click(true).icon(icon);
    builder.build(app)?;
    if let Ok(mut slot) = app.state::<Tray>().0.lock() {
        *slot = Some(toggle);
    }
    Ok(())
}

/// The tray's show/hide item follows the dock, whoever hid it.
pub fn sync_tray(app: &AppHandle, hidden: bool) {
    if let Ok(slot) = app.state::<Tray>().0.lock() {
        if let Some(item) = slot.as_ref() {
            let _ = item.set_text(if hidden { "Show dock" } else { "Hide dock" });
        }
    }
}

/// FNV-1a: a stable fingerprint of the tray picture (not for security).
fn fnv1a(bytes: &[u8]) -> u64 {
    bytes.iter().fold(0xcbf2_9ce4_8422_2325, |hash, &b| (hash ^ u64::from(b)).wrapping_mul(0x0100_0000_01b3))
}

/// Every menu choice, from any menu.
pub fn chosen(app: &AppHandle, id: &str) {
    crate::watchdog::note(format!("menu {id}"));
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
    use super::{fnv1a, web_page};

    #[test]
    fn a_new_tray_picture_gets_a_new_file_name() {
        assert_eq!(fnv1a(b""), 0xcbf2_9ce4_8422_2325);
        assert_eq!(fnv1a(b"a"), 0xaf63_dc4c_8601_ec8c); // the published FNV-1a 64 vector
        assert_ne!(fnv1a(include_bytes!("../icons/tray-light-44.png")), fnv1a(include_bytes!("../icons/tray-dark-44.png")));
    }

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
