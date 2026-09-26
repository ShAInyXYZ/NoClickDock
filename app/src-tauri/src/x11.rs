//! Compositor-side opacity for parking overlays (Linux, X11).
//!
//! GTK's own window opacity is client-side alpha: GTK repaints the window at
//! the new alpha, a frame or more later. Parking by it made the two-window
//! swap show an empty frame on Mutter (design M1 r3): the outgoing window
//! had gone transparent before the incoming one had repainted opaque.
//! `_NET_WM_WINDOW_OPACITY` is read by the compositor itself and needs no
//! repaint: both windows keep fully painted buffers, and the swap is two
//! property changes sent together inside one server grab, so the compositor
//! handles them in one batch.
use std::sync::OnceLock;

use gtk::prelude::*;
use tauri::WebviewWindow;
use x11rb::connection::Connection;
use x11rb::protocol::xproto::{Atom, AtomEnum, ConnectionExt, PropMode};
use x11rb::rust_connection::RustConnection;
use x11rb::wrapper::ConnectionExt as _;

struct Link {
    conn: RustConnection,
    opacity: Atom,
}

static LINK: OnceLock<Option<Link>> = OnceLock::new();

fn link() -> Option<&'static Link> {
    LINK.get_or_init(|| {
        let (conn, _) = x11rb::connect(None).ok()?;
        let opacity = conn.intern_atom(false, b"_NET_WM_WINDOW_OPACITY").ok()?.reply().ok()?.atom;
        Some(Link { conn, opacity })
    })
    .as_ref()
}

/// The X window of an overlay, realising it first if need be.
pub fn xid(window: &WebviewWindow) -> Option<u32> {
    let gtk_window = window.gtk_window().ok()?;
    if !gtk_window.is_realized() {
        gtk_window.realize();
    }
    let gdk_window = gtk_window.window()?;
    let x11 = gdk_window.downcast::<gdkx11::X11Window>().ok()?;
    Some(x11.xid() as u32)
}

/// Shows (`true`) or hides (`false`) windows at the compositor, all at once.
/// Showing deletes the property (fully opaque, the default); hiding sets it
/// to 0.
pub fn set_visible(changes: &[(u32, bool)]) -> bool {
    let changes: Vec<(u32, Option<u32>)> = changes.iter().map(|&(id, visible)| (id, if visible { None } else { Some(0) })).collect();
    set_opacity(&changes)
}

/// Sets windows' compositor opacity: `None` removes the property (opaque),
/// `Some(v)` sets it (0 transparent … `u32::MAX` opaque). Several changes go
/// inside one server grab, so the compositor takes them in one batch.
pub fn set_opacity(changes: &[(u32, Option<u32>)]) -> bool {
    let Some(l) = link() else { return false };
    let grab = changes.len() > 1;
    if grab && l.conn.grab_server().is_err() {
        return false;
    }
    let sent = changes.iter().all(|&(id, value)| match value {
        None => l.conn.delete_property(id, l.opacity).is_ok(),
        Some(v) => l.conn.change_property32(PropMode::REPLACE, id, l.opacity, AtomEnum::CARDINAL, &[v]).is_ok(),
    });
    // Released on every path, whatever failed above (research M2 r4): a
    // server grab left behind would freeze every client on the display.
    let released = !grab || l.conn.ungrab_server().is_ok();
    let flushed = l.conn.flush().is_ok();
    sent && released && flushed
}
