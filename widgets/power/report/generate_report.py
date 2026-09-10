#!/usr/bin/env python3
"""Aggregate the Power Monitor's hourly CSVs into one self-contained HTML report.

Runs from anywhere (paths are relative to this file). Reads the widget's own
config for currency and tariff, so every cost figure - including the
peak/off-peak split - is priced the same way as the panel, and historic rows
logged under an older rate are re-priced consistently.

Every sentence in the report is computed here; the template only renders.
"""
import csv
import datetime as dt
import glob
import json
import os
import statistics
from collections import defaultdict

HERE = os.path.dirname(os.path.abspath(__file__))
CFG_PATH = os.path.expanduser("~/.config/power-monitor/config.json")
GAP_HOURS = 3           # a hole in the log longer than this is reported as a gap
RECENT_DAYS = 30


def _cfg():
    try:
        with open(CFG_PATH) as f:
            return json.load(f)
    except (OSError, json.JSONDecodeError):
        return {}


CFG = _cfg()
CUR = CFG.get("currency", "")
FLAT = float(CFG.get("price_per_kwh") or 0.30)
TOU = bool(CFG.get("tou"))
PEAK = float(CFG.get("peak_rate") or FLAT)
OFF = float(CFG.get("off_peak_rate") or FLAT)
WEEKEND_OFF = CFG.get("tou_weekend_offpeak", True)


def _parse_hours(spec):
    out = []
    for part in str(spec or "6-13,15-22").split(","):
        if "-" in part:
            a, b = part.strip().split("-", 1)
            try:
                out.append((int(a), int(b)))
            except ValueError:
                pass
    return out


PEAK_HOURS = _parse_hours(CFG.get("peak_hours"))


def in_peak(t):
    if not TOU:
        return False
    if WEEKEND_OFF and t.weekday() >= 5:
        return False
    return any(a <= t.hour < b for a, b in PEAK_HOURS)


def rate(t):
    return (PEAK if in_peak(t) else OFF) if TOU else FLAT


# ---------------------------------------------------------------- rows
rows = []
for f in sorted(glob.glob(os.path.join(HERE, "20*", "*.csv"))):
    with open(f) as fh:
        for r in csv.DictReader(fh):
            try:
                ts = dt.datetime.strptime(r["timestamp"], "%Y-%m-%d %H:%M:%S")
                rows.append({
                    "ts": ts,
                    # the row closes an hour: price it by the hour it covers
                    "mid": ts - dt.timedelta(minutes=30),
                    "w": float(r["avg_total_w"]),
                    "cpu_w": float(r.get("avg_cpu_w") or 0),
                    "gpu_w": float(r.get("avg_gpu_w") or 0),
                    "cpu_t": float(r.get("avg_cpu_temp") or 0) or None,
                    "gpu_t": float(r.get("avg_gpu_temp") or 0) or None,
                    "kwh": float(r["kwh_period"]),
                    "samples": int(float(r.get("samples") or 0)),
                })
            except (KeyError, ValueError):
                continue
rows.sort(key=lambda r: r["ts"])
if not rows:
    raise SystemExit("no log rows found")

for r in rows:
    r["peak"] = in_peak(r["mid"])
    r["cost"] = r["kwh"] * rate(r["mid"])
    tot = r["cpu_w"] + r["gpu_w"]
    # split the hour's energy by the CPU/GPU share of measured draw; the rest
    # (board, drives, peripherals) is the overhead
    r["gpu_kwh"] = r["kwh"] * (r["gpu_w"] / r["w"]) if r["w"] else 0
    r["cpu_kwh"] = r["kwh"] * (r["cpu_w"] / r["w"]) if r["w"] else 0
    r["other_kwh"] = max(0.0, r["kwh"] - r["gpu_kwh"] - r["cpu_kwh"])

# ---------------------------------------------------------------- daily
daily = defaultdict(lambda: {"kwh": 0.0, "cost": 0.0, "maxw": 0.0, "peak_kwh": 0.0,
                             "gpu_kwh": 0.0, "cpu_kwh": 0.0, "other_kwh": 0.0, "hours": 0})
