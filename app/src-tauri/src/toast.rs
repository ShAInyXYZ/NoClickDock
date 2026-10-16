//! Toasts (DESIGN.md "Toasts"): a transition worth knowing while you can
//! still act on it, shown beside the dial that raised it, on the panel side.
//!
//! - one stack at a time, from one widget, newest nearest the dial, at most 3;
//! - each for 5 s; when the last one's time is up, the shell fades the
//!   window's compositor opacity to 0 over 160 ms and parks it (WebKit stops
//!   painting a click-through window, so the fade cannot live in the page:
//!   design M2 r2b); reduced motion dismisses at once. A toast whose time is
//!   up while newer ones stay simply leaves the stack;
//! - while any panel is open they wait, and a toast for the widget whose panel
//!   is open is dropped — the panel already says it;
//! - while the dock is hidden they wait too: no toast floats beside no rail;
//! - waiting is bounded: the newest three per widget, none older than 30 s
//!   when its turn comes; and a widget whose toasts keep coming yields the
//!   stack to one that is waiting, so a crit elsewhere is never starved;
//! - the pointer passes through them on Linux; on Windows, where click-through
//!   is unreliable, pointing at them dismisses them.
use std::collections::VecDeque;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde::Serialize;
use tauri::{AppHandle, Emitter, Manager, PhysicalPosition};

use crate::hover::{Geometry, Side};
use crate::{main_thread, overlay};

const VISIBLE: Duration = Duration::from_millis(5000);
#[cfg(target_os = "linux")]
const FADE: Duration = Duration::from_millis(160);
#[cfg(target_os = "linux")]
const FADE_STEPS: u32 = 10;
const MAX: usize = 3;
const TICK: Duration = Duration::from_millis(100);
/// A waiting toast older than this is no longer news.
const STALE: Duration = Duration::from_secs(30);

#[derive(Default)]
pub struct Toasts(Mutex<State>);

#[derive(Default)]
struct State {
    pending: VecDeque<Item>,
    /// The visible stack, newest first, all from `widget`.
    shown: Vec<Shown>,
    widget: Option<String>,
    /// The widget whose panel is open: toasts wait meanwhile.
    held: Option<String>,
    /// The dock is hidden from the tray: toasts wait meanwhile.
    dock_hidden: bool,
    next_key: u64,
    seq: u64,
    visible: bool,
    ticking: bool,
    /// The window is fading out; bumped when anything else hides it first.
    fading: bool,
    fade: u64,
    /// Linux only: the toast window lets the pointer through once shown.
    #[cfg(target_os = "linux")]
    click_through: bool,
}

#[derive(Clone)]
struct Item {
    key: u64,
    widget: String,
    tone: String,
    text: String,
    at: Instant,
}

struct Shown {
    item: Item,
    until: Instant,
}

#[derive(Clone, Serialize)]
struct Stack {
    seq: u64,
    side: Side,
    items: Vec<StackItem>,
}

#[derive(Clone, Serialize)]
struct StackItem {
    key: u64,
    tone: String,
    text: String,
}

/// A toast from the core or the shell itself, from any thread: placing it
/// reads the rail's monitor, which only the main thread may do.
pub fn push(app: &AppHandle, widget: String, tone: String, text: String) {
    if text.is_empty() {
        return;
    }
    main_thread::run(app, move |app| push_here(app, widget, tone, text));
}

fn push_here(app: &AppHandle, widget: String, tone: String, text: String) {
    main_thread::assert();
    crate::watchdog::note(format!("toast push {widget}"));
    let toasts = app.state::<Toasts>();
    let Ok(mut st) = toasts.0.lock() else { return };
    if st.held.as_deref() == Some(widget.as_str()) {
        return; // its panel is open and already says it
    }
    st.next_key += 1;
    let key = st.next_key;
    // Keep the newest three per widget waiting: older ones would only be
    // pushed out of the stack by these anyway.
    let waiting = st.pending.iter().filter(|item| item.widget == widget).count();
    if waiting >= MAX {
        if let Some(i) = st.pending.iter().position(|item| item.widget == widget) {
            st.pending.remove(i);
        }
    }
    st.pending.push_back(Item { key, widget, tone, text, at: Instant::now() });
    pump(app, &mut st);
}

