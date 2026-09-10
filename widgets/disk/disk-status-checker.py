#!/usr/bin/python3
"""
ShinyWidgets — Disk Status Checker

Always-on-top indicator for filesystem pressure, with the reclaimable
space Docker is sitting on. A full root filesystem breaks builds, package
installs and image pulls in confusing ways; this is the warning before that.

Monitors every real mounted filesystem plus `docker system df`.
"""

__version__ = "1.1.0"

import os
import sys
import subprocess
import re
import shutil

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "core"))
from shinywidget import (
    WidgetApp, Section, Row, Meter, Note, Grid,
    OK, WARN, CRIT, IDLE, AMBER, FG_DIM,
    fmt_bytes, ratio_color, run_cmd, run, IS_WINDOWS,
)

# Thresholds are on *used* fraction. Disks misbehave well before 100%:
# filesystems fragment and some reserve blocks for root only.
WARN_FRAC = 0.85
CRIT_FRAC = 0.92

# Pseudo-filesystems carry no meaning for capacity planning.
SKIP_FSTYPES = {
    "tmpfs", "devtmpfs", "squashfs", "overlay", "proc", "sysfs", "cgroup",
    "cgroup2", "devpts", "efivarfs", "autofs", "fuse.gvfsd-fuse", "fuse.portal",
    "ramfs", "mqueue", "hugetlbfs", "debugfs", "tracefs", "securityfs",
    "pstore", "bpf", "configfs", "fusectl", "binfmt_misc", "nsfs",
}
SKIP_PREFIXES = ("/snap", "/var/snap", "/run", "/sys", "/proc", "/dev")

DOCKER_REFRESH_SECS = 300   # `docker system df` walks the graph; keep it rare


def _mounts_windows():
    """Fixed and network drives, as (root, fstype). Windows has no
    /proc/mounts; the drive letters come from the volume API."""
    import ctypes
    import string
    out = []
    try:
        mask = ctypes.windll.kernel32.GetLogicalDrives()
    except Exception:
        return []
    for i, letter in enumerate(string.ascii_uppercase):
        if not (mask >> i) & 1:
            continue
        root = f"{letter}:\\"
        try:
            kind = ctypes.windll.kernel32.GetDriveTypeW(ctypes.c_wchar_p(root))
        except Exception:
            continue
        if kind not in (3, 4):          # DRIVE_FIXED, DRIVE_REMOTE
            continue
        name = ctypes.create_unicode_buffer(256)
        fsname = ctypes.create_unicode_buffer(256)
        try:
            ctypes.windll.kernel32.GetVolumeInformationW(
                ctypes.c_wchar_p(root), name, 256, None, None, None, fsname, 256)
        except Exception:
            pass
        out.append((root, (fsname.value or "").lower() or
                    ("network" if kind == 4 else "disk")))
    return out


def _mounts():
    """Real mounted filesystems, as (mountpoint, fstype)."""
    if IS_WINDOWS:
        return _mounts_windows()
    out = []
    try:
        with open("/proc/mounts") as f:
            for line in f:
                parts = line.split()
                if len(parts) < 3:
                    continue
                dev, mnt, fstype = parts[0], parts[1], parts[2]
                if fstype in SKIP_FSTYPES:
                    continue
                if mnt.startswith(SKIP_PREFIXES):
                    continue
                if not dev.startswith("/"):
                    continue
                out.append((mnt, fstype))
    except OSError:
        return []
    # de-duplicate bind mounts pointing at the same place
    seen, uniq = set(), []
    for mnt, fstype in out:
        try:
            key = os.stat(mnt).st_dev
        except OSError:
            continue
        if key in seen:
            continue
        seen.add(key)
        uniq.append((mnt, fstype))
    return sorted(uniq, key=lambda m: (m[0] != "/", m[0]))


_SIZE_RE = re.compile(r"^([\d.]+)\s*([KMGTP]?)B?$", re.IGNORECASE)
_MULT = {"": 1, "K": 1 << 10, "M": 1 << 20, "G": 1 << 30, "T": 1 << 40, "P": 1 << 50}


def _parse_size(text):
    """'83.54GB' -> bytes. Returns 0 on anything unparseable."""
    m = _SIZE_RE.match((text or "").strip())
    if not m:
        return 0
    try:
        return int(float(m.group(1)) * _MULT[m.group(2).upper()])
    except (ValueError, KeyError):
        return 0


