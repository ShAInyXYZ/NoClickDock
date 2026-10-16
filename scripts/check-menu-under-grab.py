#!/usr/bin/env python3
"""check-menu-under-grab.py — a right-click on a dial while another client
holds the keyboard grab must not freeze the dock (research: muda's context
menu spun a nested main loop forever when GTK could not grab, starving
every timer, toast and message to the main thread).

Run on a PRIVATE X display with a window manager (Xvfb + gnome-shell or
another WM), never the user's desktop, against a shell started with
NCD_TRACE=1 whose stderr goes to LOG:

    DISPLAY=:97 scripts/check-menu-under-grab.py <ncd pid> <LOG> <dial x> <dial y> <other dial y>

It grabs the keyboard from a helper connection, right-clicks the dial,
releases the grab, then hovers another dial: the shell must still open a
panel there ("show" in the trace within 2 s). Exit status 0 = not frozen.
"""
import ctypes, ctypes.util, os, subprocess, sys, time

if os.environ.get("DISPLAY") in (None, "", ":0", ":0.0"):
    sys.exit("refusing to run on the user's display: set DISPLAY to a private Xvfb")
pid, log, x, y, y2 = int(sys.argv[1]), sys.argv[2], int(sys.argv[3]), int(sys.argv[4]), int(sys.argv[5])
X = ctypes.cdll.LoadLibrary(ctypes.util.find_library("X11"))
X.XOpenDisplay.restype = ctypes.c_void_p
X.XDefaultRootWindow.restype = ctypes.c_ulong
X.XDefaultRootWindow.argtypes = [ctypes.c_void_p]
X.XGrabKeyboard.argtypes = [ctypes.c_void_p, ctypes.c_ulong, ctypes.c_int, ctypes.c_int, ctypes.c_int, ctypes.c_ulong]
X.XUngrabKeyboard.argtypes = [ctypes.c_void_p, ctypes.c_ulong]
X.XFlush.argtypes = [ctypes.c_void_p]
d = X.XOpenDisplay(None)
root = X.XDefaultRootWindow(d)


def shows():
    return sum(1 for line in open(log, errors="ignore") if line.rstrip().endswith(" show"))


def move(px, py):
    subprocess.run(["xdotool", "mousemove", str(px), str(py)])


move(x - 400, y)
time.sleep(0.6)
grabbed = X.XGrabKeyboard(d, root, 0, 1, 1, 0)  # GrabModeAsync, CurrentTime
X.XFlush(d)
print("keyboard grab:", "held" if grabbed == 0 else f"failed ({grabbed})")
move(x, y)
time.sleep(0.05)
subprocess.run(["xdotool", "click", "3"])  # the menu cannot grab now
time.sleep(1.0)
X.XUngrabKeyboard(d, 0)
X.XFlush(d)
time.sleep(0.3)
before = shows()
move(x - 400, y2)
time.sleep(0.6)
move(x, y2)  # another dial: after the dwell a panel must open
deadline = time.time() + 2
while time.time() < deadline and shows() == before:
    time.sleep(0.05)
alive = shows() > before
move(x - 400, y2)
print("the dock still opens panels after the grabbed right-click" if alive else "FROZEN: no panel after the grabbed right-click")
sys.exit(0 if alive else 1)
