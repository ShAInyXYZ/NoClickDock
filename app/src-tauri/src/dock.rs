//! The rail window: sized to what its page measures, placed where the user
//! left it — on the same monitor, against the same edge, as far along it —
//! snapped to a work-area edge when dropped near one, and put back in place
//! whenever monitors or work areas change.
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde::Deserialize;
use serde_json::{json, Value};
use tauri::{AppHandle, Manager, PhysicalPosition, WebviewWindow};

use crate::{core, hover, overlay};

/// Distance, in CSS px, within which a dropped rail snaps to an edge.
const SNAP: f64 = 24.0;
/// A drag is over when the rail has not moved for this long.
const SETTLE: Duration = Duration::from_millis(400);

/// What the rail page measured, in CSS px relative to the window.
#[derive(Clone, Deserialize)]
pub struct RailLayout {
    pub width: f64,
    pub height: f64,
    /// The page's innerWidth, to check the CSS-to-physical scale against.
    pub viewport: f64,
    pub dials: Vec<DialRect>,
}

#[derive(Clone, Deserialize)]
pub struct DialRect {
    pub widget: String,
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
}

#[derive(Default)]
pub struct Dock(Mutex<DockState>);

#[derive(Default)]
struct DockState {
    layout: Option<RailLayout>,
    size: Option<(i32, i32)>,
    fitted: Option<Instant>,
    shown: bool,
    /// Hidden from the tray: the rail stays measured but off screen.
    hidden: bool,
    moves: u64,
    /// Moves the shell made itself, which are not the user's drag.
    placing: u32,
}

impl Dock {
    pub fn layout(&self) -> Option<RailLayout> {
        self.0.lock().ok()?.layout.clone()
    }

    pub fn is_hidden(&self) -> bool {
        self.0.lock().map(|s| s.hidden).unwrap_or(false)
    }
}

/// A work area in physical px.
#[derive(Clone, Copy)]
pub struct Area {
    pub x: i32,
    pub y: i32,
    pub width: i32,
    pub height: i32,
}

impl Area {
    pub fn right(&self) -> i32 {
        self.x + self.width
    }
    pub fn bottom(&self) -> i32 {
        self.y + self.height
    }
}

/// The work area of the monitor holding a point, else of the primary monitor.
pub fn work_area(app: &AppHandle, x: i32, y: i32) -> Option<Area> {
    let monitor = app
        .monitor_from_point(f64::from(x), f64::from(y))
        .ok()
        .flatten()
        .or_else(|| app.primary_monitor().ok().flatten())?;
    let area = monitor.work_area();
    // A hot-plug can briefly report an empty work area.
    (area.size.width > 0 && area.size.height > 0).then(|| Area {
        x: area.position.x,
        y: area.position.y,
        width: area.size.width as i32,
        height: area.size.height as i32,
    })
}

fn clamp(position: (i32, i32), size: (i32, i32), area: Area) -> (i32, i32) {
    (
        position.0.clamp(area.x, (area.right() - size.0).max(area.x)),
        position.1.clamp(area.y, (area.bottom() - size.1).max(area.y)),
    )
}

/// The rail page reports its size and dials; the window follows.
#[tauri::command]
pub fn rail_layout(app: AppHandle, layout: RailLayout) -> Result<(), String> {
    let rail = app.get_webview_window("rail").ok_or("The rail window is missing.")?;
    confirm_scale(&app, &rail, &layout);
    let scale = overlay::scale(&rail);
    let size = ((layout.width * scale).ceil() as i32, (layout.height * scale).ceil() as i32);
    let vertical = layout.height >= layout.width;

    let (resized, first, hidden) = {
        let dock = app.state::<Dock>();
        let mut state = dock.0.lock().map_err(|_| "The dock is unavailable.")?;
        state.layout = Some(layout);
        let resized = state.size != Some(size);
        if resized {
            state.size = Some(size);
            state.fitted = Some(Instant::now());
        }
        let first = !state.shown && !state.hidden;
        state.shown |= first;
        (resized, first, state.hidden)
    };
    if resized {
        overlay::fit(&rail, size.0, size.1).map_err(|e| e.to_string())?;
    }
    if first {
        let (x, y) = restore(&app, size, vertical);
        placing(&app);
        overlay::reveal(&rail, PhysicalPosition::new(x, y)).map_err(|e| e.to_string())?;
    } else if resized && !hidden {
        // A new size keeps the rail on its edge, as far along it as before.
        let (x, y) = restore(&app, size, vertical);
        placing(&app);
        rail.set_position(PhysicalPosition::new(x, y)).map_err(|e| e.to_string())?;
    }
    // An open panel follows its dial.
    hover::replace(&app);
    Ok(())
}

