#!/usr/bin/python3
"""
Power Monitor — always-on-top circular indicator for system power consumption.

Uses GTK3 + Cairo for true RGBA transparency (no square background).
Hover to see full power breakdown. Drag to reposition. Right-click to quit.

Configuration:
  First run creates ~/.config/power-monitor/config.json with defaults.
  Edit price_per_kwh and currency to match your electricity rate.
  Set log_interval to "hourly", "daily", "weekly", "monthly", or "off".

Data sources (auto-detected):
  CPU power  → Intel RAPL (/sys/class/powercap) or estimation
  GPU power  → nvidia-smi (NVIDIA) or rocm-smi (AMD), every card summed
  NVMe       → /sys/class/hwmon (temps + model)
  Temps      → /sys/class/hwmon
  RAM/CPU    → /proc/meminfo, /proc/stat
  Hardware   → /proc/cpuinfo, lsusb, xrandr, EDID
"""

__version__ = "1.0.2"

import argparse
import csv
import json
import math
import os
import platform
import re
import subprocess
import sys
import threading
import time
from datetime import datetime, timezone, timedelta

IS_WINDOWS = platform.system() == "Windows"

try:
    import gi
    gi.require_version("Gtk", "3.0")
    from gi.repository import Gtk, Gdk, GLib
