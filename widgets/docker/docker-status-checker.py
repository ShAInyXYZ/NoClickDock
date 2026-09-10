#!/usr/bin/python3
"""
NoClickDock — Docker Status Checker

Always-on-top indicator for container health. The value here is the
exception, not the count: the dot stays quiet while everything is up and
healthy, and goes red the moment a container is unhealthy, restart-looping,
or has exited non-zero.

Reading the panel never needs a click. Changing things does: every
container row carries small start / stop / restart / remove buttons, and
each one has to be clicked twice - the first click only arms it.

Reads the local Docker daemon via the `docker` CLI.
"""

__version__ = "1.1.1"

import re
import sys
import os
import subprocess
import time
from collections import OrderedDict
import webbrowser
from datetime import datetime, timezone

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "core"))
from shinywidget import (
    WidgetApp, Section, Row, Note, Alert, Action,
    OK, WARN, CRIT, IDLE, AMBER, FG_DIM,
    run_cmd, run, parse_iso,
)

# Exit codes that mean "was told to stop", not "crashed": 0, SIGTERM (143) and
# SIGKILL (137) - the latter only when the kernel's OOM killer was not the one
# sending it, which docker inspect tells us.
STOP_CODES = {0, 143, 137}
DORMANT_DAYS = 7
RECENT_HOURS = 24          # a crash newer than this is still "news"
SYSTEM_EVERY = 300         # docker system df is slow; refresh it this often
LOG_FETCH_CAP = 8          # last-log-line lookups per poll

EXIT_MEANING = {
    0: "exited cleanly", 1: "error", 2: "shell misuse", 125: "daemon error",
    126: "command not executable", 127: "command not found", 128: "invalid exit",
    130: "SIGINT", 134: "abort (SIGABRT)", 137: "killed (SIGKILL)",
    139: "segfault (SIGSEGV)", 143: "stopped (SIGTERM)", 255: "exit 255",
}


def _run(args, timeout=10):
    """(rc, stdout, stderr); rc = -1 when the binary is missing or hangs."""
    try:
        p = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
        return p.returncode, p.stdout or "", p.stderr or ""
    except (OSError, subprocess.SubprocessError):
        return -1, "", ""


def _fmt_mem(text):
    """docker stats '492.1MiB / 125.1GiB' -> '492M'; '22.21GiB' -> '22.2G'."""
    m = re.match(r"\s*([\d.]+)\s*([KMGT])i?B", text or "")
    if not m:
        return ""
    val, unit = float(m.group(1)), m.group(2)
    return f"{val:.1f}{unit}" if unit in "GT" and val < 10 else f"{val:.0f}{unit}"


def _fmt_bytes_short(n):
    if n >= 1 << 30:
        return f"{n / (1 << 30):.1f}G"
    if n >= 1 << 20:
        return f"{n / (1 << 20):.0f}M"
    return ""


def _mem_exact(text):
    """docker stats '492.1MiB' -> bytes. Keeps the precision _fmt_mem drops."""
    m = re.match(r"\s*([\d.]+)\s*([KMGT]?)i?B", text or "")
    if not m:
        return 0
    unit = m.group(2)
    return float(m.group(1)) * {"": 1, "K": 1 << 10, "M": 1 << 20,
                                "G": 1 << 30, "T": 1 << 40}[unit]


def _stats():
    """name -> (cpu%, mem_text, mem_bytes) for running containers.

    The byte count is kept alongside the display string: summing a stack from
    the rounded text loses up to a gigabyte across a handful of containers,
    and anything under a megabyte disappears entirely.
    """
    out = run_cmd(["docker", "stats", "--no-stream", "--format",
                   "{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}"], timeout=8)
    res = {}
    for line in (out or "").splitlines():
        parts = line.split("\t")
        if len(parts) >= 3:
            try:
                cpu = float(parts[1].strip().rstrip("%"))
            except ValueError:
                cpu = 0.0
            used = parts[2].split("/")[0]
            res[parts[0]] = (cpu, _fmt_mem(used), _mem_exact(used))
    return res