for r in rows:
    d = daily[r["ts"].strftime("%Y-%m-%d")]
    d["kwh"] += r["kwh"]; d["cost"] += r["cost"]; d["maxw"] = max(d["maxw"], r["w"])
    d["peak_kwh"] += r["kwh"] if r["peak"] else 0
    d["gpu_kwh"] += r["gpu_kwh"]; d["cpu_kwh"] += r["cpu_kwh"]; d["other_kwh"] += r["other_kwh"]
    d["hours"] += 1
days = sorted(daily)
full_days = [d for d in days if daily[d]["hours"] >= 20]   # honest comparisons need whole days

# ---------------------------------------------------------------- monthly
monthly = defaultdict(lambda: {"kwh": 0.0, "cost": 0.0, "days": set(), "gpu_kwh": 0.0,
                               "cpu_kwh": 0.0, "other_kwh": 0.0, "peak_kwh": 0.0,
                               "gpu_t": [], "cpu_t": [], "gpu_tmax": 0.0, "cpu_tmax": 0.0,
                               "w": []})
for r in rows:
    m = monthly[r["ts"].strftime("%Y-%m")]
    m["kwh"] += r["kwh"]; m["cost"] += r["cost"]; m["days"].add(r["ts"].strftime("%Y-%m-%d"))
    m["gpu_kwh"] += r["gpu_kwh"]; m["cpu_kwh"] += r["cpu_kwh"]; m["other_kwh"] += r["other_kwh"]
    m["peak_kwh"] += r["kwh"] if r["peak"] else 0
    m["w"].append(r["w"])
    if r["gpu_t"]:
        m["gpu_t"].append(r["gpu_t"]); m["gpu_tmax"] = max(m["gpu_tmax"], r["gpu_t"])
    if r["cpu_t"]:
        m["cpu_t"].append(r["cpu_t"]); m["cpu_tmax"] = max(m["cpu_tmax"], r["cpu_t"])

# ---------------------------------------------------------------- profiles
hourly = defaultdict(list)
heat = defaultdict(list)          # (weekday, hour) -> watts
weekday_kwh = defaultdict(list)   # weekday -> daily kWh (full days only)
for r in rows:
    h = r["mid"].hour
    hourly[h].append(r["w"])
    heat[(r["mid"].weekday(), h)].append(r["w"])
for d in full_days:
    weekday_kwh[dt.date.fromisoformat(d).weekday()].append(daily[d]["kwh"])

# ---------------------------------------------------------------- coverage
first, last = rows[0]["ts"], rows[-1]["ts"]
span_h = max(1.0, (last - first).total_seconds() / 3600)
coverage = min(1.0, len(rows) / span_h)
gaps = []
for a, b in zip(rows, rows[1:]):
    hrs = (b["ts"] - a["ts"]).total_seconds() / 3600
    if hrs > GAP_HOURS:
        gaps.append({"from": a["ts"].strftime("%Y-%m-%d %H:%M"),
                     "to": b["ts"].strftime("%Y-%m-%d %H:%M"), "hours": round(hrs, 1)})
gaps.sort(key=lambda g: -g["hours"])

# ---------------------------------------------------------------- stats
watts = [r["w"] for r in rows]
total_kwh = sum(r["kwh"] for r in rows)
total_cost = sum(r["cost"] for r in rows)
peak_kwh = sum(r["kwh"] for r in rows if r["peak"])
peak_cost = sum(r["cost"] for r in rows if r["peak"])
gpu_kwh = sum(r["gpu_kwh"] for r in rows)
cpu_kwh = sum(r["cpu_kwh"] for r in rows)
other_kwh = sum(r["other_kwh"] for r in rows)

daily_full = [daily[d]["kwh"] for d in full_days] or [daily[d]["kwh"] for d in days]
avg_day = statistics.mean(daily_full)
median_day = statistics.median(daily_full)
cv = (statistics.pstdev(daily_full) / avg_day) if avg_day and len(daily_full) > 1 else 0.0
worst = max(full_days or days, key=lambda d: daily[d]["kwh"])
best = min(full_days or days, key=lambda d: daily[d]["kwh"])

