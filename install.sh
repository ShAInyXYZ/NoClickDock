#!/usr/bin/env bash
# NoClickDock installer.
#
# Shows what is installed, at which version, and what this checkout offers;
# you pick what to install, upgrade or remove. Nothing happens to a widget
# you did not select. "Installed" means a copy under ~/.local/share/noclickdock
# with an autostart entry pointing at it, so a `git pull` here never changes
# what runs until you choose to upgrade.
#
#   ./install.sh                 interactive
#   ./install.sh --list          table only
#   ./install.sh --all           install / upgrade everything
#   ./install.sh --upgrade       upgrade what is installed and behind
#   ./install.sh --install docker tailscale
#   ./install.sh --remove comfyui
#   add --start to (re)start what was installed, --no-start to never start
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PY="${PYTHON:-/usr/bin/python3}"
PREFIX="${NCD_HOME:-$HOME/.local/share/noclickdock}"
AUTOSTART="$HOME/.config/autostart"

# key | label | script relative to the checkout | autostart id
ENTRIES=(
  "dock|Dock|dock/ncd-dock.py|ncd-dock"
  "claude|Claude|widgets/claude/claude-status-checker.py|claude-status-checker"
  "codex|Codex|widgets/codex/codex-status-checker.py|codex-status-checker"
  "comfyui|ComfyUI|widgets/comfyui/comfyui-status-checker.py|comfyui-status-checker"
  "disk|Disk|widgets/disk/disk-status-checker.py|disk-status-checker"
  "docker|Docker|widgets/docker/docker-status-checker.py|docker-status-checker"
  "power|Power|widgets/power/power-monitor.py|power-monitor"
  "tailscale|Tailscale|widgets/tailscale/tailscale-status-checker.py|tailscale-status-checker"
)
N=${#ENTRIES[@]}
declare -a KEY LABEL REL ID AVAIL INST STATE PATHS RUN SEL
field() { echo "$1" | cut -d'|' -f"$2"; }

ver_of() { grep -m1 -oE '^__version__ = "[^"]+"' "$1" 2>/dev/null | cut -d'"' -f2 || true; }
installed_script() {   # the script an autostart entry runs, if any
  local f="$AUTOSTART/$1.desktop"
  [ -f "$f" ] || return 0
  grep -m1 '^Exec=' "$f" | cut -d= -f2- | awk '{print $NF}'
}
is_running() { pgrep -f "/$(basename "$1")\$" >/dev/null 2>&1 || pgrep -f "^[^ ]*python3 $(basename "$1")\$" >/dev/null 2>&1; }

scan() {
  for i in $(seq 0 $((N-1))); do
    local e="${ENTRIES[$i]}"
    KEY[$i]=$(field "$e" 1); LABEL[$i]=$(field "$e" 2); REL[$i]=$(field "$e" 3); ID[$i]=$(field "$e" 4)
    AVAIL[$i]=$(ver_of "$HERE/${REL[$i]}"); [ -n "${AVAIL[$i]}" ] || AVAIL[$i]="?"
    PATHS[$i]=$(installed_script "${ID[$i]}")
    RUN[$i]=""; is_running "${REL[$i]}" && RUN[$i]="running"
    if [ -z "${PATHS[$i]}" ]; then
      INST[$i]="—"; STATE[$i]="not installed"
    elif [ ! -f "${PATHS[$i]}" ]; then
      INST[$i]="?"; STATE[$i]="broken: ${PATHS[$i]} is missing"
    else
      INST[$i]=$(ver_of "${PATHS[$i]}"); [ -n "${INST[$i]}" ] || INST[$i]="legacy"
      if [ "${INST[$i]}" = "${AVAIL[$i]}" ] && [ "${PATHS[$i]}" = "$PREFIX/${REL[$i]}" ]; then
        STATE[$i]="up to date"
      elif [ "${PATHS[$i]}" != "$PREFIX/${REL[$i]}" ]; then
        STATE[$i]="upgrade: runs from ${PATHS[$i]%/*}"
      else
        STATE[$i]="upgrade ${INST[$i]} → ${AVAIL[$i]}"
      fi
    fi
  done
}

table() {
  printf '\n  NoClickDock — checkout at %s\n\n' "$HERE"
  printf '  %-3s %-11s %-10s %-10s %-9s %s\n' "#" "widget" "installed" "available" "" "status"
  for i in $(seq 0 $((N-1))); do
    local mark=" "; [ "${SEL[$i]:-0}" = 1 ] && mark="*"
    printf '  %s%-2s %-11s %-10s %-10s %-9s %s\n' "$mark" "$((i+1))" "${LABEL[$i]}" "${INST[$i]}" "${AVAIL[$i]}" "${RUN[$i]}" "${STATE[$i]}"
  done
  echo
}

start_one() {   # (index) start the installed copy, detached
  local script="$PREFIX/${REL[$1]}"
  # fully detached: no inherited pipes, or the caller's shell waits on the widget
  (exec 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&-; cd "$(dirname "$script")" && \
   DISPLAY="${DISPLAY:-:0}" nohup setsid "$PY" "$script" </dev/null >/dev/null 2>&1 &)
}
stop_one() { pkill -f "/$(basename "${REL[$1]}")\$" 2>/dev/null || true; pkill -f "^[^ ]*python3 $(basename "${REL[$1]}")\$" 2>/dev/null || true; }

install_one() {   # (index) copy into PREFIX and write the autostart entry
  local i=$1 rel="${REL[$i]}" src dst
  src="$HERE/$(dirname "$rel")/"; dst="$PREFIX/$(dirname "$rel")/"
  mkdir -p "$dst"
  rsync -a --delete --exclude __pycache__ "$src" "$dst"
  mkdir -p "$AUTOSTART"
  cat >"$AUTOSTART/${ID[$i]}.desktop" <<EOF
[Desktop Entry]
Name=${LABEL[$i]}$([ "${KEY[$i]}" = dock ] || echo " Status")
Comment=NoClickDock — ${LABEL[$i]}
Exec=$PY $PREFIX/$rel
Icon=utilities-system-monitor
Type=Application
Categories=Utility;
StartupNotify=false
X-GNOME-Autostart-enabled=true
EOF
  # the shared runtime every widget imports
  mkdir -p "$PREFIX/core"; rsync -a --delete --exclude __pycache__ "$HERE/core/" "$PREFIX/core/"
  echo "  installed ${LABEL[$i]} ${AVAIL[$i]} → $PREFIX/$rel"
}

remove_one() {
  local i=$1
  stop_one "$i"
  rm -f "$AUTOSTART/${ID[$i]}.desktop"
  [ "${KEY[$i]}" = dock ] || rm -rf "$PREFIX/$(dirname "${REL[$i]}")"
  echo "  removed ${LABEL[$i]} (your config under ~/.config is kept)"
}

apply_install() {   # install/upgrade every selected index; the dock rides along with any widget
  local any=0 i
  for i in $(seq 1 $((N-1))); do [ "${SEL[$i]:-0}" = 1 ] && any=1; done
  [ $any = 1 ] && SEL[0]=1
  for i in $(seq 0 $((N-1))); do
    [ "${SEL[$i]:-0}" = 1 ] || continue
    local was="${RUN[$i]}" unchanged=0
    [ "${STATE[$i]}" = "up to date" ] && unchanged=1
    install_one "$i"
    # the dock rides along with every install; do not bounce it when nothing changed
    if [ "${KEY[$i]}" = dock ] && [ $unchanged = 1 ] && [ -n "$was" ]; then continue; fi
    case "$START" in
      yes) stop_one "$i"; start_one "$i"; echo "  started ${LABEL[$i]}";;
      no) ;;
      *) if [ -n "$was" ]; then stop_one "$i"; start_one "$i"; echo "  restarted ${LABEL[$i]}"; fi;;
    esac
  done
}

