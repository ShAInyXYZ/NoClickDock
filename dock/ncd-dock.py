#!/usr/bin/env python3
"""NoClickDock — the dock that holds every NCD Status widget.

A light always-on-top pill that holds every running ShinyWidget dot in a
row (or column), with even spacing. The bar owns the layout: it watches
~/.config/status-widgets/ for live widgets and publishes each one's slot
in bar.json; the dots follow it and give up their own dragging while
docked. Drag the pill to move the whole set, right-click for orientation
and density. Closing the bar releases the dots where they stand.

Emberdeck surface: warm charcoal, hairline border, no shadow, no glow.
"""

__version__ = "1.0.0"

import json
import os
import sys
import time

try:
    import gi
    gi.require_version("Gtk", "3.0")
    from gi.repository import Gtk, Gdk, GLib
    import cairo
except (ImportError, ValueError):
    print("ERROR: GTK3 / PyGObject not found (sudo apt install python3-gi "
          "python3-gi-cairo gir1.2-gtk-3.0)", file=sys.stderr)
    sys.exit(1)
try:
    gi.require_version("GdkX11", "3.0")
    from gi.repository import GdkX11
except (ImportError, ValueError):
    GdkX11 = None   # Wayland: dots fall back to following bar.json

WIDGET_DIR = os.path.join(os.path.expanduser("~"), ".config", "status-widgets")
BAR_FILE = os.path.join(WIDGET_DIR, "bar.json")
# Settings live outside *.json: the dots prune any .json whose pid is dead,
# which is exactly what a closed bar's config would look like.
CONF_FILE = os.path.join(WIDGET_DIR, "bar.conf")

DOT = 44          # every ShinyWidget dot window is 44x44 (WIN_SIZE in the widgets)
GRIP = 14         # leading zone with the three grip dots

# density presets: gap between dots, end padding, cross-axis padding
SIZES = {
    "s": {"gap": 0,  "pad_end": 10, "pad_cross": 4},
    "m": {"gap": 4,  "pad_end": 12, "pad_cross": 5},
    "l": {"gap": 10, "pad_end": 14, "pad_cross": 7},
}

# Emberdeck tokens
BG_PANEL = (0.055, 0.051, 0.043)    # #0e0d0b
BORDER_CLR = (0.169, 0.153, 0.133)  # #2b2722 hairline
FG_DIM = (0.569, 0.545, 0.502)      # #918b80

DRAG_THRESHOLD = 4
XIDS = {}         # widget name -> X window id (filled by _active_widgets)


def _pid_alive(pid):
    try:
        os.kill(pid, 0)
        return True
    except OSError:
        return False


def _active_widgets():
    """Sorted names of live widgets — same order the dots themselves use."""
    try:
        names = []
        for fname in sorted(os.listdir(WIDGET_DIR)):
            if not fname.endswith(".json") or fname in ("corner.json", "bar.json"):
                continue
            try:
                with open(os.path.join(WIDGET_DIR, fname)) as f:
                    data = json.load(f)
                if data.get("pid") and _pid_alive(data["pid"]):
                    names.append(data["name"])
                    XIDS[data["name"]] = data.get("xid")
            except (json.JSONDecodeError, OSError, KeyError):
                pass
        return names
    except OSError:
        return []


