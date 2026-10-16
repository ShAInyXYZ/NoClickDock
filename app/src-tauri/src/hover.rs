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
//! Each window keeps what it last drew. The rail says which dial the
//! pointer is heading for while a panel is open, and that widget is drawn
//! in the window not showing before the pointer arrives; a switch back to
//! the widget shown before finds it still drawn. Either way the switch on
//! arrival is only the swap (design M1 r4: on the real desktop the drawing
//! after arrival cost ~40 ms of a 70 ms budget).
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
pub(crate) fn trace(what: &str) {
    crate::watchdog::note(format!("hover {what}"));
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
    /// Bumped by every drawing asked of a window, so a stale measurement is
    /// ignored; each window remembers the one it is on.
    seq: u64,
    side: Side,
    visible: bool,
    /// The window showing, or last shown (an index into PANELS).
    front: usize,
    win: [Win; 2],
    /// Each widget's selected page, kept here for both panel windows.
    pages: std::collections::HashMap<String, String>,
    /// Until then, pointer entries do not open a panel (a menu is popping).
    quiet_until: Option<Instant>,
    /// A menu popped over this dial (None: over the rail): its panel stays
    /// shut until the pointer really leaves or reaches another dial.
    menu_block: Option<(Option<String>, Instant)>,
}

/// What one panel window holds.
#[derive(Default, Clone)]
struct Win {
    widget: Option<String>,
    seq: u64,
    /// The page's measurement for `seq`, and whether it has painted at it.
    size: Option<(f64, f64)>,
    painted: bool,
    /// Where it was put for this widget: its header edge stays there.
    anchor: Option<(i32, i32)>,
    /// Its size and place as last set, to skip what did not change.
    fitted: Option<(i32, i32)>,
    at: Option<(i32, i32)>,
    /// What its page was last asked to draw, to ask again if the page was
    /// not listening yet (just reloaded).
    asked: Option<PanelOpen>,
}

impl State {
    /// The widget the pointer asks for, if any.
    fn wanted(&self) -> Option<&str> {
        match &self.phase {
            Phase::Closed => None,
            Phase::Dwell(w) | Phase::Open(w) | Phase::Grace(w) => Some(w),
        }
    }

    fn by_seq(&self, seq: u64) -> Option<usize> {
        (0..2).find(|&i| self.win[i].seq == seq && self.win[i].widget.is_some())
    }

