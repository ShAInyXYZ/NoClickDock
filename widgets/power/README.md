<p align="center">
  <img src="images/logo.svg" alt="Power Monitor" width="80" />
</p>

<p align="center">
  <strong>ShinyWidgets</strong> — Power Monitor
</p>

<p align="center">
  <em>Real-time desktop widgets for your creative pipeline</em>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/ShinyWidgets-Power-orange?style=for-the-badge" alt="ShinyWidgets" />
  <img src="https://img.shields.io/badge/python-3.8+-3776AB?style=for-the-badge&logo=python&logoColor=white" alt="Python 3.8+" />
  <img src="https://img.shields.io/badge/GTK-3.0-4A86CF?style=for-the-badge&logo=gnome&logoColor=white" alt="GTK3" />
  <img src="https://img.shields.io/badge/license-MIT-brightgreen?style=for-the-badge" alt="MIT License" />
</p>

---

A lightweight, always-on-top desktop indicator for **what your machine actually costs to run**. Reads real power draw from the hardware — Intel RAPL for the CPU, `nvidia-smi` / `rocm-smi` for every GPU — and turns it into watts, temperatures, and a monthly bill in your own currency.

> **Part of the [ShinyWidgets](https://github.com/ShAInyXYZ) series** — small, focused desktop widgets that let you know what's happening in your software and pipeline at a glance. Zero clutter, zero noise, just the info you need.

---

<p align="center">
  <img src="images/screenshot.png" alt="Power Monitor screenshot" width="620" />
</p>

## What It Does

| Dot Color | Draw | Meaning |
|:-:|:-:|---|
| **Green** | under 100W | Idle |
| **Blue** | 100–250W | Normal |
| **Yellow** | 250–500W | High load |
| **Red** | 500W+ | Full send |

## Features

- **Real measurement, not estimation** — Intel RAPL energy counters for the CPU and `nvidia-smi` for each GPU. Estimation is the fallback, not the default
- **Per-component breakdown** — CPU, every GPU, motherboard, each NVMe drive (with temperature), each display, and connected USB devices
- **Running cost** — live PLN/h (or your currency), projected monthly bill, and kWh/month, split by component in a donut chart
- **Utilization** — CPU, RAM, and per-card VRAM meters alongside the power figures
- **Multi-GPU aware** — every card is queried and summed separately, so a two-3090 box reports both
- **Usage logging** — optional CSV to `~/Documents/PM-Log/`, hourly / daily / weekly / monthly
- **Draggable · multi-monitor · `~` to reposition** — shared with every other ShinyWidget
- **Lightweight** — zero pip dependencies, Python standard library plus system GTK3

## Configuration

First run creates `~/.config/power-monitor/config.json`:

```json
{
  "price_per_kwh": 0.9,
  "currency": "PLN",
  "overhead_watts": 30,
  "peripheral_watts": 5,
  "poll_interval": 2,
  "log_interval": "hourly",
  "monitor_watts_each": 45
}
```

| Key | Meaning |
|---|---|
| `price_per_kwh` | Your electricity rate — the only value you *must* set for the cost figures to mean anything |
| `currency` | Label shown next to costs |
| `overhead_watts` | PSU inefficiency, fans, pumps: everything drawing power that reports nothing |
| `peripheral_watts` | USB devices, which rarely report their own draw |
| `monitor_watts_each` | Per-display estimate — most monitors are on their own PSU and invisible to the host |
| `poll_interval` | Seconds between samples |
| `log_interval` | `hourly`, `daily`, `weekly`, `monthly`, or `off` |

> The overhead, peripheral and monitor values are **estimates you supply**, not measurements. Everything else is read from the hardware. Set them once to match your setup and the totals become meaningful.

## Usage

```bash
./power-monitor.py

# or with system python (recommended if using conda/pyenv)
/usr/bin/python3 power-monitor.py
```

### Controls

| Action | Effect |
|---|---|
| **Hover** | Opens the power breakdown panel |
| **Click + Drag** | Repositions the dot |
| **`~` key** | Cycles all ShinyWidgets through screen sides (left/right, all monitors) |
| **Right-click** | Menu: refresh, widget actions, quit |

## Requirements

- Python 3.8+
- GTK3 with GObject Introspection
- Compositing window manager (for RGBA transparency)

**For real CPU power:** Intel RAPL (`/sys/class/powercap`) — present on most Intel and recent AMD chips. Without it, CPU draw is estimated from load.

**For GPU power:** `nvidia-smi` (bundled with the NVIDIA driver) or `rocm-smi` (AMD). Without either, GPUs are omitted rather than guessed at.

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

> RAPL energy counters are sometimes root-only on hardened kernels. If CPU power reads as estimated, check that `/sys/class/powercap/intel-rapl/intel-rapl:0/energy_uj` is readable by your user.

## Widget Coordination

ShinyWidgets are designed to work together. Press **`~`** on **any** widget and they all move together, stacked vertically with a 50px gap, cycling through the left and right edges of every connected monitor.

The coordination works via a shared directory (`~/.config/status-widgets/`):
- Each widget registers itself with a JSON file on startup
- A shared `corner.json` stores the current position index
- All widgets poll this file 5 times/sec and reposition when it changes
- Stale registrations (dead PIDs) are automatically cleaned up

## How It Works

CPU power comes from the RAPL energy counter, which is a monotonically increasing microjoule total — sampling it twice and dividing by elapsed time gives real average watts, not a guess from utilization. GPUs are read from `nvidia-smi` in a single CSV query per poll, one line per card. NVMe and system temperatures come from `/sys/class/hwmon`, RAM from `/proc/meminfo`, CPU load from `/proc/stat`, and display models from EDID via `xrandr`.

Nothing is polled that can't be read locally, there are no network calls, and the widget writes only to its own config and log directories.

## ShinyWidgets Family

| Widget | Monitors | Repo |
|---|---|---|
| **Claude Status** | Anthropic API health, plan limits, incidents | [claude-status-checker](https://github.com/ShAInyXYZ/claude-status-checker) |
| **ComfyUI Status** | ComfyUI generation queue, GPU/VRAM, progress | [comfyui-status-checker](https://github.com/ShAInyXYZ/comfyui-status-checker) |
| **Power Monitor** | Power draw, temperatures, running cost | *you are here* |
| **Disk Status** | Filesystem pressure, Docker reclaimable space | [disk-status-checker](https://github.com/ShAInyXYZ/disk-status-checker) |
| **Docker Status** | Container health, restart loops, failed exits | [docker-status-checker](https://github.com/ShAInyXYZ/docker-status-checker) |
| **Tailscale Status** | Tailnet reachability, key expiry, exit nodes | [tailscale-status-checker](https://github.com/ShAInyXYZ/tailscale-status-checker) |

## License

MIT — see [LICENSE](LICENSE).
