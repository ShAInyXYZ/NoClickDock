#!/bin/sh
# NoClickDock 2 installer for Linux.
#
#   scripts/install.sh                   install or upgrade, then start
#   scripts/install.sh --from DIR        use ncd and ncd-core from DIR
#   scripts/install.sh --build           build them first (pnpm, Go, Rust)
#   scripts/install.sh --replace-legacy  also stop NoClickDock 1 (the Python
#                                        widgets) and disable their login
#                                        entries — never deletes them
#   scripts/install.sh --restore-legacy  give those login entries back
#   scripts/install.sh --uninstall       remove NoClickDock 2 (settings kept)
#   scripts/install.sh --status          what is installed, nothing changed
#   add --no-start to install without starting
#
# Installs into ~/.local/share/noclickdock2 (NCD2_HOME to change), with a
# menu entry and a login entry. Settings: ~/.config/noclickdock/config.json;
# the first install brings NoClickDock 1's settings over (read only).
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
prefix=${NCD2_HOME:-$HOME/.local/share/noclickdock2}
legacy_prefix=${NCD_HOME:-$HOME/.local/share/noclickdock}
config_home=${XDG_CONFIG_HOME:-$HOME/.config}
data_home=${XDG_DATA_HOME:-$HOME/.local/share}
autostart="$config_home/autostart"
entry="$autostart/NoClickDock.desktop"   # the name the app's own launch-at-login uses
menu="$data_home/applications/noclickdock.desktop"
icon="$data_home/icons/hicolor/128x128/apps/noclickdock.png"
legacy_ids="ncd-dock claude-status-checker codex-status-checker comfyui-status-checker disk-status-checker docker-status-checker power-monitor tailscale-status-checker"

from="" build=0 replace=0 restore=0 uninstall=0 status=0 start=1
while [ $# -gt 0 ]; do
  case "$1" in
    --from) from=$2; shift ;;
    --build) build=1 ;;
    --replace-legacy) replace=1 ;;
    --restore-legacy) restore=1 ;;
    --uninstall) uninstall=1 ;;
    --status) status=1 ;;
    --no-start) start=0 ;;
    -h|--help) sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "install: unknown option $1 (try --help)" >&2; exit 2 ;;
  esac
  shift
done

say() { echo "  $*"; }

# -- NoClickDock 1 --------------------------------------------------------
legacy_pids() {
  # Only the Python widgets NoClickDock 1's installer put in its prefix.
  pgrep -f -- "$legacy_prefix/" 2>/dev/null || true
}

stop_legacy() {
  pids=$(legacy_pids)
  if [ -n "$pids" ]; then
    kill $pids 2>/dev/null || true
    say "stopped NoClickDock 1 ($(echo $pids | wc -w) process(es))"
  fi
}

# A login entry is disabled the XDG way — Hidden=true — after a copy of it
# is kept beside it (.ncd1-backup). Nothing is deleted.
disable_legacy_entries() {
  n=0
  for id in $legacy_ids; do
    f="$autostart/$id.desktop"
    [ -f "$f" ] || continue
    grep -qx 'Hidden=true' "$f" && continue
    [ -f "$f.ncd1-backup" ] || cp -p "$f" "$f.ncd1-backup"
    cmp -s "$f" "$f.ncd1-backup" || { echo "install: $f differs from its backup; left alone" >&2; continue; }
    printf 'Hidden=true\n' >> "$f"
    n=$((n + 1))
  done
  [ $n -eq 0 ] || say "disabled $n NoClickDock 1 login entr(y/ies) (backups: *.ncd1-backup)"
}

restore_legacy_entries() {
  n=0
  for id in $legacy_ids; do
    f="$autostart/$id.desktop"
    [ -f "$f.ncd1-backup" ] || continue
    cp -p "$f.ncd1-backup" "$f"
    n=$((n + 1))
  done
  say "restored $n NoClickDock 1 login entr(y/ies); they start at the next login"
}

# -- NoClickDock 2 --------------------------------------------------------
running_pids() { pgrep -f -- "^$prefix/ncd( |$)" 2>/dev/null || true; }

stop_dock() {
  pids=$(running_pids)
  [ -z "$pids" ] || { kill $pids 2>/dev/null || true; sleep 1; }
}