/// Where the rail belongs, from the saved position:
/// - its monitor if still connected, else the primary one;
/// - against its edge, as far along it as before, when it was snapped;
/// - else its corner clamped into that monitor's work area.
/// Without a saved position: against the right edge of the primary monitor,
/// centred (the bottom edge for a horizontal rail).
fn restore(app: &AppHandle, size: (i32, i32), vertical: bool) -> (i32, i32) {
    let saved = app.state::<core::Core>().dock_config().and_then(|dock| dock.get("position").cloned());
    let primary = || {
        app.primary_monitor().ok().flatten().and_then(|m| {
            let p = m.position();
            work_area(app, p.x + 1, p.y + 1)
        })
    };
    let Some(p) = saved else {
        let Some(area) = primary() else { return (0, 0) };
        return on_edge(area, size, if vertical { "right" } else { "bottom" }, 0.5);
    };
    let x = p.get("x").and_then(Value::as_i64).unwrap_or(0) as i32;
    let y = p.get("y").and_then(Value::as_i64).unwrap_or(0) as i32;
    let named = p.get("monitor").and_then(Value::as_str).and_then(|name| {
        app.available_monitors().ok()?.into_iter().find(|m| m.name().is_some_and(|n| n == name))
    });
    let area = named
        .and_then(|m| {
            let at = m.position();
            work_area(app, at.x + 1, at.y + 1)
        })
        .or_else(|| {
            // No name (an older config) or the monitor is gone: the monitor
            // under the saved corner, else the primary.
            app.monitor_from_point(f64::from(x + size.0 / 2), f64::from(y + size.1 / 2))
                .ok()
                .flatten()
                .and_then(|_| work_area(app, x + size.0 / 2, y + size.1 / 2))
        })
        .or_else(primary);
    let Some(area) = area else { return (x, y) };
    match p.get("edge").and_then(Value::as_str).filter(|e| !e.is_empty()) {
        Some(edge) => on_edge(area, size, edge, p.get("along").and_then(Value::as_f64).unwrap_or(0.5)),
        None => clamp((x, y), size, area),
    }
}

/// Flush against an edge of the work area, its centre `along` (0..1) of the
/// way down (left/right edges) or across (top/bottom).
fn on_edge(area: Area, size: (i32, i32), edge: &str, along: f64) -> (i32, i32) {
    let along = along.clamp(0.0, 1.0);
    let across_x = area.x + (f64::from(area.width) * along).round() as i32 - size.0 / 2;
    let down_y = area.y + (f64::from(area.height) * along).round() as i32 - size.1 / 2;
    let at = match edge {
        "left" => (area.x, down_y),
        "right" => (area.right() - size.0, down_y),
        "top" => (across_x, area.y),
        _ => (across_x, area.bottom() - size.1),
    };
    clamp(at, size, area)
}

/// Marks the next `Moved` as the shell's own, not a drag to save.
fn placing(app: &AppHandle) {
    if let Ok(mut state) = app.state::<Dock>().0.lock() {
        state.placing += 1;
    }
}

/// Monitors or work areas changed: the rail goes back where it belongs.
pub fn replace(app: &AppHandle) {
    let Some(rail) = app.get_webview_window("rail") else { return };
    let (size, vertical, shown) = {
        let dock = app.state::<Dock>();
        let Ok(state) = dock.0.lock() else { return };
        let vertical = state.layout.as_ref().is_some_and(|l| l.height >= l.width);
        (state.size, vertical, state.shown && !state.hidden)
    };
    let Some(size) = size.filter(|_| shown) else { return };
    hover::close(app);
    let (x, y) = restore(app, size, vertical);
    placing(app);
    let _ = rail.set_position(PhysicalPosition::new(x, y));
}

