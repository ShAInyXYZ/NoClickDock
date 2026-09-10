#!/usr/bin/python3
"""
ShinyWidgets core — shared runtime for the status-checker widget family.

Every widget is the same object: a circular always-on-top dot that changes
colour with health, a hover panel with the detail, and toasts on transition.
Only the data source and the panel rows differ, so those are all a widget
subclass has to supply.

  from shinywidget import WidgetApp, Row, Meter, run
  class MyWidget(WidgetApp):
      name = "docker"
      title = "DOCKER"
      def fetch(self): ...      # -> dict, on a background thread
      def rows(self, data): ... # -> [Row | Meter | ...], for the panel
  run(MyWidget)

Design is Emberdeck: warm charcoal, a flat surface ladder with hairlines
instead of shadows, and colour reserved for meaning rather than decoration.

Zero pip dependencies — Python standard library plus system GTK3.
"""

__version__ = "1.2.0"

import json
import math
import os
import platform
import re
import subprocess
import sys
import threading
import time
from datetime import datetime, timezone

IS_WINDOWS = platform.system() == "Windows"

try:
    import gi
    gi.require_version("Gtk", "3.0")
    from gi.repository import Gtk, Gdk, GLib, Pango  # noqa: E402
except (ImportError, ValueError):
    if IS_WINDOWS:
        print(
            "ERROR: GTK3 / PyGObject not found.\n\n"
            "On Windows, install via MSYS2:\n"
            "  1. Install MSYS2 from https://www.msys2.org/\n"
            "  2. In the MSYS2 UCRT64 terminal run:\n"
            "       pacman -S mingw-w64-ucrt-x86_64-python-gobject "
            "mingw-w64-ucrt-x86_64-gtk3\n"
            "  3. Run with the MSYS2 Python:  /ucrt64/bin/python3 <widget>.py",
            file=sys.stderr,
        )
    else:
        print(
            "ERROR: GTK3 / PyGObject not found.\n\n"
            "Install with your package manager:\n"
            "  Debian/Ubuntu:  sudo apt install python3-gi python3-gi-cairo "
            "gir1.2-gtk-3.0\n"
            "  Fedora:         sudo dnf install python3-gobject gtk3\n"
            "  Arch:           sudo pacman -S python-gobject gtk3",
            file=sys.stderr,
        )
    sys.exit(1)

WIDGET_NAME = "claude"
WIDGET_DIR = os.path.join(os.path.expanduser("~"), ".config", "status-widgets")
CORNER_FILE = os.path.join(WIDGET_DIR, "corner.json")
STACK_GAP = 50  # vertical pixels between stacked widgets


def _ensure_widget_dir():
    os.makedirs(WIDGET_DIR, exist_ok=True)


def _register_widget(name, xid=None):
    _ensure_widget_dir()
    path = os.path.join(WIDGET_DIR, f"{name}.json")
    with open(path, "w") as f:
        json.dump({"pid": os.getpid(), "name": name, "xid": xid}, f)


def _unregister_widget(name):
    try:
        os.remove(os.path.join(WIDGET_DIR, f"{name}.json"))
    except OSError:
        pass


def _pid_alive(pid):
    if IS_WINDOWS:
        import ctypes
        kernel32 = ctypes.windll.kernel32
        handle = kernel32.OpenProcess(0x100000, False, pid)
        if handle:
            kernel32.CloseHandle(handle)
            return True
        return False
    else:
        try:
            os.kill(pid, 0)
            return True
        except OSError:
            return False


def _get_active_widgets():
    """Return sorted list of active widget names."""
    _ensure_widget_dir()
    widgets = []
    for fname in sorted(os.listdir(WIDGET_DIR)):
        if fname.endswith(".json") and fname != "corner.json":
            path = os.path.join(WIDGET_DIR, fname)
            try:
                with open(path) as f:
                    data = json.load(f)
                pid = data.get("pid")
                if pid and _pid_alive(pid):
                    widgets.append(data["name"])
                else:
                    os.remove(path)
            except (json.JSONDecodeError, OSError, KeyError):
                pass
    return widgets


def _read_corner():
    try:
        with open(CORNER_FILE) as f:
            return json.load(f)
    except (OSError, json.JSONDecodeError):
        return {"corner_index": -1, "timestamp": 0}


def _write_corner(corner_index):
    _ensure_widget_dir()
    with open(CORNER_FILE, "w") as f:
        json.dump({"corner_index": corner_index, "timestamp": time.time()}, f)


BAR_FILE = os.path.join(WIDGET_DIR, "bar.json")


def _read_bar():
    """SH-widgetbar state: a dict while a live bar owns the layout, None when
    there is no bar, False when the file is mid-write (keep state, retry)."""
    try:
        with open(BAR_FILE) as f:
            raw = f.read()
    except OSError:
        return None
    try:
        bar = json.loads(raw)
    except (json.JSONDecodeError, ValueError):
        return False
    if isinstance(bar, dict) and bar.get("pid") and _pid_alive(bar["pid"]):
        return bar
    return None


def _dock_panel_pos(bar_rect, orientation, x, y, win, pw, ph, mx, my, mw, mh):
    """Panel origin for a docked dot: perpendicular to the bar so it never
    covers the neighbouring dots — below a horizontal bar, beside a vertical
    one, flipping to the other side when the monitor edge is too close."""
    bx, by, bw, bh = bar_rect
    if orientation == "horizontal":
        py = by + bh + 8
        if py + ph > my + mh:
            py = by - ph - 8
        px = x + win // 2 - pw // 2
    else:
        px = bx + bw + 8
        if px + pw > mx + mw:
            px = bx - pw - 8
        py = y
    px = max(mx + 4, min(px, mx + mw - pw - 4))
    py = max(my + 4, min(py, my + mh - ph - 4))
    return px, py


# SH-widgetbar pill surface. A docked dot paints this behind itself because
# the bar leaves a hole under every slot (see sh-widgetbar.py).
_BAR_FILL = (0.055, 0.051, 0.043, 0.96)


def _xid_of(win):
    """X11 window id of a realized Gtk.Window (None on Wayland/unrealized).
    The bar uses it to move docked dots directly, in the same frame as itself."""
    try:
        import gi
        gi.require_version("GdkX11", "3.0")
        from gi.repository import GdkX11  # noqa: F401  (adds get_xid to windows)
        return win.get_window().get_xid()
    except Exception:
        return None



# -- design tokens (Emberdeck) --------------------------------------------
# Warm charcoal, never pure black and never blue-black. Depth comes from a
# flat 3-step surface ladder plus 1px hairlines - no shadows, no gradients.
BG_PANEL = (0.055, 0.051, 0.043)   # #0e0d0b  surface 1
BG_ROW = (0.086, 0.078, 0.067)     # #161411  surface 2
BG_RAISED = (0.118, 0.106, 0.090)  # #1e1b17  surface 3
FG = (0.937, 0.925, 0.902)         # #efece6
FG_DIM = (0.569, 0.545, 0.502)     # #918b80
BORDER_CLR = (0.169, 0.153, 0.133)  # #2b2722  hairline
AMBER = (0.910, 0.529, 0.227)      # #e8873a  the accent
ANTHROPIC = (0.851, 0.412, 0.235)  # #d9693c  brand orange

OK = (0.427, 0.643, 0.416)         # #6da46a  healthy
WARN = (0.851, 0.647, 0.239)       # #d9a53d  degraded
BAD = (0.910, 0.529, 0.227)        # #e8873a  partial failure
CRIT = (0.812, 0.325, 0.278)       # #cf5347  failed
IDLE = (0.435, 0.416, 0.384)       # #6f6a62  unknown / offline

DOT_RADIUS = 14
RING_RADIUS = 18
OUTER_RADIUS = 22
WIN_SIZE = 44        # avatar + bubble fit in this; must match DOT in sh-widgetbar.py
AVATAR_RADIUS = 17   # the brand disc
BUBBLE_RADIUS = 5    # status bubble, top-right, chat-app style

TOAST_DURATION_MS = 4000
DRAG_THRESHOLD = 4   # px of movement before a click counts as a drag

# Fonts are declared with fallbacks: a missing family otherwise lands on a
# serif, which this layout cannot absorb.
MONO = "JetBrains Mono,DejaVu Sans Mono,monospace"
SANS = "Inter,DejaVu Sans,sans-serif"


def rgb_to_hex(c):
    """(r,g,b) floats 0-1 -> '#rrggbb'."""
    return "#{:02x}{:02x}{:02x}".format(
        int(c[0] * 255), int(c[1] * 255), int(c[2] * 255)
    )


FG_HEX = rgb_to_hex(FG)
FG_DIM_HEX = rgb_to_hex(FG_DIM)
AMBER_HEX = rgb_to_hex(AMBER)


def ratio_color(frac, warn=0.75, crit=0.90):
    """Colour for a 0-1 fill ratio: quiet until it matters."""
    if frac >= crit:
        return CRIT
    if frac >= warn:
        return WARN
    return OK


