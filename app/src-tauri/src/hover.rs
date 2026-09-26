//! The hover choreography (DESIGN.md), in one place fed by both windows: the
//! rail and the panel are separate X windows, so leaving a dial for its panel
//! is a leave on one page and an enter on the other.
//!
//! - pointer on a dial → the panel opens after a 120 ms dwell;
//! - while a panel is open, another dial switches it at once;
//! - off the dial and the panel → it closes after 280 ms, unless the pointer
//!   comes back to either first.
use std::sync::Mutex;
use std::time::Duration;

use serde::Serialize;
use tauri::{AppHandle, Emitter, Manager, PhysicalPosition};

use crate::dock::{self, Area, Dock};
use crate::overlay;

const DWELL: Duration = Duration::from_millis(120);
const GRACE: Duration = Duration::from_millis(280);
/// The panel sits 8 CSS px from the rail; each window keeps 1px of margin.
const GAP: f64 = 8.0 - 2.0;
/// The panel keeps this far from the work area's edges.
const EDGE: f64 = 8.0;
/// The panel is at most the work area's height less this (DESIGN.md).
const HEADROOM: f64 = 32.0;

#[derive(Clone, Copy, Default, PartialEq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Side {
    Left,
    #[default]
    Right,
    Top,
    Bottom,
}

#[derive(Default)]
pub struct Hover(Mutex<State>);

#[derive(Default)]
struct State {
    phase: Phase,
    over_dial: Option<String>,
    in_panel: bool,
    /// Bumped by every event, so a timer that fires late knows it is stale.
    generation: u64,
    /// Bumped by every panel-open, so a stale measurement is ignored.
    seq: u64,
    widget: Option<String>,
    side: Side,
    visible: bool,
    size: Option<(f64, f64)>,
}

#[derive(Default, Clone, PartialEq)]
enum Phase {
    #[default]
    Closed,
    Dwell(String),
    Open(String),
    Grace(String),
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct PanelOpen {
    widget: String,
    seq: u64,
    side: Side,
    max_height: f64,
    reveal: bool,
}

#[derive(Clone, Serialize)]
struct PanelState {
    widget: Option<String>,
    side: Side,
}

/// The pointer entered a dial (`Some`) or left the dials (`None`).
#[tauri::command]
pub fn hover_dial(app: AppHandle, widget: Option<String>) {
    let hover = app.state::<Hover>();
    let Ok(mut st) = hover.0.lock() else { return };
    st.over_dial = widget.clone();
    st.generation += 1;
    let generation = st.generation;
    match (widget, st.phase.clone()) {
        (Some(w), Phase::Closed | Phase::Dwell(_)) => {
            st.phase = Phase::Dwell(w);
            after(&app, DWELL, generation, dwell_done);
        }
        (Some(w), Phase::Open(current) | Phase::Grace(current)) => {
            st.phase = Phase::Open(w.clone());
            if current != w {
                open(&app, &mut st, w);
            }
        }
        (None, Phase::Dwell(_)) => st.phase = Phase::Closed,
        (None, Phase::Open(w)) if !st.in_panel => {
            st.phase = Phase::Grace(w);
            after(&app, GRACE, generation, grace_done);
        }
        _ => {}
    }
}

/// The pointer entered or left the panel window.
#[tauri::command]
pub fn hover_panel(app: AppHandle, inside: bool) {
    let hover = app.state::<Hover>();
    let Ok(mut st) = hover.0.lock() else { return };
    st.in_panel = inside;
    st.generation += 1;
    let generation = st.generation;
    match st.phase.clone() {
        Phase::Grace(w) if inside => st.phase = Phase::Open(w),
        Phase::Open(w) if !inside && st.over_dial.is_none() => {
            st.phase = Phase::Grace(w);
            after(&app, GRACE, generation, grace_done);
        }
        _ => {}
    }
}

/// The panel page measured itself for the widget it was asked to draw.
#[tauri::command]
pub fn panel_layout(app: AppHandle, seq: u64, width: f64, height: f64) {
    let hover = app.state::<Hover>();
    let Ok(mut st) = hover.0.lock() else { return };
    if seq != st.seq || st.widget.is_none() {
        return; // a measurement for a panel that is no longer asked for
    }
    st.size = Some((width, height));
    place(&app, &mut st);
}

/// Closes the panel at once: the rail is being dragged.
pub fn close(app: &AppHandle) {
    let hover = app.state::<Hover>();
    let Ok(mut st) = hover.0.lock() else { return };
    st.generation += 1;
    if st.phase != Phase::Closed {
        st.phase = Phase::Closed;
        hide(app, &mut st);
    }
}

/// The rail changed size or dials moved: an open panel follows its dial.
pub fn replace(app: &AppHandle) {
    let hover = app.state::<Hover>();
    let Ok(mut st) = hover.0.lock() else { return };
    if st.visible {
        place(app, &mut st);
    }
}

fn dwell_done(app: &AppHandle, st: &mut State) {
    if let Phase::Dwell(w) = st.phase.clone() {
        st.phase = Phase::Open(w.clone());
        open(app, st, w);
    }
}

fn grace_done(app: &AppHandle, st: &mut State) {
    if matches!(st.phase, Phase::Grace(_)) {
        st.phase = Phase::Closed;
        hide(app, st);
    }
}

/// Runs `then` on the main thread after `delay`, unless anything happened
/// in between.
fn after(app: &AppHandle, delay: Duration, generation: u64, then: fn(&AppHandle, &mut State)) {
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(delay).await;
        let main = app.clone();
        let _ = app.run_on_main_thread(move || {
            let hover = main.state::<Hover>();
            let Ok(mut st) = hover.0.lock() else { return };
            if st.generation == generation {
                then(&main, &mut st);
            }
        });
    });
}