recent_cut = last - dt.timedelta(days=RECENT_DAYS)
recent = [r for r in rows if r["ts"] >= recent_cut]
recent_days = sorted({r["ts"].strftime("%Y-%m-%d") for r in recent})
recent_full = [d for d in recent_days if daily[d]["hours"] >= 20]
recent_avg_day = statistics.mean([daily[d]["kwh"] for d in recent_full]) if recent_full else avg_day
recent_avg_w = statistics.mean([r["w"] for r in recent]) if recent else statistics.mean(watts)
recent_cost = sum(r["cost"] for r in recent)
recent_kwh = sum(r["kwh"] for r in recent)
trend_pct = ((recent_avg_day - avg_day) / avg_day * 100) if avg_day else 0.0

# first-half vs second-half of the whole record: is the machine drawing more over time?
half = len(full_days) // 2
first_half = statistics.mean([daily[d]["kwh"] for d in full_days[:half]]) if half else avg_day
second_half = statistics.mean([daily[d]["kwh"] for d in full_days[half:]]) if half else avg_day
drift_pct = ((second_half - first_half) / first_half * 100) if first_half else 0.0

hourly_avg = [round(statistics.mean(hourly[h]), 1) if hourly[h] else 0 for h in range(24)]
hourly_max = [round(max(hourly[h]), 1) if hourly[h] else 0 for h in range(24)]
busiest_hour = max(range(24), key=lambda h: hourly_avg[h])
quietest_hour = min(range(24), key=lambda h: hourly_avg[h])

roll = []
for i, d in enumerate(days):
    lo = max(0, i - 6)
    roll.append(round(statistics.mean(daily[x]["kwh"] for x in days[lo:i + 1]), 3))

# what a kWh buys, for the context table (typical household figures)
year_kwh = recent_avg_day * 365
context = [
    ["Fridge-freezer (A+ class, ~0.8 kWh/day)", f"{recent_avg_day / 0.8:.1f}× a fridge", "per day"],
    ["Electric kettle, 1 boil (~0.17 kWh)", f"{recent_avg_day / 0.17:.0f} boils", "per day"],
    ["Washing machine, 40 °C cycle (~0.8 kWh)", f"{recent_avg_day / 0.8:.1f} cycles", "per day"],
    ["10 W LED bulb, 5 h/evening (0.05 kWh)", f"{recent_avg_day / 0.05:.0f} bulbs", "per day"],
    ["Electric car, 100 km (~17 kWh)", f"{year_kwh / 17 * 100:,.0f} km", "per year at this rate"],
]