def fmt_bytes(n):
    """1536 -> '1.5K'; byte counts scaled to the largest sensible unit."""
    try:
        n = float(n)
    except (TypeError, ValueError):
        return "-"
    for unit, size in (("T", 1 << 40), ("G", 1 << 30), ("M", 1 << 20), ("K", 1 << 10)):
        if abs(n) >= size:
            v = n / size
            return f"{v:.1f}{unit}" if v < 10 else f"{v:.0f}{unit}"
    return f"{n:.0f}B"


def fmt_count(n):
    """Compact integer: 1.2M, 348K, 912."""
    try:
        n = int(n)
    except (TypeError, ValueError):
        return "-"
    if abs(n) >= 1_000_000:
        return f"{n / 1_000_000:.1f}M"
    if abs(n) >= 1_000:
        return f"{n / 1_000:.0f}K"
    return str(n)


def parse_iso(text):
    """Parse an ISO-8601 timestamp, tolerating what real tools emit.

    datetime.fromisoformat is strict before Python 3.11: it rejects a
    single-digit fractional second (Tailscale emits "...:00.1Z") and the
    bare "Z" suffix. Both appear in the wild, so normalise them first.
    """
    if not text:
        return None
    s = str(text).strip()
    if s.endswith("Z"):
        s = s[:-1] + "+00:00"
    # pad or trim the fractional part to exactly 6 digits
    m = re.match(r"^(.*?)\.(\d+)(.*)$", s)
    if m:
        head, frac, tail = m.groups()
        s = f"{head}.{frac[:6].ljust(6, '0')}{tail}"
    try:
        return datetime.fromisoformat(s)
    except (ValueError, TypeError):
        return None


def time_ago(dt):
    """Short humanised age: '3m ago', '2h ago', '5d ago'."""
    if dt is None:
        return ""
    try:
        delta = datetime.now(timezone.utc) - dt
    except (TypeError, ValueError):
        return ""
    secs = int(delta.total_seconds())
    if secs < 60:
        return "just now"
    if secs < 3600:
        return f"{secs // 60}m ago"
    if secs < 86400:
        return f"{secs // 3600}h ago"
    return f"{secs // 86400}d ago"


def run_cmd(args, timeout=10):
    """Run a command, returning stdout or '' on any failure.

    Widgets shell out to CLIs they don't control, so every failure mode -
    missing binary, non-zero exit, hang - collapses to an empty string and
    the caller degrades to an unknown state rather than crashing.
    """
    try:
        p = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
    except (OSError, subprocess.SubprocessError):
        return ""
    if p.returncode != 0:
        return p.stdout or ""
    return p.stdout or ""


# -- SVG path rendering (for widget glyphs) -------------------------------
def _normalize_arc_flags(d):
    """Pre-process SVG path to separate concatenated arc flags."""
    out = []
    i = 0
    in_arc = False
    arc_param = 0
    while i < len(d):
        ch = d[i]
        if ch.isalpha() and ch != 'e' and ch != 'E':
            in_arc = ch in ('a', 'A')
            arc_param = 0
            out.append(ch)
            i += 1
            continue
        if not in_arc:
            out.append(ch)
            i += 1
            continue
        if ch in (' ', ',', '\t', '\n', '\r'):
            out.append(ch)
            i += 1
            continue
        if arc_param % 7 in (3, 4):
            out.append(ch)
            out.append(',')
            arc_param += 1
            i += 1
        else:
            j = i
            if j < len(d) and d[j] in '+-':
                j += 1
            has_dot = False
            while j < len(d) and (d[j].isdigit() or (d[j] == '.' and not has_dot)):
                if d[j] == '.':
                    has_dot = True
                j += 1
            if j < len(d) and d[j] in ('e', 'E'):
                j += 1
                if j < len(d) and d[j] in '+-':
                    j += 1
                while j < len(d) and d[j].isdigit():
                    j += 1
            out.append(d[i:j])
            out.append(',')
            arc_param += 1
            i = j
    return ''.join(out)


def _parse_svg_path(d):
    """Parse SVG path 'd' attribute into (command, numbers) tuples."""
    d = _normalize_arc_flags(d)
    tokens = re.findall(
        r'[MmLlHhVvCcSsQqTtAaZz]|[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?', d
    )
    cmds = []
    i = 0
    cmd = None
    while i < len(tokens):
        if tokens[i].isalpha():
            cmd = tokens[i]
            i += 1
        nums = []
        while i < len(tokens) and not tokens[i].isalpha():
            nums.append(float(tokens[i]))
            i += 1
        if cmd:
            cmds.append((cmd, nums))
    return cmds


def _svg_arc_to_cairo(cr, rx, ry, rotation, large_arc, sweep, ex, ey, sx, sy):
    """Convert SVG arc params to Cairo arcs (simplified for circular/near-circular)."""
    # For small icons, approximate: treat as circular arc using average radius
    r = (abs(rx) + abs(ry)) / 2
    if r < 1e-6:
        cr.line_to(ex, ey)
        return
    dx = ex - sx
    dy = ey - sy
    dist = math.hypot(dx, dy)
    if dist < 1e-6:
        return
    # clamp radius
    if r < dist / 2:
        r = dist / 2
    # find center
    mx, my = (sx + ex) / 2, (sy + ey) / 2
    d = math.sqrt(max(0, r * r - (dist / 2) ** 2))
    nx, ny = -dy / dist, dx / dist
    if large_arc != sweep:
        cx_, cy_ = mx + d * nx, my + d * ny
    else:
        cx_, cy_ = mx - d * nx, my - d * ny
    a1 = math.atan2(sy - cy_, sx - cx_)
    a2 = math.atan2(ey - cy_, ex - cx_)
    if sweep:
        cr.arc(cx_, cy_, r, a1, a2)
    else:
        cr.arc_negative(cx_, cy_, r, a1, a2)


def draw_svg_logo(cr, path_d, cx, cy, size, viewbox=24):
    """Draw an SVG path centered at (cx, cy) scaled to fit 'size' pixels.

    Uses the even-odd fill rule so a subpath inside another cuts a hole
    (a ring, a spindle) regardless of which direction it is wound.
    """
    cr.set_fill_rule(1)   # CAIRO_FILL_RULE_EVEN_ODD
    scale = size / viewbox
    ox = cx - size / 2
    oy = cy - size / 2

    cmds = _parse_svg_path(path_d)
    x, y = 0.0, 0.0  # current point
    sx, sy = 0.0, 0.0  # subpath start
    lx2, ly2 = 0.0, 0.0  # last control point (for S/s)

    for cmd, nums in cmds:
        n = nums
        if cmd == 'M':
            for j in range(0, len(n), 2):
                x, y = n[j], n[j + 1]
                if j == 0:
                    cr.move_to(ox + x * scale, oy + y * scale)
                    sx, sy = x, y
                else:
                    cr.line_to(ox + x * scale, oy + y * scale)
        elif cmd == 'm':
            for j in range(0, len(n), 2):
                x += n[j]; y += n[j + 1]
                if j == 0:
                    cr.move_to(ox + x * scale, oy + y * scale)
                    sx, sy = x, y
                else:
                    cr.line_to(ox + x * scale, oy + y * scale)
        elif cmd == 'L':
            for j in range(0, len(n), 2):
                x, y = n[j], n[j + 1]
                cr.line_to(ox + x * scale, oy + y * scale)
        elif cmd == 'l':
            for j in range(0, len(n), 2):
                x += n[j]; y += n[j + 1]
                cr.line_to(ox + x * scale, oy + y * scale)
        elif cmd == 'H':
            for v in n:
                x = v
                cr.line_to(ox + x * scale, oy + y * scale)
        elif cmd == 'h':
            for v in n:
                x += v
                cr.line_to(ox + x * scale, oy + y * scale)
        elif cmd == 'V':
            for v in n:
                y = v
                cr.line_to(ox + x * scale, oy + y * scale)
        elif cmd == 'v':
            for v in n:
                y += v
                cr.line_to(ox + x * scale, oy + y * scale)
        elif cmd == 'C':
            for j in range(0, len(n), 6):
                x1, y1 = n[j], n[j+1]
                x2, y2 = n[j+2], n[j+3]
                x, y = n[j+4], n[j+5]
                cr.curve_to(
                    ox + x1 * scale, oy + y1 * scale,
                    ox + x2 * scale, oy + y2 * scale,
                    ox + x * scale, oy + y * scale,
                )
                lx2, ly2 = x2, y2
        elif cmd == 'c':
            for j in range(0, len(n), 6):
                x1, y1 = x + n[j], y + n[j+1]
                x2, y2 = x + n[j+2], y + n[j+3]
                x += n[j+4]; y += n[j+5]
                cr.curve_to(
                    ox + x1 * scale, oy + y1 * scale,
                    ox + x2 * scale, oy + y2 * scale,
                    ox + x * scale, oy + y * scale,
                )
                lx2, ly2 = x2, y2
        elif cmd == 'S':
            for j in range(0, len(n), 4):
                x1, y1 = 2 * x - lx2, 2 * y - ly2
                x2, y2 = n[j], n[j+1]
                x, y = n[j+2], n[j+3]
                cr.curve_to(
                    ox + x1 * scale, oy + y1 * scale,
                    ox + x2 * scale, oy + y2 * scale,
                    ox + x * scale, oy + y * scale,
                )
                lx2, ly2 = x2, y2
        elif cmd == 's':
            for j in range(0, len(n), 4):
                x1, y1 = 2 * x - lx2, 2 * y - ly2
                x2, y2 = x + n[j], y + n[j+1]
                x += n[j+2]; y += n[j+3]
                cr.curve_to(
                    ox + x1 * scale, oy + y1 * scale,
                    ox + x2 * scale, oy + y2 * scale,
                    ox + x * scale, oy + y * scale,
                )
                lx2, ly2 = x2, y2
        elif cmd == 'A':
            for j in range(0, len(n), 7):
                ex, ey = n[j+5], n[j+6]
                _svg_arc_to_cairo(
                    cr, n[j], n[j+1], n[j+2], int(n[j+3]), int(n[j+4]),
                    ox + ex * scale, oy + ey * scale,
                    ox + x * scale, oy + y * scale,
                )
                x, y = ex, ey
        elif cmd == 'a':
            for j in range(0, len(n), 7):
                ex, ey = x + n[j+5], y + n[j+6]
                _svg_arc_to_cairo(
                    cr, n[j] * scale, n[j+1] * scale, n[j+2],
                    int(n[j+3]), int(n[j+4]),
                    ox + ex * scale, oy + ey * scale,
                    ox + x * scale, oy + y * scale,
                )
                x, y = ex, ey
        elif cmd in ('Z', 'z'):
            cr.close_path()
            x, y = sx, sy