/// Shows or hides the whole dock (the tray's "Hide dock").
pub fn set_hidden(app: &AppHandle, hidden: bool) {
    let Some(rail) = app.get_webview_window("rail") else { return };
    let size = {
        let dock = app.state::<Dock>();
        let Ok(mut state) = dock.0.lock() else { return };
        if state.hidden == hidden {
            return;
        }
        state.hidden = hidden;
        state.shown = !hidden && state.size.is_some();
        state.size
    };
    if hidden {
        hover::close(app);
        let _ = overlay::conceal(&rail);
    } else if let Some(size) = size {
        let vertical = app.state::<Dock>().layout().is_some_and(|l| l.height >= l.width);
        let (x, y) = restore(app, size, vertical);
        placing(app);
        let _ = overlay::reveal(&rail, PhysicalPosition::new(x, y));
    }
}

/// WebKitGTK scales CSS by the text scale, which GTK reports; the page's own
/// width is the proof. Checked only once the window has settled at a size.
fn confirm_scale(app: &AppHandle, rail: &WebviewWindow, layout: &RailLayout) {
    let settled = app
        .state::<Dock>()
        .0
        .lock()
        .ok()
        .and_then(|s| s.fitted)
        .is_some_and(|at| at.elapsed() > Duration::from_millis(500));
    if !settled || layout.viewport <= 0.0 {
        return;
    }
    let Ok(inner) = rail.inner_size() else { return };
    let measured = f64::from(inner.width) / layout.viewport;
    let scale = app.state::<overlay::Scale>();
    if (measured - scale.get()).abs() / scale.get() > 0.02 {
        eprintln!("ncd: the page measures {measured:.3} physical px per CSS px, not {:.3}", scale.get());
        scale.set(measured);
    }
}

/// The rail moved: close any panel (a drag started), and once it has been
/// still for a moment, snap and save.
pub fn moved(app: &AppHandle) {
    let (generation, ours) = {
        let dock = app.state::<Dock>();
        let Ok(mut state) = dock.0.lock() else { return };
        state.moves += 1;
        let ours = state.placing > 0;
        state.placing = state.placing.saturating_sub(1);
        (state.moves, ours)
    };
    if ours {
        return; // the shell put it there; nothing to snap or save
    }
    hover::close(app);
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(SETTLE).await;
        let main = app.clone();
        let _ = app.run_on_main_thread(move || {
            let current = main.state::<Dock>().0.lock().map(|s| s.moves).unwrap_or(0);
            if current == generation {
                settle(&main);
            }
        });
    });
}

/// Snaps a dropped rail to the nearest work-area edge within 24 CSS px, then
/// saves where it rests. Moving it again lands back here through `moved`.
fn settle(app: &AppHandle) {
    let Some(rail) = app.get_webview_window("rail") else { return };
    let (Ok(at), Ok(size)) = (rail.outer_position(), rail.outer_size()) else { return };
    let size = (size.width as i32, size.height as i32);
    let Some(area) = work_area(app, at.x + size.0 / 2, at.y + size.1 / 2) else { return };
    let reach = (SNAP * overlay::scale(&rail)).round() as i32;
    let (mut x, mut y) = clamp((at.x, at.y), size, area);
    if x - area.x <= reach {
        x = area.x;
    } else if area.right() - (x + size.0) <= reach {
        x = area.right() - size.0;
    }
    if y - area.y <= reach {
        y = area.y;
    } else if area.bottom() - (y + size.1) <= reach {
        y = area.bottom() - size.1;
    }
    if (x, y) != (at.x, at.y) {
        placing(app);
        let _ = rail.set_position(PhysicalPosition::new(x, y));
    }
    // Save it as the monitor, the edge it rests on and how far along it, so
    // it survives resolution, scale and monitor changes; x/y as a fallback.
    let edge = if x == area.x {
        "left"
    } else if x + size.0 == area.right() {
        "right"
    } else if y == area.y {
        "top"
    } else if y + size.1 == area.bottom() {
        "bottom"
    } else {
        ""
    };
    let along = match edge {
        "left" | "right" => f64::from(y + size.1 / 2 - area.y) / f64::from(area.height),
        "top" | "bottom" => f64::from(x + size.0 / 2 - area.x) / f64::from(area.width),
        _ => 0.5,
    };
    let monitor = app
        .monitor_from_point(f64::from(x + size.0 / 2), f64::from(y + size.1 / 2))
        .ok()
        .flatten()
        .and_then(|m| m.name().cloned())
        .unwrap_or_default();
    let mut position = json!({"monitor": monitor, "edge": edge, "x": x, "y": y});
    if !edge.is_empty() {
        position["along"] = json!((along.clamp(0.0, 1.0) * 1000.0).round() / 1000.0);
    }
    let saved = app.state::<core::Core>().dock_config().and_then(|dock| dock.get("position").cloned());
    if saved.as_ref() != Some(&position) {
        // null first: an edge or along from before must not survive.
        let patch = json!({"type": "config.patch", "patch": {"dock": {"position": null}}});
        let _ = core::send(app, &patch);
        let patch = json!({"type": "config.patch", "patch": {"dock": {"position": position}}});
        if let Err(error) = core::send(app, &patch) {
            eprintln!("ncd: could not save the rail's position: {error}");
        }
    }
}

