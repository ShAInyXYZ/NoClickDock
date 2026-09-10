#!/usr/bin/python3
"""
ShinyWidgets — Tailscale Status Checker

Always-on-top indicator for your tailnet. Deliberately narrow: most peers
in a long-lived tailnet are dormant and alerting on them is pure noise.
What actually matters is whether *this* node is up and authenticated,
whether the peers you depend on are reachable, and whether an exit node
is silently routing your traffic.

Pin the peers you care about in ~/.config/status-widgets/tailscale.json:

    {"watch": ["shiny-nas", "win-2p93i47r07h"]}

With no config every peer seen online in this session is watched.
"""

__version__ = "1.0.0"

import json
import sys
import os
from datetime import datetime, timezone

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "core"))
from shinywidget import (
    WidgetApp, Section, Row, Note, Grid,
    OK, WARN, CRIT, IDLE, AMBER, FG_DIM,
    WIDGET_DIR, run_cmd, time_ago, parse_iso, run,
)

CONFIG_FILE = os.path.join(WIDGET_DIR, "tailscale.json")

# Key expiry is the classic silent tailnet outage: everything works until
# it abruptly doesn't. Warn while there's still time to re-authenticate.
KEY_WARN_DAYS = 7


def _fmt_rate(bps):
    """Bytes/second -> '1.2 MB/s'; quiet dash below 1 KB/s."""
    if bps is None or bps < 1024:
        return ""
    for unit, size in (("GB", 1 << 30), ("MB", 1 << 20), ("KB", 1 << 10)):
        if bps >= size:
            return f"{bps / size:.1f} {unit}/s"
    return ""


def _exposure():
    """What this node publishes: 'tailscale serve' (tailnet) and Funnel (public
    internet). Both print 'No ... config' when idle; anything else is exposure."""
    out = []
    for cmd, label in (("serve", "serve"), ("funnel", "funnel")):
        txt = (run_cmd(["tailscale", cmd, "status"], timeout=5) or "").strip()
        if txt and not txt.lower().startswith("no "):
            first = txt.splitlines()[0].strip()
            out.append((label, first))
    return out


def _load_config():
    try:
        with open(CONFIG_FILE) as f:
            cfg = json.load(f)
        watch = cfg.get("watch")
        return [str(w).lower() for w in watch] if isinstance(watch, list) else None
    except (OSError, json.JSONDecodeError, AttributeError):
        return None