apply_remove() {
  local i; for i in $(seq 0 $((N-1))); do [ "${SEL[$i]:-0}" = 1 ] && remove_one "$i"; done
  # a dock with nothing left to hold is not worth keeping
  if [ "${SEL[0]:-0}" != 1 ] && ! ls "$AUTOSTART"/*-status-checker.desktop "$AUTOSTART"/power-monitor.desktop >/dev/null 2>&1; then
    SEL=(); SEL[0]=1; remove_one 0
  fi
}

select_by() {   # all | new | upgrades | names...
  local i what="$1"; shift
  for i in $(seq 0 $((N-1))); do
    case "$what" in
      all) SEL[$i]=1;;
      new) [ "${STATE[$i]}" = "not installed" ] && SEL[$i]=1 || true;;
      upgrades) [[ "${STATE[$i]}" == upgrade* || "${STATE[$i]}" == broken* ]] && SEL[$i]=1 || true;;
      names) printf '%s\n' "$@" | grep -qx "${KEY[$i]}" && SEL[$i]=1 || true;;
    esac
  done
}

if ! "$PY" -c "import gi; gi.require_version('Gtk','3.0'); from gi.repository import Gtk" 2>/dev/null; then
  echo "GTK3 bindings not found for $PY. Debian/Ubuntu: sudo apt install python3-gi python3-gi-cairo gir1.2-gtk-3.0" >&2
  exit 1
fi
command -v rsync >/dev/null || { echo "rsync is required" >&2; exit 1; }

START=""   # "" = restart only what was running; yes / no from flags or the prompt
MODE=""; NAMES=()
while [ $# -gt 0 ]; do
  case "$1" in
    --list) MODE=list;;
    --all) MODE=install; select_by all;;
    --upgrade) MODE=install; NAMES=(upgrades);;
    --install) MODE=install; shift; while [ $# -gt 0 ] && [[ "$1" != --* ]]; do NAMES+=("$1"); shift; done; continue;;
    --remove) MODE=remove; shift; while [ $# -gt 0 ] && [[ "$1" != --* ]]; do NAMES+=("$1"); shift; done; continue;;
    --start) START=yes;;
    --no-start) START=no;;
    -h|--help) sed -n '2,16p' "$0"; exit 0;;
    *) echo "unknown option $1" >&2; exit 2;;
  esac
  shift
done

scan
if [ -n "$MODE" ]; then
  if [ "${NAMES[0]:-}" = upgrades ]; then select_by upgrades; elif [ ${#NAMES[@]} -gt 0 ]; then select_by names "${NAMES[@]}"; fi
  table
  case "$MODE" in
    list) exit 0;;
    install) apply_install;;
    remove) apply_remove;;
  esac
  exit 0
fi

# ---- interactive ----
while true; do
  table
  echo "  toggle: 1-$N   a=all  n=not installed  u=upgrades  c=clear"
  echo "  then:   i=install/upgrade selected   r=remove selected   q=quit"
  read -rp "  > " cmd || exit 0
  case "$cmd" in
    q) exit 0;;
    a) select_by all;;
    n) select_by new;;
    u) select_by upgrades;;
    c) SEL=();;
    i)
      if [ -z "$START" ]; then read -rp "  start / restart the selected widgets now? [Y/n] " yn; [[ "$yn" =~ ^[Nn] ]] && START=no || START=yes; fi
      apply_install; SEL=(); START=""; scan;;
    r) apply_remove; SEL=(); scan;;
    *)
      for t in $cmd; do
        if [[ "$t" =~ ^[0-9]+$ ]] && [ "$t" -ge 1 ] && [ "$t" -le "$N" ]; then
          i=$((t-1)); [ "${SEL[$i]:-0}" = 1 ] && SEL[$i]=0 || SEL[$i]=1
        fi
      done;;
  esac
done
