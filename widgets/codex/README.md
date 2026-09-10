<p align="center">
  <img src="images/logo.svg" alt="Codex Status Checker" width="80" />
</p>

<p align="center">
  <strong>NoClickDock</strong> — Codex Status Checker
</p>

<p align="center">
  <em>Real-time desktop widgets for your creative pipeline</em>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/NoClickDock-Codex-orange?style=for-the-badge" alt="NoClickDock - Codex" />
  <img src="https://img.shields.io/badge/python-3.8+-3776AB?style=for-the-badge&logo=python&logoColor=white" alt="Python 3.8+" />
  <img src="https://img.shields.io/badge/GTK-3.0-4A86CF?style=for-the-badge&logo=gnome&logoColor=white" alt="GTK3" />
  <img src="https://img.shields.io/badge/license-MIT-brightgreen?style=for-the-badge" alt="MIT License" />
</p>

---

A lightweight, always-on-top desktop status indicator for **OpenAI Codex**. A floating dot with a status bubble that follows the OpenAI status page, toasts on outages and recoveries, and a hover panel that leads with the number that matters: how much of your plan's rate limit is already gone.

The companion to the Claude widget — same panel, same rules, the other assistant.

## What It Does

| Bubble | Meaning |
|:-:|---|
| **Green** | All systems operational |
| **Yellow** | Minor degradation somewhere on status.openai.com |
| **Orange** | Major outage |
| **Red** | Critical outage |
| **Grey** | Status page unreachable |

## Features

- **Usage vs. your plan limits** — every rate-limit window Codex reports (the 5-hour window, the weekly one, or whichever your plan has) as a meter with the reset time. Yellow at 75 %, red at 90 %. The plan name sits in the section header.
- **Token spend** — 1h / 24h / 7d totals, the session count, and the three models that spent the most, read from the local session rollouts.
- **Codex services** — API, Codex Web, the desktop app, CLI, VS Code extension, Responses, Chat Completions, login and conversations as a chip grid. Healthy chips are quiet; anything on the OpenAI status page that is not operational is listed first, in words.
- **Active incidents** — impact, status, time since the last update, and that update's text.
- **Toasts** — the overall indicator changing, a service leaving *operational*, a new incident.
- **Hover to read, click to open [status.openai.com](https://status.openai.com), right-click for the menu.** Zero pip dependencies: Python 3 plus the system GTK3.

## How It Works

**Service health** comes from the Statuspage-compatible API that status.openai.com exposes — `status.json`, `components.json` and `incidents.json` (this page has no `unresolved.json`, so resolved incidents are filtered out client-side). Polled every 30 seconds.

**Plan limits and token spend** are read from `~/.codex/sessions/**/*.jsonl`, the rollouts every Codex session writes (CLI, desktop app and VS Code alike). Each turn logs a `token_count` event carrying `rate_limits` — the account's windows with `used_percent`, `window_minutes` and `resets_at` — and `last_token_usage` for that turn. The newest `rate_limits` record on the machine is the plan-limit figure; the footer says how old it is, since it only updates when Codex actually runs. Per-turn usage is bucketed by timestamp into 1h / 24h / 7d and attributed to the model named in the turn's `turn_context`.

Forked sessions (subagents) replay their parent's history into their own rollout in one burst under a single timestamp. Real turns never share a millisecond, so any timestamp that appears twice in a file is treated as a replay and dropped rather than counted twice. Files untouched for seven days are skipped, so a scan takes a fraction of a second.

Everything here is local: no API key, no account call, no telemetry. Token counts cover this machine's sessions only; the rate-limit percentages are account-wide.

## Usage

```bash
/usr/bin/python3 codex-status-checker.py
```

Or install it from the repository root with `./install.sh --install codex`, which copies it under `~/.local/share/noclickdock/`, writes an autostart entry and puts it in the dock.

### Controls

| Action | Effect |
|---|---|
| **Hover** | Opens the status panel |
| **Click** | Opens [status.openai.com](https://status.openai.com) |
| **Click + Drag** | Repositions the dot (when not docked) |
| **`~` key** | Cycles all widgets through screen sides |
| **Right-click** | Refresh now · Open Codex page · Quit |

## Design

Emberdeck, like every widget here: warm charcoal, hairlines instead of shadows, colour only for state. The disc is the OpenAI mark in white on the near-black the ChatGPT apps use; the dock draws a faint hairline around it so a dark brand still reads on the pill.

---

<p align="center">
  Made by <strong><a href="https://github.com/ShAInyXYZ">Sh-AI-ny</a></strong>
  <br />
  MIT License
</p>
