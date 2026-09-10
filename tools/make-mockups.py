#!/usr/bin/python3
"""
Render the README panel images from invented data.

The screenshots in this repo must never be captures of a real machine: a
status panel's whole job is to show what you are running, so a real one
publishes container names, hostnames, tailnet addresses, mounted disks and
hardware. These are mockups - the same panel chrome and the same Emberdeck
palette, drawn from fabricated data.

    ./tools/make-mockups.py            # write every panel into assets/screenshots
    ./tools/make-mockups.py docker     # just one

Needs a display (it renders real GTK windows offscreen and grabs the pixels).
"""

import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
sys.path.insert(0, os.path.join(ROOT, "core"))

import gi                                            # noqa: E402
gi.require_version("Gtk", "3.0")
gi.require_version("Gdk", "3.0")
from gi.repository import Gtk, Gdk, GLib             # noqa: E402

from shinywidget import (                            # noqa: E402
    WidgetApp, PanelWindow, Section, Row, Meter, Note, Grid, Alert,
    Limit, Tiles, Chips, Action,
    OK, WARN, CRIT, IDLE, AMBER, FG, FG_DIM, BAD,
)

OUT = os.path.join(ROOT, "assets", "screenshots")


class Mock(WidgetApp):
    """A widget whose data is a constant: no daemon, no network, no machine."""

    def __init__(self, title, badge, items, width=560, page=None, pages=(),
                 footer=None):
        self.title = title
        self._badge = badge
        self._items = items
        self.panel_width = width
        self.page = page
        self._pages = tuple(pages)
        self._footer = footer
        words = title.lower().split()
        self.name = words[0] if words else "panel"

    def fetch(self):
        return {}

    def state(self, data):
        return self._badge[1]

    def state_label(self, state, data):
        return self._badge[0]

    def pages(self, data):
        return self._pages

    def rows(self, data):
        return self._items

    def footer(self, data):
        if self._footer is not None:
            return self._footer
        bits = ["Last checked: 09:41:02"]
        if self._pages:
            bits.append("Hover a tab")
        bits += ["~: move", "Right-click: menu"]
        return "   ·   ".join(bits)


def act(label, confirm=None, danger=False):
    return Action(label, lambda: None, confirm=confirm, danger=danger)


# -- the invented panels ---------------------------------------------------

def docker():
    return Mock("DOCKER", ("2 containers unhealthy", "crit"), [
        Section("NEEDS ATTENTION", alert=True),
        Alert("api-gateway", "unhealthy · up 3 days",
              "upstream connect error: connection refused",
              color=CRIT, mono_body=True,
              actions=[act("restart", "restart?"), act("stop", "stop?")]),
        Alert("worker-queue", "exit 137 · OOM-killed · 2h ago",
              "Killed: cannot allocate memory",
              color=WARN, mono_body=True,
              actions=[act("start"), act("remove", "remove?", danger=True)]),
        Section("RUNNING · 6"),
        Row("shopfront", "4/4 up · 1.2G · 6%", dot=OK, dim_value=True,
            sub="web, api, worker, cache",
            actions=[act("restart", "restart?"), act("stop", "stop?")]),
        Row("postgres", "up 12d · 384M", dot=OK, dim_value=True,
            sub="postgres:16 · :5432 · healthy",
            actions=[act("restart", "restart?"), act("stop", "stop?")]),
        Row("redis", "up 12d · 41M", dot=OK, dim_value=True,
            sub="redis:7-alpine · :6379",
            actions=[act("restart", "restart?"), act("stop", "stop?")]),
    ], page="NOW", pages=("NOW", "STOPPED", "SYSTEM"))


def docker_stopped():
    return Mock("DOCKER", ("6 running", "ok"), [
        Section("BROKEN · 2 · crashed, not touched since", alert=True),
        Row("batch-import", "exit 1 · 3d ago", dot=CRIT, dim_value=True,
            sub="ConnectionError: could not reach host",
            actions=[act("start"), act("remove", "remove?", danger=True)]),
        Row("media-encoder", "exit 139 · 2w ago", dot=CRIT, dim_value=True,
            sub="segmentation fault (core dumped)",
            actions=[act("start"), act("remove", "remove?", danger=True)]),
        Section("STOPPED · 2 · this week"),
        Row("analytics", "5 · 2d ago", dot=IDLE, dim_value=True,
            sub="5 services · exited cleanly",
            actions=[act("start"), act("remove", "remove stack?", danger=True)]),
        Row("staging", "3 · 5d ago", dot=IDLE, dim_value=True,
            sub="3 services · stopped (SIGTERM)",
            actions=[act("start"), act("remove", "remove stack?", danger=True)]),
        Section("DORMANT · 5 · not run in a week"),
        Row("docs-site", "3w ago", dot=IDLE, dim_value=True, sub="exited cleanly",
            actions=[act("start"), act("remove", "remove?", danger=True)]),
        Row("test-runner", "6w ago", dot=IDLE, dim_value=True, sub="exited cleanly",
            actions=[act("start"), act("remove", "remove?", danger=True)]),
    ], page="STOPPED", pages=("NOW", "STOPPED", "SYSTEM"))


