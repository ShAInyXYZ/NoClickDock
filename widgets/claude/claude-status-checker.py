#!/usr/bin/python3
"""
Claude Status Light — always-on-top circular indicator for Anthropic API health.

Uses GTK3 + Cairo for true RGBA transparency (no square background).
Hover to see full status panel. Drag to reposition. Right-click to quit.

Monitors https://status.claude.com
"""

__version__ = "1.0.1"

import json
import math
import os
import platform
import re
import subprocess
import sys
import threading
import time
import webbrowser
from datetime import datetime, timezone
from urllib.request import urlopen, Request
from urllib.error import URLError

IS_WINDOWS = platform.system() == "Windows"

try:
    import gi
    gi.require_version("Gtk", "3.0")
    from gi.repository import Gtk, Gdk, GLib  # noqa: E402
except (ImportError, ValueError) as e:
    if IS_WINDOWS:
        print(
            "ERROR: GTK3 / PyGObject not found.\n\n"
            "On Windows, install via MSYS2:\n"
            "  1. Install MSYS2 from https://www.msys2.org/\n"
            "  2. In MSYS2 UCRT64 terminal run:\n"
            "       pacman -S mingw-w64-ucrt-x86_64-python-gobject mingw-w64-ucrt-x86_64-gtk3\n"
            "  3. Run this script using the MSYS2 Python:\n"
            "       /ucrt64/bin/python3 claude-status-checker.py\n\n"
            "Alternatively, install via pip (requires GTK3 runtime):\n"
            "  pip install PyGObject\n"
            "  and install GTK3 runtime from https://github.com/tschoonj/GTK-for-Windows-Runtime-Environment-Installer/releases",
            file=sys.stderr,
        )
    else:
        print(
            "ERROR: GTK3 / PyGObject not found.\n\n"
            "Install with your package manager:\n"
            "  Debian/Ubuntu:  sudo apt install python3-gi python3-gi-cairo gir1.2-gtk-3.0\n"
            "  Fedora:         sudo dnf install python3-gobject gtk3\n"
            "  Arch:           sudo pacman -S python-gobject gtk3",
            file=sys.stderr,
        )
    sys.exit(1)

# -- widget coordination (shared across status-checker widgets) -----------
WIDGET_NAME = "claude"
WIDGET_DIR = (os.path.join(os.environ.get("APPDATA")
                           or os.path.join(os.path.expanduser("~"), "AppData", "Roaming"),
                           "NoClickDock")
              if platform.system() == "Windows" else
              os.path.join(os.path.expanduser("~"), ".config", "status-widgets"))
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


# -- URLs -----------------------------------------------------------------
STATUS_URL = "https://status.claude.com/api/v2/status.json"
COMPONENTS_URL = "https://status.claude.com/api/v2/components.json"
INCIDENTS_URL = "https://status.claude.com/api/v2/incidents/unresolved.json"
STATUS_PAGE_URL = "https://status.claude.com"
POLL_SECS = 30
DRAG_THRESHOLD = 4  # px of movement before a click counts as a drag

# -- design tokens (Emberdeck) --------------------------------------------
# Warm charcoal, never pure black and never blue-black. Depth comes from a
# flat 3-step surface ladder plus 1px hairlines - no shadows, no gradients.
# AMBER is the single accent, reserved for interactive/semantic signal.
BG_PANEL = (0.055, 0.051, 0.043)   # #0e0d0b  surface 1
BG_ROW = (0.086, 0.078, 0.067)     # #161411  surface 2
BG_RAISED = (0.118, 0.106, 0.090)  # #1e1b17  surface 3
FG = (0.937, 0.925, 0.902)         # #efece6
FG_DIM = (0.569, 0.545, 0.502)     # #918b80
BORDER_CLR = (0.169, 0.153, 0.133)  # #2b2722  hairline
AMBER = (0.910, 0.529, 0.227)      # #e8873a  the one accent
ANTHROPIC = (0.851, 0.412, 0.235)  # #d9693c  brand orange (the default dot)
RED = (0.71, 0.24, 0.21)

DOT_RADIUS = 14
RING_RADIUS = 18
OUTER_RADIUS = 22
WIN_SIZE = 44        # avatar + bubble fit in this; must match DOT in sh-widgetbar.py
AVATAR_RADIUS = 17   # the brand disc
BUBBLE_RADIUS = 5    # status bubble, top-right, chat-app style

STATUS_COLORS = {
    "operational":          (0.427, 0.643, 0.416),  # #6da46a muted sage
    "degraded_performance": (0.851, 0.647, 0.239),  # #d9a53d
    "partial_outage":       (0.910, 0.529, 0.227),  # #e8873a ember
    "major_outage":         (0.812, 0.325, 0.278),  # #cf5347
    "under_maintenance":    (0.510, 0.545, 0.635),  # #828ba2 slate
}
INDICATOR_COLORS = {
    "none":     (0.427, 0.643, 0.416),   # healthy: the same green every widget uses
    "minor":    (0.851, 0.647, 0.239),
    "major":    (0.812, 0.325, 0.278),
    "critical": (0.749, 0.243, 0.204),
    "unknown":  (0.435, 0.416, 0.384),
}
STATUS_LABELS = {
    "operational":          "OPERATIONAL",
    "degraded_performance": "DEGRADED",
    "partial_outage":       "PARTIAL OUTAGE",
    "major_outage":         "MAJOR OUTAGE",
    "under_maintenance":    "MAINTENANCE",
}
COMPONENT_SHORT = {
    "claude.ai": "claude.ai",
    "Claude Console (platform.claude.com)": "console",
    "Claude API (api.anthropic.com)": "API",
    "Claude Code": "claude code",
    "Claude Cowork": "cowork",
    "Claude for Government": "gov",
}
IMPACT_COLORS = {
    "none":     (0.427, 0.643, 0.416),
    "minor":    (0.851, 0.647, 0.239),
    "major":    (0.812, 0.325, 0.278),
    "critical": (0.749, 0.243, 0.204),
}

TOAST_DURATION_MS = 4000


def rgb_to_hex(c):
    """(r,g,b) floats 0-1 -> '#rrggbb'."""
    return "#{:02x}{:02x}{:02x}".format(
        int(c[0] * 255), int(c[1] * 255), int(c[2] * 255)
    )


AMBER_HEX = rgb_to_hex(AMBER)