    /// Drawn and painted at its current size: showing it is only the swap.
    fn ready(&self, t: usize) -> bool {
        self.win[t].size.is_some() && self.win[t].painted
    }
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
    /// The page the user last chose for this widget: it follows the widget,
    /// not whichever of the two windows draws it (dock-core §4).
    page: Option<String>,
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
    if let Some((blocked, since)) = st.menu_block.clone() {
        // The menu's pointer grab sends leaves and enters as it pops and as
        // it closes: none of them opens a panel. After that, only a real
        // leave or another dial does.
        let settled = since.elapsed() >= MENU_QUIET;
        match &widget {
            Some(w) if !settled || blocked.as_deref() == Some(w.as_str()) => return,
            Some(_) => st.menu_block = None,
            None if settled => st.menu_block = None,
            None => {}
        }
    }
    if widget.is_some() && st.quiet_until.is_some_and(|t| Instant::now() < t) {
        return;
    }
    match (widget, st.phase.clone()) {
        (Some(w), Phase::Closed | Phase::Dwell(_)) => {
            st.phase = Phase::Dwell(w.clone());
            // Draw and measure now; the dwell's end only has to show it.
            draw(&app, &mut st, &w);
            after(&app, DWELL, generation, dwell_done);
        }
        (Some(w), Phase::Open(current) | Phase::Grace(current)) => {
            st.phase = Phase::Open(w.clone());
            if current != w {
                toast::hold(&app, &w);
                // Drawn ahead (the pointer was heading here) or shown
                // before: the swap now. Otherwise it shows once painted.
                if let Some(t) = draw(&app, &mut st, &w) {
                    if st.ready(t) && !(st.visible && st.front == t) {
                        show(&app, &mut st, t);
                    }
                }
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

/// The rail sees the pointer heading for another dial while a panel is
/// open: that widget is drawn now in the window not showing, so the switch
/// on arrival is only the swap. A wrong guess costs a redraw nobody sees.
#[tauri::command]
pub fn hover_approach(app: AppHandle, widget: String) {
    let hover = app.state::<Hover>();
    let Ok(mut st) = hover.0.lock() else { return };
    let Some(current) = st.wanted().map(str::to_owned) else { return };
    // Only while the panel asked for is the one showing: the window not
    // showing may still be drawing it.
    let settled = matches!(st.phase, Phase::Open(_) | Phase::Grace(_))
        && st.visible
        && st.win[st.front].widget.as_deref() == Some(current.as_str());
    if !settled || widget == current || widget.len() > 64 || !overlay::composited() {
        return;
    }
    trace(&format!("approach {widget}"));
    draw(&app, &mut st, &widget);
}

/// A panel page has loaded and listens: a drawing asked of it before then
/// (it was being reloaded after a web process crash) is asked again.
#[tauri::command]
pub fn panel_ready(app: AppHandle, window: tauri::WebviewWindow) {
    let hover = app.state::<Hover>();
    let Ok(st) = hover.0.lock() else { return };
    let Some(t) = PANELS.iter().position(|label| *label == window.label()) else { return };
    if let (Some(open), None) = (st.win[t].asked.clone(), st.win[t].size) {
        trace("panel ready: asked again");
        let _ = app.emit_to(PANELS[t], "panel-open", open);
    }
}

/// A panel page measured itself for the widget it was asked to draw.
#[tauri::command]
pub fn panel_layout(app: AppHandle, seq: u64, width: f64, height: f64) {
    let hover = app.state::<Hover>();
    let Ok(mut st) = hover.0.lock() else { return };
    let Some(t) = st.by_seq(seq) else {
        return; // a measurement for a drawing no longer asked for
    };
    st.win[t].size = Some((width, height));
    st.win[t].painted = false; // until it has painted at this size
    trace(&format!("panel_layout {width}x{height}"));
    place(&app, &mut st, t);
}

/// The page has painted the panel at the size `panel_layout` gave its
/// window. A panel drawn in the window not showing is shown now if it is
/// the one asked for (after the dwell, if one runs).
#[tauri::command]
pub fn panel_painted(app: AppHandle, seq: u64, width: f64, height: f64) {
    let t = {
        let hover = app.state::<Hover>();
        let Ok(st) = hover.0.lock() else { return };
        // A paint of an older size (a snapshot changed it since) shows
        // nothing: the paint of the new size will.
        match st.by_seq(seq) {
            Some(t) if st.win[t].size == Some((width, height)) => t,
            _ => return,
        }
    };
    // The page has painted; the window has it once GTK has drawn a frame
    // after that (design M1 r3: a sweep once showed a panel whose lower part
    // was not drawn yet).
    let Some(panel) = app.get_webview_window(PANELS[t]) else { return };
    let app2 = app.clone();
    overlay::after_next_frame(&panel, move || {
        let hover = app2.state::<Hover>();
        let Ok(mut st) = hover.0.lock() else { return };
        if st.win[t].seq != seq || st.win[t].size != Some((width, height)) {
            return;
        }
        st.win[t].painted = true;
        trace("panel painted");
        let asked = matches!(st.phase, Phase::Open(_) | Phase::Grace(_)) && st.wanted() == st.win[t].widget.as_deref();
        if asked && !(st.visible && st.front == t) {
            show(&app2, &mut st, t);
        }
    });
}

/// A panel page's tab changed: remembered for the widget, so the other
/// window opens it on the same page.
#[tauri::command]
pub fn panel_page(app: AppHandle, widget: String, page: String) {
    let hover = app.state::<Hover>();
    let Ok(mut st) = hover.0.lock() else { return };
    if widget.len() <= 64 && page.len() <= 64 {
        st.pages.insert(widget, page);
    }
}

/// Closes the panel at once: the rail is being dragged, or a menu opens
/// (then pointer entries stay quiet for a moment).
pub fn close(app: &AppHandle) {
    close_for(app, None);
}

/// A menu is about to pop over `widget`'s dial (None: over the rail): any
/// pending dwell is cancelled, an open panel shuts, and it stays shut under
/// the menu and after it until the pointer leaves or reaches another dial.
pub fn close_for_menu(app: &AppHandle, widget: Option<String>) {
    close_for(app, Some(Instant::now() + MENU_QUIET));
    let hover = app.state::<Hover>();
    if let Ok(mut st) = hover.0.lock() {
        st.menu_block = Some((widget, Instant::now()));
    };
}

fn close_for(app: &AppHandle, quiet_until: Option<Instant>) {
    let hover = app.state::<Hover>();
    let Ok(mut st) = hover.0.lock() else { return };
    st.generation += 1;
    trace("close");
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
    for i in 0..2 {
        if !(st.visible && i == st.front) {
            st.win[i].anchor = None; // placed afresh when shown
        }
    }
    if st.visible {
        let front = st.front;
        place(app, &mut st, front);
    }
}

/// Forgets what both windows hold: their pages were reloaded (a web process
/// crash, research M2 r4). The panel is closed; the next hover draws anew.
pub fn reset(app: &AppHandle) {
    let hover = app.state::<Hover>();
    let Ok(mut st) = hover.0.lock() else { return };
    st.generation += 1;
    st.phase = Phase::Closed;
    hide(app, &mut st);
    for win in &mut st.win {
        let (fitted, at) = (win.fitted, win.at);
        *win = Win { fitted, at, ..Win::default() };
    }
}

fn dwell_done(app: &AppHandle, st: &mut State) {
    trace("dwell done");
    if let Phase::Dwell(w) = st.phase.clone() {
        st.phase = Phase::Open(w.clone());
        if let Some(t) = draw(app, st, &w) {
            if st.ready(t) {
                show(app, st, t); // drawn during the dwell: show it now
            }
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

/// Makes sure a window holds `widget`: the one showing it, the parked one
/// that still has it, or else the window not showing, whose page is asked
/// to draw it (it answers with `panel_layout`, then `panel_painted`).
fn draw(app: &AppHandle, st: &mut State, widget: &str) -> Option<usize> {
    let (t, fresh) = choose(st, widget, overlay::composited());
    if !fresh {
        return Some(t);
    }
    let g = Geometry::of(app, widget)?;
    st.side = g.side();
    st.seq += 1;
    let (fitted, at) = (st.win[t].fitted, st.win[t].at);
    st.win[t] = Win { widget: Some(widget.to_owned()), seq: st.seq, fitted, at, ..Win::default() };
    let page = st.pages.get(widget).cloned();
    let max_height = (f64::from(g.area.height) - HEADROOM * g.scale) / g.scale;
    let open = PanelOpen { widget: widget.to_owned(), seq: st.seq, side: st.side, max_height, page };
    st.win[t].asked = Some(open.clone());
    let _ = app.emit_to(PANELS[t], "panel-open", open);
    Some(t)
}

/// Which window `widget` goes in, and whether it must be drawn there. Never
/// the window showing another widget: drawing there would resize what is on
/// screen. Without a compositor nothing can be parked: one window, redrawn
/// in place.
fn choose(st: &State, widget: &str, composited: bool) -> (usize, bool) {
    let holds = |i: usize| st.win[i].widget.as_deref() == Some(widget);
    if !composited {
        return (st.front, !holds(st.front));
    }
    if st.visible && holds(st.front) {
        return (st.front, false);
    }
    let back = 1 - st.front;
    if holds(back) {
        return (back, false);
    }
    if !st.visible && holds(st.front) {
        return (st.front, false);
    }
    (back, true)
}

/// Where the panel goes: centred on its dial when first placed for a
/// widget; after that, wherever keeps its header still — a panel beside a
/// vertical rail keeps its top edge and grows downward (moving up only by
/// what would leave the work area), one below a rail keeps its top, one
/// above a rail keeps the edge against the rail (the panel page gives it
/// the height of its tallest page, so its header does not move either).
fn position(st: &mut State, t: usize, g: &Geometry, size: (i32, i32)) -> (i32, i32) {
    let fresh = g.place(st.side, size);
    let (x, y) = match (st.win[t].anchor, st.side) {
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
    if st.win[t].anchor.is_none() {
        st.win[t].anchor = Some((x, y));
    }
    (x, y)
}

/// Sizes and places a window for what it holds. The visible one (its widget
/// grew, or its page changed) moves no further than its anchored header
/// allows; a parked one may go anywhere, unseen.
fn place(app: &AppHandle, st: &mut State, t: usize) {
    let (Some(widget), Some((width, height))) = (st.win[t].widget.clone(), st.win[t].size) else { return };
    let (Some(panel), Some(g)) = (app.get_webview_window(PANELS[t]), Geometry::of(app, &widget)) else { return };
    st.side = g.side();
    let size = ((width * g.scale).ceil() as i32, (height * g.scale).ceil() as i32);
    let (x, y) = position(st, t, &g, size);
    if st.win[t].fitted != Some(size) {
        if overlay::fit(&panel, size.0, size.1).is_err() {
            return;
        }
        st.win[t].fitted = Some(size);
    }
    if st.win[t].at != Some((x, y)) && panel.set_position(PhysicalPosition::new(x, y)).is_ok() {
        st.win[t].at = Some((x, y));
    }
    if st.visible && t == st.front {
        let _ = app.emit_to("rail", "panel-state", PanelState { widget: Some(widget), side: st.side });
    }
}

/// Shows window `t`, painted with the widget asked for, and parks the other
/// in the same main-loop turn: the compositor sees one swap.
fn show(app: &AppHandle, st: &mut State, t: usize) {
    let old = st.front;
    let was_visible = st.visible;
    if st.win[t].anchor.is_none() || !was_visible {
        st.win[t].anchor = None;
        place(app, st, t); // centred on its dial, where the dial is now
    }
    let (Some(widget), Some((x, y)), Some(panel)) = (st.win[t].widget.clone(), st.win[t].at, app.get_webview_window(PANELS[t])) else {
        return;
    };
    trace("show");
    // Open and switch are instant (DESIGN, amended): a switch is one change
    // at the compositor, the incoming window shown as the outgoing parks.
    let at = PhysicalPosition::new(x, y);
    let shown = match app.get_webview_window(PANELS[old]).filter(|_| was_visible && old != t) {
        Some(previous) => overlay::swap(&panel, &previous, at),
        None => overlay::reveal(&panel, at),
    };
    if shown.is_err() {
        return;
    }
    st.front = t;
    st.visible = true;
    if !was_visible {
        toast::hold(app, &widget);
    }
    let _ = app.emit_to("rail", "panel-state", PanelState { widget: Some(widget), side: st.side });
}

/// Closes the panel: parked at once (close is instant, DESIGN amended). The
/// windows keep what they drew, so reopening the same widget is instant.
fn hide(app: &AppHandle, st: &mut State) {
    let was_visible = st.visible;
    st.visible = false;
    // A window hidden under the pointer may never hear it leave.
    st.in_panel = false;
    for win in &mut st.win {
        win.anchor = None;
    }
    if was_visible {
        if let Some(panel) = app.get_webview_window(PANELS[st.front]) {
            let _ = overlay::conceal(&panel);
        }
    }
    trace("hide");
    let _ = app.emit_to("rail", "panel-state", PanelState { widget: None, side: st.side });
    toast::release(app);
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
        let first = position(&mut st, 0, &g, (663, 849));
        let shorter = position(&mut st, 0, &g, (663, 537));
        assert_eq!(first.1, shorter.1, "the header must not move under the pointer");
        // Grown past the bottom of the work area: up by the overflow only.
        let taller = position(&mut st, 0, &g, (663, 1500));
        assert_eq!(taller.1, 2208 + 2112 - 12 - 1500);
    }

    /// What `draw` does to the windows, without GTK.
    fn put(st: &mut State, widget: &str, composited: bool) -> (usize, bool) {
        let (t, fresh) = choose(st, widget, composited);
        if fresh {
            st.seq += 1;
            st.win[t] = Win { widget: Some(widget.into()), seq: st.seq, size: Some((400.0, 300.0)), painted: true, ..Win::default() };
        }
        (t, fresh)
    }

    fn shown(st: &mut State, t: usize) {
        st.front = t;
        st.visible = true;
    }

    /// Design M1 r4: the widget the pointer heads for is drawn ahead in the
    /// window not showing; the one shown before stays drawn.
    #[test]
    fn a_switch_finds_its_widget_already_drawn() {
        let mut st = State::default();
        let (a, fresh) = put(&mut st, "docker", true);
        assert!(fresh);
        shown(&mut st, a);
        // Heading for Power: drawn in the other window, unseen.
        let (b, fresh) = put(&mut st, "power", true);
        assert!(fresh && b != a);
        // Arriving: nothing to draw, only the swap.
        assert_eq!(put(&mut st, "power", true), (b, false));
        shown(&mut st, b);
        // Back to Docker: still drawn in the first window.
        assert_eq!(put(&mut st, "docker", true), (a, false));
        shown(&mut st, a);
        // Heading for Claude overwrites the parked one, never the one showing.
        let (c, fresh) = put(&mut st, "claude", true);
        assert!(fresh && c == b);
        // Closed, then reopened on the widget last shown: nothing to draw.
        st.visible = false;
        assert_eq!(put(&mut st, "docker", true), (a, false));
        // Without a compositor there is one window, redrawn in place.
        let front = st.front;
        assert_eq!(put(&mut st, "tailscale", false), (front, true));
    }

    /// Research M2 r2: a widget's tab follows the widget, not the window.
    #[test]
    fn a_widget_opens_on_its_own_page_in_either_window() {
        let mut st = State::default();
        st.pages.insert("docker".into(), "stopped".into()); // chosen in one window
        let mut windows = Vec::new();
        for widget in ["docker", "power", "claude", "docker"] {
            let (t, _) = put(&mut st, widget, true);
            windows.push(t);
            shown(&mut st, t);
        }
        assert_ne!(windows[0], windows[3], "drawn in the other window, two dials later");
        assert_eq!(st.pages.get("docker").map(String::as_str), Some("stopped"), "the shell hands this to either window");
    }

    #[test]
    fn a_toast_stack_puts_its_newest_toast_level_with_the_dial() {
        let g = right_rail();
        let (_, y) = g.beside(Side::Left, (259, 171), 28);
        assert_eq!(y, 3360 - 28);
    }
}