/// The dock was hidden from the tray (toasts wait) or shown again.
pub fn set_dock_hidden(app: &AppHandle, hidden: bool) {
    main_thread::assert();
    let toasts = app.state::<Toasts>();
    let Ok(mut st) = toasts.0.lock() else { return };
    st.dock_hidden = hidden;
    if hidden {
        requeue(&mut st);
        hide(app, &mut st);
    } else {
        pump(app, &mut st);
    }
}

/// What was showing goes back to wait, in front, with a fresh five seconds.
fn requeue(st: &mut State) {
    let shown: Vec<Item> = st.shown.drain(..).rev().map(|s| s.item).collect();
    for item in shown {
        st.pending.push_front(item);
    }
    st.widget = None;
}

/// The toast page was reloaded (a web process crash): what it showed is
/// gone with it; waiting toasts come when the next one arrives.
pub fn reset(app: &AppHandle) {
    main_thread::assert();
    let toasts = app.state::<Toasts>();
    let Ok(mut st) = toasts.0.lock() else { return };
    st.shown.clear();
    st.widget = None;
    hide(app, &mut st);
}

/// A panel opened: its widget's toasts are dropped, the rest wait.
pub fn hold(app: &AppHandle, widget: &str) {
    main_thread::assert();
    crate::watchdog::note(format!("toast hold {widget}"));
    let toasts = app.state::<Toasts>();
    let Ok(mut st) = toasts.0.lock() else { return };
    st.held = Some(widget.to_owned());
    requeue(&mut st);
    st.pending.retain(|item| item.widget != widget);
    hide(app, &mut st);
}

/// The panel closed: waiting toasts come out.
pub fn release(app: &AppHandle) {
    main_thread::assert();
    crate::watchdog::note("toast release");
    let toasts = app.state::<Toasts>();
    let Ok(mut st) = toasts.0.lock() else { return };
    st.held = None;
    pump(app, &mut st);
}

/// Windows: pointing at a toast dismisses the stack.
#[tauri::command]
pub fn toast_dismiss(app: AppHandle) {
    main_thread::assert();
    let toasts = app.state::<Toasts>();
    let Ok(mut st) = toasts.0.lock() else { return };
    st.shown.clear();
    st.widget = None;
    hide(&app, &mut st);
    pump(&app, &mut st);
}

/// The toast page measured the stack it was sent (CSS px); `anchor` is the
/// distance from its top to the middle of the newest toast.
#[tauri::command]
pub fn toast_layout(app: AppHandle, seq: u64, width: f64, height: f64, anchor: f64) {
    main_thread::assert();
    let toasts = app.state::<Toasts>();
    let Ok(mut st) = toasts.0.lock() else { return };
    if seq != st.seq || st.shown.is_empty() {
        return;
    }
    let (Some(widget), Some(window)) = (st.widget.clone(), app.get_webview_window("toast")) else { return };
    let Some(g) = Geometry::of(&app, &widget) else { return };
    let size = ((width * g.scale).ceil() as i32, (height * g.scale).ceil() as i32);
    let (x, y) = g.beside(g.side(), size, (anchor * g.scale).round() as i32);
    if overlay::fit(&window, size.0, size.1).is_err() {
        return;
    }
    let position = PhysicalPosition::new(x, y);
    let shown = if st.visible { window.set_position(position) } else { overlay::reveal(&window, position) };
    if shown.is_ok() {
        st.visible = true;
        // Only once shown: before, there is no X window to shape.
        #[cfg(target_os = "linux")]
        if !st.click_through {
            st.click_through = overlay::pass_through(&window, true).is_ok();
        }
    }
}

/// Moves waiting toasts into the stack when nothing holds them.
fn pump(app: &AppHandle, st: &mut State) {
    if st.held.is_some() || st.dock_hidden || st.fading {
        return; // a fade ends by pumping
    }
    st.pending.retain(|item| item.at.elapsed() < STALE);
    if st.widget.is_none() {
        st.widget = st.pending.front().map(|item| item.widget.clone());
    }
    let Some(widget) = st.widget.clone() else { return };
    // Another widget is waiting: this one's new toasts queue behind it
    // instead of keeping the stack for good.
    if !st.shown.is_empty() && st.pending.iter().any(|item| item.widget != widget) {
        return;
    }
    let mut moved = false;
    while let Some(i) = st.pending.iter().position(|item| item.widget == widget) {
        let item = st.pending.remove(i).expect("position is in range");
        st.shown.insert(0, Shown { item, until: Instant::now() + VISIBLE });
        moved = true;
    }
    // Newest nearest the dial; beyond three, the oldest simply goes.
    st.shown.truncate(MAX);
    if moved {
        render(app, st);
        tick_while_shown(app, st);
    }
}

