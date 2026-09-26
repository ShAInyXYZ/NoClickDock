//! The hover choreography (DESIGN.md), in one place fed by both windows: the
//! rail and the panel are separate X windows, so leaving a dial for its panel
//! is a leave on one page and an enter on the other.
//!
//! - pointer on a dial → the panel opens after a 120 ms dwell;
//! - while a panel is open, another dial switches it at once;
//! - off the dial and the panel → it closes after 280 ms, unless the pointer
//!   comes back to either first.
//!
//! There are two panel windows. The widget asked for is drawn in the one not
//! showing: its page draws and measures it, the parked window is sized and
//! placed, the page says it has painted — and only then is it shown, with
//! the other parked in the same main-loop turn. A switch between widgets of
//! different sizes never resizes a visible window, so no frame shows an
//! empty panel or one widget in another's geometry (WebKit repaints a
//! resized window a frame or several late: design M1 r2). The first panel
//! is prepared the same way during the dwell, whose end only shows it.
//!
//! Once shown, a panel's header never moves under the pointer: a page switch
//! or a growing snapshot resizes it away from the header's edge.
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde::Serialize;
use tauri::{AppHandle, Emitter, Manager, PhysicalPosition};

use crate::dock::{self, Area, Dock};
use crate::{main_thread, overlay, toast};

const DWELL: Duration = Duration::from_millis(120);
const GRACE: Duration = Duration::from_millis(280);
/// The panel page fades out over this before its window is hidden.
const FADE: Duration = Duration::from_millis(100);
/// The two panel windows.
pub const PANELS: [&str; 2] = ["panel", "panel2"];

/// The panel sits 8 CSS px from the rail; each window keeps 1px of margin.
const GAP: f64 = 8.0 - 2.0;
/// The panel keeps this far from the work area's edges.
const EDGE: f64 = 8.0;
/// The panel is at most the work area's height less this (DESIGN.md).
const HEADROOM: f64 = 32.0;
/// A native menu's pointer grab sends the rail leave and enter events as it
/// pops; the panel it just closed must not open again under the menu.
const MENU_QUIET: Duration = Duration::from_millis(400);