# -- local Claude Code usage ---------------------------------------------
# Reads the session transcripts Claude Code writes under ~/.claude/projects.
# Purely local: no network, no credentials, no pip deps. Each assistant
# message carries a `usage` record, so token spend can be totalled per window.
PROJECTS_DIR = os.path.join(os.path.expanduser("~"), ".claude", "projects")
USAGE_REFRESH_SECS = 120   # transcripts are large; refresh far slower than status
USAGE_WINDOW_DAYS = 7      # oldest bucket we report

# Plan limits come from the Claude Code CLI itself (`claude -p /usage`), the
# only authoritative source for percent-of-limit. It costs a few seconds per
# call, so it runs on its own thread on a slow interval.
LIMITS_REFRESH_SECS = 300
LIMITS_TIMEOUT_SECS = 45
LIMIT_WARN_PCT = 75        # amber at/above this
LIMIT_CRIT_PCT = 90        # red at/above this

LIMIT_RE = re.compile(
    r"^Current\s+(session|week)\s*(?:\(([^)]+)\))?\s*:\s*"
    r"(\d+(?:\.\d+)?)\s*%\s*used"
    r"(?:\s*[\u00b7\u2022-]\s*resets\s+(.+?))?\s*$",
    re.IGNORECASE,
)


def read_limits():
    """Percent-of-plan-limit per window, via the Claude Code CLI.

    Returns a list of {label, pct, resets} - session first, then the
    all-model week, then any per-model window (e.g. Fable). Returns an
    empty list if the CLI is missing, times out, or changes its wording;
    the panel then simply omits the section.
    """
    try:
        proc = subprocess.run(
            ["claude", "-p", "/usage"],
            capture_output=True, text=True,
            timeout=LIMITS_TIMEOUT_SECS,
        )
    except (OSError, subprocess.SubprocessError):
        return []
    rows = []
    for line in (proc.stdout or "").splitlines():
        m = LIMIT_RE.match(line.strip())
        if not m:
            continue
        scope, qual, pct, resets = m.groups()
        if qual and qual.lower().startswith("all"):
            label = "WEEK"
        elif qual:
            label = qual.upper()
        else:
            label = "SESSION" if scope.lower() == "session" else "WEEK"
        try:
            val = float(pct)
        except (TypeError, ValueError):
            continue
        rows.append({
            "label": label,
            "pct": val,
            "resets": _short_reset((resets or "").strip()),
        })
    return rows


def _short_reset(text):
    """'Sep 5, 3:59pm (Europe/Warsaw)' -> 'Sep 5, 3:59pm'."""
    if not text:
        return ""
    return re.sub(r"\s*\([^)]*\)\s*$", "", text).strip()


def limit_color(pct):
    """Quiet until it matters, red when it's nearly gone.

    The healthy dot is brand orange, so the warning step here is yellow
    rather than amber - two different meanings must not share a hue.
    """
    if pct >= LIMIT_CRIT_PCT:
        return STATUS_COLORS["major_outage"]
    if pct >= LIMIT_WARN_PCT:
        return STATUS_COLORS["degraded_performance"]
    return STATUS_COLORS["operational"]

MODEL_SHORT = {
    "claude-opus-5": "opus 5",
    "claude-sonnet-5": "sonnet 5",
    "claude-fable-5": "fable 5",
    "claude-haiku-4-5-20251001": "haiku 4.5",
}


def _short_model(model):
    """Human label for a model id, tolerating ids we don't know yet."""
    if model in MODEL_SHORT:
        return MODEL_SHORT[model]
    m = re.sub(r"^(us\.)?anthropic\.", "", model or "")
    m = re.sub(r"-\d{8}$", "", m)
    m = re.sub(r"^claude-", "", m)
    return m.replace("-", " ") or "unknown"


def fmt_tokens(n):
    """Compact token count: 1.2M, 348K, 912."""
    if n >= 1_000_000_000:
        return f"{n / 1_000_000_000:.1f}B"
    if n >= 1_000_000:
        return f"{n / 1_000_000:.1f}M"
    if n >= 1_000:
        return f"{n / 1_000:.0f}K"
    return str(n)


def read_usage():
    """Aggregate local token usage into 1h / 24h / 7d buckets.

    Returns a dict with per-window totals, 24h session count, and the
    top models by 24h spend. Never raises: a missing or unreadable
    transcript directory just yields zeros.
    """
    now = datetime.now(timezone.utc)
    usage = {
        "1h": 0, "24h": 0, "7d": 0,
        "sessions": 0, "models": [], "available": False,
    }
    if not os.path.isdir(PROJECTS_DIR):
        return usage

    cutoff = now.timestamp() - USAGE_WINDOW_DAYS * 86400
    sessions = set()
    models = {}
    try:
        entries = os.listdir(PROJECTS_DIR)
    except OSError:
        return usage

    for proj in entries:
        pdir = os.path.join(PROJECTS_DIR, proj)
        try:
            names = os.listdir(pdir)
        except OSError:
            continue
        for fname in names:
            if not fname.endswith(".jsonl"):
                continue
            path = os.path.join(pdir, fname)
            try:
                # skip files untouched within the window entirely
                if os.path.getmtime(path) < cutoff:
                    continue
                fh = open(path, errors="ignore")
            except OSError:
                continue
            with fh:
                for line in fh:
                    # cheap prefilter before paying for json.loads
                    if '"usage"' not in line:
                        continue
                    try:
                        rec = json.loads(line)
                    except (json.JSONDecodeError, ValueError):
                        continue
                    if rec.get("type") != "assistant":
                        continue
                    msg = rec.get("message") or {}
                    u = msg.get("usage")
                    if not isinstance(u, dict):
                        continue
                    ts = rec.get("timestamp")
                    if not ts:
                        continue
                    t = parse_iso(ts)
                    if t is None:
                        continue
                    total = (
                        u.get("input_tokens", 0) or 0
                    ) + (
                        u.get("output_tokens", 0) or 0
                    ) + (
                        u.get("cache_creation_input_tokens", 0) or 0
                    ) + (
                        u.get("cache_read_input_tokens", 0) or 0
                    )
                    if not total:
                        continue
                    age = (now - t).total_seconds()
                    if age < 0:
                        age = 0
                    if age < 3600:
                        usage["1h"] += total
                    if age < 86400:
                        usage["24h"] += total
                        sid = rec.get("sessionId")
                        if sid:
                            sessions.add(sid)
                        model = msg.get("model") or "unknown"
                        if not model.startswith("<"):   # skip <synthetic>
                            models[model] = models.get(model, 0) + total
                    if age < USAGE_WINDOW_DAYS * 86400:
                        usage["7d"] += total

    usage["sessions"] = len(sessions)
    usage["models"] = sorted(models.items(), key=lambda kv: -kv[1])[:3]
    usage["available"] = usage["7d"] > 0
    return usage