def _hours_since(iso):
    """Hours since a docker timestamp; None for the zero time / unparsable."""
    dt = parse_iso(iso)
    if dt is None or dt.year < 2000:
        return None
    return max(0.0, (datetime.now(timezone.utc) - dt).total_seconds() / 3600)


def _dur(hours):
    """46.2 -> '46h'; 0.4 -> '24m'; 90 -> '3d 18h'."""
    if hours is None:
        return ""
    if hours < 1:
        return f"{hours * 60:.0f}m"
    if hours < 48:
        return f"{hours:.0f}h"
    d, h = divmod(int(hours), 24)
    return f"{d}d {h}h" if d < 7 else f"{d}d"


def time_ago_h(hours):
    """72.0 -> '3d ago'; None -> ''. Short: it shares a row with buttons."""
    if hours is None:
        return ""
    if hours < 1:
        return f"{hours * 60:.0f}m ago"
    if hours < 48:
        return f"{hours:.0f}h ago"
    if hours < 24 * 14:
        return f"{hours / 24:.0f}d ago"
    if hours < 24 * 60:
        return f"{hours / 168:.0f}w ago"
    return f"{hours / 720:.0f}mo ago"


_ANSI = re.compile(r"\x1b\[[0-9;?]*[A-Za-z]")


def _last_log_line(name):
    """The last non-empty line a container wrote, cleaned for one row."""
    rc, out, err = _run(["docker", "logs", "--tail", "5", name], timeout=4)
    lines = [ln for ln in _ANSI.sub("", out + err).splitlines() if ln.strip()]
    if not lines:
        return ""
    line = re.sub(r"\s+", " ", lines[-1]).strip()
    # drop a leading ISO timestamp or bracketed level: the row has no room
    line = re.sub(r"^\d{4}-\d\d-\d\dT[\d:.]+Z?\s*", "", line)
    return line[:90]


def _ports(js):
    """'{"8108/tcp":[{"HostIp":"127.0.0.1","HostPort":"8108"}]}' -> ['127.0.0.1:8108->8108']."""
    import json
    try:
        d = json.loads(js or "{}") or {}
    except ValueError:
        return []
    out = []
    for cport, binds in d.items():
        for b in binds or []:
            host = b.get("HostIp") or ""
            host = "" if host in ("0.0.0.0", "::") else host
            hp = b.get("HostPort") or ""
            cp = cport.split("/")[0]
            out.append(f"{host}:{hp}" if host else f":{hp}" if hp == cp else f":{hp}->{cp}")
            break
    return out