class BarWindow(Gtk.Window):
    def __init__(self):
        super().__init__(type=Gtk.WindowType.TOPLEVEL)
        self.set_title("NoClickDock")
        self.set_decorated(False)
        self.set_keep_above(True)
        self.set_skip_taskbar_hint(True)
        self.set_skip_pager_hint(True)
        # UTILITY, not DOCK: Mutter stacks DOCK windows above every keep-above
        # window, which would pin the pill over the dots. As a plain utility
        # window in the same layer as the dots we can sink beneath them.
        self.set_type_hint(Gdk.WindowTypeHint.UTILITY)
        self.set_resizable(False)
        self.set_accept_focus(False)
        self.set_can_focus(False)

        screen = self.get_screen()
        visual = screen.get_rgba_visual()
        if visual:
            self.set_visual(visual)
        self.set_app_paintable(True)

        # config survives restarts inside bar.json
        cfg = self._load_cfg()
        self.orientation = cfg.get("orientation", "vertical")
        self.size = cfg.get("size", "m") if cfg.get("size") in SIZES else "m"
        self.x = int(cfg.get("x", 60))
        self.y = int(cfg.get("y", 60))

        self.widgets = []
        self._foreign = {}   # xid -> GdkX11 foreign window, for direct moves
        self._last_publish = 0.0
        self.dragging = False
        self._drag_dx = self._drag_dy = 0

        self.connect("draw", self._on_draw)
        self.connect("button-press-event", self._on_button)
        self.connect("button-release-event", self._on_button_release)
        self.connect("motion-notify-event", self._on_motion)
        self.connect("destroy", self._on_destroy)
        # Mutter may place us somewhere other than requested (edge/strut
        # constraints): the dots must align to where we actually are.
        self.connect("configure-event", self._on_configure)
        self.set_events(Gdk.EventMask.BUTTON_PRESS_MASK
                        | Gdk.EventMask.BUTTON_RELEASE_MASK
                        | Gdk.EventMask.POINTER_MOTION_MASK)

        self._relayout(publish=True)
        self.show_all()
        self._sink()
        GLib.timeout_add(500, self._tick)

    # -- layout -------------------------------------------------------------

    def _metrics(self):
        s = SIZES[self.size]
        n = max(1, len(self.widgets))
        length = s["pad_end"] * 2 + GRIP + n * DOT + (n - 1) * s["gap"]
        cross = DOT + 2 * s["pad_cross"]
        return s, n, length, cross

    def _relayout(self, publish=False):
        s, n, length, cross = self._metrics()
        w, h = (length, cross) if self.orientation == "horizontal" else (cross, length)
        self._clamp(w, h)
        self.set_size_request(w, h)
        self.resize(w, h)
        self.move(self.x, self.y)
        self.queue_draw()
        self._sink()
        self._place_dots()
        self._apply_input_holes()
        if publish:
            self._publish()

    def _on_configure(self, widget, event):
        ax, ay = self.get_position()
        if (ax, ay) != (self.x, self.y):
            self.x, self.y = ax, ay
            self._place_dots()
            self._publish()
        self._apply_input_holes()
        return False

    def _place_dots(self):
        """Move every docked dot window ourselves, in this same event, so
        they land in the same frame as the pill instead of trailing the
        file poll. X11 only; the dots' own poll of bar.json is the fallback
        and lands on identical coordinates."""
        if GdkX11 is None:
            return
        display = Gdk.Display.get_default()
        for name, (cx, cy) in zip(self.widgets, self._slot_centers()):
            xid = XIDS.get(name)
            if not xid:
                continue
            fw = self._foreign.get(xid)
            if fw is None:
                fw = GdkX11.X11Window.foreign_new_for_display(display, xid)
                if fw is None:
                    continue
                self._foreign[xid] = fw
            fw.move(cx - DOT // 2, cy - DOT // 2)
        display.flush()

    def _slot_rects(self):
        """Bar-local (x, y, DOT, DOT) square under each dot window."""
        return [(cx - self.x - DOT // 2, cy - self.y - DOT // 2, DOT, DOT)
                for cx, cy in self._slot_centers()]

    def _apply_input_holes(self):
        """Pointer events pass straight through the slot squares to the dots,
        so hover and click work no matter how the WM stacks us."""
        win = self.get_window()
        if not win:
            return
        w = self.get_allocated_width()
        h = self.get_allocated_height()
        region = cairo.Region(cairo.RectangleInt(0, 0, w, h))
        for x, y, sw, sh in self._slot_rects():
            region.subtract(cairo.Region(cairo.RectangleInt(x, y, sw, sh)))
        win.input_shape_combine_region(region, 0, 0)

    def _sink(self):
        """Drop to the bottom of the keep-above layer so dots stay on top."""
        win = self.get_window()
        if win:
            win.lower()

    def _slot_centers(self):
        """Absolute screen center of each slot, in widget order."""
        s, _, _, cross = self._metrics()
        centers = []
        for i, _name in enumerate(self.widgets):
            along = s["pad_end"] + GRIP + DOT // 2 + i * (DOT + s["gap"])
            if self.orientation == "horizontal":
                centers.append((self.x + along, self.y + cross // 2))
            else:
                centers.append((self.x + cross // 2, self.y + along))
        return centers

    def _clamp(self, w, h):
        display = Gdk.Display.get_default()
        try:
            geo = display.get_monitor_at_point(self.x, self.y).get_geometry()
        except Exception:
            return
        self.x = max(geo.x, min(self.x, geo.x + geo.width - w))
        self.y = max(geo.y, min(self.y, geo.y + geo.height - h))

    def _publish(self):
        os.makedirs(WIDGET_DIR, exist_ok=True)
        slots = {name: list(c) for name, c in zip(self.widgets, self._slot_centers())}
        _, _, length, cross = self._metrics()
        w, h = (length, cross) if self.orientation == "horizontal" else (cross, length)
        # Written atomically: the dots poll this file while we drag, and a
        # half-written JSON must never read as "no bar".
        self._last_publish = time.time()
        tmp = BAR_FILE + ".tmp"
        with open(tmp, "w") as f:
            json.dump({"pid": os.getpid(), "ts": time.time(), "rect": [self.x, self.y, w, h],
                       "orientation": self.orientation, "size": self.size,
                       "x": self.x, "y": self.y, "slots": slots}, f)
        os.replace(tmp, BAR_FILE)

    def _load_cfg(self):
        try:
            with open(CONF_FILE) as f:
                return json.load(f)
        except (OSError, json.JSONDecodeError):
            return {}

    def _save_cfg(self):
        os.makedirs(WIDGET_DIR, exist_ok=True)
        with open(CONF_FILE, "w") as f:
            json.dump({"orientation": self.orientation, "size": self.size,
                       "x": self.x, "y": self.y}, f)

    def _tick(self):
        current = _active_widgets()
        if current != self.widgets:
            self.widgets = current
            self._relayout(publish=True)
        return True

    # -- drawing --------------------------------------------------------------

    def _on_draw(self, widget, cr):
        import math
        cr.set_operator(0); cr.paint(); cr.set_operator(2)
        w = self.get_allocated_width()
        h = self.get_allocated_height()
        r = min(w, h) / 2

        def pill(inset):
            cr.new_path()
            rr = r - inset
            cr.arc(r, r, rr, math.pi / 2, 3 * math.pi / 2)
            cr.arc(w - r, r, rr, 3 * math.pi / 2, math.pi / 2)
            cr.close_path()

        def pill_v(inset):
            cr.new_path()
            rr = r - inset
            cr.arc(r, r, rr, math.pi, 2 * math.pi)
            cr.arc(r, h - r, rr, 0, math.pi)
            cr.close_path()

        shape = pill if self.orientation == "horizontal" else pill_v
        # The pill is painted with a square hole under every dot: each docked
        # dot paints this same surface behind itself, so the seam is invisible
        # and the dot shows whether the WM stacks it above us or below.
        cr.set_source_rgba(*BG_PANEL, 0.96)
        shape(0.5)
        for x, y, sw, sh in self._slot_rects():
            cr.rectangle(x, y, sw, sh)
        cr.set_fill_rule(cairo.FILL_RULE_EVEN_ODD)
        cr.fill()
        cr.set_fill_rule(cairo.FILL_RULE_WINDING)
        cr.set_source_rgb(*BORDER_CLR)
        cr.set_line_width(1)
        shape(0.5); cr.stroke()

        # grip: three dim dots at the leading end
        s = SIZES[self.size]
        gx = s["pad_end"] + GRIP // 2 - 2
        cr.set_source_rgba(*FG_DIM, 0.7)
        for i in range(3):
            if self.orientation == "horizontal":
                cr.arc(gx, h / 2 + (i - 1) * 6, 1.6, 0, 2 * math.pi)
            else:
                cr.arc(w / 2 + (i - 1) * 6, gx, 1.6, 0, 2 * math.pi)
            cr.fill()
        return False

    # -- interaction ------------------------------------------------------------

    def _on_button(self, widget, event):
        if event.button == 1:
            self.dragging = True
            wx, wy = self.get_position()
            self._drag_dx = int(event.x_root) - wx
            self._drag_dy = int(event.y_root) - wy
        elif event.button == 3:
            self._menu(event)

    def _on_button_release(self, widget, event):
        if event.button == 1:
            self.dragging = False
            self._publish()
            self._save_cfg()

    def _on_motion(self, widget, event):
        if self.dragging:
            self.x = int(event.x_root) - self._drag_dx
            self.y = int(event.y_root) - self._drag_dy
            self.move(self.x, self.y)
            self._place_dots()
            if time.time() - self._last_publish > 0.05:   # state sync, not motion
                self._publish()

    def _menu(self, event):
        menu = Gtk.Menu()
        menu.attach_to_widget(self, None)
        self._menu_open = menu

        def item(label, active, cb):
            it = Gtk.CheckMenuItem(label=label)
            it.set_active(active)
            it.set_draw_as_radio(True)
            it.connect("activate", cb)
            menu.append(it)

        item("Horizontal", self.orientation == "horizontal",
             lambda *_: self._set(orientation="horizontal"))
        item("Vertical", self.orientation == "vertical",
             lambda *_: self._set(orientation="vertical"))
        menu.append(Gtk.SeparatorMenuItem())
        item("Compact", self.size == "s", lambda *_: self._set(size="s"))
        item("Cozy", self.size == "m", lambda *_: self._set(size="m"))
        item("Roomy", self.size == "l", lambda *_: self._set(size="l"))
        menu.append(Gtk.SeparatorMenuItem())
        quit_it = Gtk.MenuItem(label="Close bar (release widgets)")
        quit_it.connect("activate", lambda *_: self.destroy())
        menu.append(quit_it)
        menu.show_all()
        menu.popup_at_pointer(event)

    def _set(self, orientation=None, size=None):
        menu = getattr(self, "_menu_open", None)
        if menu:                      # a choice is made: the menu goes away
            menu.popdown()
            self._menu_open = None
        if orientation:
            self.orientation = orientation
        if size:
            self.size = size
        self._relayout(publish=True)
        self._save_cfg()

    def _on_destroy(self, *_):
        try:
            os.remove(BAR_FILE)
        except OSError:
            pass
        Gtk.main_quit()


if __name__ == "__main__":
    # single instance: replace a previous bar if its pid is still alive
    try:
        with open(BAR_FILE) as f:
            old = json.load(f)
        if old.get("pid") and old["pid"] != os.getpid() and _pid_alive(old["pid"]):
            os.kill(old["pid"], 15)
            time.sleep(0.3)
    except (OSError, json.JSONDecodeError, KeyError):
        pass
    BarWindow()
    Gtk.main()