def docker_system():
    return Mock("DOCKER", ("6 running", "ok"), [
        Section("ENGINE"),
        Row("Docker", "27.4.1", dim_value=True),
        Row("Compose", "2.31.0", dim_value=True),
        Row("Containers", "24 · 6 running", dim_value=True),
        Row("Images", "38 · 4 dangling", dim_value=True),
        Row("Volumes", "17 · 11 in use", dim_value=True),
        Section("DISK · reclaimable"),
        Row("images", "31G of 92G", dot=WARN, dim_value=True,
            sub="4 dangling · prune drops every image no container uses",
            actions=[act("prune unused", "free 31G?", danger=True)]),
        Row("containers", "8.4G of 8.4G", dot=IDLE, dim_value=True,
            sub="18 stopped · prune removes every one of them",
            actions=[act("prune stopped", "remove 18?", danger=True)]),
        Row("build cache", "12G of 40G", dot=IDLE, dim_value=True,
            sub="rebuilds cost time, not data",
            actions=[act("prune", "prune?", danger=True)]),
        Row("volumes", "6.1G of 22G", dot=IDLE, dim_value=True,
            sub="data lives here · never pruned from this panel"),
        Note("Buttons ask twice. Nothing in use is ever removed.", FG_DIM),
    ], page="SYSTEM", pages=("NOW", "STOPPED", "SYSTEM"))


def codex():
    return Mock("CODEX STATUS", ("PARTIAL SYSTEM DEGRADATION", "warn"), [
        Section("USAGE · PRO PLAN"),
        Note("General limit", FG_DIM),
        Limit("5H", 34, "today, 2:15pm"),
        Limit("WEEK", 61, "Fri, 9:00am"),
        Note("GPT-5.1-Codex-Spark   ·   never reports usage", IDLE),
        Limit("5H", 0, "today, 2:15pm"),
        Limit("WEEK", 0, "Fri, 9:00am"),
        Tiles([("1H", "18.4M"), ("24H", "220.7M"), ("7D", "1.1B"), ("SESSIONS", "4")]),
        Chips([("gpt-5.1-codex", AMBER, "812M"), ("gpt-5.1-mini", FG, "184M")], columns=2),
        Section("DEGRADED · 1", alert=True),
        Row("Conversations", "DEGRADED", color=WARN, dot=WARN),
        Section("SERVICES"),
        Chips([("codex api", OK), ("codex web", OK), ("desktop app", OK),
               ("cli", OK), ("vs code", OK), ("responses", OK),
               ("completions", OK), ("login", OK), ("conversations", WARN)],
              columns=3),
        Note("24 other OpenAI services operational", FG_DIM),
        Section("ACTIVE INCIDENTS", alert=True),
        Alert("Shared projects fail to open by direct link",
              "INVESTIGATING · 1h ago",
              "We are investigating the issue.",
              color=WARN),
    ], width=588)


def claude():
    return Mock("ANTHROPIC STATUS", ("PARTIAL SYSTEM DEGRADATION", "warn"), [
        Section("USAGE  ·  MAX PLAN"),
        Limit("SESSION", 46, "today, 6:30pm"),
        Limit("WEEK", 72, "Mon, 9:00am"),
        Limit("OPUS WEEK", 23, "Mon, 9:00am"),
        Tiles([("1H", "22.8M"), ("24H", "196.4M"), ("7D", "1.4B"), ("SESSIONS", "7")]),
        Chips([("opus 4.6", AMBER, "640M"), ("sonnet 4.6", FG, "410M"),
               ("haiku 4.5", FG, "38M")], columns=3),
        Section("DEGRADED · 1", alert=True),
        Row("Console", "DEGRADED PERFORMANCE", color=WARN, dot=WARN),
        Section("SERVICES"),
        Chips([("claude.ai", OK), ("console", WARN), ("API", OK),
               ("claude code", OK), ("cowork", OK), ("gov", OK),
               ("desktop app", OK), ("mobile", OK), ("workbench", OK)], columns=3),
        Section("ACTIVE INCIDENTS", alert=True),
        Alert("Elevated error rates on the Console",
              "MONITORING · 40m ago",
              "A fix has been applied and we are monitoring the results.",
              color=WARN),
    ], width=588)


