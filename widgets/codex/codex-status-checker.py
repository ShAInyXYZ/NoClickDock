#!/usr/bin/python3
"""
NoClickDock — Codex Status Checker

Always-on-top indicator for OpenAI Codex: the status page, how much of
your plan's rate limit is gone, and what your local Codex sessions spent.

The plan limits and token spend are read from the session rollouts Codex
writes under ~/.codex/sessions — every turn logs a `token_count` event that
carries the account's rate-limit windows. Purely local: no API key, no
account call. The service health comes from https://status.openai.com.
"""

__version__ = "1.1.0"

import json
import os
import re
import sys
import time
from datetime import datetime, timezone
from urllib.request import urlopen, Request

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "core"))
from shinywidget import (
    WidgetApp, Section, Row, Note, Alert, Limit, Tiles, Chips,
    OK, WARN, BAD, CRIT, IDLE, AMBER, FG, FG_DIM,
    parse_iso, time_ago, run,
)

STATUS_URL = "https://status.openai.com/api/v2/status.json"
COMPONENTS_URL = "https://status.openai.com/api/v2/components.json"
INCIDENTS_URL = "https://status.openai.com/api/v2/incidents.json"
STATUS_PAGE_URL = "https://status.openai.com"

# The status page lists every OpenAI product. These are the ones a Codex
# user actually depends on; anything else only gets words when it is broken.
CODEX_COMPONENTS = [
    "Codex API", "Codex Web", "Codex in ChatGPT Desktop",
    "CLI", "VS Code extension", "Responses",
    "Chat Completions", "Login", "Conversations",
]
COMPONENT_SHORT = {
    "Codex in ChatGPT Desktop": "desktop app",
    "VS Code extension": "vs code",
    "Chat Completions": "completions",
}

INDICATOR_STATE = {"none": "ok", "minor": "warn", "major": "bad", "critical": "crit"}
STATUS_COLORS = {
    "operational": OK, "degraded_performance": WARN, "partial_outage": BAD,
    "major_outage": CRIT, "under_maintenance": IDLE,
}
STATUS_LABELS = {
    "operational": "operational", "degraded_performance": "degraded",
    "partial_outage": "partial outage", "major_outage": "major outage",
    "under_maintenance": "maintenance",
}
IMPACT_COLORS = {"none": OK, "minor": WARN, "major": BAD, "critical": CRIT}

# -- local Codex usage ----------------------------------------------------
SESSIONS_DIRS = [
    os.path.join(os.path.expanduser("~"), ".codex", "sessions"),
    os.path.join(os.path.expanduser("~"), ".codex", "archived_sessions"),
]
USAGE_REFRESH_SECS = 120   # rollouts are large; refresh far slower than status
USAGE_WINDOW_DAYS = 7
LIMIT_WARN_PCT = 75
LIMIT_CRIT_PCT = 90


def limit_color(pct):
    if pct >= LIMIT_CRIT_PCT:
        return CRIT
    if pct >= LIMIT_WARN_PCT:
        return WARN
    return OK


def fmt_tokens(n):
    """Compact token count: 1.2M, 348K, 912."""
    if n >= 1_000_000_000:
        return f"{n / 1_000_000_000:.1f}B"
    if n >= 1_000_000:
        return f"{n / 1_000_000:.1f}M"
    if n >= 1_000:
        return f"{n / 1_000:.0f}K"
    return str(n)


def window_label(minutes):
    """300 -> '5H', 10080 -> 'WEEK'."""
    try:
        m = int(minutes)
    except (TypeError, ValueError):
        return "LIMIT"
    if m == 10080:
        return "WEEK"
    if m % 1440 == 0 and m > 1440:
        return f"{m // 1440}D"
    if m % 60 == 0:
        return f"{m // 60}H"
    return f"{m}M"


