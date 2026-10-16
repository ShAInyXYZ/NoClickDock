#!/bin/sh
# Cross-builds NoClickDock 2 for Windows from Linux, into dist/windows:
# ncd.exe, ncd-core.exe and install.ps1 — copy that folder to Windows and
# run install.ps1. Recipe: docs/rewrite/research/tauri-windows.md
# ("Cross-building"). One-time setup:
#
#   rustup target add x86_64-pc-windows-msvc
#   cargo install cargo-xwin --locked      # downloads the MSVC CRT + SDK on first use
#   an llvm-rc on PATH (LLVM 14's is found by itself)
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
target=x86_64-pc-windows-msvc

command -v cargo-xwin >/dev/null || { echo "build-windows: cargo install cargo-xwin --locked" >&2; exit 1; }
rustup target list --installed | grep -qx "$target" || { echo "build-windows: rustup target add $target" >&2; exit 1; }
# tauri-winres compiles the icon and manifest with llvm-rc.
if ! command -v llvm-rc >/dev/null; then
  for dir in /usr/lib/llvm-*/bin; do
    if [ -x "$dir/llvm-rc" ]; then PATH="$dir:$PATH"; break; fi
  done
fi
command -v llvm-rc >/dev/null || { echo "build-windows: llvm-rc not found (install LLVM)" >&2; exit 1; }

cd "$root/app"
pnpm install --frozen-lockfile
# The before-build hook builds ncd-core.exe for this target and the pages.
pnpm tauri build --runner cargo-xwin --target "$target" --no-bundle

out="$root/dist/windows"
mkdir -p "$out"
cp "$root/app/src-tauri/target/$target/release/ncd.exe" "$root/app/src-tauri/target/$target/release/ncd-core.exe" "$root/scripts/install.ps1" "$out/"
echo "build-windows: $out"
ls -l "$out"