except (ImportError, ValueError):
    if IS_WINDOWS:
        print(
            "ERROR: GTK3 / PyGObject not found.\n\n"
            "On Windows, install via MSYS2:\n"
            "  1. Install MSYS2 from https://www.msys2.org/\n"
            "  2. In MSYS2 UCRT64 terminal run:\n"
            "       pacman -S mingw-w64-ucrt-x86_64-python-gobject mingw-w64-ucrt-x86_64-gtk3\n"
            "  3. Run this script using the MSYS2 Python:\n"
            "       /ucrt64/bin/python3 power-monitor.py",
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

# -- user config ----------------------------------------------------------
CONFIG_DIR = os.path.join(os.path.expanduser("~"), ".config", "power-monitor")
CONFIG_FILE = os.path.join(CONFIG_DIR, "config.json")
LOG_DIR = os.path.join(os.path.expanduser("~"), "Documents", "PM-Log")

# How long the donut averages over before publishing new slices. The panel
# itself keeps refreshing at poll_interval; only the chart is held steady.
DONUT_AVG_SECS = 60

DEFAULT_CONFIG = {
    "price_per_kwh": 0.30,
    "currency": "EUR",
    "overhead_watts": 30,
    "peripheral_watts": 5,
    "poll_interval": 2,
    "log_interval": "hourly",
    # Time-of-use tariff. When "tou" is on, the live cost uses peak_rate inside
    # peak_hours (weekdays; weekends off-peak unless tou_weekend_offpeak is
    # false) and off_peak_rate otherwise; projections use the weekly blend.
    "tou": False,
    "peak_rate": None,
    "off_peak_rate": None,
    "peak_hours": "6-13,15-22",
    "tou_weekend_offpeak": True,
}


def _load_config():
    os.makedirs(CONFIG_DIR, exist_ok=True)
    if os.path.exists(CONFIG_FILE):
        try:
            with open(CONFIG_FILE) as f:
                return {**DEFAULT_CONFIG, **json.load(f)}
        except (json.JSONDecodeError, OSError):
            pass
    with open(CONFIG_FILE, "w") as f:
        json.dump(DEFAULT_CONFIG, f, indent=2)
    print(f"Created config: {CONFIG_FILE}")
    return dict(DEFAULT_CONFIG)


CFG = _load_config()

# -- country presets ------------------------------------------------------
# Each preset: currency, flat rate, peak rate, off-peak rate, has_tou.
#
# These are ALL-IN marginal rates: what one extra kWh actually costs on a
# household bill — energy + distribution + transmission + levies + VAT.
# That is the number that matters here, because every watt this tool
# measures is marginal consumption.
#
# Sourced from Eurostat 2025-S2 band DC (2500-4999 kWh/yr, taxes included,
# published 2026-08-12) and national regulators. Where Eurostat's headline
# figure excludes distribution — notably Poland, where it reads 0.20 EUR
# against the regulator's ~1.30 PLN all-in G-11 rate — the national
# regulator's figure wins, since a partial rate would understate real cost.
#
# Approximations, deliberately: tariffs vary by supplier, region and
# contract. The settings dialog shows each rate and lets the user override.
COUNTRY_PRESETS = {
    # PLN: URE-approved G-11 all-in incl. distribution + 23% VAT (2026)
    "Poland":      {"currency": "PLN", "price_per_kwh": 1.30, "peak": 1.45, "off_peak": 0.90, "tou": True},
    "Germany":     {"currency": "EUR", "price_per_kwh": 0.29, "peak": 0.33, "off_peak": 0.24, "tou": False},
    "France":      {"currency": "EUR", "price_per_kwh": 0.32, "peak": 0.35, "off_peak": 0.23, "tou": True},
    # GBP: Ofgem price cap unit rate, Jul-Sep 2026
    "UK":          {"currency": "GBP", "price_per_kwh": 0.26, "peak": 0.31, "off_peak": 0.11, "tou": True},
    # USD: US residential average (~18.4 c/kWh, Aug 2026); 12c ID to 52c HI
    "USA":         {"currency": "USD", "price_per_kwh": 0.18, "peak": 0.30, "off_peak": 0.11, "tou": False},
    # JPY: national household average ~31 JPY/kWh (2026)
    "Japan":       {"currency": "JPY", "price_per_kwh": 31.0, "peak": 36.0, "off_peak": 18.0, "tou": True},
    # CAD: national average; varies 8.3c (QC) to 34c (NT)
    "Canada":      {"currency": "CAD", "price_per_kwh": 0.17, "peak": 0.20, "off_peak": 0.10, "tou": True},
    "Australia":   {"currency": "AUD", "price_per_kwh": 0.28, "peak": 0.40, "off_peak": 0.18, "tou": True},
    "Spain":       {"currency": "EUR", "price_per_kwh": 0.26, "peak": 0.30, "off_peak": 0.15, "tou": True},
    "Netherlands": {"currency": "EUR", "price_per_kwh": 0.40, "peak": 0.43, "off_peak": 0.34, "tou": True},
    "Italy":       {"currency": "EUR", "price_per_kwh": 0.30, "peak": 0.34, "off_peak": 0.25, "tou": True},
    "Belgium":     {"currency": "EUR", "price_per_kwh": 0.35, "peak": 0.39, "off_peak": 0.27, "tou": True},
    "Austria":     {"currency": "EUR", "price_per_kwh": 0.27, "peak": 0.30, "off_peak": 0.22, "tou": False},
    "Ireland":     {"currency": "EUR", "price_per_kwh": 0.39, "peak": 0.43, "off_peak": 0.25, "tou": True},
    "Portugal":    {"currency": "EUR", "price_per_kwh": 0.24, "peak": 0.28, "off_peak": 0.16, "tou": True},
    "Sweden":      {"currency": "SEK", "price_per_kwh": 2.90, "peak": 3.30, "off_peak": 2.20, "tou": True},
    "Denmark":     {"currency": "DKK", "price_per_kwh": 2.10, "peak": 2.40, "off_peak": 1.60, "tou": True},
    "Finland":     {"currency": "EUR", "price_per_kwh": 0.26, "peak": 0.30, "off_peak": 0.18, "tou": True},
    "Norway":      {"currency": "NOK", "price_per_kwh": 2.05, "peak": 2.40, "off_peak": 1.50, "tou": True},
    "Czechia":     {"currency": "CZK", "price_per_kwh": 4.20, "peak": 4.70, "off_peak": 3.10, "tou": True},
    "Greece":      {"currency": "EUR", "price_per_kwh": 0.24, "peak": 0.28, "off_peak": 0.17, "tou": True},
    "Romania":     {"currency": "RON", "price_per_kwh": 1.30, "peak": 1.45, "off_peak": 1.00, "tou": False},
    "Hungary":     {"currency": "HUF", "price_per_kwh": 43.0, "peak": 48.0, "off_peak": 32.0, "tou": True},
    "Switzerland": {"currency": "CHF", "price_per_kwh": 0.30, "peak": 0.34, "off_peak": 0.23, "tou": True},
}


def _save_config():
    os.makedirs(CONFIG_DIR, exist_ok=True)
    with open(CONFIG_FILE, "w") as f:
        json.dump(CFG, f, indent=2)


# -- widget coordination --------------------------------------------------
WIDGET_NAME = "power"
WIDGET_DIR = (os.path.join(os.environ.get("APPDATA")
                           or os.path.join(os.path.expanduser("~"), "AppData", "Roaming"),
                           "NoClickDock")
              if platform.system() == "Windows" else
              os.path.join(os.path.expanduser("~"), ".config", "status-widgets"))
CORNER_FILE = os.path.join(WIDGET_DIR, "corner.json")
STACK_GAP = 50


def _ensure_widget_dir():
    os.makedirs(WIDGET_DIR, exist_ok=True)


def _register_widget(name, xid=None):
    _ensure_widget_dir()
    with open(os.path.join(WIDGET_DIR, f"{name}.json"), "w") as f:
        json.dump({"pid": os.getpid(), "name": name, "xid": xid}, f)


def _unregister_widget(name):
    try:
        os.remove(os.path.join(WIDGET_DIR, f"{name}.json"))
    except OSError:
        pass


def _pid_alive(pid):
    if IS_WINDOWS:
        import ctypes
        h = ctypes.windll.kernel32.OpenProcess(0x100000, False, pid)
        if h:
            ctypes.windll.kernel32.CloseHandle(h); return True
        return False
    try:
        os.kill(pid, 0); return True
    except OSError:
        return False


def _get_active_widgets():
    _ensure_widget_dir()
    widgets = []
    for fname in sorted(os.listdir(WIDGET_DIR)):
        if fname.endswith(".json") and fname != "corner.json":
            path = os.path.join(WIDGET_DIR, fname)
            try:
                with open(path) as f:
                    d = json.load(f)
                if d.get("pid") and _pid_alive(d["pid"]):
                    widgets.append(d["name"])
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


def _write_corner(idx):
    _ensure_widget_dir()
    with open(CORNER_FILE, "w") as f:
        json.dump({"corner_index": idx, "timestamp": time.time()}, f)


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


# -- design tokens --------------------------------------------------------
FG = (0.96, 0.96, 0.94)
FG_DIM = (0.54, 0.54, 0.50)
DOT_RADIUS = 14
RING_RADIUS = 18
GLOW_RADIUS = 22
WIN_SIZE = 44        # avatar + bubble fit in this; must match DOT in sh-widgetbar.py
AVATAR_RADIUS = 17   # the brand disc
BUBBLE_RADIUS = 5    # status bubble, top-right, chat-app style

# Thermal alerts: a toast the moment a component crosses these, named.
# VRAM first - on GDDR6X with aged pads it is the limit that actually bites.
CORE_ALERT_C = 80
VRAM_ALERT_C = 90
CPU_ALERT_C = 85

POWER_COLORS = {
    "low": (0.13, 0.77, 0.37), "moderate": (0.23, 0.51, 0.96),
    "high": (0.92, 0.70, 0.03), "extreme": (0.94, 0.27, 0.27),
    "unknown": (0.42, 0.42, 0.50),
}
SLICE_COLORS = {
    "CPU": (0.23, 0.51, 0.96), "GPU": (0.13, 0.77, 0.37),
    "Board": (0.54, 0.54, 0.50), "NVMe": (0.92, 0.70, 0.03),
    "Displays": (0.98, 0.45, 0.09), "USB": (0.42, 0.42, 0.50),
}
TOAST_DURATION_MS = 4000
BOLT_LOGO_PATH = "M13 2L3 14h9l-1 8 10-12h-9l1-8z"
POWER_BRAND = (0.910, 0.529, 0.227)   # ember amber #e8873a
POWER_INK = (0.055, 0.051, 0.043)


# -- static hardware info -------------------------------------------------

def _get_system_info():
    info = {}

    # CPU
    try:
        with open("/proc/cpuinfo") as f:
            for line in f:
                if "model name" in line:
                    info["cpu_model"] = line.split(":")[1].strip()
                    break
    except OSError:
        pass

    # Motherboard
    for cmd in [["cat", "/sys/devices/virtual/dmi/id/board_name"],
                ["sudo", "-n", "dmidecode", "-t", "baseboard"]]:
        try:
            out = subprocess.check_output(cmd, timeout=3, stderr=subprocess.DEVNULL).decode()
            if "Product Name" in out:
                for l in out.splitlines():
                    if "Product Name" in l:
                        info["mobo"] = l.split(":")[1].strip(); break
            elif out.strip():
                info["mobo"] = out.strip()
            if info.get("mobo"):
                break
        except (subprocess.SubprocessError, OSError):
            continue

    # RAM
    try:
        out = subprocess.check_output(
            ["sudo", "-n", "dmidecode", "-t", "memory"], timeout=3, stderr=subprocess.DEVNULL
        ).decode()
        cnt = 0; sz = tp = sp = mfr = None
        for l in out.splitlines():
            l = l.strip()
            if l.startswith("Size:") and "No Module" not in l:
                cnt += 1; sz = l.split(":")[1].strip()
            elif l.startswith("Type:") and "Unknown" not in l and "Error" not in l and not tp:
                tp = l.split(":")[1].strip()
            elif l.startswith("Configured Memory Speed:") and not sp:
                sp = l.split(":")[1].strip()
            elif l.startswith("Manufacturer:") and not mfr:
                v = l.split(":")[1].strip()
                if v not in ("Unknown", "Not Specified", ""): mfr = v
        parts = [x for x in [f"{cnt}x {sz}" if cnt and sz else None, tp, sp, mfr] if x]
        if parts:
            info["ram_desc"] = " · ".join(parts)
    except (subprocess.SubprocessError, OSError):
        pass

    # NVMe drives
    nvmes = []
    hwmon = "/sys/class/hwmon"
    try:
        for entry in sorted(os.listdir(hwmon)):
            path = os.path.join(hwmon, entry)
            try:
                with open(os.path.join(path, "name")) as f:
                    if f.read().strip() != "nvme":
                        continue
            except OSError:
                continue
            model = "NVMe"
            try:
                with open(os.path.join(path, "device", "model")) as f:
                    model = f.read().strip()
            except OSError:
                pass
            nvmes.append({"hwmon": path, "model": model})
    except OSError:
        pass
    info["nvmes"] = nvmes

    # Monitors — only those with an active resolution (filters BMC/IPMI virtual displays)
    monitors = []
    try:
        out = subprocess.check_output(["xrandr", "--query"],
                                      timeout=5, stderr=subprocess.DEVNULL).decode()
        active_ports = []
        for line in out.splitlines():
            if " connected" in line:
                parts = line.split()
                port = parts[0]
                res = ""
                for p in parts:
                    if re.match(r'\d+x\d+\+', p):
                        res = p.split("+")[0]; break
                if res:  # Only include displays with active resolution
                    active_ports.append({"port": port, "res": res})
    except (subprocess.SubprocessError, OSError):
        active_ports = []

    # Gather all connected DRM connectors with valid EDID
    edid_pool = []
    try:
        for card_dir in sorted(os.listdir("/sys/class/drm")):
            full = os.path.join("/sys/class/drm", card_dir)
            status_f = os.path.join(full, "status")
            edid_f = os.path.join(full, "edid")
            try:
                with open(status_f) as f:
                    if f.read().strip() != "connected":
                        continue
            except OSError:
                continue
            try:
                data = open(edid_f, "rb").read()
                if len(data) < 128:
                    data = subprocess.check_output(
                        ["sudo", "-n", "cat", edid_f], timeout=3, stderr=subprocess.DEVNULL)
            except (OSError, subprocess.SubprocessError):
                data = b""
            if len(data) < 128:
                continue
            mid = (data[8] << 8) | data[9]
            mfr = (chr(((mid >> 10) & 0x1f) + 64) +
                   chr(((mid >> 5) & 0x1f) + 64) +
                   chr((mid & 0x1f) + 64))
            mn = ""
            for i in range(54, min(len(data), 256), 18):
                if (len(data) > i+17 and data[i] == 0 and data[i+1] == 0
                        and data[i+2] == 0 and data[i+3] == 0xfc):
                    mn = bytes(data[i+5:i+18]).decode("ascii", "ignore").strip()
                    break
            name = f"{mfr} {mn}".strip() if mn else mfr
            h_cm, v_cm = data[21], data[22]
            size_in = est_watts = None
            if h_cm and v_cm:
                size_in = math.sqrt(h_cm**2 + v_cm**2) / 2.54
                # inches * 1.1 + 2 is a generic fit that assumes a plain SDR
                # panel; it underestimates 4K/HDR displays, whose rated draw
                # runs far higher (a 27" 4K HDR is ~65W on the box vs 32W
                # here). The 1.5x buffer keeps the estimate honest rather
                # than flattering. Still an estimate, not a measurement.
                est_watts = round((size_in * 1.1 + 2) * 1.5)
            # Unique key: manufacturer ID + serial (bytes 12-15)
            serial = int.from_bytes(data[12:16], "little")
            edid_pool.append({"mfr_serial": f"{mfr}_{serial}", "name": name,
                              "size_in": size_in, "est_watts": est_watts})
    except OSError:
        pass

    # Match xrandr active ports to EDID entries (by order, since port names differ)
    used_edids = set()
    for ap in active_ports:
        best = None
        for idx, ed in enumerate(edid_pool):
            if idx not in used_edids:
                best = idx; break
        if best is not None:
            ed = edid_pool[best]
            used_edids.add(best)
            monitors.append({
                "port": ap["port"], "name": ed["name"],
                "res": ap.get("res", ""), "size_in": ed["size_in"],
                "est_watts": ed["est_watts"],
            })
        else:
            monitors.append({
                "port": ap["port"], "name": "Unknown",
                "res": ap.get("res", ""), "size_in": None, "est_watts": None,
            })
    info["monitors"] = monitors

    # USB peripherals
    peripherals = []
    skip = {"root hub", "hub", "virtual", "smart card reader"}
    try:
        out = subprocess.check_output(["lsusb"], timeout=5, stderr=subprocess.DEVNULL).decode()
        for line in out.splitlines():
            m = re.match(r'Bus \d+ Device \d+: ID [0-9a-f:]+ (.+)', line)
            if m:
                n = m.group(1).strip()
                if not any(s in n.lower() for s in skip):
                    peripherals.append(n)
    except (subprocess.SubprocessError, OSError):
        pass
    info["peripherals"] = peripherals

    return info


# -- hardware polling -----------------------------------------------------

def _read_cpu_power():
    p = "/sys/class/powercap/intel-rapl/intel-rapl:0/energy_uj"
    try:
        with open(p) as f: e1 = int(f.read().strip())
        time.sleep(0.5)
        with open(p) as f: e2 = int(f.read().strip())
        return max(0, (e2 - e1) / 500000.0)
    except (OSError, ValueError):
        return None


def _short_gpu_name(name):
    """Drop vendor/brand noise that repeats on every card."""
    for prefix in ("NVIDIA GeForce ", "NVIDIA ", "AMD Radeon ", "AMD ", "Intel(R) "):
        if name.startswith(prefix):
            return name[len(prefix):]
    return name


_VRAM_TOOL = "/usr/local/bin/gddr6-oneshot"
_vram_retry_at = 0.0  # after a failure, leave the tool alone until this time


def _read_vram_temps():
    """GDDR6X memory-junction temps by PCI bus id, e.g. {"11:00.0": 62}.

    nvidia-smi on Linux never exposes memory temps; gddr6-oneshot (built from
    olealgoritme/gddr6) reads them from the GPU's registers. It needs root, so
    it runs via a NOPASSWD sudoers rule for exactly that binary. Cards it
    doesn't support (e.g. plain-GDDR6 3060) simply don't appear. Any failure
    (no binary, no sudoers rule, a slow call) backs the probe off for a minute;
    meanwhile the widget just shows core temps as before.
    """
    global _vram_retry_at
    if time.time() < _vram_retry_at or not os.path.exists(_VRAM_TOOL):
        return {}
    try:
        out = subprocess.check_output(
            ["sudo", "-n", _VRAM_TOOL],
            timeout=5, stderr=subprocess.DEVNULL,
        ).decode()
        temps = {}
        for line in out.splitlines():
            parts = line.split()  # "11:00.0 62" (skips "Device: ..." lines)
            if len(parts) == 2 and ":" in parts[0] and parts[1].isdigit():
                temps[parts[0]] = int(parts[1])
        return temps
    except (subprocess.SubprocessError, OSError):
        # A transient hiccup (busy PCI bus, slow sudo) must not blind the widget
        # until restart: back off a minute, then try again.
        _vram_retry_at = time.time() + 60
        return {}


def _read_gpus():
    """Every GPU in the machine, in device-index order.

    nvidia-smi prints one CSV line per card, so each line is parsed on its
    own — a second card is not a special case. Returns [] when no GPU tool
    answers; sensors a card does not expose come back as "[N/A]" and are
    kept as None rather than being faked as zero.
    """
    def num(tok, cast=float):
        tok = tok.strip()
        try:
            return cast(float(tok))
        except ValueError:
            return None  # "[N/A]" on cards without that sensor

    # NVIDIA
    try:
        out = subprocess.check_output(
            ["nvidia-smi", "--query-gpu=index,power.draw,power.limit,temperature.gpu,"
             "memory.used,memory.total,name,utilization.gpu,fan.speed,pci.bus_id",
             "--format=csv,noheader,nounits"],
            timeout=5, stderr=subprocess.DEVNULL,
        ).decode().strip()
        gpus = []
        for line in out.splitlines():
            p = [x.strip() for x in line.split(",")]
            if len(p) < 7:
                continue
            idx = num(p[0], int)
            if idx is None:
                continue
            # "00000000:11:00.0" → "11:00.0", matching gddr6-oneshot output
            bus = p[9].lower().split(":", 1)[1] if len(p) > 9 and ":" in p[9] else None
            gpus.append({
                "index": idx,
                "power": num(p[1]) or 0.0,
                "power_limit": num(p[2]) or 0.0,
                "temp": num(p[3], int),
                "vram_used": num(p[4]) or 0.0,
                "vram_total": num(p[5]) or 0.0,
                "name": p[6],
                "util": num(p[7], int) if len(p) > 7 else None,
                "fan": num(p[8], int) if len(p) > 8 else None,
                "vram_temp": None,
            })
            gpus[-1]["bus"] = bus
        if gpus:
            vram = _read_vram_temps()
            for g in gpus:
                g["vram_temp"] = vram.get(g["bus"])
            return gpus
    except (subprocess.SubprocessError, OSError, ValueError, IndexError):
        pass
    # AMD
    try:
        out = subprocess.check_output(
            ["rocm-smi", "--showpower", "--showtemp", "--showmeminfo", "vram", "--json"],
            timeout=5, stderr=subprocess.DEVNULL,
        ).decode()
        gpus = []
        for i, d in enumerate(json.loads(out).values()):
            gpus.append({
                "index": i,
                "power": float(d.get("Average Graphics Package Power (W)", 0)),
                "power_limit": float(d.get("Max Graphics Package Power (W)", 0)),
                "temp": int(float(d.get("Temperature (Sensor edge) (C)", 0))),
                "vram_used": float(d.get("VRAM Total Used Memory (B)", 0)) / 1048576,
                "vram_total": float(d.get("VRAM Total Memory (B)", 0)) / 1048576,
                "name": d.get("Card series", "AMD GPU"),
                "util": None,
                "fan": None,
            })
        return gpus
    except (subprocess.SubprocessError, OSError, ValueError, KeyError, IndexError):
        return []


def _read_nvme_temps(nvmes):
    results = []
    for nv in nvmes:
        temp = None
        try:
            with open(os.path.join(nv["hwmon"], "temp1_input")) as f:
                temp = int(f.read().strip()) / 1000.0
        except (OSError, ValueError):
            pass
        results.append({"model": nv["model"], "temp": temp, "watts": 5})  # NVMe ~5W each
    return results


def _read_cpu_temp():
    try:
        for entry in os.listdir("/sys/class/hwmon"):
            p = os.path.join("/sys/class/hwmon", entry)
            with open(os.path.join(p, "name")) as f:
                name = f.read().strip()
            if name in ("k10temp", "coretemp", "zenpower"):
                with open(os.path.join(p, "temp1_input")) as f:
                    return int(f.read().strip()) / 1000.0
    except (OSError, ValueError):
        pass
    return None


def _read_ram():
    try:
        info = {}
        with open("/proc/meminfo") as f:
            for line in f:
                p = line.split()
                if len(p) >= 2: info[p[0].rstrip(":")] = int(p[1])
        total = info.get("MemTotal", 0)
        used = total - info.get("MemAvailable", 0)
        return {"used_gb": used/1048576, "total_gb": total/1048576,
                "pct": used/total*100 if total else 0}
    except (OSError, ValueError, ZeroDivisionError):
        return None


def _read_cpu_usage():
    def _r():
        with open("/proc/stat") as f:
            p = f.readline().split()
        v = [int(x) for x in p[1:9]]
        return v[3]+v[4], sum(v)
    try:
        i1, t1 = _r(); time.sleep(0.5); i2, t2 = _r()
        dt = t2 - t1
        return (1.0 - (i2-i1)/dt) * 100 if dt else 0
    except (OSError, ValueError, IndexError):
        return None


def _get_power_level(w):
    if w is None: return "unknown"
    if w < 100: return "low"
    if w < 250: return "moderate"
    if w < 500: return "high"
    return "extreme"


def _fmt_w(w):
    return f"{w:.0f}W" if w is not None else "—"


def _fmt_temp(t):
    return f"{t:.0f}°C" if t is not None else ""


def _parse_hours(spec):
    """'6-13,15-22' -> [(6, 13), (15, 22)]"""
    out = []
    for part in str(spec or "").split(","):
        if "-" in part:
            a, b = part.strip().split("-", 1)
            try:
                out.append((int(a), int(b)))
            except ValueError:
                pass
    return out


def _tou_in_peak(t):
    if CFG.get("tou_weekend_offpeak", True) and t.weekday() >= 5:
        return False
    return any(a <= t.hour < b for a, b in _parse_hours(CFG.get("peak_hours")))


def _rate_now(when=None):
    """(rate, window, next_change): window is None on a flat tariff."""
    if not CFG.get("tou"):
        return CFG["price_per_kwh"], None, None
    now = when or datetime.now()
    in_peak = _tou_in_peak(now)
    probe = now.replace(minute=0, second=0, microsecond=0)
    nxt = None
    for i in range(1, 24 * 7 + 1):
        t = probe + timedelta(hours=i)
        if _tou_in_peak(t) != in_peak:
            nxt = t
            break
    key = "peak_rate" if in_peak else "off_peak_rate"
    rate = CFG.get(key) or CFG["price_per_kwh"]
    return rate, ("peak" if in_peak else "off-peak"), nxt


def _blended_rate():
    """Flat-equivalent rate for projections: peak vs off-peak hours in a week."""
    if not CFG.get("tou"):
        return CFG["price_per_kwh"]
    days = 5 if CFG.get("tou_weekend_offpeak", True) else 7
    peak_h = days * sum(max(0, b - a) for a, b in _parse_hours(CFG.get("peak_hours")))
    peak = CFG.get("peak_rate") or CFG["price_per_kwh"]
    off = CFG.get("off_peak_rate") or CFG["price_per_kwh"]
    return (peak_h * peak + (168 - peak_h) * off) / 168


def _cost_monthly(watts):
    return watts * 24 * 30 / 1000 * _blended_rate()


def _today_from_logs(logger):
    """(kwh_today, cost_today, [(hour, watts)] for the last 24h) from the hourly
    CSVs plus the hour still in memory. Empty when logging is off."""
    now = datetime.now()
    rows = []
    for day in (now - timedelta(days=1), now):
        path = os.path.join(LOG_DIR, day.strftime("%Y-%m"), day.strftime("%Y-%m-%d") + ".csv")
        try:
            with open(path) as f:
                for r in csv.DictReader(f):
                    try:
                        ts = datetime.strptime(r["timestamp"], "%Y-%m-%d %H:%M:%S")
                        rows.append((ts, float(r["avg_total_w"]),
                                     float(r["kwh_period"]), float(r["cost_period"])))
                    except (KeyError, ValueError):
                        pass
        except OSError:
            pass
    today = [r for r in rows if r[0].date() == now.date()]
    kwh = sum(r[2] for r in today)
    cost = sum(r[3] for r in today)
    samples = getattr(logger, "_samples", None)
    if samples:
        w = sum(x["total_w"] for x in samples) / len(samples)
        hrs = max(0.0, (time.time() - logger._last_log) / 3600)
        kwh += w / 1000 * hrs
        cost += w / 1000 * hrs * _rate_now()[0]
    cutoff = now - timedelta(hours=24)
    return kwh, cost, [(r[0], r[1]) for r in rows if r[0] >= cutoff]


# -- data logging ---------------------------------------------------------


def _regen_report():
    """Rebuild the HTML report from PM-Log in the background (after each log write)."""
    script = os.path.join(LOG_DIR, "generate_report.py")
    if not os.path.exists(script):
        return
    def work():
        try:
            subprocess.run([sys.executable, script], cwd=LOG_DIR, timeout=120,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        except (subprocess.SubprocessError, OSError):
            pass
    threading.Thread(target=work, daemon=True).start()


class DataLogger:
    def __init__(self):
        self._samples = []
        self._last_log = time.time()
        interval = CFG.get("log_interval", "off")
        self._interval_secs = {
            "hourly": 3600, "daily": 86400, "weekly": 604800, "monthly": 2592000,
        }.get(interval, 0)

    def add_sample(self, data):
        if self._interval_secs <= 0:
            return
        self._samples.append({
            "time": time.time(),
            "total_w": data.get("total_watts", 0),
            "cpu_w": data.get("cpu_watts") or 0,
            "gpu_w": sum(g["power"] for g in data.get("gpus") or []),
            "cpu_temp": data.get("cpu_temp"),
            "gpu_temp": max((g["temp"] for g in data.get("gpus") or []
                             if g["temp"] is not None), default=None),
            "cpu_pct": data.get("cpu_usage"),
            "ram_pct": data["ram"]["pct"] if data.get("ram") else None,
        })
        if time.time() - self._last_log >= self._interval_secs:
            self._write_log()
            self._last_log = time.time()
            self._samples.clear()

    def _write_log(self):
        if not self._samples:
            return
        now = datetime.now()
        date_dir = os.path.join(LOG_DIR, now.strftime("%Y-%m"))
        os.makedirs(date_dir, exist_ok=True)
        path = os.path.join(date_dir, now.strftime("%Y-%m-%d") + ".csv")
        exists = os.path.exists(path)

        # Compute averages
        n = len(self._samples)
        avg = {
            "timestamp": now.strftime("%Y-%m-%d %H:%M:%S"),
            "avg_total_w": sum(s["total_w"] for s in self._samples) / n,
            "avg_cpu_w": sum(s["cpu_w"] for s in self._samples) / n,
            "avg_gpu_w": sum(s["gpu_w"] for s in self._samples) / n,
            "avg_cpu_temp": sum(s["cpu_temp"] for s in self._samples if s["cpu_temp"]) / max(1, sum(1 for s in self._samples if s["cpu_temp"])),
            "avg_gpu_temp": sum(s["gpu_temp"] for s in self._samples if s["gpu_temp"]) / max(1, sum(1 for s in self._samples if s["gpu_temp"])),
            "avg_cpu_pct": sum(s["cpu_pct"] for s in self._samples if s["cpu_pct"]) / max(1, sum(1 for s in self._samples if s["cpu_pct"])),
            "avg_ram_pct": sum(s["ram_pct"] for s in self._samples if s["ram_pct"]) / max(1, sum(1 for s in self._samples if s["ram_pct"])),
            "samples": n,
            "kwh_period": sum(s["total_w"] for s in self._samples) / n * self._interval_secs / 3600000,
            "cost_period": _cost_monthly(sum(s["total_w"] for s in self._samples) / n) / 720 * self._interval_secs / 3600,
        }

        fields = list(avg.keys())
        try:
            with open(path, "a", newline="") as f:
                w = csv.DictWriter(f, fieldnames=fields)
                if not exists:
                    w.writeheader()
                w.writerow({k: f"{v:.2f}" if isinstance(v, float) else v for k, v in avg.items()})
        except OSError as e:
            print(f"Log error: {e}", file=sys.stderr)
            return
        _regen_report()   # the report rebuilds itself after every hourly row


# -- SVG ------------------------------------------------------------------

def _parse_svg_path(d):
    tokens = re.findall(
        r'[MmLlHhVvCcSsQqTtAaZz]|[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?', d)
    cmds, i, cmd = [], 0, None
    while i < len(tokens):
        if tokens[i].isalpha(): cmd = tokens[i]; i += 1
        nums = []
        while i < len(tokens) and not tokens[i].isalpha():
            nums.append(float(tokens[i])); i += 1
        if cmd: cmds.append((cmd, nums))
    return cmds


def draw_svg_logo(cr, path_d, cx, cy, size, vb=24):
    s = size / vb; ox, oy = cx - size/2, cy - size/2
    x = y = sx = sy = 0.0
    for cmd, nums in _parse_svg_path(path_d):
        if cmd == 'M':
            for j in range(0, len(nums), 2):
                x, y = nums[j], nums[j+1]
                (cr.move_to if j == 0 else cr.line_to)(ox+x*s, oy+y*s)
                if j == 0: sx, sy = x, y
        elif cmd == 'L':
            for j in range(0, len(nums), 2):
                x, y = nums[j], nums[j+1]; cr.line_to(ox+x*s, oy+y*s)
        elif cmd == 'l':
            for j in range(0, len(nums), 2):
                x += nums[j]; y += nums[j+1]; cr.line_to(ox+x*s, oy+y*s)
        elif cmd == 'H':
            for v in nums: x = v; cr.line_to(ox+x*s, oy+y*s)
        elif cmd == 'h':
            for v in nums: x += v; cr.line_to(ox+x*s, oy+y*s)
        elif cmd == 'V':
            for v in nums: y = v; cr.line_to(ox+x*s, oy+y*s)
        elif cmd == 'v':
            for v in nums: y += v; cr.line_to(ox+x*s, oy+y*s)
        elif cmd in ('Z', 'z'): cr.close_path(); x, y = sx, sy


# ═══════════════════════════════════════════════════════════════════════════
# DotWindow
# ═══════════════════════════════════════════════════════════════════════════

class DotWindow(Gtk.Window):
    def __init__(self):
        super().__init__(type=Gtk.WindowType.TOPLEVEL)
        self.set_title("Power Monitor")
        self.set_decorated(False); self.set_keep_above(True)
        self.set_skip_taskbar_hint(True); self.set_skip_pager_hint(True)
        self.set_type_hint(Gdk.WindowTypeHint.UTILITY)
        self.set_default_size(WIN_SIZE, WIN_SIZE)
        self.set_size_request(WIN_SIZE, WIN_SIZE)
        self.set_resizable(False); self.move(20, 20)
        vis = self.get_screen().get_rgba_visual()
        if vis: self.set_visual(vis)
        self.set_app_paintable(True); self.set_can_focus(True); self.set_accept_focus(True)

        for sig, cb in [("draw", self._on_draw), ("button-press-event", self._on_button),
                         ("button-release-event", self._on_button_release),
                         ("motion-notify-event", self._on_motion),
                         ("enter-notify-event", self._on_enter),
                         ("leave-notify-event", self._on_leave),
                         ("key-press-event", self._on_key_press),
                         ("destroy", Gtk.main_quit)]:
            self.connect(sig, cb)
        self.set_events(
            Gdk.EventMask.BUTTON_PRESS_MASK | Gdk.EventMask.BUTTON_RELEASE_MASK
            | Gdk.EventMask.POINTER_MOTION_MASK | Gdk.EventMask.ENTER_NOTIFY_MASK
            | Gdk.EventMask.LEAVE_NOTIFY_MASK | Gdk.EventMask.KEY_PRESS_MASK)

        self.sys_info = _get_system_info()
        self.color = POWER_COLORS["unknown"]
        self.data = {}
        self.pulse_phase = 0.0
        self.panel = None
        self.dragging = False
        self._drag_ox = self._drag_oy = 0
        self._corner_margin = 8
        self._prev_level = None
        self._prev_hot = set()   # components currently over a thermal threshold
        self._toast = None
        self._avg_samples = []
        self._avg_watts = 0
        self._donut_acc = []      # samples since the last donut publish
        self._donut_avg = None    # published minute average, None until first
        self._donut_ts = time.monotonic()

        self.logger = DataLogger()

        _register_widget(WIDGET_NAME)
        self.connect("realize", lambda w: _register_widget(WIDGET_NAME, _xid_of(w)))
        self._last_corner_ts = 0
        self._docked = False
        self._last_bar_ts = 0
        self._bar_rect = None
        self._bar_target = None
        self._bar_orientation = "horizontal"
        self.connect("destroy", lambda w: _unregister_widget(WIDGET_NAME))

        GLib.timeout_add(50, self._tick_pulse)
        GLib.timeout_add(50, self._watch_corner)   # 50 ms: tracks a bar drag smoothly
        threading.Thread(target=self._poll_loop, daemon=True).start()

    def _on_draw(self, widget, cr):
        cr.set_operator(0); cr.paint(); cr.set_operator(2)   # clear to transparent
        if self._docked:   # the bar has a hole here: paint its surface under us
            cr.set_source_rgba(*_BAR_FILL)
            cr.paint()
        cx = cy = WIN_SIZE / 2
        R = AVATAR_RADIUS
        sr, sg, sb = self.color          # status colour lives only in the bubble

        # Something wrong: a slow breathing ring around the avatar, nothing else.
        if False:
            pulse = 0.5 + 0.5 * math.sin(self.pulse_phase)
            cr.set_source_rgba(sr, sg, sb, 0.10 + 0.25 * pulse)
            cr.set_line_width(1)
            cr.arc(cx, cy, R + 3, 0, 2 * math.pi)
            cr.stroke()

        # brand disc, with a faint hairline so dark brands still read on the pill
        cr.set_source_rgb(*POWER_BRAND)
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
        cr.set_source_rgb(*POWER_INK)
        draw_svg_logo(cr, BOLT_LOGO_PATH, cx, cy, R * 2 * 0.62, 24)
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
        self.pulse_phase += 0.08; self.queue_draw(); return True

    def _poll_loop(self):
        while True:
            d = self._fetch()
            GLib.idle_add(self._apply_data, d)
            time.sleep(CFG["poll_interval"])

    def _fetch(self):
        cpu_watts = _read_cpu_power()
        gpus = _read_gpus()
        cpu_temp = _read_cpu_temp()
        cpu_usage = _read_cpu_usage()
        ram = _read_ram()
        nvmes = _read_nvme_temps(self.sys_info.get("nvmes", []))
        gpu_w = sum(g["power"] for g in gpus)
        nvme_w = sum(n["watts"] for n in nvmes)
        overhead = CFG["overhead_watts"]
        periph_w = CFG["peripheral_watts"] if self.sys_info.get("peripherals") else 0

        # PC total = only what goes through the PC PSU (not monitors — they have own PSU)
        if cpu_watts is not None:
            total = cpu_watts + gpu_w + overhead + nvme_w + periph_w
        elif cpu_usage is not None:
            total = (10 + cpu_usage/100*190) + gpu_w + overhead + nvme_w + periph_w
        else:
            total = gpu_w + overhead + nvme_w + periph_w

        # Monitor power is separate (estimated from EDID screen size, own PSU)
        mon_est = sum(m.get("est_watts") or 0 for m in self.sys_info.get("monitors", []))

        return {"level": _get_power_level(total), "cpu_watts": cpu_watts, "gpus": gpus,
                "cpu_temp": cpu_temp, "cpu_usage": cpu_usage, "ram": ram, "nvmes": nvmes,
                "nvme_w": nvme_w, "mon_est": mon_est, "periph_w": periph_w,
                "total_watts": total, "last_check": datetime.now(timezone.utc)}

    def _apply_data(self, data):
        # Running average
        self._avg_samples.append(data.get("total_watts", 0))
        if len(self._avg_samples) > 1800:  # ~1 hour at 2s poll
            self._avg_samples = self._avg_samples[-1800:]
        self._avg_watts = sum(self._avg_samples) / len(self._avg_samples)
        data["avg_watts"] = self._avg_watts

        # Per-component minute average for the donut. Polling every 2s makes
        # the slices twitch on every sample, which reads as noise rather than
        # information — the donut is about the shape of the draw, not the
        # instant. Accumulate here and publish a new set of slices once a
        # minute; until the first minute is up, show the live values so the
        # chart is never empty on startup.
        self._donut_acc.append({
            "cpu": data.get("cpu_watts") or 0,
            "gpu": sum(g["power"] for g in data.get("gpus") or []),
            "nvme": data.get("nvme_w", 0),
            "periph": data.get("periph_w", 0),
            "total": data.get("total_watts", 0),
        })
        now = time.monotonic()
        if now - self._donut_ts >= DONUT_AVG_SECS and self._donut_acc:
            n = len(self._donut_acc)
            self._donut_avg = {k: sum(s[k] for s in self._donut_acc) / n
                               for k in self._donut_acc[0]}
            self._donut_acc = []
            self._donut_ts = now
        data["donut"] = self._donut_avg  # None until the first minute closes

        self.logger.add_sample(data)

        # Thermal alerts: toast once per component per excursion, not per degree.
        hot = {}
        for g in data.get("gpus") or []:
            tag = f'GPU{g["index"]} {_short_gpu_name(g["name"])}'
            if g.get("vram_temp") is not None and g["vram_temp"] >= VRAM_ALERT_C:
                hot[tag] = f'{tag}: VRAM {g["vram_temp"]:.0f}°C'
            elif g.get("temp") is not None and g["temp"] >= CORE_ALERT_C:
                hot[tag] = f'{tag}: core {g["temp"]:.0f}°C'
        if data.get("cpu_temp") is not None and data["cpu_temp"] >= CPU_ALERT_C:
            hot["CPU"] = f'CPU {data["cpu_temp"]:.0f}°C'
        for tag in sorted(set(hot) - self._prev_hot):
            self._show_toast(hot[tag], POWER_COLORS["extreme"])
        self._prev_hot = set(hot)

        new_level = data["level"]
        if self._prev_level is not None and new_level != self._prev_level:
            c = POWER_COLORS.get(new_level, POWER_COLORS["unknown"])
            labels = {"low": "Idle", "moderate": "Normal", "high": "High load", "extreme": "Full send"}
            msg = labels.get(new_level, "?")
            if data.get("total_watts"): msg += f"  {data['total_watts']:.0f}W"
            self._show_toast(msg, c)
        self._prev_level = new_level
        self.data = data
        self.color = POWER_COLORS.get(new_level, POWER_COLORS["unknown"])
        self.queue_draw()
        if self.panel and self.panel.get_visible():
            self.panel.update_data(data, self.sys_info)
        return False

    def _show_toast(self, msg, color=None):
        if self._toast:
            try: self._toast.destroy()
            except: pass
        self._toast = ToastWindow(self, msg, color); self._toast.popup()

    def _on_button(self, w, e):
        if e.button == 1:
            self._close_panel(); self.dragging = True
            self._press = (int(e.x_root), int(e.y_root))
            wx, wy = self.get_position()
            self._drag_ox = int(e.x_root) - wx; self._drag_oy = int(e.y_root) - wy
        elif e.button == 3: self._show_context_menu(e)

    def _show_context_menu(self, event):
        menu = Gtk.Menu()
        settings_item = Gtk.MenuItem(label="Settings")
        settings_item.connect("activate", lambda w: self._show_settings())
        menu.append(settings_item)
        report_item = Gtk.MenuItem(label="Open power report")
        report_item.connect("activate", lambda w: self._open_report())
        menu.append(report_item)
        sep = Gtk.SeparatorMenuItem()
        menu.append(sep)
        quit_item = Gtk.MenuItem(label="Quit")
        quit_item.connect("activate", lambda w: self.destroy())
        menu.append(quit_item)
        menu.show_all()
        menu.popup(None, None, None, None, event.button, event.time)

    def _show_settings(self):
        SettingsDialog(self).run_dialog()

    def _on_button_release(self, w, e):
        if e.button == 1:
            self.dragging = False
            px, py = getattr(self, "_press", (None, None))
            if px is not None and abs(int(e.x_root) - px) <= 4 and abs(int(e.y_root) - py) <= 4:
                self._open_report()   # a click, not a drag

    def _open_report(self):
        """Rebuild the HTML report from PM-Log and open it in the browser."""
        def work():
            script = os.path.join(LOG_DIR, "generate_report.py")
            html = os.path.join(LOG_DIR, "power-consumption-report.html")
            if os.path.exists(script):
                try:
                    subprocess.run([sys.executable, script], cwd=LOG_DIR, timeout=60,
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                except (subprocess.SubprocessError, OSError):
                    pass
            if os.path.exists(html):
                subprocess.Popen(["xdg-open", html],
                                 stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                GLib.idle_add(self._show_toast, "Power report opened", None)
            else:
                GLib.idle_add(self._show_toast, "No report yet — logging builds it",
                              POWER_COLORS["unknown"])
        threading.Thread(target=work, daemon=True).start()

    def _on_motion(self, w, e):
        if self.dragging and not self._docked:
            self.move(int(e.x_root)-self._drag_ox, int(e.y_root)-self._drag_oy)

    def _on_key_press(self, w, e):
        if e.keyval == Gdk.KEY_grave:
            if not self._docked: self._cycle_corner()
            return True
        return False

    def _cycle_corner(self):
        self._close_panel()
        n = Gdk.Display.get_default().get_n_monitors() * 2
        c = _read_corner(); ni = (c["corner_index"]+1) % n
        _write_corner(ni); self._apply_corner(ni)

    def _watch_corner(self):
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
        c = _read_corner()
        if c["timestamp"] > self._last_corner_ts:
            self._last_corner_ts = c["timestamp"]
            if c["corner_index"] >= 0: self._apply_corner(c["corner_index"])
        return True

    def _apply_corner(self, ci):
        self._close_panel()
        widgets = _get_active_widgets()
        try: my = widgets.index(WIDGET_NAME)
        except ValueError: my = 0
        disp = Gdk.Display.get_default()
        pos = []
        for i in range(disp.get_n_monitors()):
            g = disp.get_monitor(i).get_geometry()
            pos.append((g.x, g.y, g.width, g.height))
            pos.append((g.x, g.y, g.width, g.height))
        idx = ci % len(pos); mx, my2, sw, sh = pos[idx]
        nw = len(widgets) or 1
        th = (nw-1)*STACK_GAP + WIN_SIZE
        cy = my2 + (sh-th)//2 + my*STACK_GAP
        bx = mx + sw - WIN_SIZE - self._corner_margin if idx % 2 else mx + self._corner_margin
        self.move(bx, cy)

    def _on_enter(self, w, e):
        if not self.dragging: self._show_panel()

    def _on_leave(self, w, e):
        GLib.timeout_add(200, self._check_close_panel)

    def _show_panel(self):
        if self.panel and self.panel.get_visible(): return
        if self.panel: self.panel.destroy()
        self.panel = PanelWindow(self)
        self.panel.update_data(self.data, self.sys_info)
        x, y = self.get_position()
        self.panel.show_all()
        pw = self.panel.get_allocated_width(); ph = self.panel.get_allocated_height()

        # keep the panel on the monitor the dot sits on (not the whole desktop)
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
            if px + pw > mx + mw:          # no room right -> flip left
                px = x - pw - 6
            px = max(mx + 4, min(px, mx + mw - pw - 4))
            py = max(my + 4, min(y, my + mh - ph - 4))
        self.panel.move(px, py)

    def _close_panel(self):
        if self.panel and self.panel.get_visible(): self.panel.hide()

    def _check_close_panel(self):
        if not self.panel or not self.panel.get_visible(): return False
        _, mx, my = Gdk.Display.get_default().get_default_seat().get_pointer().get_position()
        dx, dy = self.get_position()
        if dx <= mx <= dx+WIN_SIZE and dy <= my <= dy+WIN_SIZE: return False
        px, py = self.panel.get_position()
        pw, ph = self.panel.get_allocated_width(), self.panel.get_allocated_height()
        if px <= mx <= px+pw and py <= my <= py+ph: return False
        self._close_panel(); return False


# ═══════════════════════════════════════════════════════════════════════════
# ToastWindow
# ═══════════════════════════════════════════════════════════════════════════

class ToastWindow(Gtk.Window):
    def __init__(self, parent, msg, color=None):
        super().__init__(type=Gtk.WindowType.POPUP)
        self.set_decorated(False); self.set_keep_above(True); self.set_skip_taskbar_hint(True)
        self.set_type_hint(Gdk.WindowTypeHint.NOTIFICATION); self.set_app_paintable(True)
        self.parent_dot = parent; self._opacity = 1.0; self._color = color or FG
        vis = self.get_screen().get_rgba_visual()
        if vis: self.set_visual(vis)
        self.connect("draw", self._on_draw)
        c = self._color
        self._label = Gtk.Label()
        self._label.set_markup(
            f'<span font_family="JetBrains Mono" font_size="9000" '
            f'foreground="#{int(c[0]*255):02x}{int(c[1]*255):02x}{int(c[2]*255):02x}">'
            f'{GLib.markup_escape_text(msg)}</span>')
        self._label.set_margin_start(12); self._label.set_margin_end(12)
        self._label.set_margin_top(8); self._label.set_margin_bottom(8)
        self.add(self._label)

    def popup(self):
        self.show_all()
        x, y = self.parent_dot.get_position()
        self.move(x+WIN_SIZE+8, y+(WIN_SIZE-self.get_allocated_height())//2)
        GLib.timeout_add(TOAST_DURATION_MS, self._start_fade)

    def _start_fade(self): GLib.timeout_add(30, self._fade); return False
    def _fade(self):
        self._opacity -= 0.06
        if self._opacity <= 0: self.destroy(); return False
        self.queue_draw(); return True

    def _on_draw(self, w, cr):
        a = self.get_allocation()
        cr.set_source_rgba(0.08, 0.08, 0.08, 0.92*self._opacity)
        self._rr(cr, 0, 0, a.width, a.height, 6); cr.fill()
        cr.set_source_rgba(0.17, 0.17, 0.17, self._opacity); cr.set_line_width(1)
        self._rr(cr, .5, .5, a.width-1, a.height-1, 6); cr.stroke()
        self._label.set_opacity(self._opacity); return False

    @staticmethod
    def _rr(cr, x, y, w, h, r):
        cr.arc(x+w-r, y+r, r, -math.pi/2, 0); cr.arc(x+w-r, y+h-r, r, 0, math.pi/2)
        cr.arc(x+r, y+h-r, r, math.pi/2, math.pi); cr.arc(x+r, y+r, r, math.pi, 3*math.pi/2)
        cr.close_path()


# ═══════════════════════════════════════════════════════════════════════════
# PanelWindow — two-column layout: info left, donut right
# ═══════════════════════════════════════════════════════════════════════════

class PanelWindow(Gtk.Window):
    def __init__(self, parent):
        super().__init__(type=Gtk.WindowType.TOPLEVEL)
        self.parent_dot = parent
        self.set_decorated(False); self.set_keep_above(True)
        self.set_skip_taskbar_hint(True); self.set_skip_pager_hint(True)
        self.set_type_hint(Gdk.WindowTypeHint.UTILITY if IS_WINDOWS else Gdk.WindowTypeHint.TOOLTIP)
        self.set_resizable(False)
        vis = self.get_screen().get_rgba_visual()
        if vis: self.set_visual(vis)
        self.set_app_paintable(True)
        self.connect("draw", self._on_draw_bg)
        self.connect("leave-notify-event", lambda w, e: GLib.timeout_add(200, parent._check_close_panel))
        self.set_events(Gdk.EventMask.LEAVE_NOTIFY_MASK)
        self.box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=0)
        self.box.set_margin_start(1); self.box.set_margin_end(1)
        self.box.set_margin_top(1); self.box.set_margin_bottom(1)
        self.inner = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=0)
        self.inner.set_margin_start(14); self.inner.set_margin_end(14)
        self.inner.set_margin_top(10); self.inner.set_margin_bottom(10)
        self.box.pack_start(self.inner, True, True, 0)
        self.add(self.box); self._apply_css()

    def _apply_css(self):
        css = b"""
        window { background-color: rgba(20,20,20,0.96); border: 1px solid #2a2a2a; border-radius: 6px; }
        .title { font-family: "Teko"; font-size: 18px; font-weight: bold; color: #F5F5F0; }
        .section { font-family: "JetBrains Mono"; font-size: 9px; font-weight: bold; color: #8A8A80; }
        .name { font-family: "JetBrains Mono"; font-size: 12px; color: #F5F5F0; }
        .footer { font-family: "Inter"; font-size: 9px; color: #5a5a54; }
        .row { background-color: rgba(26,26,26,0.9); border-radius: 4px; padding: 5px 8px; margin: 1px 0; }
        .sep { background-color: #2a2a2a; min-height: 1px; }
        """
        p = Gtk.CssProvider(); p.load_from_data(css)
        Gtk.StyleContext.add_provider_for_screen(
            Gdk.Screen.get_default(), p, Gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)

    def _on_draw_bg(self, w, cr):
        a = self.get_allocation()
        cr.set_source_rgba(0.08, 0.08, 0.08, 0.96); cr.rectangle(0, 0, a.width, a.height); cr.fill()
        cr.set_source_rgba(0.17, 0.17, 0.17, 1); cr.set_line_width(1)
        cr.rectangle(.5, .5, a.width-1, a.height-1); cr.stroke()
        return False

    def update_data(self, data, sys_info=None):
        for c in self.inner.get_children(): self.inner.remove(c)
        si = sys_info or {}
        gpus = data.get("gpus") or []
        level = data.get("level", "unknown")
        ct = POWER_COLORS.get(level, POWER_COLORS["unknown"])
        ch = "#{:02x}{:02x}{:02x}".format(int(ct[0]*255), int(ct[1]*255), int(ct[2]*255))
        cur = CFG["currency"]
        total_w = data.get("total_watts") or 0
        avg_w = data.get("avg_watts") or total_w

        # ── Two-column top: title+badge left, nothing right (donut below) ──
        labels = {"low": "IDLE", "moderate": "NORMAL", "high": "HIGH LOAD", "extreme": "FULL SEND"}
        title_box = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=12)
        left_top = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=2)
        t = Gtk.Label(label="POWER MONITOR"); t.get_style_context().add_class("title"); t.set_halign(Gtk.Align.START)
        left_top.pack_start(t, False, False, 0)
        badge = Gtk.Label()
        badge.set_markup(f'<span font_family="JetBrains Mono" font_size="8500" font_weight="bold" '
                         f'background="{ch}" foreground="#0D0D0D">'
                         f'  {labels.get(level,"—")}  —  {total_w:.0f}W  </span>')
        badge.set_halign(Gtk.Align.START)
        left_top.pack_start(badge, False, False, 0)
        title_box.pack_start(left_top, True, True, 0)

        # Avg + cost on right of title
        avg_box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=1)
        avg_box.set_valign(Gtk.Align.CENTER)
        al = Gtk.Label()
        al.set_markup(f'<span font_family="JetBrains Mono" font_size="8500" foreground="#8A8A80">'
                      f'avg {avg_w:.0f}W</span>')
        al.set_halign(Gtk.Align.END)
        avg_box.pack_start(al, False, False, 0)
        rate_live, tou_window, tou_next = _rate_now()
        cost_h = total_w / 1000 * rate_live
        cl = Gtk.Label()
        cl.set_markup(f'<span font_family="JetBrains Mono" font_size="8500" foreground="#8A8A80">'
                      f'{cost_h:.3f} {cur}/h</span>')
        cl.set_halign(Gtk.Align.END)
        avg_box.pack_start(cl, False, False, 0)

        # Tariff in the gap between badge and avg/cost: the rate every cost
        # figure on the panel is derived from, so it is visible without
        # opening settings. Two decimals reads as money (1.30, not 1.3); a
        # third only when the rate actually needs it (0.185). Whole-unit
        # currencies like JPY/HUF drop the decimals entirely.
        _r = rate_live
        if _r >= 10:                       # JPY, HUF — no decimals
            rate_txt = f"{_r:.0f}"
        elif round(_r, 2) == round(_r, 3):  # 1.30, 0.29
            rate_txt = f"{_r:.2f}"
        else:                               # 0.185
            rate_txt = f"{_r:.3f}"
        rl = Gtk.Label()
        if tou_window:
            # Time-of-use: say which window we are in and when it flips. Peak is
            # the one you act on, so it alone gets the accent.
            when = ""
            if tou_next is not None:
                same_day = tou_next.date() == datetime.now().date()
                when = f'  until {tou_next:%H:%M}' if same_day else f'  until {tou_next:%a %H:%M}'
            wc = "#e8873a" if tou_window == "peak" else "#8A8A80"
            rl.set_markup(f'<span font_family="JetBrains Mono" font_size="8500" '
                          f'foreground="#8A8A80">{rate_txt} {cur}/kWh</span>\n'
                          f'<span font_family="JetBrains Mono" font_size="7500" '
                          f'foreground="{wc}">{tou_window.upper()}{when}</span>')
            rl.set_justify(Gtk.Justification.RIGHT)
        else:
            rl.set_markup(f'<span font_family="JetBrains Mono" font_size="8500" '
                          f'foreground="#8A8A80">{rate_txt} {cur}/kWh</span>')
        rl.set_halign(Gtk.Align.END)
        rl.set_valign(Gtk.Align.CENTER)
        rl.set_tooltip_text("Tariff used for all cost figures — right-click → Settings to change")
        title_box.pack_end(rl, False, False, 12)

        title_box.pack_end(avg_box, False, False, 0)
        self.inner.pack_start(title_box, False, False, 0)
        self._sep(6)

        # ── Two-column body: left = info, right = donut ──
        body = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=14)

        # LEFT COLUMN
        left = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=0)

        # POWER DRAW section
        cpu_w = data.get("cpu_watts") or 0
        gpu_w = sum(g["power"] for g in gpus)
        # When any card reports a VRAM temp, every row gets the extra column so
        # core temps stay aligned; non-GPU rows just leave it blank.
        vram_col = any(g.get("vram_temp") is not None for g in gpus)
        self._section_cols(left, "POWER DRAW",
                           [("W / CAP", 7), ("CORE", 4)] + ([("MEM", 4)] if vram_col else []))
        self._comp_row(left, "CPU", si.get("cpu_model", "—"), cpu_w, data.get("cpu_temp"),
                       vram_col=vram_col)
        # One row per card — "GPU" alone when there is only one, GPU0/GPU1... when several
        for g in gpus:
            tag = "GPU" if len(gpus) == 1 else f'GPU{g["index"]}'
            self._comp_row(left, tag, _short_gpu_name(g["name"]), g["power"], g["temp"],
                           vram_temp=g.get("vram_temp"), vram_col=vram_col,
                           cap=g.get("power_limit") or None)
        self._comp_row(left, "Board", si.get("mobo") or "—", CFG["overhead_watts"], None,
                       vram_col=vram_col)

        # NVMe section
        nvmes = data.get("nvmes", [])
        if nvmes:
            self._section_label(left, "STORAGE")
            for nv in nvmes:
                self._comp_row(left, "NVMe", nv["model"], nv["watts"], nv["temp"], vram_col=vram_col)

        # Displays (own PSU — not counted in PC total)
        monitors = si.get("monitors", [])
        if monitors:
            self._section_label(left, f"DISPLAYS ({len(monitors)}) — own PSU")
            for m in monitors:
                sz = f' {m["size_in"]:.0f}"' if m.get("size_in") else ""
                res = f' {m["res"]}' if m.get("res") else ""
                est = m.get("est_watts")
                self._comp_row(left, m["port"], f'{m["name"]}{sz}{res}', est, None, vram_col=vram_col)

        # USB
        peripherals = si.get("peripherals", [])
        if peripherals:
            self._section_label(left, f"USB ({len(peripherals)})")
            for p in peripherals:
                self._comp_row(left, "", p, None, None, vram_col=vram_col)

        body.pack_start(left, True, True, 0)

        # RIGHT COLUMN — donut chart
        right = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=4)
        right.set_valign(Gtk.Align.CENTER)

        nvme_w = data.get("nvme_w", 0)
        periph_w = data.get("periph_w", 0)
        # The donut shows the last full minute's average, not the live sample,
        # so the slices stop twitching every 2s. Falls back to live values for
        # the first minute after launch, before an average exists.
        dn = data.get("donut")
        d_cpu = dn["cpu"] if dn else cpu_w
        d_gpu = dn["gpu"] if dn else gpu_w
        d_nvme = dn["nvme"] if dn else nvme_w
        d_periph = dn["periph"] if dn else periph_w
        d_total = dn["total"] if dn else total_w
        slices = [
            (d_cpu, SLICE_COLORS["CPU"], "CPU"),
            (d_gpu, SLICE_COLORS["GPU"], "GPU"),
            (CFG["overhead_watts"], SLICE_COLORS["Board"], "Board"),
            (d_nvme, SLICE_COLORS["NVMe"], "NVMe"),
            (d_periph, SLICE_COLORS["USB"], "USB"),
        ]
        total_cost = _cost_monthly(d_total)

        donut = Gtk.DrawingArea()
        donut.set_size_request(200, 200)
        _sl, _tc, _cur = [(w, c, n) for w, c, n in slices if w > 0], total_cost, cur

        def _draw_donut(widget, cr, sl=_sl, tc=_tc, cu=_cur):
            cx, cy, r, ir = 100, 100, 90, 58
            total = sum(s[0] for s in sl)
            if total <= 0: return False
            angle = -math.pi/2
            for watts, color, _ in sl:
                sweep = (watts/total) * 2*math.pi
                cr.set_source_rgb(*color)
                cr.move_to(cx+ir*math.cos(angle), cy+ir*math.sin(angle))
                cr.line_to(cx+r*math.cos(angle), cy+r*math.sin(angle))
                cr.arc(cx, cy, r, angle, angle+sweep)
                cr.line_to(cx+ir*math.cos(angle+sweep), cy+ir*math.sin(angle+sweep))
                cr.arc_negative(cx, cy, ir, angle+sweep, angle)
                cr.close_path(); cr.fill(); angle += sweep
            cr.set_source_rgb(0.96, 0.96, 0.94)
            cr.select_font_face("JetBrains Mono", 0, 1); cr.set_font_size(22)
            t1 = f"{tc:.0f}"; e1 = cr.text_extents(t1)
            cr.move_to(cx-e1.width/2, cy+4); cr.show_text(t1)
            cr.set_source_rgb(0.54, 0.54, 0.50); cr.set_font_size(11)
            t2 = f"{cu}/mo"; e2 = cr.text_extents(t2)
            cr.move_to(cx-e2.width/2, cy+20); cr.show_text(t2)
            return False

        donut.connect("draw", _draw_donut)

        # Donut + legend side by side
        donut_row = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=10)
        donut_row.pack_start(donut, False, False, 0)

        legend_box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=2)
        legend_box.set_valign(Gtk.Align.CENTER)

        # Legend
        for watts, color, name in slices:
            if watts <= 0: continue
            lr = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=5)
            dot = Gtk.DrawingArea(); dot.set_size_request(8, 8); dot.set_valign(Gtk.Align.CENTER)
            _c = color
            dot.connect("draw", lambda w, cr, c=_c: (
                cr.set_source_rgb(*c), cr.arc(4, 4, 3, 0, 2*math.pi), cr.fill()) and False)
            lr.pack_start(dot, False, False, 0)
            cost = _cost_monthly(watts)
            chx = "#{:02x}{:02x}{:02x}".format(int(color[0]*255), int(color[1]*255), int(color[2]*255))
            l = Gtk.Label()
            l.set_markup(f'<span font_family="JetBrains Mono" font_size="8500" '
                         f'foreground="{chx}">{name} {cost:.0f} {cur}</span>')
            l.set_halign(Gtk.Align.START)
            lr.pack_start(l, False, False, 0)
            legend_box.pack_start(lr, False, False, 0)

        kwh = total_w * 24 * 30 / 1000
        kl = Gtk.Label()
        kl.set_markup(f'<span font_family="JetBrains Mono" font_size="8000" '
                      f'foreground="#5a5a54">{kwh:.0f} kWh/mo</span>')
        kl.set_halign(Gtk.Align.START)
        legend_box.pack_start(kl, False, False, 2)

        # Today so far (hourly log + the hour in progress) and the last 24h shape.
        kwh_t, cost_t, spark = _today_from_logs(getattr(self.parent_dot, "logger", None))
        if spark or kwh_t:
            tl = Gtk.Label()
            tl.set_markup(f'<span font_family="JetBrains Mono" font_size="8500" foreground="#8A8A80">today  </span>'
                          f'<span font_family="JetBrains Mono" font_size="8500" foreground="#F5F5F0">'
                          f'{kwh_t:.1f} kWh · {cost_t:.2f} {cur}</span>')
            tl.set_halign(Gtk.Align.START)
            legend_box.pack_start(tl, False, False, 4)
            sp = Gtk.DrawingArea(); sp.set_size_request(130, 22)
            sp.connect("draw", lambda w, cr, sk=spark: self._draw_spark(w, cr, sk))
            legend_box.pack_start(sp, False, False, 0)

        donut_row.pack_start(legend_box, False, False, 0)
        right.pack_start(donut_row, False, False, 0)

        # Utilization bars under the donut
        self._section_label(right, "UTILIZATION")
        cpu_usage = data.get("cpu_usage")
        ram = data.get("ram")
        if cpu_usage is not None:
            self._bar_row(right, "CPU", cpu_usage)
        if ram:
            self._bar_row(right, "RAM", ram["pct"], f'{ram["used_gb"]:.0f}/{ram["total_gb"]:.0f}G')
        for g in gpus:
            vp = g["vram_used"]/g["vram_total"]*100 if g["vram_total"] else 0
            tag = "VRAM" if len(gpus) == 1 else f'VRAM{g["index"]}'
            self._bar_row(right, tag, vp, f'{g["vram_used"]/1024:.1f}/{g["vram_total"]/1024:.0f}G')

        body.pack_start(right, False, False, 0)
        self.inner.pack_start(body, True, True, 0)

        # Footer
        self._sep(6)
        ts = data["last_check"].strftime("%H:%M:%S") if data.get("last_check") else ""
        log_status = f"logging {CFG.get('log_interval', 'off')}" if CFG.get("log_interval", "off") != "off" else ""
        # Rate lives in the title bar now — not repeated here.
        ft = Gtk.Label(label=f"{ts}  ·  {log_status}  ·  Right-click: menu")
        ft.get_style_context().add_class("footer"); ft.set_halign(Gtk.Align.START)
        self.inner.pack_start(ft, False, False, 0)
        self.show_all()

    def _sep(self, margin=4):
        s = Gtk.Separator(); s.get_style_context().add_class("sep")
        self.inner.pack_start(s, False, False, margin)

    @staticmethod
    def _draw_spark(widget, cr, spark):
        """24 hourly bars, oldest left; the hour in progress carries the accent."""
        a = widget.get_allocation(); w, h = a.width, a.height
        vals = [v for _, v in spark][-24:]
        if not vals:
            return False
        mx = max(vals) or 1
        slot = w / 24
        for i, v in enumerate(vals):
            x = w - (len(vals) - i) * slot
            bh = max(2, (h - 2) * v / mx)
            last = i == len(vals) - 1
            cr.set_source_rgb(*((0.910, 0.529, 0.227) if last else (0.30, 0.28, 0.25)))
            cr.rectangle(x + 1, h - bh, slot - 2, bh); cr.fill()
        return False

    def _section_cols(self, parent, text, cols):
        """Section label with dim captions right-aligned over _comp_row's columns."""
        row = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=5)
        l = Gtk.Label(label=text); l.get_style_context().add_class("section")
        l.set_halign(Gtk.Align.START); l.set_xalign(0)
        row.pack_start(l, True, True, 0)
        for cap, width in cols:
            c = Gtk.Label()
            c.set_markup(f'<span font_family="JetBrains Mono" font_size="6500" '
                         f'foreground="#5a5a54">{cap}</span>')
            c.set_width_chars(width); c.set_xalign(1)
            row.pack_start(c, False, False, 0)
        row.set_margin_top(4); row.set_margin_end(10)   # .row pads 10px each side
        parent.pack_start(row, False, False, 2)

    def _section_label(self, parent, text):
        l = Gtk.Label(label=text); l.get_style_context().add_class("section")
        l.set_halign(Gtk.Align.START); l.set_margin_top(4)
        parent.pack_start(l, False, False, 2)

    def _comp_row(self, parent, name, model, watts, temp, vram_temp=None, vram_col=False, cap=None):
        row = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=5)
        row.get_style_context().add_class("row")
        if name:
            nl = Gtk.Label(label=name); nl.get_style_context().add_class("name")
            nl.set_width_chars(5); nl.set_xalign(0)
            row.pack_start(nl, False, False, 0)
        ms = (model[:26] + "…") if len(model) > 27 else model
        ml = Gtk.Label()
        ml.set_markup(f'<span font_family="JetBrains Mono" font_size="8000" '
                      f'foreground="#6b6b65">{GLib.markup_escape_text(ms)}</span>')
        ml.set_halign(Gtk.Align.START); ml.set_hexpand(True); ml.set_xalign(0); ml.set_ellipsize(3)
        row.pack_start(ml, True, True, 0)
        if watts is not None:
            wl = Gtk.Label()
            if cap:   # draw against its power cap: "8/280W", the cap quiet
                wl.set_markup(f'<span font_family="JetBrains Mono" font_size="8500" '
                              f'foreground="#F5F5F0">{watts:.0f}</span>'
                              f'<span font_family="JetBrains Mono" font_size="8000" '
                              f'foreground="#6b6b65">/{cap:.0f}W</span>')
            else:
                wl.set_markup(f'<span font_family="JetBrains Mono" font_size="8500" '
                              f'foreground="#F5F5F0">{_fmt_w(watts)}</span>')
            wl.set_width_chars(7); wl.set_xalign(1)
            row.pack_start(wl, False, False, 0)
        ts = _fmt_temp(temp)
        tc = "#8A8A80"
        if ts and temp is not None:
            tc = "#F04545" if temp > 85 else "#EBB307" if temp > 70 else "#8A8A80"
        tl = Gtk.Label()
        tl.set_markup(f'<span font_family="JetBrains Mono" font_size="8500" foreground="{tc}">{ts or ""}</span>')
        tl.set_width_chars(4); tl.set_xalign(1)
        row.pack_start(tl, False, False, 0)
        # VRAM (memory-junction) temp column — GDDR6X runs 20-30°C hotter than
        # core and throttles near 110, so its warn/alarm thresholds sit higher.
        if vram_col:
            vs = _fmt_temp(vram_temp)
            vc = "#8A8A80"
            if vram_temp is not None:
                vc = "#F04545" if vram_temp > 94 else "#EBB307" if vram_temp > 84 else "#8A8A80"
            vl = Gtk.Label()
            vl.set_markup(f'<span font_family="JetBrains Mono" font_size="8500" foreground="{vc}">{vs or ""}</span>')
            vl.set_width_chars(4); vl.set_xalign(1)
            row.pack_start(vl, False, False, 0)
        parent.pack_start(row, False, False, 0)

    def _bar_row(self, parent, name, pct, detail=None):
        pct = min(100, max(0, pct or 0))
        row = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=6)
        row.get_style_context().add_class("row")
        nl = Gtk.Label(label=name); nl.get_style_context().add_class("name")
        nl.set_width_chars(5); nl.set_xalign(0)
        row.pack_start(nl, False, False, 0)
        bar = Gtk.DrawingArea(); bar.set_size_request(80, 5)
        bar.set_valign(Gtk.Align.CENTER); bar.set_hexpand(True)
        _p = pct
        def _draw(w, cr, p=_p):
            a = w.get_allocation()
            cr.set_source_rgba(0.16, 0.16, 0.16, 0.8); cr.rectangle(0, 0, a.width, a.height); cr.fill()
            c = (0.94, 0.27, 0.27) if p > 80 else (0.92, 0.70, 0.03) if p > 50 else (0.13, 0.77, 0.37)
            cr.set_source_rgb(*c); cr.rectangle(0, 0, a.width*p/100, a.height); cr.fill()
            return False
        bar.connect("draw", _draw)
        row.pack_start(bar, True, True, 0)
        vl = Gtk.Label()
        text = detail or f"{pct:.0f}%"
        vl.set_markup(f'<span font_family="JetBrains Mono" font_size="8500" '
                      f'foreground="#8A8A80">{GLib.markup_escape_text(text)}</span>')
        vl.set_width_chars(9); vl.set_xalign(1)
        row.pack_end(vl, False, False, 0)
        parent.pack_start(row, False, False, 0)