def _minutes(window_minutes):
    """window_minutes as an int for sorting; unknown sorts last."""
    try:
        return int(window_minutes)
    except (TypeError, ValueError):
        return 1 << 30


def fmt_reset(epoch):
    """1789178782 -> 'Sep 12, 3:59pm' in local time; '' if unknown."""
    try:
        t = datetime.fromtimestamp(float(epoch))
    except (TypeError, ValueError, OSError, OverflowError):
        return ""
    day = t.strftime("%b ") + str(t.day)
    clock = t.strftime("%I:%M%p").lstrip("0").lower()
    return f"{day}, {clock}"


def _limit_title(group):
    """A heading for one allowance.

    `limit_name` is the server's label for the bucket, not the model you are
    running - the same bucket bills several models - so it is shown as a
    bucket name and never as "your model".
    """
    name = group.get("name") or "General limit"
    bits = [name]
    age = time_ago(group.get("seen")) if group.get("seen") else ""
    if age and age != "just now":
        bits.append(age)
    if group.get("idle"):
        bits.append("never reports usage")
    return "   ·   ".join(bits)


def _short_model(model):
    m = re.sub(r"-\d{4}-\d{2}-\d{2}$", "", model or "")
    return m or "unknown"


def _rollout_files(cutoff):
    for root in SESSIONS_DIRS:
        if not os.path.isdir(root):
            continue
        for dirpath, _, names in os.walk(root):
            for fname in names:
                if not fname.endswith(".jsonl"):
                    continue
                path = os.path.join(dirpath, fname)
                try:
                    if os.path.getmtime(path) >= cutoff:
                        yield path
                except OSError:
                    continue