data = {
    "cfg": {"currency": CUR, "tou": TOU, "flat": FLAT, "peak": PEAK, "off": OFF,
            "peak_hours": CFG.get("peak_hours", "6-13,15-22") if TOU else "",
            "weekend_off": WEEKEND_OFF},
    "period": {"first": first.strftime("%Y-%m-%d"), "last": last.strftime("%Y-%m-%d"),
               "nDays": len(days), "fullDays": len(full_days), "nRecords": len(rows),
               "samples": sum(r["samples"] for r in rows),
               "coverage": round(coverage * 100, 1), "gaps": gaps[:8], "nGaps": len(gaps),
               "generated": dt.datetime.now().strftime("%Y-%m-%d %H:%M")},
    "stats": {
        "totalKwh": round(total_kwh, 1), "totalCost": round(total_cost, 2),
        "effRate": round(total_cost / total_kwh, 3) if total_kwh else FLAT,
        "avgDayKwh": round(avg_day, 2), "medianDayKwh": round(median_day, 2),
        "avgDayCost": round(avg_day * (total_cost / total_kwh if total_kwh else FLAT), 2),
        "cv": round(cv * 100, 0),
        "avgW": round(statistics.mean(watts), 0), "maxW": round(max(watts), 0),
        "minW": round(min(watts), 0),
        "worstDay": worst, "worstKwh": round(daily[worst]["kwh"], 2), "worstCost": round(daily[worst]["cost"], 2),
        "bestDay": best, "bestKwh": round(daily[best]["kwh"], 2),
        "peakShare": round(peak_kwh / total_kwh * 100, 0) if total_kwh else 0,
        "peakKwh": round(peak_kwh, 1), "peakCost": round(peak_cost, 2),
        "offKwh": round(total_kwh - peak_kwh, 1), "offCost": round(total_cost - peak_cost, 2),
        "gpuShare": round(gpu_kwh / total_kwh * 100, 0) if total_kwh else 0,
        "cpuShare": round(cpu_kwh / total_kwh * 100, 0) if total_kwh else 0,
        "otherShare": round(other_kwh / total_kwh * 100, 0) if total_kwh else 0,
        "busiestHour": busiest_hour, "quietestHour": quietest_hour,
        "drift": round(drift_pct, 0),
        "gpuTmax": round(max((r["gpu_t"] for r in rows if r["gpu_t"]), default=0), 0),
        "cpuTmax": round(max((r["cpu_t"] for r in rows if r["cpu_t"]), default=0), 0),
    },
    "recent": {"days": RECENT_DAYS, "kwh": round(recent_kwh, 1), "cost": round(recent_cost, 2),
               "avgDayKwh": round(recent_avg_day, 2), "avgW": round(recent_avg_w, 0),
               "trend": round(trend_pct, 0),
               "yearKwh": round(year_kwh, 0),
               "yearCost": round(recent_cost / max(1, len(recent_days)) * 365, 0),
               "monthCost": round(recent_cost / max(1, len(recent_days)) * 30.4, 2)},
    "daily": [{"d": d, "kwh": round(daily[d]["kwh"], 3), "cost": round(daily[d]["cost"], 3),
               "maxw": round(daily[d]["maxw"], 0), "hours": daily[d]["hours"],
               "peak": round(daily[d]["peak_kwh"], 3)} for d in days],
    "rolling": roll,
    "monthly": [{"m": m, "kwh": round(v["kwh"], 1), "cost": round(v["cost"], 2),
                 "days": len(v["days"]), "avg": round(v["kwh"] / max(1, len(v["days"])), 2),
                 "gpu": round(v["gpu_kwh"], 1), "cpu": round(v["cpu_kwh"], 1),
                 "other": round(v["other_kwh"], 1), "peak": round(v["peak_kwh"], 1),
                 "avgW": round(statistics.mean(v["w"]), 0),
                 "gpuT": round(statistics.mean(v["gpu_t"]), 0) if v["gpu_t"] else None,
                 "gpuTmax": round(v["gpu_tmax"], 0) or None,
                 "cpuT": round(statistics.mean(v["cpu_t"]), 0) if v["cpu_t"] else None,
                 "cpuTmax": round(v["cpu_tmax"], 0) or None}
                for m, v in sorted(monthly.items())],
    "hourly": hourly_avg, "hourlyMax": hourly_max,
    "hourlyPeak": [any(a <= h < b for a, b in PEAK_HOURS) if TOU else False for h in range(24)],
    "weekday": [round(statistics.mean(weekday_kwh[i]), 2) if weekday_kwh[i] else 0 for i in range(7)],
    "heat": [[round(statistics.mean(heat[(wd, h)]), 0) if heat[(wd, h)] else None
              for h in range(24)] for wd in range(7)],
    "context": context,
}

with open(os.path.join(HERE, "report_data.json"), "w") as fh:
    json.dump(data, fh)
html = open(os.path.join(HERE, "report_template.html")).read()
html = html.replace("/*__DATA__*/", json.dumps(data))
with open(os.path.join(HERE, "power-consumption-report.html"), "w") as fh:
    fh.write(html)
print("wrote power-consumption-report.html  (%d days, %.1f kWh, %.2f %s, coverage %.0f%%)"
      % (len(days), total_kwh, total_cost, CUR, coverage * 100))