/// `NCD_TRACE=1` prints hover timings (wall-clock ms) to stderr, for
/// measuring the path from pointer to panel against an outside clock.
fn trace(what: &str) {
    if std::env::var_os("NCD_TRACE").is_some() {
        let ms = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_secs_f64() * 1000.0)
            .unwrap_or_default();
        eprintln!("ncd-trace {ms:.1} {what}");
    }
}

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
    /// The window showing, or last shown (an index into PANELS), and the one
    /// the current widget is drawn in.
    front: usize,
    target: usize,
    /// The last panel whose page has painted it at its size.
    painted: u64,
    size: Option<(f64, f64)>,
    /// Where the panel was put for this widget: its header edge stays there.
    anchor: Option<(i32, i32)>,
    /// Each window's size and place as last set, to skip what did not change.
    fitted: [Option<(i32, i32)>; 2],
    at: [Option<(i32, i32)>; 2],
    /// Until then, pointer entries do not open a panel (a menu is popping).
    quiet_until: Option<Instant>,
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
    trace(&format!("hover_dial {widget:?}"));
    st.over_dial = widget.clone();
    st.generation += 1;
    let generation = st.generation;
    if widget.is_some() && st.quiet_until.is_some_and(|t| Instant::now() < t) {
        return;
    }
    match (widget, st.phase.clone()) {
        (Some(w), Phase::Closed | Phase::Dwell(_)) => {
            st.phase = Phase::Dwell(w.clone());
            // Draw and measure now; the dwell's end only has to show it.
            if st.widget.as_deref() != Some(w.as_str()) || st.size.is_none() {
                open(&app, &mut st, w);
            }
            after(&app, DWELL, generation, dwell_done);
        }
        (Some(w), Phase::Open(current) | Phase::Grace(current)) => {
            st.phase = Phase::Open(w.clone());
            if current != w {
                open(&app, &mut st, w.clone());
                toast::hold(&app, &w);
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
    trace(&format!("panel_layout {width}x{height}"));
    if st.phase != Phase::Closed {
        place(&app, &mut st);
    }
}

/// The page has painted the panel at the size `panel_layout` gave its
/// window. A panel drawn in the window not showing is shown now (after the
/// dwell, if one runs).
#[tauri::command]
pub fn panel_painted(app: AppHandle, seq: u64) {
    let hover = app.state::<Hover>();
    let Ok(mut st) = hover.0.lock() else { return };
    if seq != st.seq || st.size.is_none() {
        return;
    }
    st.painted = seq;
    trace("panel painted");
    if matches!(st.phase, Phase::Open(_) | Phase::Grace(_)) && !(st.visible && st.target == st.front) {
        show(&app, &mut st);
    }
}

/// Closes the panel at once: the rail is being dragged, or a menu opens
/// (then pointer entries stay quiet for a moment).
pub fn close(app: &AppHandle) {
    close_for(app, None);
}

pub fn close_for_menu(app: &AppHandle) {
    close_for(app, Some(Instant::now() + MENU_QUIET));
}

fn close_for(app: &AppHandle, quiet_until: Option<Instant>) {
    let hover = app.state::<Hover>();
    let Ok(mut st) = hover.0.lock() else { return };
    st.generation += 1;
    if quiet_until.is_some() {
        st.quiet_until = quiet_until;
    }
    if st.phase != Phase::Closed {
        st.phase = Phase::Closed;
        hide(app, &mut st);
    }
}

/// The rail changed size or dials moved: an open panel follows its dial.
pub fn replace(app: &AppHandle) {
    let hover = app.state::<Hover>();
    let Ok(mut st) = hover.0.lock() else { return };
    if st.visible && st.target == st.front {
        place(app, &mut st);
    }
}

fn dwell_done(app: &AppHandle, st: &mut State) {
    trace("dwell done");
    if let Phase::Dwell(w) = st.phase.clone() {
        st.phase = Phase::Open(w.clone());
        if st.widget.as_deref() != Some(w.as_str()) {
            open(app, st, w);
        } else if st.painted == st.seq {
            show(app, st); // drawn during the dwell: show it now
        }
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
        main_thread::run(&app, move |app| {
            let hover = app.state::<Hover>();
            let Ok(mut st) = hover.0.lock() else { return };
            if st.generation == generation {
                then(app, &mut st);
            }
        });
    });
}

/// Asks the panel page to draw a widget. It answers with its size
/// (`panel_layout`); the window is then staged (during a dwell) or placed.
fn open(app: &AppHandle, st: &mut State, widget: String) {
    let Some(g) = Geometry::of(app, &widget) else { return };
    st.seq += 1;
    st.widget = Some(widget.clone());
    st.side = g.side();
    st.size = None;
    st.anchor = None;
    // Drawn where it cannot be seen: never in the window showing, or still
    // fading out.
    st.target = 1 - st.front;
    let max_height = (f64::from(g.area.height) - HEADROOM * g.scale) / g.scale;
    let _ = app.emit_to(PANELS[st.target], "panel-open", PanelOpen { widget, seq: st.seq, side: st.side, max_height });
}

/// Where the panel goes: centred on its dial when first placed for a
/// widget; after that, wherever keeps its header still — a panel beside a
/// vertical rail keeps its top edge and grows downward (moving up only by
/// what would leave the work area), one below a rail keeps its top, one
/// above a rail keeps the edge against the rail (the panel page gives it
/// the height of its tallest page, so its header does not move either).
fn position(st: &mut State, g: &Geometry, size: (i32, i32)) -> (i32, i32) {
    let fresh = g.place(st.side, size);
    let (x, y) = match (st.anchor, st.side) {
        (Some((_, top)), Side::Left | Side::Right) => {
            let edge = (EDGE * g.scale).round() as i32;
            (fresh.0, top.min(g.area.bottom() - edge - size.1).max(g.area.y + edge))
        }
        (Some((left, _)), Side::Bottom) => {
            let edge = (EDGE * g.scale).round() as i32;
            (left.min(g.area.right() - edge - size.0).max(g.area.x + edge), fresh.1)
        }
        _ => fresh,
    };
    if st.anchor.is_none() {
        st.anchor = Some((x, y));
    }
    (x, y)
}

/// Sizes and places the window the current panel is drawn in. A visible
/// one (the shown widget grew, or its page changed) moves no further than
/// its anchored header allows; one not showing waits for the page to paint.
fn place(app: &AppHandle, st: &mut State) {
    let (Some(widget), Some((width, height))) = (st.widget.clone(), st.size) else { return };
    let t = st.target;
    let (Some(panel), Some(g)) = (app.get_webview_window(PANELS[t]), Geometry::of(app, &widget)) else { return };
    st.side = g.side();
    let size = ((width * g.scale).ceil() as i32, (height * g.scale).ceil() as i32);
    let (x, y) = position(st, &g, size);
    if st.fitted[t] != Some(size) {
        if overlay::fit(&panel, size.0, size.1).is_err() {
            return;
        }
        st.fitted[t] = Some(size);
    }
    if st.at[t] != Some((x, y)) && panel.set_position(PhysicalPosition::new(x, y)).is_ok() {
        st.at[t] = Some((x, y));
    }
    if st.visible && t == st.front {
        let _ = app.emit_to("rail", "panel-state", PanelState { widget: Some(widget), side: st.side });
    }
}

/// Shows the window the page has painted the current panel in, and parks
/// the other in the same main-loop turn: the compositor sees one swap.
fn show(app: &AppHandle, st: &mut State) {
    let (t, old) = (st.target, st.front);
    let (Some(widget), Some((x, y)), Some(panel)) = (st.widget.clone(), st.at[t], app.get_webview_window(PANELS[t])) else {
        return;
    };
    let was_visible = st.visible;
    if !was_visible {
        // The page starts its arrival animation as the window shows; a
        // switch is immediate (the menu-bar model).
        let _ = app.emit_to(PANELS[t], "panel-reveal", st.side);
    }
    trace("show");
    if overlay::reveal(&panel, PhysicalPosition::new(x, y)).is_err() {
        return;
    }
    if was_visible && old != t {
        if let Some(previous) = app.get_webview_window(PANELS[old]) {
            let _ = overlay::conceal(&previous);
        }
    }
    st.front = t;
    st.visible = true;
    if !was_visible {
        toast::hold(app, &widget);
    }
    let _ = app.emit_to("rail", "panel-state", PanelState { widget: Some(widget), side: st.side });
}

/// Fades the panel out (the page does it on "panel-close"), then hides its
/// window — unless it has been asked open again meanwhile.
fn hide(app: &AppHandle, st: &mut State) {
    let was_visible = st.visible;
    st.visible = false;
    st.widget = None;
    st.size = None;
    // A window hidden under the pointer may never hear it leave.
    st.in_panel = false;
    st.anchor = None;
    let front = PANELS[st.front];
    let _ = app.emit_to(front, "panel-close", ());
    let _ = app.emit_to("rail", "panel-state", PanelState { widget: None, side: st.side });
    toast::release(app);
    if !was_visible {
        return;
    }
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(FADE).await;
        main_thread::run(&app, move |app| {
            let hover = app.state::<Hover>();
            let Ok(st) = hover.0.lock() else { return };
            // Parked unless it is showing again.
            if !(st.visible && PANELS[st.front] == front) {
                if let Some(panel) = app.get_webview_window(front) {
                    let _ = overlay::conceal(&panel);
                }
            }
        });
    });
}