def disk():
    return Mock("DISK", ("/ 91% FULL", "warn"), [
        Section("FILESYSTEMS"),
        Meter("/", 0.91, "73G free", note="727G used of 800G  ·  91%  ·  ext4"),
        Meter("/boot/efi", 0.41, "602M free", note="418M used of 1022M  ·  41%  ·  vfat"),
        Meter("/home", 0.64, "712G free", note="1.3T used of 2.0T  ·  64%  ·  ext4"),
        Meter("/mnt/media", 0.33, "24T free", note="12T used of 36T  ·  33%  ·  cifs"),
        Meter("/mnt/backup", 0.72, "1.1T free", note="2.9T used of 4.0T  ·  72%  ·  ext4"),
        Section("DOCKER · RECLAIMABLE"),
        Note("58G could be freed on /var/lib/docker", AMBER),
        Grid([("images", AMBER, "31G"), ("local volumes", IDLE, "6G"),
              ("containers", IDLE, "8G"), ("build cache", IDLE, "12G")], columns=2),
        Note("docker system prune -a   ·   docker builder prune", FG_DIM),
    ], width=520)


def tailscale():
    return Mock("TAILSCALE", ("4 PEERS UP", "ok"), [
        Section("THIS NODE"),
        Row("workstation", "Running", color=OK, dot=OK),
        Row("address", "100.64.12.31", dim_value=True),
        Row("magicdns", "workstation.tailnet-example.ts.net", dim_value=True),
        Row("version", "1.78.1", dim_value=True),
        Row("key expires", "142d", dim_value=True),
        Section("EXPOSURE"),
        Row("serve / funnel", "nothing exposed", dim_value=True, dot=IDLE),
        Row("route via fileserver", "192.168.1.0/24", dim_value=True, dot=OK),
        Section("ONLINE · 4"),
        Row("fileserver", "linux  ·  direct  ·  ↓2.4M ↑180K", dot=OK, dim_value=True),
        Row("laptop", "macOS  ·  direct", dot=OK, dim_value=True),
        Row("phone", "iOS  ·  relay fra", dot=WARN, dim_value=True),
        Row("build-agent", "linux  ·  direct  ·  ↓14K ↑8K", dot=OK, dim_value=True),
        Section("DORMANT · 3"),
        Grid([("old-laptop", IDLE, "6d ago"), ("spare-pi", IDLE, "3w ago"),
              ("tablet", CRIT, "key expired")], columns=2),
        Note("pin peers in status-widgets/tailscale.json", FG_DIM),
    ], width=520)


def comfyui():
    return Mock("COMFYUI", ("GENERATING · 2 QUEUED", "ok"), [
        Section("QUEUE"),
        Row("running", "flux-portrait.json  ·  step 14/28", dot=OK, dim_value=True),
        Row("pending", "2", dim_value=True),
        Section("GPU 0  ·  NVIDIA GeForce RTX 4090"),
        Meter("VRAM", 0.62, "15.0G / 24G"),
        Meter("Torch VRAM", 0.48, "11.5G / 24G"),
        Row("load  ·  temp", "94%  ·  71°C", dim_value=True),
        Row("power  ·  fan", "384W / 450W  ·  62%", dim_value=True),
        Section("GPU 1  ·  NVIDIA GeForce RTX 4070"),
        Meter("VRAM", 0.11, "1.3G / 12G"),
        Meter("Torch VRAM", 0.04, "0.5G / 12G"),
        Row("load  ·  temp", "3%  ·  44°C", dim_value=True),
        Row("power  ·  fan", "18W / 220W  ·  31%", dim_value=True),
        Section("HOST"),
        Meter("CPU", 0.28, "28%"),
        Meter("RAM", 0.44, "28G / 64G"),
        Section("VERSIONS"),
        Row("ComfyUI", "0.3.26", dim_value=True),
        Row("Torch", "2.5.1+cu124", dim_value=True),
        Row("Python", "3.12.4", dim_value=True),
        Note("http://127.0.0.1:8188", FG_DIM),
    ], width=480)


