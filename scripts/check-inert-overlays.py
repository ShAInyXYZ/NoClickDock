#!/usr/bin/env python3
"""check-inert-overlays.py — right after launch, no NoClickDock overlay but
the rail may take the pointer (design M2 r1b: never-shown panels parked at
0,0 swallowed clicks from launch until two panels had been opened).

Run on a PRIVATE X display only (Xvfb, a compositor such as picom on it),
never the user's desktop:

    DISPLAY=:97 scripts/check-inert-overlays.py app/src-tauri/target/debug/ncd --mock --instance check

It maps a Tk probe window at 0,0 (200×200) first, so the shell's windows
stack above it; starts the shell (with a dead session bus: no tray on the
real desktop); for a few seconds samples the pointer at the corner and
centre of every overlay window except the rail — the X server must route
it past them; then clicks at 50,50 and expects the probe to get it. Exit
status 0 = inert. The shell and the probe are stopped by pid.
"""
import ctypes, ctypes.util, os, subprocess, sys, time

if os.environ.get("DISPLAY") in (None, "", ":0", ":0.0"):
    sys.exit("refusing to run on the user's display: set DISPLAY to a private Xvfb")
X = ctypes.cdll.LoadLibrary(ctypes.util.find_library("X11"))
X.XOpenDisplay.restype = ctypes.c_void_p
d = X.XOpenDisplay(None)
X.XDefaultRootWindow.restype = ctypes.c_ulong
X.XDefaultRootWindow.argtypes = [ctypes.c_void_p]
root = X.XDefaultRootWindow(d)
P = ctypes.POINTER
X.XQueryPointer.argtypes = [ctypes.c_void_p, ctypes.c_ulong] + [P(ctypes.c_ulong)] * 2 + [P(ctypes.c_int)] * 4 + [P(ctypes.c_uint)]
X.XSync.argtypes = [ctypes.c_void_p, ctypes.c_int]
X.XWarpPointer.argtypes = [ctypes.c_void_p, ctypes.c_ulong, ctypes.c_ulong, ctypes.c_int, ctypes.c_int, ctypes.c_uint, ctypes.c_uint, ctypes.c_int, ctypes.c_int]

PROBE = r'''
import sys, tkinter as tk
r = tk.Tk(); r.title("inert-probe"); r.geometry("200x200+0+0")
r.bind("<ButtonPress>", lambda e: (print("PRESS", flush=True)))
r.after(300, lambda: print("READY", flush=True))
r.mainloop()
'''


def under(x, y):
    X.XWarpPointer(d, 0, root, 0, 0, 0, 0, x, y)
    X.XSync(d, 0)
    r, c = ctypes.c_ulong(), ctypes.c_ulong()
    i = [ctypes.c_int() for _ in range(4)]
    m = ctypes.c_uint()
    X.XQueryPointer(d, root, ctypes.byref(r), ctypes.byref(c), *[ctypes.byref(v) for v in i], ctypes.byref(m))
    return c.value


def windows(pid):
    out = []
    tree = subprocess.run(["xwininfo", "-root", "-tree"], capture_output=True, text=True).stdout
    for line in tree.splitlines():
        if '"NoClickDock ' not in line:
            continue
        wid, name = line.split()[0], line.split('"')[1]
        prop = subprocess.run(["xprop", "-id", wid, "_NET_WM_PID"], capture_output=True, text=True).stdout
        if prop.split()[-1:] != [str(pid)]:
            continue
        wh, _, xy = line.split()[-2].partition("+")
        w, h = map(int, wh.split("x"))
        x, y = map(int, xy.split("+"))
        out.append((int(wid, 16), name, x, y, w, h))
    return out


def main():
    probe = subprocess.Popen([sys.executable, "-c", PROBE], stdout=subprocess.PIPE, text=True)
    assert probe.stdout.readline().strip() == "READY"
    env = dict(os.environ, DBUS_SESSION_BUS_ADDRESS="unix:path=/nonexistent")
    shell = subprocess.Popen(sys.argv[1:], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    bad = seen = 0
    try:
        end = time.time() + 8
        while time.time() < end:
            for wid, name, x, y, w, h in windows(shell.pid):
                if name == "NoClickDock rail":
                    continue
                seen += 1
                for px, py in ((x, y), (x + w // 2, y + h // 2)):
                    if under(px, py) == wid:
                        print(f"TAKES THE POINTER: {name} at {px},{py} ({w}x{h}+{x}+{y})")
                        bad += 1
            time.sleep(0.25)
        # Click inside the probe, wherever a window manager put it.
        info = subprocess.run(["xwininfo", "-name", "inert-probe"], capture_output=True, text=True).stdout
        px = int(info.split("Absolute upper-left X:")[1].split()[0]) + 20
        py = int(info.split("Absolute upper-left Y:")[1].split()[0]) + 20
        under(px, py)
        subprocess.run(["xdotool", "click", "1"])
        time.sleep(0.5)
    finally:
        shell.terminate()
        probe.terminate()
    clicked = "PRESS" in (probe.communicate(timeout=5)[0] or "")
    shell.wait(timeout=10)
    print(f"{seen} window samples, {bad} on an inert window; the probe {'got' if clicked else 'did NOT get'} the click")
    sys.exit(0 if not bad and clicked and seen else 1)


if __name__ == "__main__":
    main()
