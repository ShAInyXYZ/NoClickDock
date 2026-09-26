//! The rail window: sized to what its page measures, placed where the user
//! left it, snapped to a work-area edge when dropped near one, and its
//! position saved to the core's config.
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde::Deserialize;
use serde_json::json;
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
    moves: u64,
}

impl Dock {
    pub fn layout(&self) -> Option<RailLayout> {
        self.0.lock().ok()?.layout.clone()
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

    let (resized, first) = {
        let dock = app.state::<Dock>();
        let mut state = dock.0.lock().map_err(|_| "The dock is unavailable.")?;
        state.layout = Some(layout);
        let resized = state.size != Some(size);
        if resized {
            state.size = Some(size);
            state.fitted = Some(Instant::now());
        }
        let first = !state.shown;
        state.shown = true;
        (resized, first)
    };
    if resized {
        overlay::fit(&rail, size.0, size.1).map_err(|e| e.to_string())?;
    }
    if first {
        let (x, y) = initial_position(&app, size, vertical);
        overlay::reveal(&rail, PhysicalPosition::new(x, y)).map_err(|e| e.to_string())?;
    } else if resized {
        // A rail that grew must stay inside the work area.
        let now = rail.outer_position().map_err(|e| e.to_string())?;
        if let Some(area) = work_area(&app, now.x + size.0 / 2, now.y + size.1 / 2) {
            let (x, y) = clamp((now.x, now.y), size, area);
            if (x, y) != (now.x, now.y) {
                rail.set_position(PhysicalPosition::new(x, y)).map_err(|e| e.to_string())?;
            }
        }
    }
    // An open panel follows its dial.
    hover::replace(&app);
    Ok(())
}

/// The saved position when it is still on a monitor, else against the right
/// edge of the primary monitor, centred (the bottom edge for a horizontal
/// rail) — where a drag near an edge would snap it anyway.
fn initial_position(app: &AppHandle, size: (i32, i32), vertical: bool) -> (i32, i32) {
    let saved = app.state::<core::Core>().dock_config().and_then(|dock| {
        let p = dock.get("position")?;
        Some((p.get("x")?.as_i64()? as i32, p.get("y")?.as_i64()? as i32))
    });
    if let Some((x, y)) = saved {
        if let Ok(Some(_)) = app.monitor_from_point(f64::from(x + size.0 / 2), f64::from(y + size.1 / 2)) {
            if let Some(area) = work_area(app, x + size.0 / 2, y + size.1 / 2) {
                return clamp((x, y), size, area);
            }
        }
    }
    let Some(area) = app.primary_monitor().ok().flatten().and_then(|m| {
        let p = m.position();
        work_area(app, p.x + 1, p.y + 1)
    }) else {
        return (0, 0);
    };
    if vertical {
        (area.right() - size.0, area.y + (area.height - size.1) / 2)
    } else {
        (area.x + (area.width - size.0) / 2, area.bottom() - size.1)
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
    let generation = {
        let dock = app.state::<Dock>();
        let Ok(mut state) = dock.0.lock() else { return };
        state.moves += 1;
        state.moves
    };
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
        let _ = rail.set_position(PhysicalPosition::new(x, y));
        return;
    }
    let saved = app.state::<core::Core>().dock_config().and_then(|dock| dock.get("position").cloned());
    if saved != Some(json!({"x": x, "y": y})) {
        let patch = json!({"type": "config.patch", "patch": {"dock": {"position": {"x": x, "y": y}}}});
        if let Err(error) = core::send(app, &patch) {
            eprintln!("ncd: could not save the rail's position: {error}");
        }
    }
}