class TailscaleWidget(WidgetApp):
    name = "tailscale"
    title = "TAILSCALE"
    poll_secs = 30

    # The 3x3 dot grid of the Tailscale mark.
    brand = (0.141, 0.141, 0.141)     # Tailscale #242424, monochrome mark
    logo_fg = (0.937, 0.925, 0.902)
    logo_scale = 0.62
    glyph = (
    "M24 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0zm-9 9a3 3 0 1 1-6 0 3 3 0 0 1 6 0zm0-9a3 3 0 1 1-6 0 3 "
    "3 0 0 1 6 0zm6-6a3 3 0 1 1 0-6 3 3 0 0 1 0 6zm0-.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5zM3 "
    "24a3 3 0 1 1 0-6 3 3 0 0 1 0 6zm0-.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5zm18 .5a3 3 0 1 1 "
    "0-6 3 3 0 0 1 0 6zm0-.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5zM6 12a3 3 0 1 1-6 0 3 3 0 0 1 "
    "6 0zm9-9a3 3 0 1 1-6 0 3 3 0 0 1 6 0zm-3 2.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5zM6 3a3 3 "
    "0 1 1-6 0 3 3 0 0 1 6 0zM3 5.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5z"
)

    def __init__(self):
        self._watch = _load_config()
        self._seen = set()   # peers seen online this run, when nothing is pinned
        self._prev = {}      # host -> (time, rx, tx): throughput between polls

    def fetch(self):
        out = run_cmd(["tailscale", "status", "--json"], timeout=15)
        data = {"available": False, "last_check": datetime.now(timezone.utc)}
        if not out.strip():
            return data
        try:
            st = json.loads(out)
        except (json.JSONDecodeError, ValueError):
            return data

        now = datetime.now(timezone.utc)
        self_node = st.get("Self") or {}
        peers = list((st.get("Peer") or {}).values())

        watch = self._watch
        if watch is None:
            # No pins: remember any peer seen online during this run. A peer
            # that blips offline then stays watched, which is the whole point;
            # peers dormant since before startup are never added.
            for p in peers:
                if p.get("Online"):
                    self._seen.add((p.get("HostName") or "").lower())
            watch = list(self._seen)

        watched, others_online, all_peers = [], 0, []
        exit_options, routes = [], []
        for p in peers:
            host = (p.get("HostName") or "").lower()
            last = parse_iso(p.get("LastSeen"))
            key_dt = parse_iso(p.get("KeyExpiry"))
            rx, tx = int(p.get("RxBytes") or 0), int(p.get("TxBytes") or 0)
            # throughput since the previous poll, per peer
            rate = None
            prev = self._prev.get(host)
            if prev:
                dt = (now - prev[0]).total_seconds()
                if dt > 0 and rx >= prev[1] and tx >= prev[2]:
                    rate = ((rx - prev[1]) + (tx - prev[2])) / dt
            self._prev[host] = (now, rx, tx)

            entry = {
                "host": p.get("HostName") or "?",
                "online": bool(p.get("Online")),
                "active": bool(p.get("Active")),
                "exit": bool(p.get("ExitNode")),
                "os": p.get("OS") or "",
                "last": last,
                "watched": host in watch,
                "direct": bool(p.get("CurAddr")),
                "relay": p.get("Relay") or "",
                "rate": rate,
                "rx": rx, "tx": tx,
                "key_expired": key_dt is not None and key_dt < now,
            }
            all_peers.append(entry)
            if host in watch:
                watched.append(entry)
            elif entry["online"]:
                others_online += 1
            if p.get("ExitNodeOption"):
                exit_options.append(entry["host"])
            for r in p.get("PrimaryRoutes") or []:
                routes.append((entry["host"], r))

        # Key expiry, when the daemon reports it
        expiry_days = None
        dt = parse_iso(self_node.get("KeyExpiry"))
        if dt is not None:
            expiry_days = (dt - now).total_seconds() / 86400

        exit_node = None
        for p in peers:
            if p.get("ExitNode"):
                exit_node = p.get("HostName")
                break

        ips = self_node.get("TailscaleIPs") or st.get("TailscaleIPs") or []
        ip4 = next((ip for ip in ips if "." in ip), ips[0] if ips else "")
        dns = (self_node.get("DNSName") or "").rstrip(".")
        if not dns and st.get("MagicDNSSuffix"):
            dns = f'{(self_node.get("HostName") or "").lower()}.{st["MagicDNSSuffix"]}'
        cv = st.get("ClientVersion") or {}
        version = (st.get("Version") or "").split("-")[0]

        data.update({
            "available": True,
            "backend": st.get("BackendState") or "Unknown",
            "self": self_node.get("HostName") or "?",
            "self_online": bool(self_node.get("Online")),
            "self_ip": ip4,
            "self_dns": dns,
            "self_relay": self_node.get("Relay") or "",
            "self_exit_option": bool(self_node.get("ExitNodeOption")),
            "self_routes": list(self_node.get("PrimaryRoutes") or []),
            "version": version,
            "update_available": cv.get("RunningLatest") is False,
            "health": [h for h in (st.get("Health") or []) if h],
            "exposure": _exposure(),
            "exit_options": sorted(exit_options),
            "routes": routes,
            "watched": sorted(watched, key=lambda w: (not w["online"], w["host"])),
            "others_online": others_online,
            "peer_total": len(peers),
            "peers": all_peers,
            "expiry_days": expiry_days,
            "exit_node": exit_node,
            "pinned": self._watch is not None,
        })
        return data
    # -- state ------------------------------------------------------------

    def state(self, data):
        if not (data or {}).get("available"):
            return "idle"
        if data.get("backend") != "Running" or not data.get("self_online"):
            return "crit"
        if any(not w["online"] for w in data.get("watched") or []):
            return "warn"
        if data.get("health"):
            return "warn"
        days = data.get("expiry_days")
        if days is not None and days <= KEY_WARN_DAYS:
            return "warn"
        return "ok"

    def state_label(self, state, data):
        if not (data or {}).get("available"):
            return "TAILSCALED UNREACHABLE"
        backend = data.get("backend")
        if backend != "Running":
            return backend.upper()
        down = [w["host"] for w in data.get("watched") or [] if not w["online"]]
        if down:
            n = len(down)
            return down[0] + " offline" if n == 1 else f"{n} peers offline"
        if data.get("health"):
            return data["health"][0][:40]
        days = data.get("expiry_days")
        if days is not None and days <= KEY_WARN_DAYS:
            return f"key expires in {days:.0f}d"
        up = sum(1 for w in data.get("watched") or [] if w["online"])
        return f"{up} peer{'' if up == 1 else 's'} up"

    def alert_ids(self, data):
        ids = []
        if (data or {}).get("available") and data.get("backend") != "Running":
            ids.append("backend:" + str(data.get("backend")))
        for w in (data or {}).get("watched") or []:
            if not w["online"]:
                ids.append("down:" + w["host"])
        for h in (data or {}).get("health") or []:
            ids.append("health:" + h)
        return ids

    def alert_message(self, alert_id, data):
        kind, _, rest = alert_id.partition(":")
        if kind == "backend":
            return f"tailscaled: {rest}"
        if kind == "health":
            return rest[:80]
        return f"{rest}: offline"

    def menu_items(self, dot):
        d = dot.data or {}
        items = []
        if d.get("self_ip"):
            items.append((f'Copy address   {d["self_ip"]}', lambda: dot.copy_text(d["self_ip"])))
        if d.get("self_dns"):
            items.append((f'Copy MagicDNS name', lambda: dot.copy_text(d["self_dns"])))
        return items

    # -- panel ------------------------------------------------------------

    def rows(self, data):
        if not (data or {}).get("available"):
            return [
                Section("DAEMON", alert=True),
                Note("tailscaled is not reachable. Try:", CRIT),
                Note("  tailscale status", FG_DIM),
            ]

        items = []

        # Health first: when the daemon has something to say, it outranks
        # everything else on this panel.
        if data.get("health"):
            items.append(Section("HEALTH", alert=True))
            for h in data["health"]:
                items.append(Note(h, WARN, wrap=True))

        items.append(Section("THIS NODE"))
        items.append(Row(data["self"], data["backend"],
                         dot=OK if data["self_online"] else CRIT,
                         dim_value=True))
        # The two strings you actually come here to copy.
        if data.get("self_ip"):
            items.append(Row("address", data["self_ip"], dim_value=True))
        if data.get("self_dns"):
            items.append(Row("magicdns", data["self_dns"], dim_value=True))

        ver = data.get("version") or ""
        if data.get("update_available"):
            items.append(Row("version", f"{ver} · update available", color=WARN, dot=WARN))
        elif ver:
            items.append(Row("version", ver, dim_value=True))

        days = data.get("expiry_days")
        if days is not None:
            if days <= KEY_WARN_DAYS:
                items.append(Row("key expires", f"{days:.0f}d", color=WARN, dot=WARN))
            else:
                items.append(Row("key expires", f"{days:.0f}d", dim_value=True))

        if data.get("exit_node"):
            items.append(Row("exit node", data["exit_node"], color=AMBER, dot=AMBER))

        # Exposure: what this machine publishes, and what the tailnet offers.
        # "nothing exposed" is worth saying out loud - it is the reassuring case.
        items.append(Section("EXPOSURE"))
        exposure = data.get("exposure") or []
        if exposure:
            for label, what in exposure:
                items.append(Row(label, what,
                                 color=CRIT if label == "funnel" else AMBER,
                                 dot=CRIT if label == "funnel" else AMBER))
        else:
            items.append(Row("serve / funnel", "nothing exposed", dot=OK, dim_value=True))
        if data.get("self_exit_option"):
            items.append(Row("exit node", "offered by this node", color=AMBER, dot=AMBER))
        for r in data.get("self_routes") or []:
            items.append(Row("route advertised", r, color=AMBER, dot=AMBER))
        if data.get("exit_options"):
            items.append(Row("exit nodes available", ", ".join(data["exit_options"]), dim_value=True))
        for host, r in data.get("routes") or []:
            items.append(Row(f"route via {host}", r, dim_value=True))

        peers = data.get("peers") or []
        online = [p for p in peers if p["online"]]
        offline = [p for p in peers if not p["online"]]

        # Every peer is named. "5 dormant" tells you nothing you can act on;
        # knowing it is the NAS versus an old phone tells you everything.
        # Online peers get their path (direct or which relay) and live rate:
        # relayed is the one thing here you can usually fix.
        if online:
            items.append(Section(f"ONLINE · {len(online)}"))
            for p in sorted(online, key=lambda x: (not x["active"], x["host"])):
                path = "direct" if p["direct"] else (f'relay {p["relay"]}' if p["relay"] else "idle")
                rate = _fmt_rate(p["rate"])
                bits = [p["os"], path] + ([rate] if rate else [])
                relayed = bool(p["relay"]) and not p["direct"]
                items.append(Row(p["host"], "  ·  ".join(b for b in bits if b),
                                 color=WARN if relayed else None,
                                 dot=OK if p["active"] else IDLE,
                                 dim_value=not relayed))

        if offline:
            watched_off = [p for p in offline if p["watched"]]
            others_off = [p for p in offline if not p["watched"]]

            if watched_off:
                items.append(Section(f"WATCHED · OFFLINE · {len(watched_off)}", alert=True))
                for p in sorted(watched_off, key=lambda x: x["host"]):
                    items.append(Row(p["host"], time_ago(p["last"]) or "offline",
                                     color=WARN, dot=WARN))

            if others_off:
                items.append(Section(f"DORMANT · {len(others_off)}"))
                cells = []
                for p in sorted(others_off,
                                key=lambda x: (x["last"] is None,
                                               -(x["last"].timestamp() if x["last"] else 0))):
                    if p["key_expired"]:   # cannot come back without re-auth
                        cells.append((p["host"], WARN, "key expired"))
                    else:
                        cells.append((p["host"], IDLE, time_ago(p["last"]) or "-"))
                items.append(Grid(cells, columns=2))

        if not peers:
            items.append(Note("No peers in this tailnet", IDLE))
        elif not data.get("pinned"):
            items.append(Note("pin peers in status-widgets/tailscale.json", FG_DIM))
        return items
if __name__ == "__main__":
    run(TailscaleWidget)