# -- Claude logo SVG path (viewBox 0 0 24 24) ----------------------------
CLAUDE_BRAND = (0.851, 0.467, 0.341)   # #D97757
CLAUDE_IVORY = (0.941, 0.933, 0.902)
CLAUDE_LOGO_PATH = (
    "m4.7144 15.9555 4.7174-2.6471.079-.2307-.079-.1275h-.2307l-.7893-.0486-2.6956-.0729-2.3375"
    "-.0971-2.2646-.1214-.5707-.1215-.5343-.7042.0546-.3522.4797-.3218.686.0608 1.5179.1032 2.2"
    "767.1578 1.6514.0972 2.4468.255h.3886l.0546-.1579-.1336-.0971-.1032-.0972L6.973 9.8356l-2."
    "55-1.6879-1.3356-.9714-.7225-.4918-.3643-.4614-.1578-1.0078.6557-.7225.8803.0607.2246.0607"
    ".8925.686 1.9064 1.4754 2.4893 1.8336.3643.3035.1457-.1032.0182-.0728-.164-.2733-1.3539-2."
    "4467-1.445-2.4893-.6435-1.032-.17-.6194c-.0607-.255-.1032-.4674-.1032-.7285L6.287.1335 6.6"
    "997 0l.9957.1336.419.3642.6192 1.4147 1.0018 2.2282 1.5543 3.0296.4553.8985.2429.8318.091."
    "255h.1579v-.1457l.1275-1.706.2368-2.0947.2307-2.6957.0789-.7589.3764-.9107.7468-.4918.5828"
    ".2793.4797.686-.0668.4433-.2853 1.8517-.5586 2.9021-.3643 1.9429h.2125l.2429-.2429.9835-1."
    "3053 1.6514-2.0643.7286-.8196.85-.9046.5464-.4311h1.0321l.759 1.1293-.34 1.1657-1.0625 1.3"
    "478-.8804 1.1414-1.2628 1.7-.7893 1.36.0729.1093.1882-.0183 2.8535-.607 1.5421-.2794 1.839"
    "6-.3157.8318.3886.091.3946-.3278.8075-1.967.4857-2.3072.4614-3.4364.8136-.0425.0304.0486.0"
    "607 1.5482.1457.6618.0364h1.621l3.0175.2247.7892.522.4736.6376-.079.4857-1.2142.6193-1.639"
    "3-.3886-3.825-.9107-1.3113-.3279h-.1822v.1093l1.0929 1.0686 2.0035 1.8092 2.5075 2.3314.12"
    "75.5768-.3218.4554-.34-.0486-2.2039-1.6575-.85-.7468-1.9246-1.621h-.1275v.17l.4432.6496 2."
    "3436 3.5214.1214 1.0807-.17.3521-.6071.2125-.6679-.1214-1.3721-1.9246L14.38 17.959l-1.1414"
    "-1.9428-.1397.079-.674 7.2552-.3156.3703-.7286.2793-.6071-.4614-.3218-.7468.3218-1.4753.38"
    "86-1.9246.3157-1.53.2853-1.9004.17-.6314-.0121-.0425-.1397.0182-1.4328 1.9672-2.1796 2.944"
    "6-1.7243 1.8456-.4128.164-.7164-.3704.0667-.6618.4008-.5889 2.386-3.0357 1.4389-1.882.929-"
    "1.0868-.0062-.1579h-.0546l-6.3385 4.1164-1.1293.1457-.4857-.4554.0608-.7467.2307-.2429 1.9"
    "064-1.3114Z"
)  # official mark (Simple Icons)


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
    """Draw an SVG path centered at (cx, cy) scaled to fit 'size' pixels."""
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


def hex_to_rgb(h):
    h = h.lstrip("#")
    return tuple(int(h[i:i+2], 16) / 255 for i in (0, 2, 4))


def parse_iso(text):
    """Parse an ISO-8601 timestamp, tolerating what real APIs emit.

    datetime.fromisoformat is strict before Python 3.11: it rejects a
    single-digit fractional second and, on older versions, the bare "Z".
    Normalise both rather than silently dropping the timestamp.
    """
    if not text:
        return None
    s = str(text).strip()
    if s.endswith("Z"):
        s = s[:-1] + "+00:00"
    m = re.match(r"^(.*?)\.(\d+)(.*)$", s)
    if m:
        head, frac, tail = m.groups()
        s = f"{head}.{frac[:6].ljust(6, '0')}{tail}"
    try:
        return datetime.fromisoformat(s)
    except (ValueError, TypeError):
        return None


def time_ago(dt):
    now = datetime.now(timezone.utc)
    diff = now - dt
    mins = int(diff.total_seconds() / 60)
    if mins < 1:
        return "just now"
    if mins < 60:
        return f"{mins}m ago"
    hours = mins // 60
    if hours < 24:
        return f"{hours}h ago"
    return f"{hours // 24}d ago"