/// Where the rail and one of its dials are, in physical px.
pub struct Geometry {
    rail: (i32, i32, i32, i32),
    /// The dial's centre.
    dial: (i32, i32),
    vertical: bool,
    pub area: Area,
    pub scale: f64,
}

impl Geometry {
    pub fn of(app: &AppHandle, widget: &str) -> Option<Self> {
        main_thread::assert();
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
    pub fn side(&self) -> Side {
        let (x, y, w, h) = self.rail;
        if self.vertical {
            if self.area.right() - (x + w) >= x - self.area.x { Side::Right } else { Side::Left }
        } else if self.area.bottom() - (y + h) >= y - self.area.y {
            Side::Bottom
        } else {
            Side::Top
        }
    }

    /// A toast stack beside the rail: its newest toast (`anchor` px from the
    /// stack's top) level with the dial on a vertical rail, the stack
    /// centred on the dial on a horizontal one.
    pub fn beside(&self, side: Side, size: (i32, i32), anchor: i32) -> (i32, i32) {
        let (x, y) = self.place(side, size);
        match side {
            Side::Left | Side::Right => {
                let edge = (EDGE * self.scale).round() as i32;
                let top = (self.dial.1 - anchor).clamp(self.area.y + edge, (self.area.bottom() - edge - size.1).max(self.area.y + edge));
                (x, top)
            }
            Side::Top | Side::Bottom => (x, y),
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

#[cfg(test)]
mod tests {
    use super::*;

    /// A rail against the right edge of a 3840×2112 work area at 1.5×.
    fn right_rail() -> Geometry {
        Geometry {
            rail: (3759, 2906, 81, 717),
            dial: (3800, 3360),
            vertical: true,
            area: Area { x: 0, y: 2208, width: 3840, height: 2112 },
            scale: 1.5,
        }
    }

    #[test]
    fn a_panel_opens_on_the_roomier_side_centred_on_its_dial() {
        let g = right_rail();
        assert!(matches!(g.side(), Side::Left));
        let (x, y) = g.place(Side::Left, (663, 849));
        assert_eq!(x, 3759 - 9 - 663); // 6 CSS px of gap at 1.5
        assert_eq!(y, 3360 - 849 / 2);
    }

    #[test]
    fn a_tall_panel_is_clamped_along_the_rail_not_across_it() {
        let g = right_rail();
        let (x, y) = g.place(Side::Left, (663, 2080));
        assert_eq!(x, 3759 - 9 - 663);
        // As close to centred on the dial as the work area allows, 8 CSS px
        // from its bottom edge.
        assert_eq!(y, 2208 + 2112 - 12 - 2080);
    }

    #[test]
    fn a_placed_panel_keeps_its_top_when_it_resizes() {
        let g = right_rail();
        let mut st = State { side: Side::Left, ..State::default() };
        let first = position(&mut st, &g, (663, 849));
        let shorter = position(&mut st, &g, (663, 537));
        assert_eq!(first.1, shorter.1, "the header must not move under the pointer");
        // Grown past the bottom of the work area: up by the overflow only.
        let taller = position(&mut st, &g, (663, 1500));
        assert_eq!(taller.1, 2208 + 2112 - 12 - 1500);
    }

    #[test]
    fn a_toast_stack_puts_its_newest_toast_level_with_the_dial() {
        let g = right_rail();
        let (_, y) = g.beside(Side::Left, (259, 171), 28);
        assert_eq!(y, 3360 - 28);
    }
}