def _load_widget(folder, fname):
    """Import a widget module so its own panel class can be used directly."""
    import importlib.util
    path = os.path.join(ROOT, "widgets", folder, fname + ".py")
    spec = importlib.util.spec_from_file_location("w_" + folder, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def _shoot(panel, name, delay=500):
    """Save a panel window's pixels once the compositor has painted it."""
    panel.move(80, 80)
    panel.show_all()
    out_path = os.path.join(OUT, name + ".png")

    def grab():
        win = panel.get_window()
        w, h = win.get_width(), win.get_height()
        Gdk.pixbuf_get_from_window(win, 0, 0, w, h).savev(out_path, "png", [], [])
        print(f"  {name}.png  {w}x{h}")
        Gtk.main_quit()
        return False

    GLib.timeout_add(delay, grab)
    Gtk.main()


def render_claude(name):
    """The Claude widget's own panel, fed an invented status page."""
    from datetime import datetime, timezone
    cw = _load_widget("claude", "claude-status-checker")
    data = {
        "indicator": "minor",
        "description": "Partial system degradation",
        "components": [
            {"name": "claude.ai", "status": "operational"},
            {"name": "Console", "status": "degraded_performance"},
            {"name": "API", "status": "operational"},
            {"name": "Claude Code", "status": "operational"},
            {"name": "Cowork", "status": "operational"},
            {"name": "Claude on Gov Cloud", "status": "operational"},
        ],
        "incidents": [{
            "name": "Elevated error rates on the Console",
            "status": "monitoring",
            "created_at": "2026-01-01T09:00:00.000Z",
            "incident_updates": [{
                "status": "monitoring",
                "body": "A fix has been applied and we are monitoring the results.",
                "created_at": "2026-01-01T09:40:00.000Z"}],
        }],
        "limits": [
            {"label": "SESSION", "pct": 46, "resets": "today, 6:30pm"},
            {"label": "WEEK", "pct": 72, "resets": "Mon, 9:00am"},
            {"label": "OPUS WEEK", "pct": 23, "resets": "Mon, 9:00am"},
        ],
        "usage": {"available": True, "1h": 22_800_000, "24h": 196_400_000,
                  "7d": 1_400_000_000, "sessions": 7,
                  "models": [("claude-opus-5", 640_000_000),
                             ("claude-sonnet-5", 410_000_000),
                             ("claude-haiku-4-5-20251001", 38_000_000)]},
        "last_check": datetime.now(timezone.utc),
    }
    panel = cw.PanelWindow(_PowerParent())
    panel.update_data(data)
    _shoot(panel, name)


def render_comfyui(name):
    """The ComfyUI widget's own panel, fed an invented server."""
    cu = _load_widget("comfyui", "comfyui-status-checker")
    data = {
        "state": "generating", "running": 1, "pending": 2,
        "devices": [
            {"index": 0, "label": "cuda:0", "name": "NVIDIA GeForce RTX 4090",
             "type": "cuda",
             "vram_total": 25_757_220_864, "vram_free": 9_663_676_416,
             "vram_used": 16_093_544_448,
             "torch_vram_total": 25_757_220_864, "torch_vram_free": 13_958_643_712,
             "torch_vram_used": 11_798_577_152,
             "temp": 71, "util": 94, "power": 384, "fan": 62},
            {"index": 1, "label": "cuda:1", "name": "NVIDIA GeForce RTX 4070",
             "type": "cuda",
             "vram_total": 12_884_901_888, "vram_free": 11_453_246_668,
             "vram_used": 1_431_655_220,
             "torch_vram_total": 12_884_901_888, "torch_vram_free": 12_348_030_976,
             "torch_vram_used": 536_870_912,
             "temp": 44, "util": 3, "power": 18, "fan": 31},
        ],
        "cpu": {"temp": 54, "util": 28},
        "ram_total": 68_719_476_736, "ram_free": 38_654_705_664,
        "comfyui_version": "0.3.26", "pytorch_version": "2.5.1+cu124",
        "python_version": "3.12.4", "os": "posix",
        "last_check": __import__("datetime").datetime.now(),
    }
    panel = cu.PanelWindow(_PowerParent())
    panel.update_data(data, "http://127.0.0.1:8188")
    _shoot(panel, name)


class _PowerParent:
    """The little the Power panel asks of the dot it belongs to."""

    def get_position(self):
        return (0, 0)

    def _check_close_panel(self):
        return False


def render_power(name):
    """Shoot the Power widget's own panel, fed invented readings.

    Power does not use the shared panel primitives - it has its own header
    strip, two-column body and donut - so drawing a lookalike here would be
    inventing a second design. This runs the real class and changes only the
    numbers.
    """
    import importlib.util
    from datetime import datetime, timezone

    path = os.path.join(ROOT, "widgets", "power", "power-monitor.py")
    spec = importlib.util.spec_from_file_location("power_monitor", path)
    pm = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(pm)

    # The widget reads its tariff from the user's config; pin a neutral one so
    # the mockup never shows what anyone actually pays.
    pm.CFG.update({"currency": "EUR", "price_per_kwh": 0.28,
                   "peak_rate": 0.34, "off_peak_rate": 0.19, "tou": True})

    def gpu(i, nm, w, cap, temp, vtemp, used, total, util, fan):
        return {"index": i, "name": nm, "power": w, "power_limit": cap,
                "temp": temp, "vram_temp": vtemp, "vram_used": used,
                "vram_total": total, "util": util, "fan": fan, "bus": None}

    gpus = [gpu(0, "RTX 4090", 104, 350, 57, 62, 7580, 24564, 62, 48),
            gpu(1, "RTX 4070", 21, 220, 44, 48, 920, 12282, 4, 31)]
    cpu_w, nvme_w, periph_w, board_w = 88, 11, 4, 32
    total = cpu_w + sum(g["power"] for g in gpus) + nvme_w + periph_w + board_w

    data = {
        "level": "moderate",
        "cpu_watts": cpu_w, "cpu_temp": 52, "cpu_usage": 22,
        "gpus": gpus,
        "ram": {"used_gb": 26.4, "total_gb": 64.0, "pct": 41.2},
        "nvmes": [{"model": "Samsung SSD 990 PRO 2TB", "watts": 6, "temp": 43},
                  {"model": "WD_BLACK SN850X 2000GB", "watts": 5, "temp": 38}],
        "nvme_w": nvme_w, "periph_w": periph_w, "mon_est": 90,
        "total_watts": total, "avg_watts": total - 6,
        "donut": {"cpu": cpu_w, "gpu": sum(g["power"] for g in gpus),
                  "nvme": nvme_w, "periph": periph_w, "total": total},
        "last_check": datetime.now(timezone.utc),
    }
    sys_info = {
        "cpu_model": "AMD Ryzen 9 7950X 16-Core Processor",
        "mobo": "ASUS ProArt X670E-CREATOR WIFI",
        "monitors": [{"port": "DP-0", "name": "DELL U2723QE", "size_in": 27,
                      "res": "3840x2160", "est_watts": 45},
                     {"port": "DP-2", "name": "DELL U2723QE", "size_in": 27,
                      "res": "3840x2160", "est_watts": 45}],
        "peripherals": ["Logitech, Inc. MX Master 3",
                        "Blue Microphones Yeti",
                        "Anker PowerExpand Hub"],
    }

    panel = pm.PanelWindow(_PowerParent())
    panel.update_data(data, sys_info)
    panel.move(80, 80)
    panel.show_all()
    out_path = os.path.join(OUT, name + ".png")

    def grab():
        win = panel.get_window()
        w, h = win.get_width(), win.get_height()
        Gdk.pixbuf_get_from_window(win, 0, 0, w, h).savev(out_path, "png", [], [])
        print(f"  {name}.png  {w}x{h}")
        Gtk.main_quit()
        return False

    GLib.timeout_add(500, grab)
    Gtk.main()



# The dock image is composited from the same logo SVGs the README shows, so
# the two can never drift. Statuses are invented, and chosen to show the whole
# vocabulary at once: mostly quiet, one warning, one failure.
DOCK_ORDER = ("claude", "codex", "comfyui", "disk", "docker", "power", "tailscale")
DOCK_STATES = {"claude": OK, "codex": OK, "comfyui": OK, "disk": WARN,
               "docker": CRIT, "power": OK, "tailscale": OK}


def dock():
    """The pill, drawn the way the dock draws it, with invented statuses."""
    import math
    import cairo
    import gi as _gi
    _gi.require_version("Rsvg", "2.0")
    from gi.repository import Rsvg
    from shinywidget import WIN_SIZE, BG_PANEL, BORDER_CLR, BUBBLE_RADIUS

    logos = os.path.join(ROOT, "assets", "logos")
    names = [n for n in DOCK_ORDER
             if os.path.exists(os.path.join(logos, n + ".svg"))]
    if not names:
        print("  dock: no logos found")
        return None

    pad, gap, dot = 10, 6, WIN_SIZE
    w = dot + pad * 2
    h = pad * 2 + len(names) * dot + (len(names) - 1) * gap + 16
    surf = cairo.ImageSurface(cairo.FORMAT_ARGB32, w, h)
    cr = cairo.Context(surf)

    r = 22                                        # the pill
    cr.new_sub_path()
    cr.arc(w - r, r, r, -math.pi / 2, 0)
    cr.arc(w - r, h - r, r, 0, math.pi / 2)
    cr.arc(r, h - r, r, math.pi / 2, math.pi)
    cr.arc(r, r, r, math.pi, 3 * math.pi / 2)
    cr.close_path()
    cr.set_source_rgba(*BG_PANEL, 0.96)
    cr.fill_preserve()
    cr.set_source_rgba(*BORDER_CLR, 1)
    cr.set_line_width(1)
    cr.stroke()

    for i in range(3):                            # the grip
        cr.set_source_rgba(*FG_DIM, 0.5)
        cr.arc(w / 2 - 7 + i * 7, 11, 1.6, 0, 2 * math.pi)
        cr.fill()

    for idx, name in enumerate(names):
        x = pad
        y = pad + 16 + idx * (dot + gap)
        cr.save()
        cr.translate(x, y)
        Rsvg.Handle.new_from_file(
            os.path.join(logos, name + ".svg")).render_cairo(cr)
        cr.restore()

        # the status bubble, punched out of the disc like the real dot
        cx, cy = x + dot / 2, y + dot / 2
        bx, by = cx + 17 * 0.70, cy - 17 * 0.70
        cr.set_source_rgba(*BG_PANEL, 0.96)
        cr.arc(bx, by, BUBBLE_RADIUS + 2, 0, 2 * math.pi)
        cr.fill()
        cr.set_source_rgb(*DOCK_STATES.get(name, IDLE))
        cr.arc(bx, by, BUBBLE_RADIUS, 0, 2 * math.pi)
        cr.fill()

    path = os.path.join(OUT, "dock.png")
    surf.write_to_png(path)
    print(f"  dock.png  {w}x{h}  ({len(names)} dots)")
    return path



PANELS = {
    "panel-docker": docker,
    "panel-docker-stopped": docker_stopped,
    "panel-docker-system": docker_system,
    "panel-codex": codex,
    "panel-disk": disk,
    "panel-tailscale": tailscale,
}


def render(name, factory):
    """Draw one panel and save it. Runs in its own process (see main): GTK
    does not reliably hand back the pixels of a second window in one run."""
    app = factory()

    class FakeDot:
        def __init__(self, a):
            self.app = a
        def get_position(self):
            return (0, 0)
        def _check_close_panel(self):
            return False
        def repaint_panel(self):
            pass

    panel = PanelWindow(FakeDot(app))
    # Mockups show the whole panel: nothing here is long enough to need the
    # scrolling a real one falls back on, and a clipped last row reads as a bug.
    panel.scroller.set_max_content_height(4000)
    panel.update_data({})
    panel.move(80, 80)
    panel.show_all()
    path = os.path.join(OUT, name + ".png")

    def grab():
        win = panel.get_window()
        w, h = win.get_width(), win.get_height()
        pb = Gdk.pixbuf_get_from_window(win, 0, 0, w, h)
        pb.savev(path, "png", [], [])
        print(f"  {name}.png  {w}x{h}")
        Gtk.main_quit()
        return False

    # let the compositor actually map and paint the window before grabbing
    GLib.timeout_add(450, grab)
    Gtk.main()


def main():
    os.makedirs(OUT, exist_ok=True)
    args = sys.argv[1:]

    if args and args[0] == "--one":          # child process: draw one panel
        name = args[1]
        if name == "panel-power":
            render_power(name)
        elif name == "panel-claude":
            render_claude(name)
        elif name == "panel-comfyui":
            render_comfyui(name)
        else:
            render(name, PANELS[name])
        return 0

    names = sorted(list(PANELS)
                   + ["panel-power", "panel-claude", "panel-comfyui"]) + ["dock"]
    todo = [k for k in names if not args or any(a in k for a in args)]
    if not todo:
        print("nothing matched; known:", ", ".join(names))
        return 1
    print(f"rendering into {OUT}")
    import subprocess
    for name in todo:
        if name == "dock":
            dock()
            continue
        r = subprocess.run([sys.executable, os.path.abspath(__file__), "--one", name])
        if r.returncode != 0:
            print(f"  FAILED: {name}")
            return r.returncode
    return 0


if __name__ == "__main__":
    sys.exit(main())