class DotWindow(Gtk.Window):
    """The small circular always-on-top dot."""

    def __init__(self):
        super().__init__(type=Gtk.WindowType.TOPLEVEL)
        self.set_title("Claude Status")
        self.set_decorated(False)
        self.set_keep_above(True)
        self.set_skip_taskbar_hint(True)
        self.set_skip_pager_hint(True)
        # UTILITY receives keyboard focus; DOCK does not on most Linux WMs
        self.set_type_hint(Gdk.WindowTypeHint.UTILITY)
        self.set_default_size(WIN_SIZE, WIN_SIZE)
        self.set_size_request(WIN_SIZE, WIN_SIZE)
        self.set_resizable(False)
        self.move(20, 20)

        # RGBA transparency
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

        # state
        self.color = INDICATOR_COLORS["unknown"]
        self.data = {
            "indicator": "unknown",
            "description": "Checking...",
            "components": [],
            "incidents": [],
            "last_check": None,
            "usage": None,
            "limits": None,
        }
        self.pulse_phase = 0.0
        self.panel = None
        self.dragging = False
        self._drag_offset_x = 0
        self._drag_offset_y = 0
        self._corner_margin = 8
        self._prev_indicator = None
        self._prev_comp_statuses = {}
        self._prev_incident_ids = set()
        self._toast = None
        self._usage = None
        self._limits = None
        self._press_x = 0
        self._press_y = 0
        self._moved = False

        # widget coordination
        _register_widget(WIDGET_NAME)
        self.connect("realize", lambda w: _register_widget(WIDGET_NAME, _xid_of(w)))
        self._last_corner_ts = 0
        self._docked = False
        self._last_bar_ts = 0
        self._bar_rect = None
        self._bar_target = None
        self._bar_orientation = "horizontal"
        self.connect("destroy", lambda w: _unregister_widget(WIDGET_NAME))

        # pulse timer (20fps)
        GLib.timeout_add(50, self._tick_pulse)
        # watch shared corner file (5 checks/sec)
        GLib.timeout_add(50, self._watch_corner)   # 50 ms: tracks a bar drag smoothly
        # poll immediately, then every POLL_SECS
        threading.Thread(target=self._poll_loop, daemon=True).start()
        threading.Thread(target=self._limits_loop, daemon=True).start()

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
        if self.data["indicator"] not in ("none", "unknown"):
            pulse = 0.5 + 0.5 * math.sin(self.pulse_phase)
            cr.set_source_rgba(sr, sg, sb, 0.10 + 0.25 * pulse)
            cr.set_line_width(1)
            cr.arc(cx, cy, R + 3, 0, 2 * math.pi)
            cr.stroke()

        # brand disc, with a faint hairline so dark brands still read on the pill
        cr.set_source_rgb(*CLAUDE_BRAND)
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
        cr.set_source_rgb(*CLAUDE_IVORY)
        draw_svg_logo(cr, CLAUDE_LOGO_PATH, cx, cy, R * 2 * 0.66, 24)
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
        self.pulse_phase += 0.08
        self.queue_draw()
        return True  # keep timer

    # -- polling ----------------------------------------------------------

    def _poll_loop(self):
        last_usage = 0.0
        while True:
            data = self._fetch()
            # Scanning transcripts is far heavier than the status fetch, so
            # refresh it on a slower cadence and reuse the cached result.
            now = time.monotonic()
            if now - last_usage >= USAGE_REFRESH_SECS or self._usage is None:
                try:
                    self._usage = read_usage()
                except Exception:
                    self._usage = self._usage or {}
                last_usage = now
            data["usage"] = self._usage
            data["limits"] = self._limits
            GLib.idle_add(self._apply_data, data)
            time.sleep(POLL_SECS)

    def _limits_loop(self):
        """Plan limits on their own thread: the CLI call takes seconds."""
        while True:
            try:
                rows = read_limits()
                if rows:
                    self._limits = rows
                    GLib.idle_add(self._refresh_limits)
            except Exception:
                pass
            time.sleep(LIMITS_REFRESH_SECS)

    def _refresh_limits(self):
        self.data["limits"] = self._limits
        if self.panel and self.panel.get_visible():
            self.panel.update_data(self.data)
        return False

    def _fetch(self):
        data = {
            "indicator": "unknown",
            "description": "Unable to reach status page",
            "components": [],
            "incidents": [],
            "last_check": datetime.now(timezone.utc),
            "usage": None,
            "limits": None,
        }
        headers = {"User-Agent": "claude-status-light/2.0"}
        try:
            with urlopen(Request(STATUS_URL, headers=headers), timeout=10) as r:
                s = json.loads(r.read())
            data["indicator"] = s["status"]["indicator"]
            data["description"] = s["status"]["description"]
            with urlopen(Request(COMPONENTS_URL, headers=headers), timeout=10) as r:
                data["components"] = json.loads(r.read()).get("components", [])
            with urlopen(Request(INCIDENTS_URL, headers=headers), timeout=10) as r:
                data["incidents"] = json.loads(r.read()).get("incidents", [])
        except (URLError, KeyError, json.JSONDecodeError, OSError):
            pass
        return data

    def _apply_data(self, data):
        # detect changes and show toasts
        new_ind = data["indicator"]
        if self._prev_indicator is not None and new_ind != self._prev_indicator:
            color = INDICATOR_COLORS.get(new_ind, INDICATOR_COLORS["unknown"])
            if new_ind == "none":
                self._show_toast("All systems operational", color)
            else:
                self._show_toast(data["description"], color)

        # per-component status changes
        new_comp = {}
        for comp in data["components"]:
            name = COMPONENT_SHORT.get(comp["name"], comp["name"])
            status = comp.get("status", "unknown")
            new_comp[name] = status
        if self._prev_comp_statuses:
            for name, status in new_comp.items():
                old = self._prev_comp_statuses.get(name)
                if old and old != status:
                    slabel = STATUS_LABELS.get(status, status.upper())
                    color = STATUS_COLORS.get(status, (0.42, 0.42, 0.50))
                    self._show_toast(f"{name}: {slabel}", color)
        self._prev_comp_statuses = new_comp

        # new incidents
        new_ids = {inc["id"] for inc in data["incidents"]}
        if self._prev_incident_ids:
            for inc in data["incidents"]:
                if inc["id"] not in self._prev_incident_ids:
                    color = IMPACT_COLORS.get(inc.get("impact", "minor"), (0.92, 0.70, 0.03))
                    self._show_toast(inc["name"], color)
        self._prev_incident_ids = new_ids

        self._prev_indicator = new_ind
        self.data = data
        self.color = INDICATOR_COLORS.get(new_ind, INDICATOR_COLORS["unknown"])
        self.queue_draw()
        if self.panel and self.panel.get_visible():
            self.panel.update_data(data)
        return False

    def _show_toast(self, message, color=None):
        if self._toast:
            try:
                self._toast.destroy()
            except Exception:
                pass
        self._toast = ToastWindow(self, message, color)
        self._toast.popup()

    # -- mouse events -----------------------------------------------------

    def _on_button(self, widget, event):
        if event.button == 1:
            self._close_panel()
            self.dragging = True
            self._moved = False
            self._press_x = int(event.x_root)
            self._press_y = int(event.y_root)
            # store offset from mouse to window origin — stays constant
            wx, wy = self.get_position()
            self._drag_offset_x = int(event.x_root) - wx
            self._drag_offset_y = int(event.y_root) - wy
        elif event.button == 3:
            self._show_menu(event)

    def _on_button_release(self, widget, event):
        if event.button == 1:
            self.dragging = False
            # a click that never moved is a click, not a drag
            if not self._moved:
                self._open_status_page()

    def _on_motion(self, widget, event):
        if self.dragging and not self._docked:
            new_x = int(event.x_root) - self._drag_offset_x
            new_y = int(event.y_root) - self._drag_offset_y
            if (abs(new_x + self._drag_offset_x - self._press_x) > DRAG_THRESHOLD
                    or abs(new_y + self._drag_offset_y - self._press_y) > DRAG_THRESHOLD):
                self._moved = True
            self.move(new_x, new_y)

    def _show_menu(self, event):
        """Refresh, open the page, and Quit last behind a separator."""
        self._close_panel()
        menu = Gtk.Menu()
        menu.attach_to_widget(self, None)
        self._menu = menu

        def add(label, cb):
            it = Gtk.MenuItem(label=label)
            it.connect("activate", lambda *_: cb())
            menu.append(it)

        add("Refresh now", self._refresh_now)
        add("Open status page", self._open_status_page)
        menu.append(Gtk.SeparatorMenuItem())
        add("Quit Claude widget", self.destroy)
        menu.show_all()
        menu.popup_at_pointer(event)

    def _refresh_now(self):
        def work():
            try:
                data = self._fetch()
                # keep the slow-cadence extras the poll loop would have added
                for k in ("usage", "limits"):
                    if isinstance(self.data, dict) and k in self.data and k not in data:
                        data[k] = self.data[k]
            except Exception:
                return
            GLib.idle_add(self._apply_data, data)
        threading.Thread(target=work, daemon=True).start()

    def _open_status_page(self):
        """Open the public status page in the default browser."""
        try:
            webbrowser.open(STATUS_PAGE_URL)
        except Exception:
            pass

    def _on_key_press(self, widget, event):
        if event.keyval == Gdk.KEY_grave:  # ~ / ` key
            if not self._docked:
                self._cycle_corner()
            return True
        return False

    def _cycle_corner(self):
        """Advance to next side position and broadcast to all widgets."""
        self._close_panel()
        n_positions = Gdk.Display.get_default().get_n_monitors() * 2
        corner = _read_corner()
        new_index = (corner["corner_index"] + 1) % n_positions
        _write_corner(new_index)
        self._apply_corner(new_index)

    def _watch_corner(self):
        """Poll shared corner file for changes from other widgets."""
        # SH-widgetbar takes precedence: while a live bar publishes a slot
        # for us, sit in it and ignore corner cycling.
        bar = _read_bar()
        if bar is False:            # bar.json mid-write (bar being dragged): hold
            return True
        slot = (bar or {}).get("slots", {}).get(WIDGET_NAME)
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
        """Move this widget to the given side, cycling across all monitors."""
        self._close_panel()
        widgets = _get_active_widgets()
        try:
            my_order = widgets.index(WIDGET_NAME)
        except ValueError:
            my_order = 0

        # build list of all positions: 2 sides per monitor
        display = Gdk.Display.get_default()
        n_mon = display.get_n_monitors()
        positions = []
        for i in range(n_mon):
            mon = display.get_monitor(i)
            geo = mon.get_geometry()
            positions.append((geo.x, geo.y, geo.width, geo.height))  # left
            positions.append((geo.x, geo.y, geo.width, geo.height))  # right

        idx = corner_index % len(positions)
        mx, my, sw, sh = positions[idx]
        is_right = idx % 2 == 1

        m = self._corner_margin
        n_widgets = len(widgets) if widgets else 1
        total_height = (n_widgets - 1) * STACK_GAP + WIN_SIZE
        center_y = my + (sh - total_height) // 2 + my_order * STACK_GAP
        if is_right:
            bx = mx + sw - WIN_SIZE - m
        else:
            bx = mx + m
        self.move(bx, center_y)

    def _on_enter(self, widget, event):
        if not self.dragging:
            self._show_panel()

    def _on_leave(self, widget, event):
        # small delay so user can move cursor into the panel
        GLib.timeout_add(200, self._check_close_panel)

    def _show_panel(self):
        if self.panel and self.panel.get_visible():
            return
        if self.panel:
            self.panel.destroy()
        self.panel = PanelWindow(self)
        self.panel.update_data(self.data)
        # position next to the dot, kept inside the monitor it sits on
        x, y = self.get_position()
        self.panel.show_all()
        pw = self.panel.get_allocated_width()
        ph = self.panel.get_allocated_height()

        display = Gdk.Display.get_default()
        try:
            mon = display.get_monitor_at_point(x + WIN_SIZE // 2, y + WIN_SIZE // 2)
            geo = mon.get_geometry()
            mx, my, mw, mh = geo.x, geo.y, geo.width, geo.height
        except Exception:
            mx, my = 0, 0
            mw, mh = self.get_screen().get_width(), self.get_screen().get_height()

        if self._docked and self._bar_rect:
            px, py = _dock_panel_pos(self._bar_rect, self._bar_orientation, x, y,
                                     WIN_SIZE, pw, ph, mx, my, mw, mh)
        else:
            px = x + WIN_SIZE + 6
            if px + pw > mx + mw:          # no room right -> flip left
                px = x - pw - 6
            px = max(mx + 4, min(px, mx + mw - pw - 4))

            # the panel grew with the usage section: keep it on-screen vertically
            py = min(y, my + mh - ph - 4)
            py = max(my + 4, py)
        self.panel.move(px, py)

    def _close_panel(self):
        if self.panel and self.panel.get_visible():
            self.panel.hide()

    def _check_close_panel(self):
        if not self.panel or not self.panel.get_visible():
            return False
        display = Gdk.Display.get_default()
        seat = display.get_default_seat()
        pointer = seat.get_pointer()
        _, mx, my = pointer.get_position()

        # check if pointer is over dot window
        dx, dy = self.get_position()
        if dx <= mx <= dx + WIN_SIZE and dy <= my <= dy + WIN_SIZE:
            return False

        # check if pointer is over panel
        px, py = self.panel.get_position()
        pw = self.panel.get_allocated_width()
        ph = self.panel.get_allocated_height()
        if px <= mx <= px + pw and py <= my <= py + ph:
            return False

        self._close_panel()
        return False


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
        self._color = color or (0.96, 0.96, 0.94)

        screen = self.get_screen()
        visual = screen.get_rgba_visual()
        if visual:
            self.set_visual(visual)

        self.connect("draw", self._on_draw)

        self._label = Gtk.Label()
        self._label.set_markup(
            f'<span font_family="JetBrains Mono,DejaVu Sans Mono,monospace" font_size="9000" '
            f'foreground="#efece6">'
            f'{GLib.markup_escape_text(message)}</span>'
        )
        self._label.set_margin_start(14)
        self._label.set_margin_end(12)
        self._label.set_margin_top(8)
        self._label.set_margin_bottom(8)
        self.add(self._label)

    def popup(self, duration_ms=TOAST_DURATION_MS):
        self.show_all()
        x, y = self.parent_dot.get_position()
        toast_h = self.get_allocated_height()
        ty = y + (WIN_SIZE - toast_h) // 2
        self.move(x + WIN_SIZE + 8, ty)
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
        cr.set_source_rgba(BG_PANEL[0], BG_PANEL[1], BG_PANEL[2], 0.95 * self._opacity)
        self._rounded_rect(cr, 0, 0, alloc.width, alloc.height, 3)
        cr.fill()
        cr.set_source_rgba(BORDER_CLR[0], BORDER_CLR[1], BORDER_CLR[2], self._opacity)
        cr.set_line_width(1)
        self._rounded_rect(cr, 0.5, 0.5, alloc.width - 1, alloc.height - 1, 3)
        cr.stroke()
        # semantic edge: the toast's own status colour, as a 2px left rule
        r, g, b = self._color
        cr.set_source_rgba(r, g, b, self._opacity)
        cr.rectangle(0, 1, 2, alloc.height - 2)
        cr.fill()
        self._label.set_opacity(self._opacity)
        return False

    @staticmethod
    def _rounded_rect(cr, x, y, w, h, r):
        cr.arc(x + w - r, y + r, r, -math.pi / 2, 0)
        cr.arc(x + w - r, y + h - r, r, 0, math.pi / 2)
        cr.arc(x + r, y + h - r, r, math.pi / 2, math.pi)
        cr.arc(x + r, y + r, r, math.pi, 3 * math.pi / 2)
        cr.close_path()


class PanelWindow(Gtk.Window):
    """The hover detail panel."""

    def __init__(self, parent):
        super().__init__(type=Gtk.WindowType.TOPLEVEL)
        self.parent_dot = parent
        self.set_decorated(False)
        self.set_keep_above(True)
        self.set_skip_taskbar_hint(True)
        self.set_skip_pager_hint(True)
        # TOOLTIP works on Linux; UTILITY is more reliable on Windows
        if IS_WINDOWS:
            self.set_type_hint(Gdk.WindowTypeHint.UTILITY)
        else:
            self.set_type_hint(Gdk.WindowTypeHint.TOOLTIP)
        self.set_resizable(False)

        screen = self.get_screen()
        visual = screen.get_rgba_visual()
        if visual:
            self.set_visual(visual)
        self.set_app_paintable(True)
        self.connect("draw", self._on_draw_bg)
        self.connect("leave-notify-event", self._on_leave)
        self.set_events(Gdk.EventMask.LEAVE_NOTIFY_MASK)

        # container
        self.box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=0)
        self.box.set_margin_start(1)
        self.box.set_margin_end(1)
        self.box.set_margin_top(1)
        self.box.set_margin_bottom(1)

        self.inner = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=0)
        self.inner.set_margin_start(16)
        self.inner.set_margin_end(16)
        self.inner.set_margin_top(12)
        self.inner.set_margin_bottom(12)
        self.box.pack_start(self.inner, True, True, 0)
        self.add(self.box)

        self._apply_css()

    def _apply_css(self):
        css = b"""
        window { background-color: rgba(14,13,11,0.97); border: 1px solid #2b2722; border-radius: 4px; }
        .panel-title { font-family: "Teko", "DejaVu Sans", sans-serif; font-size: 18px; font-weight: bold; color: #efece6; }
        .panel-section { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace; font-size: 8px; font-weight: bold; color: #918b80; }
        .panel-section-red { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace; font-size: 8px; font-weight: bold; color: #cf5347; }
        .comp-name { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace; font-size: 12px; color: #efece6; }
        .comp-status { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace; font-size: 11px; }
        .badge { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace; font-size: 8px; font-weight: bold; padding: 2px 8px; border-radius: 2px; }
        .inc-title { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace; font-size: 10px; color: #efece6; }
        .inc-meta { font-family: "Inter", "DejaVu Sans", sans-serif; font-size: 9px; color: #918b80; }
        .inc-body { font-family: "Inter", "DejaVu Sans", sans-serif; font-size: 9px; color: #6f6a60; }
        .footer { font-family: "Inter", "DejaVu Sans", sans-serif; font-size: 8px; color: #6f6a60; }
        .row { background-color: #161411; border-radius: 3px; padding: 6px 10px; margin: 1px 0; }
        .usage-key { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace; font-size: 9px; color: #918b80; }
        .usage-val { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace; font-size: 12px; color: #efece6; }
        .usage-model { font-family: "JetBrains Mono", "DejaVu Sans Mono", monospace; font-size: 9px; color: #918b80; }
        .usage-none { font-family: "Inter", "DejaVu Sans", sans-serif; font-size: 9px; color: #6f6a60; }
        .sep { background-color: #2b2722; min-height: 1px; }
        """
        provider = Gtk.CssProvider()
        provider.load_from_data(css)
        Gtk.StyleContext.add_provider_for_screen(
            Gdk.Screen.get_default(), provider,
            Gtk.STYLE_PROVIDER_PRIORITY_APPLICATION,
        )

    def _on_draw_bg(self, widget, cr):
        alloc = self.get_allocation()
        cr.set_source_rgba(BG_PANEL[0], BG_PANEL[1], BG_PANEL[2], 0.97)
        cr.rectangle(0, 0, alloc.width, alloc.height)
        cr.fill()
        # hairline border
        cr.set_source_rgba(BORDER_CLR[0], BORDER_CLR[1], BORDER_CLR[2], 1)
        cr.set_line_width(1)
        cr.rectangle(0.5, 0.5, alloc.width - 1, alloc.height - 1)
        cr.stroke()
        return False

    def _on_leave(self, widget, event):
        GLib.timeout_add(200, self.parent_dot._check_close_panel)

    def _section(self, text, cls="panel-section"):
        lbl = Gtk.Label(label=text)
        lbl.get_style_context().add_class(cls)
        lbl.set_halign(Gtk.Align.START)
        self.inner.pack_start(lbl, False, False, 4)

    def update_data(self, data):
        # clear inner
        for child in self.inner.get_children():
            self.inner.remove(child)

        color_tup = INDICATOR_COLORS.get(data["indicator"], INDICATOR_COLORS["unknown"])
        color_hex = "#{:02x}{:02x}{:02x}".format(
            int(color_tup[0] * 255), int(color_tup[1] * 255), int(color_tup[2] * 255)
        )

        # header: title on first line, status badge on second
        title = Gtk.Label(label="ANTHROPIC STATUS")
        title.get_style_context().add_class("panel-title")
        title.set_halign(Gtk.Align.START)
        self.inner.pack_start(title, False, False, 0)

        badge = Gtk.Label()
        badge.set_markup(
            f'<span font_family="JetBrains Mono,DejaVu Sans Mono,monospace" font_size="7000" '
            f'font_weight="bold" background="{color_hex}" '
            f'foreground="#0e0d0b">  {data["description"].upper()}  </span>'
        )
        badge.set_halign(Gtk.Align.START)
        self.inner.pack_start(badge, False, False, 2)

        # separator
        sep = Gtk.Separator()
        sep.get_style_context().add_class("sep")
        self.inner.pack_start(sep, False, False, 8)

        # ---- USAGE first: the headline is how much of your plan is gone ----
        limits = data.get("limits")
        usage = data.get("usage")
        if limits or usage:
            self._section("USAGE")

        if limits:
            for row in limits:
                pct = row["pct"]
                lc = limit_color(pct)
                lhex = rgb_to_hex(lc)

                # one line per window: name | meter | percent | resets
                lrow = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=10)
                lrow.get_style_context().add_class("row")

                nm = Gtk.Label()
                nm.set_markup(
                    f'<span font_family="JetBrains Mono,DejaVu Sans Mono,monospace" font_size="8500" '
                    f'foreground="#918b80">{GLib.markup_escape_text(row["label"])}</span>'
                )
                nm.set_size_request(72, -1)   # px: width_chars would measure the default font
                nm.set_xalign(0)
                lrow.pack_start(nm, False, False, 0)

                meter = Gtk.DrawingArea()
                meter.set_size_request(120, 10)
                meter.set_valign(Gtk.Align.CENTER)
                meter.connect("draw", lambda w, cr, p=pct, c=lc: self._draw_meter(w, cr, p, c))
                lrow.pack_start(meter, True, True, 0)

                pv = Gtk.Label()
                pv.set_markup(
                    f'<span font_family="JetBrains Mono,DejaVu Sans Mono,monospace" font_size="10000" '
                    f'foreground="{lhex}">{pct:.0f}%</span>'
                )
                pv.set_size_request(44, -1)
                pv.set_xalign(1)
                lrow.pack_start(pv, False, False, 0)

                rs = Gtk.Label()
                rs.set_markup(
                    f'<span font_family="Inter,DejaVu Sans,sans-serif" font_size="7500" '
                    f'foreground="#6f6a60">'
                    f'{("resets " + GLib.markup_escape_text(row["resets"])) if row.get("resets") else ""}</span>'
                )
                rs.set_size_request(150, -1)
                rs.set_xalign(1)
                lrow.pack_start(rs, False, False, 0)

                self.inner.pack_start(lrow, False, False, 0)

        # token spend: a strip of four stat tiles, then the models that spent it
        if usage and usage.get("available"):
            strip = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=3)
            strip.set_homogeneous(True)
            sess = usage.get("sessions", 0)
            tiles = [("1H", fmt_tokens(usage.get("1h", 0))),
                     ("24H", fmt_tokens(usage.get("24h", 0))),
                     ("7D", fmt_tokens(usage.get("7d", 0))),
                     ("SESSIONS", str(sess))]
            for key, val in tiles:
                tile = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=1)
                tile.get_style_context().add_class("row")
                k = Gtk.Label(label=key)
                k.get_style_context().add_class("usage-key")
                k.set_xalign(0)
                v = Gtk.Label(label=val)
                v.get_style_context().add_class("usage-val")
                v.set_xalign(0)
                tile.pack_start(k, False, False, 0)
                tile.pack_start(v, False, False, 0)
                strip.pack_start(tile, True, True, 0)
            self.inner.pack_start(strip, False, False, 3)

            models = usage.get("models", [])
            if models:
                mrow = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=3)
                mrow.set_homogeneous(True)
                for i, (model, tok) in enumerate(models):
                    chip = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=12)
                    chip.get_style_context().add_class("row")
                    name_hex = AMBER_HEX if i == 0 else "#efece6"   # top spender is the signal
                    ml = Gtk.Label()
                    ml.set_markup(
                        f'<span font_family="JetBrains Mono,DejaVu Sans Mono,monospace" font_size="8500" '
                        f'foreground="{name_hex}">{GLib.markup_escape_text(_short_model(model))}</span>'
                    )
                    ml.set_xalign(0)
                    chip.pack_start(ml, True, True, 0)
                    tl = Gtk.Label()
                    tl.set_markup(
                        f'<span font_family="JetBrains Mono,DejaVu Sans Mono,monospace" font_size="8500" '
                        f'foreground="#918b80">{fmt_tokens(tok)}</span>'
                    )
                    tl.set_xalign(1)
                    chip.pack_end(tl, False, False, 0)
                    mrow.pack_start(chip, True, True, 0)
                self.inner.pack_start(mrow, False, False, 0)
        elif usage is not None and not limits:
            nlbl = Gtk.Label(label="No recent Claude Code sessions")
            nlbl.get_style_context().add_class("usage-none")
            nlbl.set_halign(Gtk.Align.START)
            self.inner.pack_start(nlbl, False, False, 2)

        # ---- SERVICES: a grid of chips, three per row. Healthy chips are just
        # a green dot and a quiet name; only a real problem gets words. ----
        sep = Gtk.Separator()
        sep.get_style_context().add_class("sep")
        self.inner.pack_start(sep, False, False, 8)
        self._section("SERVICES")

        comps = data["components"]
        for i in range(0, len(comps), 3):
            grid_row = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=3)
            grid_row.set_homogeneous(True)
            for comp in comps[i:i + 3]:
                name = COMPONENT_SHORT.get(comp["name"], comp["name"])
                status = comp.get("status", "unknown")
                scolor = STATUS_COLORS.get(status, (0.435, 0.416, 0.384))

                chip = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=7)
                chip.get_style_context().add_class("row")

                dot_area = Gtk.DrawingArea()
                dot_area.set_size_request(8, 8)
                dot_area.set_valign(Gtk.Align.CENTER)
                dot_area.connect("draw", lambda w, cr, c=scolor: self._draw_dot(cr, c))
                chip.pack_start(dot_area, False, False, 0)

                nlbl = Gtk.Label(label=name)
                nlbl.get_style_context().add_class("comp-name")
                nlbl.set_xalign(0)
                nlbl.set_ellipsize(3)
                chip.pack_start(nlbl, True, True, 0)

                if status != "operational":
                    slbl = Gtk.Label()
                    slbl.set_markup(
                        f'<span font_family="JetBrains Mono,DejaVu Sans Mono,monospace" font_size="7500" '
                        f'foreground="{rgb_to_hex(scolor)}">{STATUS_LABELS.get(status, status.upper())}</span>'
                    )
                    slbl.set_xalign(1)
                    chip.pack_end(slbl, False, False, 0)
                grid_row.pack_start(chip, True, True, 0)
            # keep the grid rectangular when the count isn't a multiple of three
            for _ in range(3 - len(comps[i:i + 3])):
                grid_row.pack_start(Gtk.Box(), True, True, 0)
            self.inner.pack_start(grid_row, False, False, 0)

        # incidents
        if data["incidents"]:
            sep2 = Gtk.Separator()
            sep2.get_style_context().add_class("sep")
            self.inner.pack_start(sep2, False, False, 8)

            ilbl = Gtk.Label(label="ACTIVE INCIDENTS")
            ilbl.get_style_context().add_class("panel-section-red")
            ilbl.set_halign(Gtk.Align.START)
            self.inner.pack_start(ilbl, False, False, 4)

            for inc in data["incidents"][:3]:
                impact = inc.get("impact", "minor")
                icolor = IMPACT_COLORS.get(impact, (0.92, 0.70, 0.03))
                icolor_hex = "#{:02x}{:02x}{:02x}".format(
                    int(icolor[0] * 255), int(icolor[1] * 255), int(icolor[2] * 255)
                )

                inc_box = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=8)
                inc_box.get_style_context().add_class("row")

                # impact color bar
                bar = Gtk.DrawingArea()
                bar.set_size_request(3, -1)
                _ic = icolor
                bar.connect("draw", lambda w, cr, c=_ic: self._draw_bar(w, cr, c))
                inc_box.pack_start(bar, False, False, 0)

                txt_box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=2)

                t = Gtk.Label(label=inc["name"])
                t.get_style_context().add_class("inc-title")
                t.set_halign(Gtk.Align.START)
                t.set_xalign(0)
                t.set_line_wrap(True)
                t.set_max_width_chars(40)
                txt_box.pack_start(t, False, False, 0)

                meta_parts = [inc.get("status", "").upper()]
                updated = inc.get("updated_at", "")
                if updated:
                    dt = parse_iso(updated)
                    if dt is not None:
                        meta_parts.append(time_ago(dt))

                m = Gtk.Label()
                m.set_markup(
                    f'<span font_family="Inter,DejaVu Sans,sans-serif" font_size="8000" '
                    f'foreground="{icolor_hex}">{"  ·  ".join(meta_parts)}</span>'
                )
                m.get_style_context().add_class("inc-meta")
                m.set_halign(Gtk.Align.START)
                txt_box.pack_start(m, False, False, 0)

                updates = inc.get("incident_updates", [])
                if updates:
                    body = updates[0].get("body", "")[:160]
                    if body:
                        b = Gtk.Label(label=body)
                        b.get_style_context().add_class("inc-body")
                        b.set_halign(Gtk.Align.START)
                        b.set_xalign(0)
                        b.set_line_wrap(True)
                        b.set_max_width_chars(40)
                        txt_box.pack_start(b, False, False, 0)

                inc_box.pack_start(txt_box, True, True, 0)
                self.inner.pack_start(inc_box, False, False, 0)

        # footer
        sep3 = Gtk.Separator()
        sep3.get_style_context().add_class("sep")
        self.inner.pack_start(sep3, False, False, 8)

        check_str = ""
        if data["last_check"]:
            check_str = f"Last checked: {data['last_check'].strftime('%H:%M:%S UTC')}"
        footer = Gtk.Label(
            label=f"{check_str}   ·   Click: status page   ·   ~: move   ·   Right-click: menu"
        )
        footer.get_style_context().add_class("footer")
        footer.set_halign(Gtk.Align.START)
        self.inner.pack_start(footer, False, False, 0)

        self.show_all()

    @staticmethod
    def _draw_meter(widget, cr, pct, color):
        alloc = widget.get_allocation()
        w, h = alloc.width, alloc.height
        r = h / 2

        def pill(width):
            cr.new_path()
            cr.arc(r, r, r, math.pi / 2, 3 * math.pi / 2)
            cr.arc(max(r, width - r), r, r, 3 * math.pi / 2, math.pi / 2)
            cr.close_path()

        # track
        cr.set_source_rgb(*BG_RAISED)
        pill(w)
        cr.fill()
        # fill
        frac = max(0.0, min(1.0, pct / 100.0))
        if frac > 0:
            cr.set_source_rgb(*color)
            pill(max(h, w * frac))
            cr.fill()
        return False

    @staticmethod
    def _draw_dot(cr, color):
        r, g, b = color
        cr.set_source_rgb(r, g, b)
        cr.arc(5, 5, 4, 0, 2 * math.pi)
        cr.fill()
        return False

    @staticmethod
    def _draw_bar(widget, cr, color):
        r, g, b = color
        alloc = widget.get_allocation()
        cr.set_source_rgb(r, g, b)
        cr.rectangle(0, 0, 3, alloc.height)
        cr.fill()
        return False


if __name__ == "__main__":
    dot = DotWindow()
    dot.show_all()
    Gtk.main()
