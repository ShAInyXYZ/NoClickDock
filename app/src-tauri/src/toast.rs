//! Toasts (DESIGN.md "Toasts"): a transition worth knowing while you can
//! still act on it, shown beside the dial that raised it, on the panel side.
//!
//! - one stack at a time, from one widget, newest nearest the dial, at most 3;
//! - each for 5 s, then a 160 ms fade;
//! - while any panel is open they wait, and a toast for the widget whose panel
//!   is open is dropped — the panel already says it;
//! - the pointer passes through them on Linux; on Windows, where click-through
//!   is unreliable, pointing at them dismisses them.
use std::collections::VecDeque;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde::Serialize;
use tauri::{AppHandle, Emitter, Manager, PhysicalPosition};

use crate::hover::{Geometry, Side};
use crate::overlay;

const VISIBLE: Duration = Duration::from_millis(5000);
const FADE: Duration = Duration::from_millis(160);
const MAX: usize = 3;
const TICK: Duration = Duration::from_millis(100);

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
    next_key: u64,
    seq: u64,
    visible: bool,
    ticking: bool,
    click_through: bool,
}

#[derive(Clone)]
struct Item {
    key: u64,
    widget: String,
    tone: String,
    text: String,
}

struct Shown {
    item: Item,
    until: Instant,
    leaving: bool,
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
    leaving: bool,
}

/// A toast from the core or the shell itself.
pub fn push(app: &AppHandle, widget: String, tone: String, text: String) {
    if text.is_empty() {
        return;
    }
    let toasts = app.state::<Toasts>();
    let Ok(mut st) = toasts.0.lock() else { return };
    if st.held.as_deref() == Some(widget.as_str()) {
        return; // its panel is open and already says it
    }
    st.next_key += 1;
    let key = st.next_key;
    st.pending.push_back(Item { key, widget, tone, text });
    pump(app, &mut st);
}

/// A panel opened: its widget's toasts are dropped, the rest wait.
pub fn hold(app: &AppHandle, widget: &str) {
    let toasts = app.state::<Toasts>();
    let Ok(mut st) = toasts.0.lock() else { return };
    st.held = Some(widget.to_owned());
    // What was showing goes back to wait, in front, with a fresh five seconds.
    let shown: Vec<Item> = st.shown.drain(..).rev().map(|s| s.item).collect();
    for item in shown {
        st.pending.push_front(item);
    }
    st.pending.retain(|item| item.widget != widget);
    st.widget = None;
    hide(app, &mut st);
}

/// The panel closed: waiting toasts come out.
pub fn release(app: &AppHandle) {
    let toasts = app.state::<Toasts>();
    let Ok(mut st) = toasts.0.lock() else { return };
    st.held = None;
    pump(app, &mut st);
}

/// Windows: pointing at a toast dismisses the stack.
#[tauri::command]
pub fn toast_dismiss(app: AppHandle) {
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
        // Only after the first show: before it, tao aborts the app (research).
        #[cfg(not(windows))]
        if !st.click_through {
            st.click_through = window.set_ignore_cursor_events(true).is_ok();
        }
    }
}

/// Moves waiting toasts into the stack when nothing holds them.
fn pump(app: &AppHandle, st: &mut State) {
    if st.held.is_some() {
        return;
    }
    if st.widget.is_none() {
        st.widget = st.pending.front().map(|item| item.widget.clone());
    }
    let Some(widget) = st.widget.clone() else { return };
    let mut moved = false;
    while let Some(i) = st.pending.iter().position(|item| item.widget == widget) {
        let item = st.pending.remove(i).expect("position is in range");
        st.shown.insert(0, Shown { item, until: Instant::now() + VISIBLE, leaving: false });
        moved = true;
    }
    // Newest nearest the dial; beyond three, the oldest simply goes.
    st.shown.truncate(MAX);
    if moved {
        render(app, st);
        tick_while_shown(app, st);
    }
}

/// Fades what has been up five seconds, removes what has faded, and when
/// the stack is empty moves on to the next widget's toasts.
fn tick(app: &AppHandle, st: &mut State) -> bool {
    let now = Instant::now();
    let mut changed = false;
    for s in st.shown.iter_mut().filter(|s| !s.leaving && now >= s.until) {
        s.leaving = true;
        changed = true;
    }
    let before = st.shown.len();
    st.shown.retain(|s| !(s.leaving && now >= s.until + FADE));
    changed |= st.shown.len() != before;
    if st.shown.is_empty() {
        st.widget = None;
        hide(app, st);
        pump(app, st);
    } else if changed {
        render(app, st);
    }
    !st.shown.is_empty()
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
            .map(|s| StackItem { key: s.item.key, tone: s.item.tone.clone(), text: s.item.text.clone(), leaving: s.leaving })
            .collect(),
    };
    let _ = app.emit_to("toast", "toasts", stack);
}

fn hide(app: &AppHandle, st: &mut State) {
    if st.visible {
        if let Some(window) = app.get_webview_window("toast") {
            let _ = overlay::conceal(&window);
        }
        st.visible = false;
    }
    st.seq += 1;
    let _ = app.emit_to("toast", "toasts", Stack { seq: st.seq, side: Side::default(), items: Vec::new() });
}