/// Watches monitors and work areas (GNOME publishes per-monitor work areas;
/// a hot-plug or a panel change moves them) and puts the rail back once a
/// burst of notifications has settled. Main thread, once.
#[cfg(target_os = "linux")]
pub fn observe_monitors(app: &AppHandle) {
    use gtk::prelude::*;
    use std::sync::atomic::{AtomicU64, Ordering};
    use std::sync::Arc;

    let Some(display) = gtk::gdk::Display::default() else { return };
    let bursts = Arc::new(AtomicU64::new(0));
    let queue = {
        let app = app.clone();
        move || {
            let burst = bursts.fetch_add(1, Ordering::AcqRel) + 1;
            let bursts = bursts.clone();
            let app = app.clone();
            gtk::glib::timeout_add_local_once(Duration::from_millis(150), move || {
                if bursts.load(Ordering::Acquire) == burst {
                    replace(&app);
                }
            });
        }
    };
    let watch = {
        let queue = queue.clone();
        move |monitor: &gtk::gdk::Monitor| {
            let (a, b, c) = (queue.clone(), queue.clone(), queue.clone());
            monitor.connect_workarea_notify(move |_| a());
            monitor.connect_geometry_notify(move |_| b());
            monitor.connect_scale_factor_notify(move |_| c());
        }
    };
    for i in 0..display.n_monitors() {
        if let Some(monitor) = display.monitor(i) {
            watch(&monitor);
        }
    }
    let (added, removed, changed) = (queue.clone(), queue.clone(), queue);
    display.connect_monitor_added(move |_, monitor| {
        watch(monitor);
        added();
    });
    display.connect_monitor_removed(move |_, _| removed());
    display.default_screen().connect_monitors_changed(move |_| changed());
}

/// Windows re-places on ScaleChanged instead (on_window_event).
#[cfg(not(target_os = "linux"))]
pub fn observe_monitors(_app: &AppHandle) {}

#[cfg(test)]
mod tests {
    use super::*;

    const AREA: Area = Area { x: 0, y: 2208, width: 3840, height: 2112 };

    #[test]
    fn a_rail_goes_back_against_its_edge_as_far_along_as_before() {
        let size = (81, 717);
        assert_eq!(on_edge(AREA, size, "right", 0.5), (3840 - 81, 2208 + 1056 - 358));
        assert_eq!(on_edge(AREA, size, "left", 0.0), (0, 2208)); // clamped into the area
        let wide = (717, 81);
        assert_eq!(on_edge(AREA, wide, "bottom", 0.25), (960 - 358, 2208 + 2112 - 81));
        assert_eq!(on_edge(AREA, wide, "top", 1.0), (3840 - 717, 2208));
    }

    #[test]
    fn clamping_keeps_the_whole_rail_on_the_work_area() {
        assert_eq!(clamp((5000, 100), (81, 717), AREA), (3840 - 81, 2208));
        assert_eq!(clamp((-40, 5000), (81, 717), AREA), (0, 2208 + 2112 - 717));
        // Bigger than the area: pinned to its origin, never negative.
        assert_eq!(clamp((10, 10), (4000, 81), AREA), (0, 2208));
    }
}