/// Asks the panel page to draw a widget. The window is placed and revealed
/// when the page answers with its size (`panel_layout`).
fn open(app: &AppHandle, st: &mut State, widget: String) {
    let Some(g) = Geometry::of(app, &widget) else { return };
    st.seq += 1;
    st.widget = Some(widget.clone());
    st.side = g.side();
    st.size = None;
    let max_height = (f64::from(g.area.height) - HEADROOM * g.scale) / g.scale;
    let _ = app.emit_to(
        "panel",
        "panel-open",
        PanelOpen { widget, seq: st.seq, side: st.side, max_height, reveal: !st.visible },
    );
}

fn place(app: &AppHandle, st: &mut State) {
    let (Some(widget), Some((width, height))) = (st.widget.clone(), st.size) else { return };
    let Some(panel) = app.get_webview_window("panel") else { return };
    let Some(g) = Geometry::of(app, &widget) else { return };
    st.side = g.side();
    let size = ((width * g.scale).ceil() as i32, (height * g.scale).ceil() as i32);
    let (x, y) = g.place(st.side, size);
    if overlay::fit(&panel, size.0, size.1).is_err() {
        return;
    }
    let position = PhysicalPosition::new(x, y);
    let shown = if st.visible { panel.set_position(position) } else { overlay::reveal(&panel, position) };
    if shown.is_ok() {
        st.visible = true;
        let _ = app.emit_to("rail", "panel-state", PanelState { widget: Some(widget), side: st.side });
    }
}

fn hide(app: &AppHandle, st: &mut State) {
    if let Some(panel) = app.get_webview_window("panel") {
        let _ = overlay::conceal(&panel);
    }
    st.visible = false;
    st.widget = None;
    st.size = None;
    // A window hidden under the pointer may never hear it leave.
    st.in_panel = false;
    let _ = app.emit_to("panel", "panel-close", ());
    let _ = app.emit_to("rail", "panel-state", PanelState { widget: None, side: st.side });
}

/// Where the rail and one of its dials are, in physical px.
struct Geometry {
    rail: (i32, i32, i32, i32),
    /// The dial's centre.
    dial: (i32, i32),
    vertical: bool,
    area: Area,
    scale: f64,
}

impl Geometry {
    fn of(app: &AppHandle, widget: &str) -> Option<Self> {
        let rail = app.get_webview_window("rail")?;
        let layout = app.state::<Dock>().layout()?;
        let dial = layout.dials.iter().find(|d| d.widget == widget)?;
        let scale = overlay::scale(&rail);
        let at = rail.outer_position().ok()?;
        let size = rail.outer_size().ok()?;
        let (w, h) = (size.width as i32, size.height as i32);
        let area = dock::work_area(app, at.x + w / 2, at.y + h / 2)?;
        Some(Self {
            rail: (at.x, at.y, w, h),
            dial: (
                at.x + ((dial.x + dial.width / 2.0) * scale).round() as i32,
                at.y + ((dial.y + dial.height / 2.0) * scale).round() as i32,
            ),
            vertical: layout.height >= layout.width,
            area,
            scale,
        })
    }

    /// Perpendicular to the rail, on the side with more room.
    fn side(&self) -> Side {
        let (x, y, w, h) = self.rail;
        if self.vertical {
            if self.area.right() - (x + w) >= x - self.area.x { Side::Right } else { Side::Left }
        } else if self.area.bottom() - (y + h) >= y - self.area.y {
            Side::Bottom
        } else {
            Side::Top
        }
    }

    /// Beside the rail, centred on the dial, clamped along the rail to the
    /// work area — never across the rail, so it never covers another dial.
    fn place(&self, side: Side, (pw, ph): (i32, i32)) -> (i32, i32) {
        let (x, y, w, h) = self.rail;
        let gap = (GAP * self.scale).round() as i32;
        let edge = (EDGE * self.scale).round() as i32;
        let along = |centre: i32, length: i32, start: i32, end: i32| {
            (centre - length / 2).clamp(start + edge, (end - edge - length).max(start + edge))
        };
        match side {
            Side::Right => (x + w + gap, along(self.dial.1, ph, self.area.y, self.area.bottom())),
            Side::Left => (x - gap - pw, along(self.dial.1, ph, self.area.y, self.area.bottom())),
            Side::Bottom => (along(self.dial.0, pw, self.area.x, self.area.right()), y + h + gap),
            Side::Top => (along(self.dial.0, pw, self.area.x, self.area.right()), y - gap - ph),
        }
    }
}