# -- panel row primitives -------------------------------------------------
# A widget describes its panel declaratively; the core renders it. This keeps
# every widget's panel visually identical without copying layout code.

class Section:
    """A small caps heading, optionally in the alert colour."""

    def __init__(self, label, alert=False):
        self.label = label
        self.alert = alert


class Action:
    """A small text button on a row: `stop`, `start`, `remove`.

    Reading a panel never needs a click; changing the world does. With
    `confirm` set the first click only arms the button (its label becomes the
    confirm text, coloured amber or - when `danger` - red) and a second click
    within a few seconds runs the callback. Moving on un-arms it. The callback
    runs on the GTK thread: hand anything slow to dot.run_action().
    """

    def __init__(self, label, callback, confirm=None, danger=False, key=None):
        self.label = label
        self.callback = callback
        self.confirm = confirm
        self.danger = danger
        # Identity that survives a panel rebuild. Rows are re-created from
        # scratch on every poll, so armed state cannot live in the widget.
        self.key = key


class Row:
    """name ......... value, with an optional status dot on the left."""

    def __init__(self, name, value="", color=None, dot=None, dim_value=False,
                 actions=None, sub=""):
        self.name = name
        self.value = value
        self.color = color          # colour for the value text
        self.dot = dot              # colour for the leading dot, None = none
        self.dim_value = dim_value  # render value quiet even if dot is coloured
        self.actions = actions or []
        self.sub = sub              # one quiet line under the name (a diagnostic)


class Meter:
    """label, percentage, and a horizontal fill bar. For anything bounded."""

    def __init__(self, label, frac, value="", color=None, note=""):
        self.label = label
        self.frac = max(0.0, min(1.0, frac or 0.0))
        self.value = value
        self.color = color or ratio_color(self.frac)
        self.note = note


class Note:
    """A quiet line of supporting text."""

    def __init__(self, text, color=None, wrap=False, indent=0):
        self.text = text
        self.color = color
        self.wrap = wrap
        self.indent = indent


class Grid:
    """Every item, in columns. The point is completeness at a glance.

    A status panel that summarises ("15 containers up") and then names only
    the failures forces the reader to go and look elsewhere for the rest.
    This renders them all, compactly, with a dot carrying the state.
    """

    def __init__(self, items, columns=2):
        # items: list of (label, color) or (label, color, value)
        self.items = items
        self.columns = max(1, columns)


class Alert:
    """An incident-style block: title, meta line, optional body."""

    def __init__(self, title, meta="", body="", color=None, actions=None, mono_body=False):
        self.title = title
        self.meta = meta
        self.body = body
        self.color = color or CRIT
        self.actions = actions or []
        self.mono_body = mono_body   # body is a log line, not prose


class Limit:
    """One line per bounded window: name | meter | percent | reset time.

    The Claude and Codex widgets read the same way: how much of a plan is
    gone, and when it comes back. Percent is 0-100.
    """

    def __init__(self, label, pct, resets="", color=None):
        self.label = label
        self.pct = max(0.0, min(100.0, float(pct or 0)))
        self.resets = resets
        self.color = color or ratio_color(self.pct / 100.0)


class Tiles:
    """A strip of equal stat tiles: small key above a large value."""

    def __init__(self, items):
        self.items = items          # [(key, value)]


class Chips:
    """Items in their own small boxes, N per row, the grid kept rectangular.

    items: (label, color) draws a status dot in `color` and a plain label;
    (label, color, value) colours the label itself and right-aligns a quiet
    value - for a model split, say. A fourth element is a dot colour when
    both are wanted.
    """

    def __init__(self, items, columns=3):
        self.items = items
        self.columns = max(1, columns)


