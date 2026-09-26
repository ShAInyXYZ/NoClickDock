#!/usr/bin/env python3
"""check-web-process-crash.py — a killed WebKit web process must not leave
a dock that looks alive and is dead (research M2 r4: the four views share
one web process; after a SIGKILL the rail kept its last frame and no hover
opened anything).

Run on a PRIVATE X display with a window manager and a compositor (Xvfb +
gnome-shell, or Xvfb + picom), never the user's desktop, against a shell
started with NCD_TRACE=1 whose stderr goes to LOG:

    DISPLAY=:97 scripts/check-web-process-crash.py <ncd pid> <LOG> <dial x> <dial y>

It kills (SIGKILL) the WebKitWebProcess children of that ncd — the shell
the caller started — then expects within 3 s: the shell's "interface
reloaded" line, a new web process, and the rail laying itself out again
(its page rendered); then a hover on the dial must open a panel ("show" in
the trace) within 3 s more. Exit status 0 = recovered.
"""
import os, signal, subprocess, sys, time

if os.environ.get("DISPLAY") in (None, "", ":0", ":0.0"):
    sys.exit("refusing to run on the user's display: set DISPLAY to a private Xvfb")
pid, log, x, y = int(sys.argv[1]), sys.argv[2], int(sys.argv[3]), int(sys.argv[4])


def children(parent):
    out = []
    for entry in os.listdir("/proc"):
        if not entry.isdigit():
            continue
        try:
            with open(f"/proc/{entry}/stat") as f:
                stat = f.read()
            with open(f"/proc/{entry}/comm") as f:
                comm = f.read().strip()
        except OSError:
            continue
        ppid = int(stat.rsplit(")", 1)[1].split()[1])
        if ppid == parent:
            out.append((int(entry), comm))
    return out


def web_processes():
    return [p for p, comm in children(pid) if comm.startswith("WebKitWebProces")]


def lines():
    with open(log, errors="ignore") as f:
        return f.read().splitlines()


def count(suffix):
    return sum(1 for line in lines() if line.rstrip().endswith(suffix))


def rail_layouts():
    return sum(1 for line in lines() if " rail_layout " in line)


def wait(what, ok, seconds):
    deadline = time.time() + seconds
    while time.time() < deadline:
        if ok():
            return True
        time.sleep(0.05)
    print(f"FAILED: {what} within {seconds} s")
    return False


def move(px, py):
    subprocess.run(["xdotool", "mousemove", str(px), str(py)])


old = web_processes()
if not old:
    sys.exit(f"no WebKitWebProcess under pid {pid}")
reloads, layouts, shows = count("interface reloaded"), rail_layouts(), count(" show")
move(x - 400, y)
time.sleep(0.5)
for p in old:
    os.kill(p, signal.SIGKILL)
print(f"killed web process {old}")
start = time.time()
ok = (
    wait("the shell reloading the interface", lambda: count("interface reloaded") > reloads, 3)
    and wait("a new web process", lambda: any(p not in old for p in web_processes()), 3)
    and wait("the rail rendering again", lambda: rail_layouts() > layouts, 3)
)
if ok:
    print(f"interface back in {time.time() - start:.2f} s (web process {web_processes()})")
    time.sleep(0.3)
    move(x, y)
    ok = wait("a panel opening on hover", lambda: count(" show") > shows, 3)
    move(x - 400, y)
print("recovered: the rail draws and a hover opens a panel" if ok else "NOT RECOVERED")
sys.exit(0 if ok else 1)