find_binaries() {
  if [ -n "$from" ]; then src=$from
  elif [ $build -eq 1 ]; then
    (cd "$root/app" && pnpm install --frozen-lockfile && pnpm tauri build --no-bundle) >&2
    src="$root/app/src-tauri/target/release"
  else src="$root/app/src-tauri/target/release"
  fi
  if [ ! -x "$src/ncd" ] || [ ! -x "$src/ncd-core" ]; then
    echo "install: no ncd and ncd-core in $src — run with --build, or --from a folder that has them" >&2
    exit 1
  fi
  echo "$src"
}

check_libraries() {
  missing=$(ldd "$prefix/ncd" 2>/dev/null | awk '/not found/ {print $1}')
  if [ -n "$missing" ]; then
    echo "install: missing libraries: $missing" >&2
    echo "  Debian/Ubuntu: sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0" >&2
    echo "  Fedora: sudo dnf install webkit2gtk4.1 gtk3 · Arch: sudo pacman -S webkit2gtk-4.1 gtk3" >&2
    exit 1
  fi
  ldconfig -p 2>/dev/null | grep -q 'libayatana-appindicator3\|libappindicator3' ||
    say "no AppIndicator library: no tray icon (the rail's menu has the same items)"
}

install_dock() {
  src=$(find_binaries)
  stop_dock
  mkdir -p "$prefix" "$autostart" "$(dirname "$menu")" "$(dirname "$icon")"
  for b in ncd ncd-core; do
    cp "$src/$b" "$prefix/$b.new"
    chmod 755 "$prefix/$b.new"
    mv -f "$prefix/$b.new" "$prefix/$b"   # a rename in one directory: never half a binary
  done
  say "installed into $prefix"
  check_libraries
  cp "$root/app/src-tauri/icons/128x128.png" "$icon"
  cat > "$menu" <<EOF
[Desktop Entry]
Type=Application
Name=NoClickDock
Comment=Every status you need, with no click
Exec=$prefix/ncd
Icon=noclickdock
Categories=Utility;Monitor;
StartupNotify=false
Actions=settings;

[Desktop Action settings]
Name=Settings
Exec=$prefix/ncd --settings
EOF
  # The first install brings NoClickDock 1's settings over — before its
  # login entries are disabled, since they say which widgets were in use.
  "$prefix/ncd-core" --migrate | sed 's/^/  /'
  if [ $replace -eq 1 ]; then
    stop_legacy
    disable_legacy_entries
  fi
  cat > "$entry" <<EOF
[Desktop Entry]
Type=Application
Version=1.0
Name=NoClickDock
Comment=NoClickDock at login
Exec=$prefix/ncd
StartupNotify=false
Terminal=false
EOF
  say "starts at login (Settings → Start changes it)"
  if [ $start -eq 1 ]; then
    if [ -z "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]; then
      say "no desktop session here: start it from the menu or at the next login"
    else
      (setsid "$prefix/ncd" >/dev/null 2>&1 &)
      say "started"
    fi
  fi
}

uninstall_dock() {
  stop_dock
  rm -f "$prefix/ncd" "$prefix/ncd-core" "$menu" "$icon" "$entry"
  rmdir "$prefix" 2>/dev/null || true
  say "removed NoClickDock 2; settings kept in $config_home/noclickdock"
  ls "$autostart"/*.ncd1-backup >/dev/null 2>&1 &&
    say "NoClickDock 1's login entries are disabled: --restore-legacy gives them back"
  return 0
}

show_status() {
  if [ -x "$prefix/ncd" ]; then i="installed ($prefix)"; else i="not installed"; fi
  if [ -f "$entry" ]; then l=yes; else l=no; fi
  if [ -n "$(running_pids)" ]; then r=yes; else r=no; fi
  n=$(echo $(legacy_pids) | wc -w)
  d=0; for id in $legacy_ids; do [ -f "$autostart/$id.desktop.ncd1-backup" ] && d=$((d + 1)); done
  printf '  %-24s %s\n' "NoClickDock 2" "$i" "starts at login" "$l" "running" "$r" \
    "NoClickDock 1 running" "$n process(es)" "NoClickDock 1 disabled" "$d login entr(y/ies)"
}

echo "NoClickDock 2"
if [ $status -eq 1 ]; then show_status; exit 0; fi
if [ $restore -eq 1 ]; then restore_legacy_entries; show_status; exit 0; fi
if [ $uninstall -eq 1 ]; then uninstall_dock; show_status; exit 0; fi
install_dock
show_status