def docker_reclaimable():
    """Total reclaimable bytes per `docker system df`, or None if unavailable."""
    out = run_cmd(["docker", "system", "df", "--format",
                   "{{.Type}}\t{{.Reclaimable}}"], timeout=25)
    if not out.strip():
        return None
    total, rows = 0, []
    for line in out.strip().splitlines():
        parts = line.split("\t")
        if len(parts) < 2:
            continue
        kind, recl = parts[0], parts[1]
        # Reclaimable reads like "83.54GB (18%)" - take the size
        size = _parse_size(recl.split("(")[0].strip())
        total += size
        rows.append((kind, size))
    root = "/"
    try:
        out2 = run_cmd(["docker", "info", "--format", "{{.DockerRootDir}}"], timeout=10)
        if out2.strip():
            root = out2.strip()
    except Exception:
        pass
    return {"total": total, "rows": rows, "root": root}


class DiskWidget(WidgetApp):
    name = "disk"
    title = "DISK"
    poll_secs = 30

    # Platter with a spindle hole - reads as a disk at 28px.
    brand = (0.0, 0.169, 0.055)       # deep green #002B0E
    logo_fg = (0.937, 0.925, 0.902)
    logo_scale = 0.60
    glyph = (
    "M2 20h20v-4H2v4zm2-3h2v2H4v-2zM2 4v4h20V4H2zm4 3H4V5h2v2zm-4 7h20v-4H2v4zm2-3h2v2H4v-2z"
)   # stacked drives (Material 'storage')

    def __init__(self):
        self._docker = None
        self._docker_at = 0.0

    def fetch(self):
        import time
        from datetime import datetime, timezone

        disks = []
        for mnt, fstype in _mounts():
            try:
                u = shutil.disk_usage(mnt)
            except OSError:
                continue
            if u.total <= 0:
                continue
            disks.append({
                "mount": mnt,
                "fstype": fstype,
                "total": u.total,
                "used": u.used,
                "free": u.free,
                "frac": u.used / u.total,
            })

        now = time.monotonic()
        if self._docker is None or now - self._docker_at >= DOCKER_REFRESH_SECS:
            self._docker = docker_reclaimable()
            self._docker_at = now

        return {
            "disks": disks,
            "docker": self._docker,
            "last_check": datetime.now(timezone.utc),
        }

    # -- state ------------------------------------------------------------

    def state(self, data):
        disks = (data or {}).get("disks") or []
        if not disks:
            return "idle"
        worst = max(d["frac"] for d in disks)
        if worst >= CRIT_FRAC:
            return "crit"
        if worst >= WARN_FRAC:
            return "warn"
        return "ok"

    def state_label(self, state, data):
        disks = (data or {}).get("disks") or []
        if not disks:
            return "NO FILESYSTEMS"
        worst = max(disks, key=lambda d: d["frac"])
        pct = worst["frac"] * 100
        if state == "ok":
            return f"{fmt_bytes(min(d['free'] for d in disks))} free"
        return f"{worst['mount']} {pct:.0f}% full"

    def alert_ids(self, data):
        return [d["mount"] for d in (data or {}).get("disks") or []
                if d["frac"] >= WARN_FRAC]

    def alert_message(self, alert_id, data):
        for d in (data or {}).get("disks") or []:
            if d["mount"] == alert_id:
                return f"{alert_id}: {d['frac'] * 100:.0f}% full · {fmt_bytes(d['free'])} left"
        return f"{alert_id}: low space"

    # -- panel ------------------------------------------------------------

    def menu_items(self, dot):
        items = []
        if shutil.which("baobab"):
            items.append(("Open Disk Usage Analyzer", lambda: subprocess.Popen(["baobab"])))
        return items

    def rows(self, data):
        disks = (data or {}).get("disks") or []
        items = [Section("FILESYSTEMS")]
        if not disks:
            items.append(Note("No filesystems found", IDLE))

        # Used / total, spelled out. A bare "85%" leaves you asking 85% of
        # what, and how much room that actually leaves.
        for d in disks:
            items.append(Meter(
                d["mount"],
                d["frac"],
                f"{fmt_bytes(d['free'])} free",
                color=ratio_color(d["frac"], WARN_FRAC, CRIT_FRAC),
                note=f"{fmt_bytes(d['used'])} used of {fmt_bytes(d['total'])}"
                     f"   ·   {d['frac'] * 100:.0f}%   ·   {d['fstype']}",
            ))

        docker = (data or {}).get("docker")
        if docker:
            total = docker["total"]
            items.append(Section("DOCKER · RECLAIMABLE"))
            # Say plainly what this is: space you could get back, and where.
            items.append(Note(
                f"{fmt_bytes(total)} could be freed on {docker.get('root', '/')}",
                AMBER if total > (20 << 30) else FG_DIM))
            cells = []
            for kind, size in docker["rows"]:
                cells.append((kind.lower(), AMBER if size > (10 << 30) else IDLE,
                              fmt_bytes(size)))
            if cells:
                items.append(Grid(cells, columns=2))
            if total > (20 << 30):
                items.append(Note("docker system prune -a   ·   "
                                  "docker builder prune", AMBER))
        return items

if __name__ == "__main__":
    run(DiskWidget)