# -- the dot --------------------------------------------------------------
class DotWindow(Gtk.Window):
    """The small circular always-on-top dot."""

    def __init__(self, app):
        super().__init__(type=Gtk.WindowType.TOPLEVEL)
        self.app = app
        self.set_title(app.title.title() + " Status")
        self.set_decorated(False)
        self.set_keep_above(True)
        self.set_skip_taskbar_hint(True)
        self.set_skip_pager_hint(True)
        self.set_type_hint(Gdk.WindowTypeHint.UTILITY)
        self.set_default_size(WIN_SIZE, WIN_SIZE)
        self.set_size_request(WIN_SIZE, WIN_SIZE)
        self.set_resizable(False)
        self.move(20, 20)

        screen = self.get_screen()
        visual = screen.get_rgba_visual()
        if visual:
            self.set_visual(visual)
        self.set_app_paintable(True)
        self.set_can_focus(True)
        self.set_accept_focus(True)

        self.connect("draw", self._on_draw)
        self.connect("button-press-event", self._on_button)
        self.connect("button-release-event", self._on_button_release)
        self.connect("motion-notify-event", self._on_motion)
        self.connect("enter-notify-event", self._on_enter)
        self.connect("leave-notify-event", self._on_leave)
        self.connect("key-press-event", self._on_key_press)
        self.connect("destroy", Gtk.main_quit)
        self.set_events(
            Gdk.EventMask.BUTTON_PRESS_MASK
            | Gdk.EventMask.BUTTON_RELEASE_MASK
            | Gdk.EventMask.POINTER_MOTION_MASK
            | Gdk.EventMask.ENTER_NOTIFY_MASK
            | Gdk.EventMask.LEAVE_NOTIFY_MASK
            | Gdk.EventMask.KEY_PRESS_MASK
        )

        self.color = IDLE
        self.data = {}
        self.pulse_phase = 0.0
        self.panel = None
        self.dragging = False
        self._drag_offset_x = 0
        self._drag_offset_y = 0
        self._corner_margin = 8
        self._press_x = 0
        self._press_y = 0
        self._moved = False
        self._toast = None
        self._prev_state = None
        self._prev_alerts = set()

        app.dot = self       # before the poll thread starts: fetch() may use it
        _register_widget(app.name)
        self.connect("realize", lambda w: _register_widget(app.name, _xid_of(w)))
        self._last_corner_ts = 0
        self._docked = False      # True while SH-widgetbar owns our position
        self._last_bar_ts = 0
        self._bar_rect = None
        self._bar_target = None
        self._bar_orientation = "horizontal"
        self.connect("destroy", lambda w: _unregister_widget(app.name))

        GLib.timeout_add(50, self._tick_pulse)
        GLib.timeout_add(50, self._watch_corner)   # 50 ms: tracks a bar drag smoothly
        threading.Thread(target=self._poll_loop, daemon=True).start()

    # -- drawing ----------------------------------------------------------

    def _on_draw(self, widget, cr):
        cr.set_operator(0); cr.paint(); cr.set_operator(2)   # clear to transparent
        if self._docked:   # the bar has a hole here: paint its surface under us
            cr.set_source_rgba(*_BAR_FILL)
            cr.paint()
        cx = cy = WIN_SIZE / 2
        R = AVATAR_RADIUS
        sr, sg, sb = self.color          # status colour lives only in the bubble

        # Something wrong: a slow breathing ring around the avatar, nothing else.
        if self.app.is_alert(self.data):
            pulse = 0.5 + 0.5 * math.sin(self.pulse_phase)
            cr.set_source_rgba(sr, sg, sb, 0.10 + 0.25 * pulse)
            cr.set_line_width(1)
            cr.arc(cx, cy, R + 3, 0, 2 * math.pi)
            cr.stroke()

        # brand disc, with a faint hairline so dark brands still read on the pill
        cr.set_source_rgb(*self.app.brand)
        cr.arc(cx, cy, R, 0, 2 * math.pi)
        cr.fill()
        cr.set_source_rgba(0.937, 0.925, 0.902, 0.14)
        cr.set_line_width(1)
        cr.arc(cx, cy, R - 0.5, 0, 2 * math.pi)
        cr.stroke()

        # the logo, in its brand foreground
        cr.save()
        cr.arc(cx, cy, R, 0, 2 * math.pi)
        cr.clip()
        cr.set_source_rgb(*self.app.logo_fg)
        draw_svg_logo(cr, self.app.glyph, cx, cy, R * 2 * self.app.logo_scale, self.app.glyph_viewbox)
        cr.fill()
        cr.restore()

        # status bubble, top-right, cut out of the disc like a chat presence dot
        bx, by = cx + R * 0.70, cy - R * 0.70
        if self._docked:
            cr.set_source_rgba(*_BAR_FILL)
        else:
            cr.set_operator(0)           # free-floating: punch a transparent gap
        cr.arc(bx, by, BUBBLE_RADIUS + 2, 0, 2 * math.pi)
        cr.fill()
        cr.set_operator(2)
        cr.set_source_rgb(sr, sg, sb)
        cr.arc(bx, by, BUBBLE_RADIUS, 0, 2 * math.pi)
        cr.fill()
        return False

    def _tick_pulse(self):
        # Only repaint while the breathing ring is actually animating.
        if self.app.is_alert(self.data):
            self.pulse_phase += 0.08
            self.queue_draw()
        return True

    # -- polling ----------------------------------------------------------

    def _poll_loop(self):
        while True:
            try:
                data = self.app.fetch()
            except Exception:
                data = {"error": True}
            GLib.idle_add(self._apply, data)
            time.sleep(self.app.poll_secs)

    def _apply(self, data):
        state = self.app.state(data)
        if self._prev_state is not None and state != self._prev_state:
            msg = self.app.transition_message(self._prev_state, state, data)
            if msg:
                self.show_toast(msg, self.app.state_color(state))

        alerts = set(self.app.alert_ids(data))
        if self._prev_alerts:
            for a in alerts - self._prev_alerts:
                text = self.app.alert_message(a, data)
                if text:
                    self.show_toast(text, CRIT)
        self._prev_alerts = alerts
        self._prev_state = state

        self.data = data
        self.color = self.app.state_color(state)
        self.queue_draw()
        if self.panel and self.panel.get_visible():
            self.panel.update_data(data)
            self._place_panel()   # a grown panel would otherwise run off-screen
        return False

    def show_toast(self, message, color=None):
        if self._toast:
            try:
                self._toast.destroy()
            except Exception:
                pass
        self._toast = ToastWindow(self, message, color)
        self._toast.popup()

    # -- mouse / keyboard -------------------------------------------------

    def _on_button(self, widget, event):
        if event.button == 1:
            self._close_panel()
            self.dragging = True
            self._moved = False
            self._press_x = int(event.x_root)
            self._press_y = int(event.y_root)
            wx, wy = self.get_position()
            self._drag_offset_x = int(event.x_root) - wx
            self._drag_offset_y = int(event.y_root) - wy
        elif event.button == 3:
            self._show_menu(event)

    # -- right-click menu ---------------------------------------------------

    def _show_menu(self, event):
        """Refresh, the widget's own actions, and Quit last behind a separator."""
        self._close_panel()
        menu = Gtk.Menu()
        menu.attach_to_widget(self, None)
        self._menu = menu   # keep a reference while it is up

        def add(label, cb):
            it = Gtk.MenuItem(label=label)
            it.connect("activate", lambda *_: cb())
            menu.append(it)

        add("Refresh now", self._refresh_now)
        for label, cb in self.app.menu_items(self):
            add(label, cb)
        if self.app.click_url:
            add(f"Open {self.app.title.title()} page", lambda: self.app.on_click(self))
        menu.append(Gtk.SeparatorMenuItem())
        add(f"Quit {self.app.title.title()} widget", self.destroy)
        menu.show_all()
        menu.popup_at_pointer(event)

    def _refresh_now(self):
        def work():
            try:
                data = self.app.fetch()
            except Exception:
                data = {"error": True}
            GLib.idle_add(self._apply, data)
        threading.Thread(target=work, daemon=True).start()

    refresh = _refresh_now

    def run_action(self, fn, done=None):
        """Run fn() off the GTK thread, then refresh. fn returns (ok, message);
        the message becomes a toast in the state colour. `done` runs on the
        GTK thread first, for the widget to clear any "stopping..." marker."""
        def work():
            try:
                ok, msg = fn()
            except Exception as e:      # an action must never take the dot down
                ok, msg = False, str(e)[:80]
            def finish():
                if done:
                    done(ok, msg)
                if msg:
                    self.show_toast(msg, OK if ok else CRIT)
                self._refresh_now()
                return False
            GLib.idle_add(finish)
        threading.Thread(target=work, daemon=True).start()

    def repaint_panel(self):
        """Re-render the open panel from the last data (after a page switch
        or an action changed what a row should say)."""
        if self.panel and self.panel.get_visible():
            self.panel.update_data(self.data)
            self._place_panel()

    def copy_text(self, text):
        Gtk.Clipboard.get(Gdk.SELECTION_CLIPBOARD).set_text(text, -1)
        self.show_toast(f"Copied  {text}", OK)

    def _on_button_release(self, widget, event):
        if event.button == 1:
            self.dragging = False
            if not self._moved:
                self.app.on_click(self)

    def _on_motion(self, widget, event):
        if self.dragging and not self._docked:  # the bar owns docked positions
            nx = int(event.x_root) - self._drag_offset_x
            ny = int(event.y_root) - self._drag_offset_y
            if (abs(int(event.x_root) - self._press_x) > DRAG_THRESHOLD
                    or abs(int(event.y_root) - self._press_y) > DRAG_THRESHOLD):
                self._moved = True
            self.move(nx, ny)

    def _on_key_press(self, widget, event):
        if event.keyval == Gdk.KEY_grave:   # ~ / ` key
            if not self._docked:            # corner cycling is a free-mode move
                self._cycle_corner()
            return True
        return False

    def _cycle_corner(self):
        self._close_panel()
        n = Gdk.Display.get_default().get_n_monitors() * 2
        corner = _read_corner()
        idx = (corner["corner_index"] + 1) % n
        _write_corner(idx)
        self._apply_corner(idx)

    def _watch_corner(self):
        # SH-widgetbar takes precedence over free placement: while a live bar
        # publishes a slot for us, we sit in it and ignore corner cycling.
        bar = _read_bar()
        if bar is False:            # bar.json mid-write (bar being dragged): hold
            return True
        slot = (bar or {}).get("slots", {}).get(self.app.name)
        if slot:
            target = (int(slot[0]) - WIN_SIZE // 2, int(slot[1]) - WIN_SIZE // 2)
            if target != self._bar_target:
                self._bar_target = target
                self._close_panel()
                self.move(*target)
                try:
                    self.get_window().raise_()  # dots sit on top of the pill
                except Exception:
                    pass
            self._bar_rect = bar.get("rect")
            self._bar_orientation = bar.get("orientation", "horizontal")
            self._docked = True
            # Swallow corner changes while docked: the bar decides where we
            # are, and leaving it later must leave us where we stand.
            self._last_corner_ts = max(self._last_corner_ts,
                                       _read_corner()["timestamp"])
            return True
        self._docked = False
        self._bar_target = None
        corner = _read_corner()
        if corner["timestamp"] > self._last_corner_ts:
            self._last_corner_ts = corner["timestamp"]
            if corner["corner_index"] >= 0:
                self._apply_corner(corner["corner_index"])
        return True

    def _apply_corner(self, corner_index):
        self._close_panel()
        widgets = _get_active_widgets()
        try:
            my_order = widgets.index(self.app.name)
        except ValueError:
            my_order = 0

        display = Gdk.Display.get_default()
        positions = []
        for i in range(display.get_n_monitors()):
            geo = display.get_monitor(i).get_geometry()
            positions.append((geo.x, geo.y, geo.width, geo.height))
            positions.append((geo.x, geo.y, geo.width, geo.height))

        idx = corner_index % len(positions)
        mx, my, sw, sh = positions[idx]
        is_right = idx % 2 == 1
        m = self._corner_margin
        n_widgets = len(widgets) if widgets else 1
        total_h = (n_widgets - 1) * STACK_GAP + WIN_SIZE
        cy = my + (sh - total_h) // 2 + my_order * STACK_GAP
        bx = mx + sw - WIN_SIZE - m if is_right else mx + m
        self.move(bx, cy)

    # -- panel ------------------------------------------------------------

    def _on_enter(self, widget, event):
        if not self.dragging:
            self._show_panel()

    def _on_leave(self, widget, event):
        GLib.timeout_add(200, self._check_close_panel)

    def _show_panel(self):
        if self.panel and self.panel.get_visible():
            return
        if self.panel:
            self.panel.destroy()
        self.panel = PanelWindow(self)
        self.panel.update_data(self.data)
        self.panel.show_all()
        self._place_panel()

    def _place_panel(self):
        """Put the panel beside the dot, clamped to the monitor. Uses the
        natural size so it is right even before GTK's next layout pass."""
        x, y = self.get_position()
        nat = self.panel.get_preferred_size()[1]
        pw, ph = nat.width, nat.height

        display = Gdk.Display.get_default()
        try:
            geo = display.get_monitor_at_point(
                x + WIN_SIZE // 2, y + WIN_SIZE // 2).get_geometry()
            mx, my, mw, mh = geo.x, geo.y, geo.width, geo.height
        except Exception:
            scr = self.get_screen()
            mx, my, mw, mh = 0, 0, scr.get_width(), scr.get_height()

        if self._docked and self._bar_rect:
            px, py = _dock_panel_pos(self._bar_rect, self._bar_orientation, x, y,
                                     WIN_SIZE, pw, ph, mx, my, mw, mh)
        else:
            px = x + WIN_SIZE + 6
            if px + pw > mx + mw:
                px = x - pw - 6
            px = max(mx + 4, min(px, mx + mw - pw - 4))
            py = max(my + 4, min(y, my + mh - ph - 4))
        self.panel.move(px, py)

    def _close_panel(self):
        if self.panel and self.panel.get_visible():
            self.panel.hide()

    def _check_close_panel(self):
        if not self.panel:
            return False
        display = Gdk.Display.get_default()
        seat = display.get_default_seat()
        ptr = seat.get_pointer()
        _, px, py = ptr.get_position()

        for win in (self, self.panel):
            if not win.get_visible():
                continue
            wx, wy = win.get_position()
            w = win.get_allocated_width()
            h = win.get_allocated_height()
            if wx - 4 <= px <= wx + w + 4 and wy - 4 <= py <= wy + h + 4:
                return False
        self._close_panel()
        return False


# -- toast ----------------------------------------------------------------
class ToastWindow(Gtk.Window):
    """Small notification popup that auto-dismisses."""

    def __init__(self, parent_dot, message, color=None):
        super().__init__(type=Gtk.WindowType.POPUP)
        self.set_decorated(False)
        self.set_keep_above(True)
        self.set_skip_taskbar_hint(True)
        self.set_type_hint(Gdk.WindowTypeHint.NOTIFICATION)
        self.set_app_paintable(True)
        self.parent_dot = parent_dot
        self._opacity = 1.0
        self._color = color or FG

        visual = self.get_screen().get_rgba_visual()
        if visual:
            self.set_visual(visual)
        self.connect("draw", self._on_draw)

        self._label = Gtk.Label()
        self._label.set_markup(
            f'<span font_family="{MONO}" font_size="9000" '
            f'foreground="{FG_HEX}">{GLib.markup_escape_text(message)}</span>'
        )
        self._label.set_margin_start(14)
        self._label.set_margin_end(12)
        self._label.set_margin_top(8)
        self._label.set_margin_bottom(8)
        self.add(self._label)

    def popup(self, duration_ms=TOAST_DURATION_MS):
        self.show_all()
        x, y = self.parent_dot.get_position()
        th = self.get_allocated_height()
        self.move(x + WIN_SIZE + 8, y + (WIN_SIZE - th) // 2)
        GLib.timeout_add(duration_ms, self._start_fade)

    def _start_fade(self):
        GLib.timeout_add(30, self._fade_tick)
        return False

    def _fade_tick(self):
        self._opacity -= 0.06
        if self._opacity <= 0:
            self.destroy()
            return False
        self.queue_draw()
        return True

    def _on_draw(self, widget, cr):
        alloc = self.get_allocation()
        cr.set_source_rgba(*BG_PANEL, 0.95 * self._opacity)
        self._rounded_rect(cr, 0, 0, alloc.width, alloc.height, 3)
        cr.fill()
        cr.set_source_rgba(*BORDER_CLR, self._opacity)
        cr.set_line_width(1)
        self._rounded_rect(cr, 0.5, 0.5, alloc.width - 1, alloc.height - 1, 3)
        cr.stroke()
        # semantic edge: the toast's own status colour as a 2px left rule
        cr.set_source_rgba(*self._color, self._opacity)
        cr.rectangle(0, 1, 2, alloc.height - 2)
        cr.fill()
        self._label.set_opacity(self._opacity)
        return False

    @staticmethod
    def _rounded_rect(cr, x, y, w, h, r):
        cr.new_sub_path()
        cr.arc(x + w - r, y + r, r, -math.pi / 2, 0)
        cr.arc(x + w - r, y + h - r, r, 0, math.pi / 2)
        cr.arc(x + r, y + h - r, r, math.pi / 2, math.pi)
        cr.arc(x + r, y + r, r, math.pi, 3 * math.pi / 2)
        cr.close_path()


# -- panel ----------------------------------------------------------------
_CSS = ("""
window { background-color: rgba(14,13,11,0.97); border: 1px solid #2b2722;
         border-radius: 4px; }
.panel-title { font-family: "Teko", "DejaVu Sans", sans-serif; font-size: 18px;
               font-weight: bold; color: #efece6; }
.panel-section { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace;
                 font-size: 8px; font-weight: bold; color: #918b80; }
.panel-section-alert { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace;
                       font-size: 8px; font-weight: bold; color: #cf5347; }
.row { background-color: #161411; border-radius: 3px; padding: 6px 10px;
       margin: 1px 0; }
.footer { font-family: "Inter", "DejaVu Sans", sans-serif; font-size: 8px;
          color: #6f6a60; }
.sep { background-color: #2b2722; min-height: 1px; }
.tile-key { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace;
            font-size: 9px; color: #918b80; }
.tile-val { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace;
            font-size: 12px; color: #efece6; }
.subline { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace;
           font-size: 8px; color: #918b80; }
button.act { background: none; background-image: none; border: 1px solid #2b2722;
             border-radius: 3px; box-shadow: none; padding: 0px 5px; min-height: 0;
             min-width: 0; margin: 0; outline: none; text-shadow: none; }
button.act:hover { background-color: #1e1b17; border-color: #6f6a62; }
button.act:active { background-color: #2b2722; }
label.tab { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace;
            font-size: 9px; font-weight: bold; color: #6f6a62;
            padding: 3px 1px 4px 1px; border-bottom: 2px solid transparent; }
label.tab-active { color: #efece6; border-bottom: 2px solid #e8873a; }
scrolledwindow, viewport { background: none; background-color: transparent;
                           border: none; }
scrollbar { background: none; background-color: transparent; border: none; }
scrollbar slider { background-color: #2b2722; border-radius: 2px; min-width: 4px;
                   border: none; }
scrollbar slider:hover { background-color: #6f6a62; }
""").encode()

_css_loaded = False


class _CappedBox(Gtk.Box):
    """A vertical box whose natural width never exceeds `cap` (0 = free).
    GTK sizes a non-scrolling window to its content's natural width; this
    is how a panel keeps one width across pages and lets labels ellipsize."""

    def __init__(self, cap):
        super().__init__(orientation=Gtk.Orientation.VERTICAL, spacing=0)
        self.cap = cap
        if cap:
            self.set_size_request(cap, -1)

    def do_get_preferred_width(self):
        mn, nat = Gtk.Box.do_get_preferred_width(self)
        if self.cap:
            mn = max(mn, self.cap)
            nat = max(mn, min(nat, self.cap))
        return mn, nat


class PanelWindow(Gtk.Window):
    """The hover detail panel. Renders the primitives a widget returns."""

    def __init__(self, parent):
        super().__init__(type=Gtk.WindowType.TOPLEVEL)
        self.parent_dot = parent
        self.app = parent.app
        self.set_decorated(False)
        self.set_keep_above(True)
        self.set_skip_taskbar_hint(True)
        self.set_skip_pager_hint(True)
        self.set_type_hint(Gdk.WindowTypeHint.UTILITY if IS_WINDOWS
                           else Gdk.WindowTypeHint.TOOLTIP)
        self.set_resizable(False)

        visual = self.get_screen().get_rgba_visual()
        if visual:
            self.set_visual(visual)
        self.set_app_paintable(True)
        self.connect("draw", self._on_draw_bg)
        self.connect("leave-notify-event", self._on_leave)
        self.set_events(Gdk.EventMask.LEAVE_NOTIFY_MASK)

        box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=0)
        for side in ("start", "end", "top", "bottom"):
            getattr(box, f"set_margin_{side}")(1)

        # Title, badge and page tabs stay put; the body scrolls when a widget
        # has more to say than fits; the footer stays put.
        cap = self.app.panel_width - 34 if self.app.panel_width else 0
        self.header = _CappedBox(cap)
        self.header.set_margin_start(16)
        self.header.set_margin_end(16)
        self.header.set_margin_top(12)
        box.pack_start(self.header, False, False, 0)

        self.scroller = Gtk.ScrolledWindow()
        self.scroller.set_policy(Gtk.PolicyType.NEVER, Gtk.PolicyType.AUTOMATIC)
        self.scroller.set_propagate_natural_height(True)
        self.scroller.set_propagate_natural_width(True)
        self.scroller.set_overlay_scrolling(True)
        self.scroller.set_max_content_height(self._max_body_height())
        # A fixed panel width means the body's natural width is the cap, and
        # rows ellipsize to fit rather than pushing the window wider.
        self.inner = _CappedBox(cap)
        self.inner.set_margin_start(16)
        self.inner.set_margin_end(16)
        self.scroller.add(self.inner)
        box.pack_start(self.scroller, True, True, 0)

        self.footer_box = _CappedBox(cap)
        self.footer_box.set_margin_start(16)
        self.footer_box.set_margin_end(16)
        self.footer_box.set_margin_bottom(12)
        box.pack_start(self.footer_box, False, False, 0)
        self.add(box)
        self._tab_timer = None
        # A pending dwell timer must not outlive the window it would act on.
        self.connect("destroy", self._cancel_timers)

        global _css_loaded
        if not _css_loaded:
            provider = Gtk.CssProvider()
            provider.load_from_data(_CSS)
            Gtk.StyleContext.add_provider_for_screen(
                Gdk.Screen.get_default(), provider,
                Gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
            _css_loaded = True

    def _on_draw_bg(self, widget, cr):
        alloc = self.get_allocation()
        cr.set_source_rgba(*BG_PANEL, 0.97)
        cr.rectangle(0, 0, alloc.width, alloc.height)
        cr.fill()
        cr.set_source_rgba(*BORDER_CLR, 1)
        cr.set_line_width(1)
        cr.rectangle(0.5, 0.5, alloc.width - 1, alloc.height - 1)
        cr.stroke()
        return False

    def _on_leave(self, widget, event):
        GLib.timeout_add(200, self.parent_dot._check_close_panel)

    def _max_body_height(self):
        """The body may grow to the widget's cap or 70 % of the monitor,
        whichever is smaller; past that it scrolls."""
        try:
            x, y = self.parent_dot.get_position()
            geo = Gdk.Display.get_default().get_monitor_at_point(
                x + WIN_SIZE // 2, y + WIN_SIZE // 2).get_geometry()
            return max(200, min(self.app.panel_max_height, int(geo.height * 0.70)))
        except Exception:
            return self.app.panel_max_height

    def update_data(self, data):
        # Keep the reader's place across a poll refresh. `data` is a fresh dict
        # every fetch, so identity would never match - the page identity is
        # what decides whether this is the same view being redrawn.
        adj = self.scroller.get_vadjustment()
        view = (self.app.page, id(self.app))
        keep = adj.get_value() if view == self._last_view else 0.0
        self._last_view = view
        for box in (self.header, self.inner, self.footer_box):
            for child in box.get_children():
                box.remove(child)

        state = self.app.state(data)
        scolor = self.app.state_color(state)

        title = Gtk.Label(label=self.app.title)
        title.get_style_context().add_class("panel-title")
        title.set_halign(Gtk.Align.START)
        self.header.pack_start(title, False, False, 0)

        badge_text = self.app.state_label(state, data)
        if badge_text:
            badge = Gtk.Label()
            badge.set_markup(
                f'<span font_family="{MONO}" font_size="7000" font_weight="bold" '
                f'background="{rgb_to_hex(scolor)}" foreground="#0e0d0b">'
                f'  {GLib.markup_escape_text(badge_text.upper())}  </span>'
            )
            badge.set_halign(Gtk.Align.START)
            self.header.pack_start(badge, False, False, 2)

        pages = list(self.app.pages(data) or [])
        if pages:
            if self.app.page not in pages:
                self.app.page = pages[0]
            self.header.pack_start(self._tab_strip(pages), False, False, 6)
        else:
            self.inner.set_margin_top(0)

        try:
            items = self.app.rows(data) or []
        except Exception:
            items = [Note("Unable to render status", CRIT)]
        for item in items:
            self._render(item)
        self.inner.set_margin_bottom(4)

        sep = Gtk.Separator()
        sep.get_style_context().add_class("sep")
        self.footer_box.pack_start(sep, False, False, 8)
        footer = Gtk.Label(label=self.app.footer(data))
        footer.get_style_context().add_class("footer")
        footer.set_halign(Gtk.Align.START)
        footer.set_line_wrap(True)
        if self.app.panel_width:
            footer.set_size_request(self.app.panel_width - 34, -1)
        else:
            footer.set_max_width_chars(110)
        footer.set_xalign(0)
        self.footer_box.pack_start(footer, False, False, 0)
        self.show_all()

        # A data refresh must not throw the reader back to the top.
        def restore():
            adj.set_value(min(keep, max(0.0, adj.get_upper() - adj.get_page_size())))
            return False
        GLib.idle_add(restore)

    _last_view = None

    # -- pages -------------------------------------------------------------

    def _tab_strip(self, pages):
        """Page names in a row. Hovering one switches - reading needs no
        click - after a short dwell so crossing the strip does not flip
        pages; a click switches at once."""
        strip = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=16)
        for name in pages:
            lbl = Gtk.Label(label=name)
            lbl.get_style_context().add_class("tab")
            if name == self.app.page:
                lbl.get_style_context().add_class("tab-active")
            ev = Gtk.EventBox()
            ev.set_visible_window(False)
            ev.add(lbl)
            ev.add_events(Gdk.EventMask.ENTER_NOTIFY_MASK
                          | Gdk.EventMask.BUTTON_PRESS_MASK)
            ev.connect("enter-notify-event", lambda w, e, n=name: self._tab_hover(n))
            ev.connect("button-press-event", lambda w, e, n=name: self._go_page(n))
            strip.pack_start(ev, False, False, 0)
        return strip

    def _cancel_timers(self, *_a):
        if self._tab_timer:
            GLib.source_remove(self._tab_timer)
            self._tab_timer = None

    def _tab_hover(self, name):
        if self._tab_timer:
            GLib.source_remove(self._tab_timer)
        self._tab_timer = GLib.timeout_add(160, self._tab_dwell, name)
        return False

    def _tab_dwell(self, name):
        self._tab_timer = None
        if not self.get_visible() or self.get_window() is None:
            return False          # panel closed while the pointer was crossing
        # still over the strip? (the pointer may just have crossed it)
        ptr = Gdk.Display.get_default().get_default_seat().get_pointer()
        _, px, py = ptr.get_position()
        wx, wy = self.get_position()
        hy = self.header.get_allocation()
        if wy + hy.y <= py <= wy + hy.y + hy.height:
            self._go_page(name)
        return False

    def _go_page(self, name):
        if name != self.app.page:
            self.app.page = name
            self._last_view = None            # a new page starts at the top
            self.parent_dot.repaint_panel()
        return False

    # -- actions -----------------------------------------------------------

    def _action_button(self, action):
        """A button whose armed state lives on the app, keyed by the action.

        The panel rebuilds every row on each poll, so a closure over this
        button would lose the arming - and with it the user's first click -
        every time the data refreshed. The app outlives the render.
        """
        btn = Gtk.Button()
        btn.get_style_context().add_class("act")
        btn.set_relief(Gtk.ReliefStyle.NONE)
        btn.set_can_focus(False)
        lbl = Gtk.Label()
        btn.add(lbl)
        key = action.key or f"{action.label}\x00{action.confirm}"

        def paint():
            armed = self.app.is_armed(key)
            colour = (CRIT if action.danger else AMBER) if armed else FG_DIM
            text = action.confirm if armed else action.label
            lbl.set_markup(
                f'<span font_family="{MONO}" font_size="7500" font_weight="bold" '
                f'foreground="{rgb_to_hex(colour)}">{GLib.markup_escape_text(text)}</span>')

        def clicked(_b):
            if action.confirm and not self.app.is_armed(key):
                self.app.arm(key, self.parent_dot)
                paint()
                return
            self.app.disarm(key)
            paint()
            action.callback()

        btn.connect("clicked", clicked)
        paint()
        return btn

    def _actions_box(self, actions):
        box = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=2)
        box.set_valign(Gtk.Align.CENTER)
        for a in actions:
            box.pack_start(self._action_button(a), False, False, 0)
        return box

    # -- primitive renderers ---------------------------------------------

    def _render(self, item):
        if isinstance(item, Section):
            sep = Gtk.Separator()
            sep.get_style_context().add_class("sep")
            self.inner.pack_start(sep, False, False, 8)
            lbl = Gtk.Label(label=item.label)
            lbl.get_style_context().add_class(
                "panel-section-alert" if item.alert else "panel-section")
            lbl.set_halign(Gtk.Align.START)
            self.inner.pack_start(lbl, False, False, 4)

        elif isinstance(item, Row):
            row = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=8)
            row.get_style_context().add_class("row")
            if item.dot:
                da = Gtk.DrawingArea()
                da.set_size_request(10, 10)
                da.set_valign(Gtk.Align.CENTER)
                da.connect("draw", lambda w, cr, c=item.dot: self._draw_dot(cr, c))
                row.pack_start(da, False, False, 0)
            nm = Gtk.Label()
            nm.set_markup(
                f'<span font_family="{MONO}" font_size="9000" '
                f'foreground="{FG_HEX}">{GLib.markup_escape_text(item.name)}</span>')
            nm.set_xalign(0)      # FILL + xalign 0: takes the free width, then ellipsizes
            nm.set_ellipsize(Pango.EllipsizeMode.MIDDLE)
            nm.set_max_width_chars(30 if not item.actions else 14)
            if item.sub:
                col = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=1)
                col.pack_start(nm, False, False, 0)
                sb = Gtk.Label(label=item.sub)
                sb.get_style_context().add_class("subline")
                sb.set_xalign(0)
                sb.set_ellipsize(Pango.EllipsizeMode.END)
                sb.set_max_width_chars(20)
                col.pack_start(sb, False, False, 0)
                row.pack_start(col, True, True, 0)
            else:
                row.pack_start(nm, True, True, 0)
            if item.actions:
                row.pack_end(self._actions_box(item.actions), False, False, 0)
            if item.value:
                vhex = FG_DIM_HEX if item.dim_value else rgb_to_hex(item.color or FG)
                vl = Gtk.Label()
                vl.set_markup(
                    f'<span font_family="{MONO}" font_size="8500" '
                    f'foreground="{vhex}">{GLib.markup_escape_text(item.value)}</span>')
                vl.set_halign(Gtk.Align.END)
                vl.set_valign(Gtk.Align.CENTER)
                row.pack_end(vl, False, False, 0)
            self.inner.pack_start(row, False, False, 0)

        elif isinstance(item, Meter):
            box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=4)
            box.get_style_context().add_class("row")
            head = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=8)
            lb = Gtk.Label()
            lb.set_markup(
                f'<span font_family="{MONO}" font_size="8500" '
                f'foreground="{FG_DIM_HEX}">{GLib.markup_escape_text(item.label)}</span>')
            lb.set_halign(Gtk.Align.START)
            head.pack_start(lb, False, False, 0)
            if item.value:
                vv = Gtk.Label()
                vv.set_markup(
                    f'<span font_family="{MONO}" font_size="10000" '
                    f'foreground="{rgb_to_hex(item.color)}">'
                    f'{GLib.markup_escape_text(item.value)}</span>')
                vv.set_halign(Gtk.Align.END)
                head.pack_end(vv, False, False, 0)
            box.pack_start(head, False, False, 0)

            meter = Gtk.DrawingArea()
            meter.set_size_request(-1, 10)
            meter.connect("draw", lambda w, cr, f=item.frac, c=item.color:
                          self._draw_meter(w, cr, f, c))
            box.pack_start(meter, False, False, 0)

            if item.note:
                nt = Gtk.Label()
                nt.set_markup(
                    f'<span font_family="{SANS}" font_size="7500" '
                    f'foreground="#6f6a60">{GLib.markup_escape_text(item.note)}</span>')
                nt.set_halign(Gtk.Align.START)
                box.pack_start(nt, False, False, 0)
            self.inner.pack_start(box, False, False, 0)

        elif isinstance(item, Note):
            nl = Gtk.Label()
            nl.set_markup(
                f'<span font_family="{MONO}" font_size="8000" '
                f'foreground="{rgb_to_hex(item.color or FG_DIM)}">'
                f'{GLib.markup_escape_text(item.text)}</span>')
            nl.set_halign(Gtk.Align.START)
            nl.set_xalign(0)
            if item.indent:
                nl.set_margin_start(item.indent)
            if item.wrap:
                nl.set_line_wrap(True)
                nl.set_max_width_chars(44)
            self.inner.pack_start(nl, False, False, 4)

        elif isinstance(item, Grid):
            if not item.items:
                return
            grid = Gtk.Grid()
            grid.set_column_spacing(14)
            grid.set_row_spacing(0)
            grid.set_column_homogeneous(True)
            grid.get_style_context().add_class("row")
            n = len(item.items)
            cols = item.columns
            rows = (n + cols - 1) // cols
            for idx, entry in enumerate(item.items):
                label, color = entry[0], entry[1]
                value = entry[2] if len(entry) > 2 else ""
                cell = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=6)
                da = Gtk.DrawingArea()
                da.set_size_request(7, 7)
                da.set_valign(Gtk.Align.CENTER)
                da.connect("draw", lambda w, cr, c=color: self._draw_small_dot(cr, c))
                cell.pack_start(da, False, False, 0)
                lb = Gtk.Label()
                lb.set_markup(
                    f'<span font_family="{MONO}" font_size="8000" '
                    f'foreground="{FG_HEX}">{GLib.markup_escape_text(str(label))}</span>')
                lb.set_halign(Gtk.Align.START)
                lb.set_xalign(0)
                lb.set_ellipsize(Pango.EllipsizeMode.MIDDLE)
                lb.set_max_width_chars(26)
                cell.pack_start(lb, True, True, 0)
                if value:
                    vl = Gtk.Label()
                    vl.set_markup(
                        f'<span font_family="{MONO}" font_size="7500" '
                        f'foreground="{FG_DIM_HEX}">{GLib.markup_escape_text(str(value))}</span>')
                    vl.set_halign(Gtk.Align.END)
                    cell.pack_end(vl, False, False, 0)
                grid.attach(cell, idx // rows, idx % rows, 1, 1)
            self.inner.pack_start(grid, False, False, 0)

        elif isinstance(item, Limit):
            lrow = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=10)
            lrow.get_style_context().add_class("row")
            nm = Gtk.Label()
            nm.set_markup(
                f'<span font_family="{MONO}" font_size="8500" '
                f'foreground="{FG_DIM_HEX}">{GLib.markup_escape_text(item.label)}</span>')
            nm.set_size_request(72, -1)   # px: width_chars would measure the default font
            nm.set_xalign(0)
            lrow.pack_start(nm, False, False, 0)
            meter = Gtk.DrawingArea()
            meter.set_size_request(120, 10)
            meter.set_valign(Gtk.Align.CENTER)
            meter.connect("draw", lambda w, cr, f=item.pct / 100.0, c=item.color:
                          self._draw_meter(w, cr, f, c))
            lrow.pack_start(meter, True, True, 0)
            pv = Gtk.Label()
            pv.set_markup(
                f'<span font_family="{MONO}" font_size="10000" '
                f'foreground="{rgb_to_hex(item.color)}">{item.pct:.0f}%</span>')
            pv.set_size_request(44, -1)
            pv.set_xalign(1)
            lrow.pack_start(pv, False, False, 0)
            rs = Gtk.Label()
            rs.set_markup(
                f'<span font_family="{SANS}" font_size="7500" foreground="#6f6a60">'
                f'{("resets " + GLib.markup_escape_text(item.resets)) if item.resets else ""}</span>')
            rs.set_size_request(150, -1)
            rs.set_xalign(1)
            lrow.pack_start(rs, False, False, 0)
            self.inner.pack_start(lrow, False, False, 0)

        elif isinstance(item, Tiles):
            strip = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=3)
            strip.set_homogeneous(True)
            for key, val in item.items:
                tile = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=1)
                tile.get_style_context().add_class("row")
                k = Gtk.Label(label=str(key))
                k.get_style_context().add_class("tile-key")
                k.set_xalign(0)
                v = Gtk.Label(label=str(val))
                v.get_style_context().add_class("tile-val")
                v.set_xalign(0)
                tile.pack_start(k, False, False, 0)
                tile.pack_start(v, False, False, 0)
                strip.pack_start(tile, True, True, 0)
            self.inner.pack_start(strip, False, False, 3)

        elif isinstance(item, Chips):
            cols = item.columns
            for i in range(0, len(item.items), cols):
                line = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=3)
                line.set_homogeneous(True)
                chunk = item.items[i:i + cols]
                for entry in chunk:
                    label, color = entry[0], entry[1]
                    value = entry[2] if len(entry) > 2 else ""
                    dot = entry[3] if len(entry) > 3 else (None if value else color)
                    chip = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=7)
                    chip.get_style_context().add_class("row")
                    if dot:
                        da = Gtk.DrawingArea()
                        da.set_size_request(8, 8)
                        da.set_valign(Gtk.Align.CENTER)
                        da.connect("draw", lambda w, cr, c=dot: self._draw_dot(cr, c))
                        chip.pack_start(da, False, False, 0)
                    lhex = rgb_to_hex(color) if value else FG_HEX
                    lb = Gtk.Label()
                    lb.set_markup(
                        f'<span font_family="{MONO}" font_size="9000" '
                        f'foreground="{lhex}">{GLib.markup_escape_text(str(label))}</span>')
                    lb.set_xalign(0)
                    lb.set_ellipsize(Pango.EllipsizeMode.END)
                    chip.pack_start(lb, True, True, 0)
                    if value:
                        vl = Gtk.Label()
                        vl.set_markup(
                            f'<span font_family="{MONO}" font_size="8500" '
                            f'foreground="{FG_DIM_HEX}">{GLib.markup_escape_text(str(value))}</span>')
                        vl.set_xalign(1)
                        chip.pack_end(vl, False, False, 0)
                    line.pack_start(chip, True, True, 0)
                for _ in range(cols - len(chunk)):   # keep the grid rectangular
                    line.pack_start(Gtk.Box(), True, True, 0)
                self.inner.pack_start(line, False, False, 0)

        elif isinstance(item, Alert):
            box = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=8)
            box.get_style_context().add_class("row")
            bar = Gtk.DrawingArea()
            bar.set_size_request(3, -1)
            bar.connect("draw", lambda w, cr, c=item.color: self._draw_bar(w, cr, c))
            box.pack_start(bar, False, False, 0)
            txt = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=2)
            t = Gtk.Label()
            t.set_markup(
                f'<span font_family="{MONO}" font_size="9500" '
                f'foreground="{FG_HEX}">{GLib.markup_escape_text(item.title)}</span>')
            t.set_halign(Gtk.Align.START)
            t.set_xalign(0)
            t.set_line_wrap(True)
            t.set_max_width_chars(40)
            txt.pack_start(t, False, False, 0)
            if item.meta:
                mm = Gtk.Label()
                mm.set_markup(
                    f'<span font_family="{SANS}" font_size="8000" '
                    f'foreground="{rgb_to_hex(item.color)}">'
                    f'{GLib.markup_escape_text(item.meta)}</span>')
                mm.set_halign(Gtk.Align.START)
                txt.pack_start(mm, False, False, 0)
            if item.body:
                bb = Gtk.Label()
                fam = MONO if item.mono_body else SANS
                bb.set_markup(
                    f'<span font_family="{fam}" font_size="7800" '
                    f'foreground="#6f6a60">{GLib.markup_escape_text(item.body)}</span>')
                bb.set_halign(Gtk.Align.START)
                bb.set_xalign(0)
                bb.set_line_wrap(True)
                bb.set_max_width_chars(42)
                txt.pack_start(bb, False, False, 0)
            box.pack_start(txt, True, True, 0)
            if item.actions:
                acts = self._actions_box(item.actions)
                acts.set_valign(Gtk.Align.START)
                box.pack_end(acts, False, False, 0)
            self.inner.pack_start(box, False, False, 0)

    @staticmethod
    def _draw_dot(cr, color):
        cr.set_source_rgb(*color)
        cr.arc(5, 5, 4, 0, 2 * math.pi)
        cr.fill()
        return False

    @staticmethod
    def _draw_small_dot(cr, color):
        cr.set_source_rgb(*color)
        cr.arc(3.5, 3.5, 3, 0, 2 * math.pi)
        cr.fill()
        return False

    @staticmethod
    def _draw_bar(widget, cr, color):
        cr.set_source_rgb(*color)
        cr.rectangle(0, 0, 3, widget.get_allocation().height)
        cr.fill()
        return False

    @staticmethod
    def _draw_meter(widget, cr, frac, color):
        alloc = widget.get_allocation()
        w, h = alloc.width, alloc.height
        r = h / 2

        def pill(width):   # rounded ends; a fill shorter than its height stays a dot
            cr.new_path()
            cr.arc(r, r, r, math.pi / 2, 3 * math.pi / 2)
            cr.arc(max(r, width - r), r, r, 3 * math.pi / 2, math.pi / 2)
            cr.close_path()

        cr.set_source_rgb(*BG_RAISED)
        pill(w)
        cr.fill()
        if frac > 0:
            cr.set_source_rgb(*color)
            pill(max(h, w * frac))
            cr.fill()
        return False