/// Takes out what has been up five seconds; when that is every toast, the
/// window fades out, then the next widget's toasts come.
fn tick(app: &AppHandle, st: &mut State) -> bool {
    if st.fading {
        return true;
    }
    let now = Instant::now();
    if st.shown.is_empty() {
        st.widget = None;
        hide(app, st);
        pump(app, st);
        return !st.shown.is_empty();
    }
    if st.shown.iter().all(|s| now >= s.until) {
        fade_out(app, st);
        return st.fading;
    }
    let before = st.shown.len();
    st.shown.retain(|s| now < s.until);
    if st.shown.len() != before {
        render(app, st);
    }
    true
}

/// Fades the toast window out at the compositor, then parks it and moves
/// on. Instant with reduced motion, without a compositor, or off Linux.
fn fade_out(app: &AppHandle, st: &mut State) {
    #[cfg(target_os = "linux")]
    if st.visible && overlay::composited() && animations() {
        if let Some(id) = app.get_webview_window("toast").as_ref().and_then(crate::x11::xid) {
            st.fading = true;
            st.fade += 1;
            let (fade, app) = (st.fade, app.clone());
            let step = FADE / FADE_STEPS;
            let mut i = 0;
            gtk::glib::timeout_add_local(step, move || {
                i += 1;
                let toasts = app.state::<Toasts>();
                let Ok(mut st) = toasts.0.lock() else { return gtk::glib::ControlFlow::Break };
                if !st.fading || st.fade != fade {
                    return gtk::glib::ControlFlow::Break; // something else hid it
                }
                if i < FADE_STEPS {
                    let left = f64::from(FADE_STEPS - i) / f64::from(FADE_STEPS);
                    crate::x11::set_opacity(&[(id, Some((f64::from(u32::MAX) * left) as u32))]);
                    return gtk::glib::ControlFlow::Continue;
                }
                st.fading = false;
                finish(&app, &mut st);
                gtk::glib::ControlFlow::Break
            });
            return;
        }
    }
    finish(app, st);
}

fn finish(app: &AppHandle, st: &mut State) {
    st.shown.clear();
    st.widget = None;
    hide(app, st);
    pump(app, st);
}

/// GTK's "enable animations", which WebKit also reads for
/// prefers-reduced-motion.
#[cfg(target_os = "linux")]
fn animations() -> bool {
    use gtk::prelude::GtkSettingsExt;
    gtk::Settings::default().map(|s| s.is_gtk_enable_animations()).unwrap_or(true)
}

fn tick_while_shown(app: &AppHandle, st: &mut State) {
    if st.ticking {
        return;
    }
    st.ticking = true;
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        loop {
            tokio::time::sleep(TICK).await;
            let (tx, rx) = tokio::sync::oneshot::channel();
            let main = app.clone();
            let queued = app.run_on_main_thread(move || {
                let toasts = main.state::<Toasts>();
                let busy = toasts.0.lock().map(|mut st| {
                    let busy = tick(&main, &mut st);
                    st.ticking = busy;
                    busy
                });
                let _ = tx.send(busy.unwrap_or(false));
            });
            if queued.is_err() || !rx.await.unwrap_or(false) {
                break;
            }
        }
    });
}

/// Sends the stack to the toast page, which measures it and answers with
/// `toast_layout`.
fn render(app: &AppHandle, st: &mut State) {
    let Some(widget) = st.widget.clone() else { return };
    let side = Geometry::of(app, &widget).map(|g| g.side()).unwrap_or_default();
    st.seq += 1;
    let stack = Stack {
        seq: st.seq,
        side,
        items: st
            .shown
            .iter()
            .map(|s| StackItem { key: s.item.key, tone: s.item.tone.clone(), text: s.item.text.clone() })
            .collect(),
    };
    let _ = app.emit_to("toast", "toasts", stack);
}

fn hide(app: &AppHandle, st: &mut State) {
    st.fading = false;
    st.fade += 1;
    if st.visible {
        if let Some(window) = app.get_webview_window("toast") {
            let _ = overlay::conceal(&window);
        }
        st.visible = false;
    }
    st.seq += 1;
    let _ = app.emit_to("toast", "toasts", Stack { seq: st.seq, side: Side::default(), items: Vec::new() });
}