# -- settings dialog ------------------------------------------------------

class SettingsDialog:
    def __init__(self, parent):
        self.parent = parent

    def run_dialog(self):
        d = Gtk.Dialog(title="Power Monitor Settings", transient_for=self.parent,
                       flags=Gtk.DialogFlags.MODAL | Gtk.DialogFlags.DESTROY_WITH_PARENT)
        d.set_default_size(380, -1)
        d.set_keep_above(True)
        d.add_button("Cancel", Gtk.ResponseType.CANCEL)
        d.add_button("Save", Gtk.ResponseType.OK)

        box = d.get_content_area()
        box.set_spacing(8)
        box.set_margin_start(16); box.set_margin_end(16)
        box.set_margin_top(12); box.set_margin_bottom(12)

        # Country preset dropdown
        preset_label = Gtk.Label(label="Country preset:")
        preset_label.set_halign(Gtk.Align.START)
        box.pack_start(preset_label, False, False, 0)

        preset_combo = Gtk.ComboBoxText()
        preset_combo.append_text("— Custom —")
        for name in COUNTRY_PRESETS:
            p = COUNTRY_PRESETS[name]
            preset_combo.append_text(f"{name}  ({p['price_per_kwh']} {p['currency']}/kWh)")
        preset_combo.set_active(0)
        box.pack_start(preset_combo, False, False, 0)

        sep1 = Gtk.Separator()
        box.pack_start(sep1, False, False, 4)

        # Price per kWh
        grid = Gtk.Grid()
        grid.set_row_spacing(6); grid.set_column_spacing(12)

        grid.attach(Gtk.Label(label="Price per kWh:", halign=Gtk.Align.START), 0, 0, 1, 1)
        price_spin = Gtk.SpinButton()
        price_spin.set_adjustment(Gtk.Adjustment(value=CFG["price_per_kwh"],
                                                  lower=0.001, upper=999, step_increment=0.01, page_increment=0.1))
        price_spin.set_digits(3)
        grid.attach(price_spin, 1, 0, 1, 1)

        # Currency
        grid.attach(Gtk.Label(label="Currency:", halign=Gtk.Align.START), 0, 1, 1, 1)
        currency_entry = Gtk.Entry()
        currency_entry.set_text(CFG["currency"])
        currency_entry.set_max_length(5)
        grid.attach(currency_entry, 1, 1, 1, 1)

        # Overhead
        grid.attach(Gtk.Label(label="Board overhead (W):", halign=Gtk.Align.START), 0, 2, 1, 1)
        overhead_spin = Gtk.SpinButton()
        overhead_spin.set_adjustment(Gtk.Adjustment(value=CFG["overhead_watts"],
                                                     lower=0, upper=200, step_increment=5, page_increment=10))
        overhead_spin.set_digits(0)
        grid.attach(overhead_spin, 1, 2, 1, 1)

        # Peripheral watts
        grid.attach(Gtk.Label(label="USB peripheral (W):", halign=Gtk.Align.START), 0, 3, 1, 1)
        periph_spin = Gtk.SpinButton()
        periph_spin.set_adjustment(Gtk.Adjustment(value=CFG["peripheral_watts"],
                                                    lower=0, upper=50, step_increment=1, page_increment=5))
        periph_spin.set_digits(0)
        grid.attach(periph_spin, 1, 3, 1, 1)

        # Log interval
        grid.attach(Gtk.Label(label="Log interval:", halign=Gtk.Align.START), 0, 4, 1, 1)
        log_combo = Gtk.ComboBoxText()
        log_options = ["off", "hourly", "daily", "weekly", "monthly"]
        for opt in log_options:
            log_combo.append_text(opt)
        current_log = CFG.get("log_interval", "off")
        log_combo.set_active(log_options.index(current_log) if current_log in log_options else 0)
        grid.attach(log_combo, 1, 4, 1, 1)

        box.pack_start(grid, False, False, 0)

        # When preset changes, update fields
        def on_preset_changed(combo):
            idx = combo.get_active()
            if idx <= 0:
                return
            name = list(COUNTRY_PRESETS.keys())[idx - 1]
            p = COUNTRY_PRESETS[name]
            price_spin.set_value(p["price_per_kwh"])
            currency_entry.set_text(p["currency"])

        preset_combo.connect("changed", on_preset_changed)

        # Note about presets
        note = Gtk.Label()
        note.set_markup('<span font_size="8000" foreground="#8A8A80">'
                        'Presets are approximate — adjust price to your actual rate.\n'
                        'All-in residential rates (energy + distribution + taxes),\n'
                        'Eurostat 2025-S2 and national regulators. Your bill will differ\n'
                        'by supplier, region and contract — check it and edit above.</span>')
        note.set_halign(Gtk.Align.START)
        note.set_line_wrap(True)
        box.pack_start(note, False, False, 4)

        d.show_all()
        response = d.run()

        if response == Gtk.ResponseType.OK:
            CFG["price_per_kwh"] = price_spin.get_value()
            CFG["currency"] = currency_entry.get_text().strip() or "EUR"
            CFG["overhead_watts"] = int(overhead_spin.get_value())
            CFG["peripheral_watts"] = int(periph_spin.get_value())
            CFG["log_interval"] = log_combo.get_active_text() or "off"
            _save_config()

        d.destroy()