def read_codex_usage():
    """Plan limits and token spend from the local Codex rollouts.

    Returns {"limits": [...], "plan": str, "limits_seen": datetime|None,
             "1h", "24h", "7d", "sessions", "models", "available"}.
    Never raises: an absent or unreadable directory yields an empty result.

    An account can carry more than one allowance at a time - a general one and
    a per-model one, each with its own id, windows and reset clocks. They are
    kept apart: collapsing them to whichever turn happened to run last makes
    the panel swap between two different meanings of "used", which is how a
    busy week can read 0 %.
    """
    now = datetime.now(timezone.utc)
    out = {"limits": [], "plan": "", "limits_seen": None,
           "1h": 0, "24h": 0, "7d": 0, "sessions": 0, "models": [], "available": False}
    cutoff = now.timestamp() - USAGE_WINDOW_DAYS * 86400
    sessions = set()
    models = {}
    newest_rl = {}     # limit_id -> (timestamp, rate_limits)
    live_ids = set()   # buckets seen above 0 % at least once

    for path in _rollout_files(cutoff):
        try:
            fh = open(path, errors="ignore")
        except OSError:
            continue
        with fh:
            session_id = None
            model = None
            # Per-turn usage keyed by timestamp.
            #
            # A resumed session rewrites its whole history into the new file
            # under a single timestamp - the moment of the resume - with a
            # fresh `ordinal` per record. Those are real turns, but their real
            # times are lost, so counting them would charge an entire session's
            # spend to the hour the resume happened. Dropping the group is a
            # deliberate under-count: better a low 1H figure than one inflated
            # by orders of magnitude.
            turns = {}
            replayed = set()
            for line in fh:
                if '"session_meta"' in line and session_id is None:
                    try:
                        p = json.loads(line).get("payload") or {}
                        session_id = p.get("session_id") or p.get("id")
                    except (json.JSONDecodeError, ValueError):
                        pass
                    continue
                if '"turn_context"' in line:
                    try:
                        model = (json.loads(line).get("payload") or {}).get("model") or model
                    except (json.JSONDecodeError, ValueError):
                        pass
                    continue
                if '"token_count"' not in line:
                    continue
                try:
                    rec = json.loads(line)
                except (json.JSONDecodeError, ValueError):
                    continue
                payload = rec.get("payload") or {}
                if payload.get("type") != "token_count":
                    continue
                ts = rec.get("timestamp")
                rl = payload.get("rate_limits") or {}
                if rl.get("primary") and ts:
                    lid = str(rl.get("limit_id") or "")
                    # Compare parsed instants: fractional precision varies
                    # between writers, so '...00.9Z' would sort after
                    # '...00.15Z' as text while being the earlier moment.
                    when = parse_iso(ts)
                    prev = newest_rl.get(lid)
                    if when is not None and (prev is None or when > prev[0]):
                        newest_rl[lid] = (when, rl)
                    for wkey in ("primary", "secondary"):
                        w = rl.get(wkey)
                        if isinstance(w, dict) and w.get("used_percent"):
                            live_ids.add(lid)     # this bucket does move
                            break
                info = payload.get("info") or {}
                last = info.get("last_token_usage") or {}
                total = last.get("total_tokens") or 0
                if not total or not ts:
                    continue
                if ts in turns:
                    replayed.add(ts)
                    continue
                turns[ts] = (total, model)

            for ts, (total, tmodel) in turns.items():
                if ts in replayed:
                    continue
                t = parse_iso(ts)
                if t is None:
                    continue
                age = max(0.0, (now - t).total_seconds())
                if age < 3600:
                    out["1h"] += total
                if age < 86400:
                    out["24h"] += total
                    if session_id:
                        sessions.add(session_id)
                    key = tmodel or "unknown"
                    models[key] = models.get(key, 0) + total
                if age < USAGE_WINDOW_DAYS * 86400:
                    out["7d"] += total

    out["sessions"] = len(sessions)
    out["models"] = sorted(models.items(), key=lambda kv: -kv[1])[:3]
    out["available"] = out["7d"] > 0

    # A bucket the account reports but never fills is noise on a usage panel:
    # drawing it as a full-weight bar next to a real one reads as "you have
    # used nothing", which is the opposite of true.
    idle_ids = set(newest_rl) - live_ids

    for lid, (seen, rl) in newest_rl.items():
        if seen and (out["limits_seen"] is None or seen > out["limits_seen"]):
            out["limits_seen"] = seen
        if not out["plan"]:
            out["plan"] = str(rl.get("plan_type") or "")
        rows = []
        for key in ("primary", "secondary"):
            win = rl.get(key)
            if not isinstance(win, dict) or win.get("used_percent") is None:
                continue
            try:
                pct = float(win["used_percent"])
            except (TypeError, ValueError):
                continue
            rows.append({
                "label": window_label(win.get("window_minutes")),
                "pct": pct,
                "resets": fmt_reset(win.get("resets_at")),
                "minutes": _minutes(win.get("window_minutes")),
            })
        if not rows:
            continue
        rows.sort(key=lambda r: r["minutes"])   # shortest window first
        out["limits"].append({
            "id": lid,
            "name": str(rl.get("limit_name") or ""),
            "seen": seen,
            "rows": rows,
            "idle": all(r["pct"] == 0.0 for r in rows) and lid in idle_ids,
        })

    # Real usage leads; an idle bucket sinks below it, newest first within each.
    out["limits"].sort(key=lambda g: (g["idle"],
                                      -(g["seen"] or datetime.min.replace(
                                          tzinfo=timezone.utc)).timestamp()))
    return out