def _sizes(text):
    """docker system df 'Reclaimable' cell: '83.54GB (17%)' -> (83.54e9 bytes, '17%')."""
    m = re.match(r"\s*([\d.]+)\s*([kKMGT]?B)\s*(?:\((\d+%)\))?", text or "")
    if not m:
        return 0, ""
    mult = {"B": 1, "kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12}[m.group(2)]
    return float(m.group(1)) * mult, m.group(3) or ""


def _fmt_dec(n):
    for unit, size in (("T", 1e12), ("G", 1e9), ("M", 1e6), ("K", 1e3)):
        if n >= size:
            v = n / size
            return f"{v:.1f}{unit}" if v < 10 else f"{v:.0f}{unit}"
    return f"{n:.0f}B" if n else "0"


class DockerWidget(WidgetApp):
    name = "docker"
    title = "DOCKER"
    poll_secs = 20
    panel_width = 560
    panel_max_height = 640

    # A shipping crate, simplified to survive the knockout at 28px.
    brand = (0.141, 0.588, 0.929)     # Docker blue #2496ED
    logo_fg = (1.0, 1.0, 1.0)
    logo_scale = 0.70
    glyph = (
    "M13.983 11.078h2.119a.186.186 0 00.186-.185V9.006a.186.186 0 00-.186-.186h-2.119a.185.185 "
    "0 00-.185.185v1.888c0 .102.083.185.185.185m-2.954-5.43h2.118a.186.186 0 00.186-.186V3.574a"
    ".186.186 0 00-.186-.185h-2.118a.185.185 0 00-.185.185v1.888c0 .102.082.185.185.185m0 2.716"
    "h2.118a.187.187 0 00.186-.186V6.29a.186.186 0 00-.186-.185h-2.118a.185.185 0 00-.185.185v1"
    ".887c0 .102.082.185.185.186m-2.93 0h2.12a.186.186 0 00.184-.186V6.29a.185.185 0 00-.185-.1"
    "85H8.1a.185.185 0 00-.185.185v1.887c0 .102.083.185.185.186m-2.964 0h2.119a.186.186 0 00.18"
    "5-.186V6.29a.185.185 0 00-.185-.185H5.136a.186.186 0 00-.186.185v1.887c0 .102.084.185.186."
    "186m5.893 2.715h2.118a.186.186 0 00.186-.185V9.006a.186.186 0 00-.186-.186h-2.118a.185.185"
    " 0 00-.185.185v1.888c0 .102.082.185.185.185m-2.93 0h2.12a.185.185 0 00.184-.185V9.006a.185"
    ".185 0 00-.184-.186h-2.12a.185.185 0 00-.184.185v1.888c0 .102.083.185.185.185m-2.964 0h2.1"
    "19a.185.185 0 00.185-.185V9.006a.185.185 0 00-.184-.186h-2.12a.186.186 0 00-.186.186v1.887"
    "c0 .102.084.185.186.185m-2.92 0h2.12a.185.185 0 00.184-.185V9.006a.185.185 0 00-.184-.186h"
    "-2.12a.185.185 0 00-.184.185v1.888c0 .102.082.185.185.185M23.763 9.89c-.065-.051-.672-.51-"
    "1.954-.51-.338.001-.676.03-1.01.087-.248-1.7-1.653-2.53-1.716-2.566l-.344-.199-.226.327c-."
    "284.438-.49.922-.612 1.43-.23.97-.09 1.882.403 2.661-.595.332-1.55.413-1.744.42H.751a.751."
    "751 0 00-.75.748 11.376 11.376 0 00.692 4.062c.545 1.428 1.355 2.48 2.41 3.124 1.18.723 3."
    "1 1.137 5.275 1.137.983.003 1.963-.086 2.93-.266a12.248 12.248 0 003.823-1.389c.98-.567 1."
    "86-1.288 2.61-2.136 1.252-1.418 1.998-2.997 2.553-4.4h.221c1.372 0 2.215-.549 2.68-1.009.3"
    "09-.293.55-.65.707-1.046l.098-.288Z"
)

    PAGES = ("NOW", "STOPPED", "SYSTEM")

    def __init__(self):
        self._pending = {}        # name -> "stopping" while an action runs
        self._log_cache = OrderedDict()   # (name, finished_at) -> last log line
        self._sys = None          # cached SYSTEM page data
        self._sys_ts = 0
        self._compose = None      # docker compose v2 present?

    # -- data -------------------------------------------------------------

    def fetch(self):
        rc, out, _ = _run(["docker", "ps", "-aq"], timeout=20)
        if rc != 0:
            return {"available": False, "last_check": datetime.now(timezone.utc)}
        ids = out.split()
        rows = self._inspect(ids) if ids else []

        stats = _stats() if any(c["state"] == "running" for c in rows) else {}
        for c in rows:
            c["cpu"], c["mem"], c["mem_b"] = stats.get(c["name"], (None, "", 0))

        # Units: a compose project is one unit, a standalone container is its own.
        units = {}
        for c in rows:
            key = c["project"] or c["name"]
            u = units.setdefault(key, {"name": key, "stack": bool(c["project"]), "members": []})
            u["members"].append(c)
        for u in units.values():
            m = u["members"]
            u["running"] = [c for c in m if c["state"] in ("running", "paused")]
            u["bad"] = [c for c in m if c["state"] == "restarting"
                        or (c["state"] == "running" and c["health"] == "unhealthy")]
            u["crashed"] = [c for c in m if c["crashed"]]
            ages = [c["age_h"] for c in m if c["age_h"] is not None and c["state"] != "running"]
            u["age_h"] = min(ages) if ages else None      # most recent activity
            u["mem"] = sum(c["mem_b"] for c in u["running"])
            u["cpu"] = sum(c["cpu"] or 0 for c in u["running"])

        self._fetch_logs(rows)

        portainer = None
        for c in rows:
            if c["name"] == "portainer" and c["state"] == "running":
                # 9443 is the HTTPS UI, 9000 the HTTP one; 8000 is the edge
                # agent tunnel and serves no page, so it is never the first pick.
                published = [p.split("->")[0].rsplit(":", 1)[-1] for p in c["ports"]]
                hp = next((x for x in ("9443", "9000") if x in published),
                          next((x for x in published if x != "8000"), ""))
                if hp:
                    portainer = f'{"https" if hp == "9443" else "http"}://localhost:{hp}'

        if self._compose is None:
            self._compose = _run(["docker", "compose", "version"], timeout=5)[0] == 0
        if time.time() - self._sys_ts > SYSTEM_EVERY:
            self._sys = self._system(rows)
            self._sys_ts = time.time()

        return {
            "available": True,
            "portainer_url": portainer,
            "units": sorted(units.values(), key=lambda u: u["name"].lower()),
            "containers": rows,
            "system": self._sys,
            "last_check": datetime.now(timezone.utc),
        }

    _INSPECT = (
        "{{.Name}}\t{{.State.Status}}\t{{.State.ExitCode}}\t{{.State.OOMKilled}}\t"
        "{{.State.StartedAt}}\t{{.State.FinishedAt}}\t{{.RestartCount}}\t"
        "{{if .State.Health}}{{.State.Health.Status}}{{end}}\t{{.Config.Image}}\t"
        '{{index .Config.Labels "com.docker.compose.project"}}\t'
        '{{index .Config.Labels "com.docker.compose.service"}}\t'
        "{{.HostConfig.RestartPolicy.Name}}\t{{json .NetworkSettings.Ports}}"
    )

    def _inspect(self, ids):
        """One docker inspect for everything: exact codes, times, health."""
        rc, out, _ = _run(["docker", "inspect", "--format", self._INSPECT] + ids, timeout=20)
        rows = []
        for line in out.splitlines():
            p = (line.split("\t") + [""] * 13)[:13]
            state = p[1].lower()
            try:
                code = int(p[2])
            except ValueError:
                code = None
            running = state in ("running", "paused", "restarting")
            c = {
                "name": p[0].lstrip("/"), "state": state,
                "exit_code": None if running else code,
                "oom": p[3] == "true",
                "started": p[4], "finished": p[5],
                "restarts": int(p[6]) if p[6].isdigit() else 0,
                "health": p[7] if state == "running" and p[7] else None,
                "image": p[8], "project": p[9], "service": p[10],
                "policy": p[11] if p[11] not in ("", "no") else "",
                "ports": _ports(p[12]),
            }
            c["up_h"] = _hours_since(c["started"]) if running else None
            c["age_h"] = None if running else _hours_since(c["finished"])
            c["crashed"] = (state in ("exited", "dead")
                            and (c["oom"] or (code is not None and code not in STOP_CODES)))
            c["log"] = ""
            rows.append(c)
        return rows

    def _fetch_logs(self, rows):
        """Last log line for anything broken or unhealthy - the quickest
        diagnostic there is. Cached per (name, stop time): a stopped
        container's last line never changes."""
        want = [c for c in rows if c["crashed"] or c["health"] == "unhealthy"
                or c["state"] == "restarting"]
        want.sort(key=lambda c: c["age_h"] if c["age_h"] is not None else -1)
        fetched = 0
        for c in want:
            # A restart-looping container's finish time advances every poll, so
            # keying on it would never hit and would fill the cache with misses.
            live = c["state"] in ("running", "restarting")
            key = (c["name"], "live" if live else c["finished"])
            if key in self._log_cache and not live:
                self._log_cache.move_to_end(key)
                c["log"] = self._log_cache[key]
                continue
            if fetched >= LOG_FETCH_CAP:
                continue
            c["log"] = self._log_cache[key] = _last_log_line(c["name"])
            self._log_cache.move_to_end(key)
            fetched += 1
        while len(self._log_cache) > 200:     # oldest out, rather than all out
            self._log_cache.popitem(last=False)

    def _system(self, rows):
        """Engine, compose, counts and what `docker system df` says is reclaimable."""
        sysd = {"engine": run_cmd(["docker", "version", "--format", "{{.Server.Version}}"],
                                  timeout=5).strip()}
        cv = run_cmd(["docker", "compose", "version", "--short"], timeout=5).strip()
        sysd["compose"] = cv
        sysd["dangling"] = len(run_cmd(["docker", "images", "-f", "dangling=true", "-q"],
                                       timeout=10).split())
        df = {}
        out = run_cmd(["docker", "system", "df", "--format",
                       "{{.Type}}\t{{.TotalCount}}\t{{.Active}}\t{{.Size}}\t{{.Reclaimable}}"],
                      timeout=30)
        for line in out.splitlines():
            p = (line.split("\t") + [""] * 5)[:5]
            size, _ = _sizes(p[3])
            recl, pct = _sizes(p[4])
            df[p[0].lower()] = {"total": p[1], "active": p[2], "size": size,
                                "reclaimable": recl, "pct": pct}
        sysd["df"] = df
        return sysd

    # -- state ------------------------------------------------------------

    def _attention(self, data):
        """(unit, container, reason) for everything that needs a human today."""
        out = []
        for u in (data or {}).get("units") or []:
            for c in u["bad"]:
                out.append((u, c, "restarting" if c["state"] == "restarting" else "unhealthy"))
            for c in u["crashed"]:
                if c["age_h"] is not None and c["age_h"] < RECENT_HOURS:
                    out.append((u, c, "OOM-killed" if c["oom"] else f"exit {c['exit_code']}"))
        return out

    def state(self, data):
        if not (data or {}).get("available"):
            return "idle"
        att = self._attention(data)
        if any(r in ("restarting", "unhealthy") for _, _, r in att):
            return "crit"
        if att:
            return "warn"
        return "ok"

    def state_label(self, state, data):
        if not (data or {}).get("available"):
            return "DAEMON UNREACHABLE"
        att = self._attention(data)
        bad = sum(1 for _, _, r in att if r in ("restarting", "unhealthy"))
        if bad:
            return f"{bad} container{'' if bad == 1 else 's'} unhealthy"
        if att:
            return f"{len(att)} crashed today"
        n = sum(len(u["running"]) for u in data.get("units") or [])
        return f"{n} running"

    def alert_ids(self, data):
        return [f"{r}:{c['name']}" for _, c, r in self._attention(data)
                if r in ("restarting", "unhealthy")]

    def alert_message(self, alert_id, data):
        kind, _, name = alert_id.partition(":")
        return f"{name}: {kind.upper()}"

    def menu_items(self, dot):
        url = (dot.data or {}).get("portainer_url")
        return [("Open Portainer", lambda: webbrowser.open(url))] if url else []

    def pages(self, data):
        return self.PAGES if (data or {}).get("available") else []

    # -- actions ----------------------------------------------------------

    _PAST = {"stopping": "Stopped", "starting": "Started", "restarting": "Restarted",
             "removing": "Removed", "resuming": "Resumed", "pruning": "Pruned"}

    def _act(self, verb, label, names, cmd, fallback=None, scope="unit"):
        """Mark `names` pending, run cmd off-thread, toast the outcome, refresh.
        `fallback` runs instead when cmd fails (compose absent or confused).

        Keys are namespaced: a container really can be named `images`, and it
        must not read as "pruning" because a SYSTEM-page prune is running.
        """
        for n in names:
            self._pending[("unit", n)] = verb
        self._pending[(scope, label)] = verb
        self.dot.repaint_panel()

        def work():
            rc, out, err = _run(cmd, timeout=120)
            if rc != 0 and fallback:
                rc, out, err = _run(fallback, timeout=120)
            if rc == 0:
                return True, f"{self._PAST[verb]} {label}"
            tail = (err or out).strip().splitlines()
            return False, f"{label}: {tail[-1][:70] if tail else 'failed'}"

        def done(ok, msg):
            for n in names:
                self._pending.pop(("unit", n), None)
            self._pending.pop((scope, label), None)
            if verb == "pruning":
                self._sys_ts = 0            # df is stale now

        self.dot.run_action(work, done)

    def _unit_cmd(self, u, verb):
        """A compose stack is driven by project name (order, networks); a
        standalone container by its own name. Both fall back to plain docker."""
        names = [c["name"] for c in u["members"]]
        plain = {"stop": ["docker", "stop"], "start": ["docker", "start"],
                 "restart": ["docker", "restart"], "rm": ["docker", "rm"]}[verb] + names
        if u["stack"] and self._compose:
            cverb = "down" if verb == "rm" else verb
            return ["docker", "compose", "-p", u["name"], cverb], plain
        return plain, None

    def _unit_actions(self, u):
        names = [c["name"] for c in u["members"]]
        if ("unit", u["name"]) in self._pending:
            return []
        acts = []
        if u["running"]:
            if any(c["state"] == "paused" for c in u["running"]):
                acts.append(Action("resume", lambda u=u: self._act(
                    "resuming", u["name"], names,
                    ["docker", "unpause"] + [c["name"] for c in u["running"]])))
            acts.append(Action("restart", lambda u=u: self._act(
                "restarting", u["name"], names, *self._unit_cmd(u, "restart")),
                confirm="restart?"))
            acts.append(Action("stop", lambda u=u: self._act(
                "stopping", u["name"], names, *self._unit_cmd(u, "stop")),
                confirm="stop?"))
        else:
            acts.append(Action("start", lambda u=u: self._act(
                "starting", u["name"], names, *self._unit_cmd(u, "start"))))
            what = "remove stack?" if u["stack"] and len(names) > 1 else "remove?"
            acts.append(Action("remove", lambda u=u: self._act(
                "removing", u["name"], names, *self._unit_cmd(u, "rm")),
                confirm=what, danger=True))
        return acts

    def _container_actions(self, c):
        """Row buttons for one container inside a stack."""
        if (("unit", c["name"]) in self._pending
                or (c["project"] and ("unit", c["project"]) in self._pending)):
            return []
        n = c["name"]
        if c["state"] in ("running", "restarting"):
            return [Action("restart", lambda: self._act("restarting", n, [n],
                                                        ["docker", "restart", n]),
                           confirm="restart?"),
                    Action("stop", lambda: self._act("stopping", n, [n], ["docker", "stop", n]),
                           confirm="stop?")]
        if c["state"] == "paused":
            return [Action("resume", lambda: self._act("resuming", n, [n],
                                                       ["docker", "unpause", n]))]
        return [Action("start", lambda: self._act("starting", n, [n], ["docker", "start", n])),
                Action("remove", lambda: self._act("removing", n, [n], ["docker", "rm", n]),
                       confirm="remove?", danger=True)]

    def _prune(self, label, cmd):
        self._act("pruning", label, [], cmd, scope="prune")

    # -- panel ------------------------------------------------------------

    def _pending_row(self, name, dot=None, scope="unit"):
        return Row(name, f"{self._pending[(scope, name)]}…", color=AMBER, dot=dot or AMBER)

    def _why(self, c):
        """One line on what happened to a stopped container."""
        if c["state"] == "created":
            return "created, never started"
        code = c["exit_code"]
        if c["oom"]:
            why = "OOM-killed (137)"
        elif code is None:
            why = c["state"]
        elif code in (0, 143):
            why = EXIT_MEANING[code]
        elif code == 137:
            why = "killed (137, docker stop timeout or kill)"
        else:
            why = f"exit {code} · {EXIT_MEANING.get(code, 'error')}"
        if c["policy"] and c["crashed"]:
            why += f" · restart={c['policy']} but not back"
        return why

    def rows(self, data):
        if not (data or {}).get("available"):
            return [
                Section("DAEMON", alert=True),
                Note("Docker is not running, or this user cannot reach", CRIT),
                Note("the socket. Try:  docker info", FG_DIM),
            ]
        page = self.page or "NOW"
        if page == "STOPPED":
            return self._page_stopped(data)
        if page == "SYSTEM":
            return self._page_system(data)
        return self._page_now(data)

    # NOW: what needs a human, then what is running. -------------------------

    def _page_now(self, data):
        items = []
        units = data.get("units") or []

        att = self._attention(data)
        if att:
            items.append(Section("NEEDS ATTENTION", alert=True))
            for u, c, why in att:
                colour = CRIT if why in ("restarting", "unhealthy") else WARN
                if ("unit", c["name"]) in self._pending:
                    items.append(self._pending_row(c["name"]))
                    continue
                if c["state"] == "running":
                    meta = f"{why} · up {_dur(c['up_h'])}"
                else:
                    meta = f"{self._why(c)} · {time_ago_h(c['age_h'])}"
                if c["restarts"]:
                    meta += f" · restarted {c['restarts']}×"
                body = c["log"] or c["image"]
                items.append(Alert(c["name"], meta, body, color=colour, mono_body=bool(c["log"]),
                                   actions=self._container_actions(c)))

        running = [u for u in units if u["running"]]
        if running:
            n = sum(len(u["running"]) for u in running)
            items.append(Section(f"RUNNING · {n}"))
            for u in sorted(running, key=lambda x: -x["mem"]):
                if ("unit", u["name"]) in self._pending:
                    items.append(self._pending_row(u["name"]))
                    continue
                mem = _fmt_bytes_short(u["mem"])
                cpu = f"{u['cpu']:.0f}%" if u["cpu"] >= 1 else ""
                if u["stack"] and len(u["members"]) > 1:
                    partial = len(u["running"]) < len(u["members"])
                    head = f"{len(u['running'])}/{len(u['members'])} up"
                    dot = WARN if partial else OK
                    live = u["running"]
                    sub = ", ".join(sorted(c["service"] or c["name"] for c in live))
                    if partial:
                        down = sorted(c["service"] or c["name"] for c in u["members"]
                                      if c not in live)
                        sub += f"   down: {', '.join(down)}"
                else:
                    c = u["running"][0]
                    head = "paused" if c["state"] == "paused" else f"up {_dur(c['up_h'])}"
                    dot = WARN if c["health"] == "starting" or c["state"] == "paused" else OK
                    bits = [c["image"]] + c["ports"]
                    if c["health"]:
                        bits.append(c["health"])
                    if c["restarts"]:
                        bits.append(f"restarted {c['restarts']}×")
                        dot = WARN
                    sub = " · ".join(bits)
                items.append(Row(u["name"], " · ".join(b for b in (head, mem, cpu) if b),
                                 dot=dot, dim_value=True, sub=sub, actions=self._unit_actions(u)))
        elif not att:
            items.append(Note("Nothing running", IDLE))

        # the rest, as one quiet count that points at the next page
        stopped = [u for u in units if not u["running"]]
        if stopped:
            crashed = sum(1 for u in stopped if u["crashed"])
            n = len(stopped)
            text = f"{n} stopped stack{'s' if n != 1 else ''}"
            if crashed:
                text += f", {crashed} broken"
            items.append(Section("ELSEWHERE"))
            items.append(Note(f"{text}  →  STOPPED page", FG_DIM))
        if not units:
            items.append(Note("No containers", IDLE))
        return items

    # STOPPED: every unit that is not running, nothing collapsed. --------------

    def _page_stopped(self, data):
        items = []
        units = [u for u in (data.get("units") or []) if not u["running"]]
        if not units:
            return [Note("Everything is running", IDLE)]

        def line(u):
            if ("unit", u["name"]) in self._pending:
                return self._pending_row(u["name"])
            m = u["members"]
            when = time_ago_h(u["age_h"]) or "never started"
            worst = next((c for c in m if c["crashed"]), m[0])
            bad = [c for c in m if c["crashed"]]
            if len(m) > 1:
                sub = (f"{len(bad)}/{len(m)} crashed · " if bad else f"{len(m)} services · ")
                sub += self._why(worst)
            else:
                sub = self._why(worst)
            value = when
            if worst["log"]:          # the last log line beats the exit code as a
                sub = worst["log"]    # diagnostic; keep the code in the value cell
                short = "OOM" if worst["oom"] else f"exit {worst['exit_code']}"
                value = f"{short} · {value}"
            dot = CRIT if u["crashed"] else IDLE
            return Row(u["name"], value, dot=dot, dim_value=True, sub=sub,
                       actions=self._unit_actions(u))

        broken = sorted([u for u in units if u["crashed"]],
                        key=lambda u: u["age_h"] if u["age_h"] is not None else 1e9)
        quiet = [u for u in units if not u["crashed"]]
        recent = sorted([u for u in quiet if u["age_h"] is not None
                         and u["age_h"] < DORMANT_DAYS * 24], key=lambda u: u["age_h"])
        dormant = sorted([u for u in quiet if u["age_h"] is None
                          or u["age_h"] >= DORMANT_DAYS * 24],
                         key=lambda u: u["age_h"] if u["age_h"] is not None else 1e9)

        if broken:
            items.append(Section(f"BROKEN · {len(broken)} · crashed, not touched since",
                                 alert=True))
            items += [line(u) for u in broken]
        if recent:
            items.append(Section(f"STOPPED · {len(recent)} · this week"))
            items += [line(u) for u in recent]
        if dormant:
            items.append(Section(f"DORMANT · {len(dormant)} · not run in a week"))
            items += [line(u) for u in dormant]
        return items

    # SYSTEM: the engine, and what could be reclaimed. ------------------------

    def _page_system(self, data):
        s = data.get("system") or {}
        df = s.get("df") or {}
        rows = data.get("containers") or []
        running = sum(1 for c in rows if c["state"] == "running")
        items = [Section("ENGINE")]
        items.append(Row("Docker", s.get("engine") or "?", dim_value=True))
        items.append(Row("Compose", (s.get("compose") or "not installed"), dim_value=True))
        items.append(Row("Containers", f"{len(rows)} · {running} running", dim_value=True))
        img = df.get("images", {})
        vol = df.get("local volumes", {})
        items.append(Row("Images", f"{img.get('total', '?')} · {s.get('dangling', 0)} dangling",
                         dim_value=True))
        items.append(Row("Volumes", f"{vol.get('total', '?')} · {vol.get('active', '?')} in use",
                         dim_value=True))

        if df:
            items.append(Section("DISK · reclaimable"))
            stopped_n = len(rows) - running

            def drow(label, key, sub, action=None):
                d = df.get(key, {})
                recl = d.get("reclaimable", 0)
                value = f"{_fmt_dec(recl)} of {_fmt_dec(d.get('size', 0))}"
                pend = ("prune", label) in self._pending
                return (self._pending_row(label, IDLE, scope="prune") if pend else
                        Row(label, value, dim_value=True, sub=sub,
                            dot=WARN if recl > 20e9 else IDLE,
                            actions=[action] if action else []))

            img_recl = img.get("reclaimable", 0)
            items.append(drow(
                "images", "images",
                f"{s.get('dangling', 0)} dangling · prune drops every image no container uses",
                Action("prune unused",
                       lambda: self._prune("images", ["docker", "image", "prune", "-a", "-f"]),
                       confirm=f"free {_fmt_dec(img_recl)}?", danger=True) if img_recl else None))
            items.append(drow(
                "containers", "containers",
                f"{stopped_n} stopped · prune removes every one of them",
                Action("prune stopped",
                       lambda: self._prune("containers", ["docker", "container", "prune", "-f"]),
                       confirm=f"remove {stopped_n}?", danger=True) if stopped_n else None))
            items.append(drow(
                "build cache", "build cache", "rebuilds cost time, not data",
                Action("prune",
                       lambda: self._prune("build cache", ["docker", "builder", "prune", "-f"]),
                       confirm="prune?", danger=True)))
            items.append(drow("volumes", "local volumes",
                              "data lives here · never pruned from this panel"))
            items.append(Note("Buttons ask twice. Nothing in use is ever removed.", FG_DIM))
        else:
            items.append(Note("docker system df did not answer", IDLE))
        return items

    def footer(self, data):
        base = super().footer(data)
        if (data or {}).get("available"):
            return base.replace("~: move", "Hover a tab   ·   ~: move")
        return base


if __name__ == "__main__":
    run(DockerWidget)
