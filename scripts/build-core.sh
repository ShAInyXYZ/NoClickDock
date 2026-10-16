#!/bin/sh
# Build ncd-core for the Tauri shell to bundle as its sidecar.
#
#   scripts/build-core.sh                 # the target Tauri is building for, else this machine's
#   scripts/build-core.sh <target-triple> # e.g. x86_64-pc-windows-msvc
#
# Tauri looks for app/src-tauri/binaries/ncd-core-<target-triple>[.exe] and
# copies it next to the app as plain ncd-core[.exe]. As the before-build
# hook, `cargo tauri build --target <triple>` exports TAURI_ENV_TARGET_TRIPLE,
# so every target gets a core built for it — never a stale one left in
# binaries/ from an earlier build.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
triple=${1:-${TAURI_ENV_TARGET_TRIPLE:-$(rustc --print host-tuple)}}
out="$root/app/src-tauri/binaries"
mkdir -p "$out"

case "$triple" in
  x86_64-unknown-linux-gnu)  goos=linux   goarch=amd64 ext="" ldflags="-s -w" ;;
  aarch64-unknown-linux-gnu) goos=linux   goarch=arm64 ext="" ldflags="-s -w" ;;
  # No console window: the core is a GUI-subsystem process that still reads
  # and writes the pipes Tauri gives it.
  x86_64-pc-windows-*)       goos=windows goarch=amd64 ext=".exe" ldflags="-s -w -H=windowsgui" ;;
  aarch64-pc-windows-*)      goos=windows goarch=arm64 ext=".exe" ldflags="-s -w -H=windowsgui" ;;
  *) echo "build-core: unsupported target $triple" >&2; exit 1 ;;
esac

cd "$root/core"
CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch \
  go build -trimpath -buildvcs=false -ldflags "$ldflags" \
  -o "$out/ncd-core-$triple$ext" ./cmd/ncd-core
echo "build-core: $out/ncd-core-$triple$ext"