class CodexWidget(WidgetApp):
    name = "codex"
    title = "CODEX STATUS"
    poll_secs = 30
    click_url = STATUS_PAGE_URL
    panel_width = 588        # the Claude panel's width: same meters, same chips

    # The OpenAI blossom, white on the near-black the ChatGPT apps use.
    brand = (0.10, 0.10, 0.10)
    logo_fg = (1.0, 1.0, 1.0)
    logo_scale = 0.64
    glyph = (
        "M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9A6.0651 "
        "6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 .7427 7.09"
        "66 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 0 13.25"
        "99 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 6.0557 "
        "0 0 0-.7475-7.0729zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 4.7783-"
        "2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 .038.052v5.5826a4"
        ".504 4.504 0 0 1-4.4945 4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.085"
        "2 4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615"
        "L9.74 19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1"
        ".9728V11.6a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071"
        " 0l-4.8303-2.7865A4.504 4.504 0 0 1 2.3408 7.872zm16.5963 3.8558L13.1038 8.364 15.1192 7"
        ".2a.0757.0757 0 0 1 .071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79"
        " 0 0 0-.407-.667zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.409 "
        "9.2297V6.8974a.0662.0662 0 0 1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66"
        "zM8.3065 12.863l-2.02-1.1638a.0804.0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.37"
        "57-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 0-.3927.6813zm1.0976-2.3654l2.602-1.4998"
        " 2.6069 1.4998v2.9994l-2.5974 1.4997-2.6067-1.4997Z"
    )   # official mark (Simple Icons)

    def __init__(self):
        self._usage = None
        self._usage_at = 0.0

    # -- data -------------------------------------------------------------

    def fetch(self):
        data = {
            "indicator": "unknown", "description": "Unable to reach status page",
            "components": [], "incidents": [],
            "last_check": datetime.now(timezone.utc), "usage": None,
        }
        headers = {"User-Agent": "noclickdock-codex/1.0"}
        try:
            with urlopen(Request(STATUS_URL, headers=headers), timeout=10) as r:
                s = json.loads(r.read())
            data["indicator"] = s["status"]["indicator"]
            data["description"] = s["status"]["description"]
            with urlopen(Request(COMPONENTS_URL, headers=headers), timeout=10) as r:
                data["components"] = json.loads(r.read()).get("components", [])
            with urlopen(Request(INCIDENTS_URL, headers=headers), timeout=10) as r:
                incs = json.loads(r.read()).get("incidents", [])
            # incident.io's mirror of the Statuspage API has no unresolved.json
            data["incidents"] = [i for i in incs
                                 if i.get("status") not in ("resolved", "postmortem")]
        except Exception:
            pass

        now = time.monotonic()
        if self._usage is None or now - self._usage_at >= USAGE_REFRESH_SECS:
            try:
                self._usage = read_codex_usage()
            except Exception:
                self._usage = self._usage or {}
            self._usage_at = now
        data["usage"] = self._usage
        return data

    # -- state ------------------------------------------------------------

    def state(self, data):
        return INDICATOR_STATE.get((data or {}).get("indicator"), "idle")

    def state_label(self, state, data):
        return (data or {}).get("description") or "unknown"

    def transition_message(self, old, new, data):
        return (data or {}).get("description") or ""

    def _broken(self, data):
        return [c for c in (data or {}).get("components", [])
                if c.get("status") not in (None, "operational")]

    def alert_ids(self, data):
        ids = [f"inc:{i.get('id')}" for i in (data or {}).get("incidents", [])]
        ids += [f"comp:{c.get('name')}:{c.get('status')}" for c in self._broken(data)]
        return ids

    def alert_message(self, alert_id, data):
        if alert_id.startswith("inc:"):
            for inc in data.get("incidents", []):
                if f"inc:{inc.get('id')}" == alert_id:
                    return inc.get("name", "")
            return ""
        _, name, status = alert_id.split(":", 2)
        return f"{COMPONENT_SHORT.get(name, name)}: {STATUS_LABELS.get(status, status)}"

    def footer(self, data):
        base = super().footer(data)
        seen = ((data or {}).get("usage") or {}).get("limits_seen")
        if seen is not None:
            base = f"Limits as of {time_ago(seen)}   ·   " + base
        return base

    # -- panel ------------------------------------------------------------

    def rows(self, data):
        """Same arrangement as the Claude widget: usage first, then services,
        then incidents. Problems get words; the healthy majority is quiet."""
        items = []
        usage = (data or {}).get("usage") or {}
        limits = usage.get("limits") or []

        # USAGE: every allowance the account carries, each with its own
        # windows, then the token spend as stat tiles, then the models that
        # spent it. Two allowances mean two meanings of "used", so they are
        # never merged into one bar.
        if limits or usage.get("available"):
            plan = usage.get("plan", "")
            items.append(Section(f"USAGE · {plan.upper()} PLAN" if plan else "USAGE"))
        # Every allowance the account carries, always, each under its own
        # heading. A bucket that has never reported usage still gets its bars
        # so the panel's shape stays put; its heading says it is not moving,
        # because an empty bar on its own would read as spare capacity.
        for group in limits:
            if len(limits) > 1:      # one allowance needs no heading
                items.append(Note(_limit_title(group),
                                  IDLE if group["idle"] else FG_DIM))
            for row in group["rows"]:
                items.append(Limit(row["label"], row["pct"], row["resets"],
                                   color=limit_color(row["pct"])))
        if usage.get("available"):
            items.append(Tiles([("1H", fmt_tokens(usage["1h"])),
                                ("24H", fmt_tokens(usage["24h"])),
                                ("7D", fmt_tokens(usage["7d"])),
                                ("SESSIONS", str(usage["sessions"]))]))
            models = usage.get("models") or []
            if models:   # the top spender is the signal, the rest are context
                items.append(Chips([(_short_model(m), AMBER if i == 0 else FG, fmt_tokens(t))
                                    for i, (m, t) in enumerate(models)],
                                   columns=min(3, len(models))))
        elif usage is not None and not limits:
            items.append(Section("USAGE"))
            items.append(Note("No Codex sessions in the last 7 days", IDLE))

        # SERVICES: the Codex-facing chips, three per row, a dot each. Anything
        # on the whole OpenAI page that is not operational is spelled out first.
        comps = {c.get("name"): c for c in (data or {}).get("components", [])}
        broken = self._broken(data)
        if broken:
            items.append(Section(f"DEGRADED · {len(broken)}", alert=True))
            for c in broken:
                st = c.get("status", "")
                items.append(Row(c.get("name", ""), STATUS_LABELS.get(st, st).upper(),
                                 color=STATUS_COLORS.get(st, WARN),
                                 dot=STATUS_COLORS.get(st, WARN)))
        if comps:
            items.append(Section("SERVICES"))
            chips, seen = [], set()
            for name in CODEX_COMPONENTS:
                c = comps.get(name)
                if not c or name in seen:
                    continue
                seen.add(name)
                st = c.get("status", "operational")
                chips.append((COMPONENT_SHORT.get(name, name).lower(),
                              STATUS_COLORS.get(st, IDLE)))
            items.append(Chips(chips, columns=3))
            others = len(comps) - len(seen)
            if others > 0 and not broken:
                items.append(Note(f"{others} other OpenAI services operational", FG_DIM))
        elif (data or {}).get("indicator") == "unknown":
            items.append(Section("STATUS PAGE", alert=True))
            items.append(Note("status.openai.com unreachable", IDLE))

        incidents = (data or {}).get("incidents", [])
        if incidents:
            items.append(Section("ACTIVE INCIDENTS", alert=True))
            for inc in incidents[:3]:
                impact = inc.get("impact", "minor")
                updates = inc.get("incident_updates") or []
                latest = updates[0] if updates else {}
                when = parse_iso(latest.get("created_at") or inc.get("updated_at"))
                meta = "  ·  ".join(p for p in (
                    (inc.get("status") or "").upper(), time_ago(when) if when else "") if p)
                items.append(Alert(inc.get("name", ""), meta, (latest.get("body") or "")[:160],
                                   color=IMPACT_COLORS.get(impact, WARN)))
        return items


if __name__ == "__main__":
    run(CodexWidget)