# -- the widget contract --------------------------------------------------
class WidgetApp:
    """Subclass this, implement fetch() and rows(), call run().

    Everything else - the dot, the panel chrome, toasts, dragging, the `~`
    multi-monitor stacking, transparency - comes from the core.
    """

    name = "widget"          # registration key; also the stacking order key
    title = "WIDGET"         # panel heading
    poll_secs = 10
    glyph = None             # SVG path of the brand mark, drawn inside the disc
    glyph_viewbox = 24
    brand = BG_RAISED        # disc colour (the brand's), constant — state is the bubble
    logo_fg = FG             # mark colour on the disc
    logo_scale = 0.62        # mark size relative to the disc diameter
    click_url = None         # if set, clicking the dot opens this
    dot = None               # the DotWindow, once running: run_action, repaint_panel
    panel_width = None       # fixed panel width in px; None = fit the content
    panel_max_height = 640   # the body scrolls past this (or 70 % of the monitor)
    page = None              # the page rows() is being asked for, when pages() is set

    # -- data -------------------------------------------------------------

    def fetch(self):
        """Collect state. Runs on a background thread; must not touch GTK."""
        raise NotImplementedError

    def rows(self, data):
        """Return panel primitives (Section / Row / Meter / Note / Alert / Grid)
        for the current page (self.page) when pages() is non-empty."""
        raise NotImplementedError

    def pages(self, data):
        """Page names for the tab strip, or [] for a single-page panel.
        Hovering a tab switches; the core keeps the choice in self.page."""
        return []

    # -- armed confirm buttons --------------------------------------------
    # Held here rather than on the panel: the panel rebuilds its rows on every
    # poll, and an arming that vanished with the render would silently eat the
    # user's first click on a destructive action.

    _armed = None          # key -> GLib timer id
    ARM_SECS = 4

    def is_armed(self, key):
        return bool(self._armed) and key in self._armed

    def arm(self, key, dot=None):
        if self._armed is None:
            self._armed = {}
        self.disarm(key)

        def expire():
            self._armed.pop(key, None)
            if dot is not None:
                dot.repaint_panel()
            return False

        self._armed[key] = GLib.timeout_add(self.ARM_SECS * 1000, expire)

    def disarm(self, key):
        if self._armed:
            timer = self._armed.pop(key, None)
            if timer:
                GLib.source_remove(timer)

    def disarm_all(self):
        for key in list(self._armed or {}):
            self.disarm(key)

    # -- state ------------------------------------------------------------

    def state(self, data):
        """Short state key, e.g. 'ok' / 'warn' / 'crit' / 'idle'."""
        return "idle" if not data else data.get("state", "ok")

    def state_color(self, state):
        return {"ok": OK, "warn": WARN, "bad": BAD,
                "crit": CRIT, "idle": IDLE}.get(state, IDLE)

    def state_label(self, state, data):
        """Badge text under the title. Return '' for no badge."""
        return state.upper()

    def is_alert(self, data):
        """True while the dot should breathe."""
        return self.state(data) in ("warn", "bad", "crit")

    # -- notifications ----------------------------------------------------

    def transition_message(self, old, new, data):
        return self.state_label(new, data)

    def alert_ids(self, data):
        """Stable ids of current problems; new ones raise a toast."""
        return []

    def alert_message(self, alert_id, data):
        return str(alert_id)

    # -- chrome -----------------------------------------------------------

    def footer(self, data):
        parts = []
        ts = (data or {}).get("last_check")
        if ts is not None:
            try:
                parts.append("Last checked: " + ts.strftime("%H:%M:%S"))
            except AttributeError:
                pass
        if self.click_url:
            parts.append("Click: open")
        parts += ["~: move", "Right-click: menu"]
        return "   ·   ".join(parts)

    def menu_items(self, dot):
        """Extra right-click entries: [(label, callback)]. dot.data holds the
        last fetch, dot.copy_text() puts a string on the clipboard."""
        return []

    def on_click(self, dot):
        if self.click_url:
            import webbrowser
            try:
                webbrowser.open(self.click_url)
            except Exception:
                pass


def run(app_cls):
    """Entry point: instantiate the widget and start the GTK loop."""
    app = app_cls()
    dot = DotWindow(app)     # sets app.dot before starting its poll thread
    dot.show_all()
    try:
        Gtk.main()
    except KeyboardInterrupt:
        pass
