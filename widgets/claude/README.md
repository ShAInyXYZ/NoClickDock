<p align="center">
  <img src="images/logo.svg" alt="Claude Status Checker" width="80" />
</p>

<p align="center">
  <strong>ShinyWidgets</strong> — Claude Status Checker
</p>

<p align="center">
  <em>Real-time desktop widgets for your creative pipeline</em>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/ShinyWidgets-Claude-orange?style=for-the-badge" alt="ShinyWidgets - Claude" />
  <img src="https://img.shields.io/badge/python-3.8+-3776AB?style=for-the-badge&logo=python&logoColor=white" alt="Python 3.8+" />
  <img src="https://img.shields.io/badge/GTK-3.0-4A86CF?style=for-the-badge&logo=gnome&logoColor=white" alt="GTK3" />
  <img src="https://img.shields.io/badge/license-MIT-brightgreen?style=for-the-badge" alt="MIT License" />
</p>

---

A lightweight, always-on-top desktop status indicator for **Anthropic's Claude** services. Shows a floating circular dot that changes color based on real-time API health, with toast notifications for outages and recoveries, and a detailed hover panel for per-service breakdowns and active incidents.

> **Part of the [ShinyWidgets](https://github.com/ShAInyXYZ) series** — small, focused desktop widgets that let you know what's happening in your software and pipeline at a glance. Zero clutter, zero noise, just the info you need.

---

<p align="center">
  <img src="images/screenshot.png" alt="Claude Status Checker screenshot" width="400" />
</p>

## What It Does

| Dot Color | Meaning |
|:-:|---|
| **Green** | All systems operational |
| **Yellow** | Degraded performance |
| **Orange** | Partial outage |
| **Red** | Major outage |
| **Grey** | Status page unreachable |

## Features

- **Floating status dot** — transparent circular indicator, always on top. Sits flat and quiet when healthy; a slow breathing ring appears only when something is wrong
- **Live toast notifications** — real-time alerts that pop up next to the dot:
  - Service status changes (e.g. `API: DEGRADED`, `claude.ai: OPERATIONAL`)
  - Overall health transitions (`All systems operational`)
  - New incident alerts with severity-colored text
- **Hover panel** — detailed breakdown of all Anthropic services:
  - claude.ai
  - Claude Console (platform.claude.com)
  - Claude API (api.anthropic.com)
  - Claude Code
  - Claude Cowork
  - Claude for Government
- **Active incidents** — shows current incidents with severity, status, time since update, and latest update body
- **Usage vs. your plan limits** — the number that actually matters: percent of your session (5-hour) window, your weekly all-model limit, and any per-model limit such as Fable, each with a meter and a reset time. Bars turn yellow at 75% and red at 90%, so you can see a cap coming before you hit it
- **Token spend** — supporting detail beneath the limits: raw tokens over the past hour / day / week, session count, and the model split, read from the session transcripts in `~/.claude/projects`
- **Click to open** — click the dot to open the full status page in your browser
- **Draggable** — click and drag to reposition anywhere on screen
- **Multi-monitor aware** — `~` key cycles through left/right sides of every connected display
- **Cross-platform** — auto-detects Linux / Windows and adjusts window hints
- **Lightweight** — zero pip dependencies, uses only Python standard library + system GTK3
- **Auto-refresh** — polls Anthropic's status page every 30 seconds

## Requirements

- Python 3.8+
- GTK3 with GObject Introspection
- Compositing window manager (for RGBA transparency)

The app **auto-detects your platform** at startup and adjusts accordingly. If GTK3 is missing, it prints platform-specific installation instructions.

<details>
<summary><strong>Linux</strong></summary>

GTK3 is pre-installed on most Linux desktops. If not:

**Debian / Ubuntu:**
```bash
sudo apt install python3-gi python3-gi-cairo gir1.2-gtk-3.0
```

**Fedora:**
```bash
sudo dnf install python3-gobject gtk3
```

**Arch:**
```bash
sudo pacman -S python-gobject gtk3
```

> **Note:** If you use conda/pyenv/virtualenv, the system `python3-gi` package may not be visible. Run with `/usr/bin/python3` or create a venv with `--system-site-packages`.

</details>

<details>
<summary><strong>Windows</strong></summary>

GTK3 is not bundled with Windows. Two options:

**Option A: MSYS2 (recommended)**

1. Install [MSYS2](https://www.msys2.org/)
2. Open the **MSYS2 UCRT64** terminal and run:
   ```bash
   pacman -S mingw-w64-ucrt-x86_64-python-gobject mingw-w64-ucrt-x86_64-gtk3
   ```
3. Run the script using the MSYS2 Python:
   ```bash
   /ucrt64/bin/python3 claude-status-checker.py
   ```

**Option B: pip + GTK3 Runtime**

1. Install the [GTK3 Runtime for Windows](https://github.com/tschoonj/GTK-for-Windows-Runtime-Environment-Installer/releases) and make sure it's on your `PATH`
2. Install PyGObject via pip:
   ```bash
   pip install PyGObject
   ```
3. Run normally:
   ```bash
   python claude-status-checker.py
   ```

> RGBA transparency requires Windows 7+ with desktop composition enabled (default on modern Windows).

</details>

## Usage

```bash
# Run directly
./claude-status-checker.py

# Or with system python (recommended if using conda/pyenv)
/usr/bin/python3 claude-status-checker.py
```

### Controls

| Action | Effect |
|---|---|
| **Hover** | Opens the status detail panel |
| **Click** | Opens [status.claude.com](https://status.claude.com) in your browser |
| **Click + Drag** | Repositions the dot |
| **`~` key** | Cycles all ShinyWidgets through screen sides (left/right, all monitors) |
| **Right-click** | Menu: refresh, widget actions, quit |

### Desktop Entry (Linux)

```bash
cat > ~/.local/share/applications/claude-status-checker.desktop << 'EOF'
[Desktop Entry]
Name=ShinyWidgets — Claude Status
Comment=Always-on-top Anthropic API status indicator
Exec=/usr/bin/python3 /path/to/claude-status-checker.py
Icon=network-transmit-receive
Type=Application
Categories=Utility;
StartupNotify=false
EOF
```

To autostart on login:
```bash
cp ~/.local/share/applications/claude-status-checker.desktop ~/.config/autostart/
```

## Widget Coordination

ShinyWidgets are designed to work together. Running multiple widgets (e.g. Claude + ComfyUI) side by side? Press **`~`** on **any** widget and they all move together, stacked vertically with a 50px gap, cycling through the left and right edges of every connected monitor.

The coordination works via a shared directory (`~/.config/status-widgets/`):
- Each widget registers itself with a JSON file on startup
- A shared `corner.json` stores the current position index
- All widgets poll this file 5 times/sec and reposition when it changes
- Stale registrations (dead PIDs) are automatically cleaned up

## How It Works

The app polls three Anthropic Statuspage.io API endpoints every 30 seconds:

| Endpoint | Data |
|---|---|
| `/api/v2/status.json` | Overall system health indicator |
| `/api/v2/components.json` | Per-service status breakdown |
| `/api/v2/incidents/unresolved.json` | Active incidents with updates |

**Toast notifications** fire automatically when:
- The overall indicator changes (e.g. `none` → `minor`)
- Any individual service changes status (e.g. `API: OPERATIONAL` → `API: DEGRADED`)
- A new incident appears

The dot window uses GTK3 with Cairo drawing on an RGBA visual for true pixel-level transparency — no visible window frame or background.

### Usage panel

Two different things, stacked by importance.

**Plan limits** come from the Claude Code CLI itself (`claude -p /usage`) — the only authoritative source for percent-of-limit, since no local file caches it. Each window gets a row: the 5-hour session window, the weekly all-model limit, and any per-model limit (Fable, for example) your plan reports. The meter turns yellow at 75% and red at 90%.

The call takes a few seconds, so it runs on its own thread every 5 minutes, separate from the 30-second status poll. If the CLI is missing, times out, or changes its wording, the parser returns nothing and the section is simply omitted — the widget never blocks or errors on it.

**Token spend** is read directly from the JSONL transcripts under `~/.claude/projects/`, where every assistant message carries a `usage` record. Those are totalled into rolling 1h / 24h / 7d windows plus the model split. This part is entirely local — no API keys, no account calls, no telemetry — and skips any file whose mtime falls outside the window, so a scan takes well under a second.

Note that token counts are per-machine: they cover local sessions only, not other devices or claude.ai. The plan-limit percentages are account-wide and authoritative.

### Design

The interface follows a warm-charcoal palette: a flat three-step surface ladder with 1px hairlines instead of shadows or gradients, and colour reserved for meaning rather than decoration. Healthy services read as quiet grey text so that a degraded or failing one is the only thing on the panel competing for your attention.

The dot itself is Anthropic orange when everything is healthy. Because that hue is taken, the usage meters escalate through yellow to red rather than orange, so "brand" and "warning" never look alike.

Fonts are declared with fallback chains (`JetBrains Mono` → `DejaVu Sans Mono` → `monospace`). Without them a missing family silently lands on a serif, which is the one thing this layout cannot absorb.

## ShinyWidgets Family

| Widget | Monitors | Repo |
|---|---|---|
| **Claude Status** | Anthropic API health, service status, incidents | *you are here* |
| **ComfyUI Status** | ComfyUI generation queue, GPU/VRAM, progress | [comfyui-status-checker](https://github.com/ShAInyXYZ/comfyui-status-checker) |
| *more coming...* | | |

> ShinyWidgets are small, single-purpose desktop indicators designed to keep you informed about the tools you depend on — without getting in the way. Each one is a single Python file with zero pip dependencies.

---

<p align="center">
  Made by <strong><a href="https://github.com/ShAInyXYZ">Sh-AI-ny</a></strong>
  <br />
  MIT License
</p>