# -- autostart ------------------------------------------------------------

def _install_autostart():
    script = os.path.abspath(__file__)
    desktop = f"""[Desktop Entry]
Name=Power Monitor
Comment=System power consumption indicator
Exec=/usr/bin/python3 {script}
Icon=battery-full-charged
Type=Application
Categories=Utility;
StartupNotify=false
X-GNOME-Autostart-enabled=true
"""
    autostart_dir = os.path.join(os.path.expanduser("~"), ".config", "autostart")
    os.makedirs(autostart_dir, exist_ok=True)
    path = os.path.join(autostart_dir, "power-monitor.desktop")
    with open(path, "w") as f:
        f.write(desktop)
    print(f"Autostart installed: {path}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Power Monitor widget")
    parser.add_argument("--price", type=float, help="Electricity price per kWh")
    parser.add_argument("--currency", type=str, help="Currency code (EUR, USD, PLN...)")
    parser.add_argument("--install-autostart", action="store_true", help="Install autostart entry")
    args = parser.parse_args()
    if args.install_autostart:
        _install_autostart(); sys.exit(0)
    if args.price is not None: CFG["price_per_kwh"] = args.price
    if args.currency: CFG["currency"] = args.currency
    dot = DotWindow(); dot.show_all(); Gtk.main()
