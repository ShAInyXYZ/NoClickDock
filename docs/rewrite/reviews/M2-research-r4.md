# M2 — research review, round 4

Reviewed: `9a9b2e9` (after [M2-research-r3.md](M2-research-r3.md)), on a
clean `git archive 9a9b2e9` tree, built release as shipped. An unstripped
copy was also built for `gdb`.

Runs used private Xvfb displays (`:96`; `:98` carried the soak), the app on
a nonexistent session bus, `--instance research-m2`, and only my own PIDs.
The compositor was either picom (`--config /dev/null --backend xrender`) or
the bare Mutter 42 described in r3. Every Mutter capture had a desktop-type
window behind the dock; see the process note for why.

Regression checks at `9a9b2e9`:
- UI: `svelte-check`, 0 errors and 0 warnings.
- Shell: `cargo test --lib`, 9 passed; `cargo check --target
  x86_64-pc-windows-msvc` ok.
- The executor's `scripts/check-inert-overlays.py` passes on `:96`: 58
  samples, 0 on an inert window, and the probe got the click.

## r3 items

| r3 | item | verified |
|---|---|---|
| MUST 1 | never-shown panels swallow clicks at startup | **Fixed.** From launch, 28 clicks at 100,100 (every 300 ms, starting before the shell maps anything) all reached an `xev` window under the old default geometry, under picom and under Mutter. Every overlay sample (~65 per window over 9 s) had an empty input shape whenever it was mapped. Primed windows are now 1×1 at compositor opacity 0 before their first map. |
| MUST 2 | a right-click under a foreign keyboard grab freezes the dock | **Fixed.** Under Mutter, with gdb as parent, I ran dial, rail and dial right-clicks while another client held the keyboard. Each time: no menu, one log line (`the menu could not open (another application holds the keyboard)`), then dwell and hide both ran (+2 timer closures in every probe, and still 10 s later). SIGINT finds the main thread in tao's own `gtk_main_iteration_do`. A normal rail menu opens afterwards, and its Readouts item changes the config. |
| S1 | a core that dies mid-action leaves a pill busy | **Fixed.** Held result, then the whole sidecar SIGKILLed while `remove` ran. After the shell restarted the core, the pill was no longer dimmed, and one click armed it (`remove?`). `running` is cleared on a link drop and on `hello`. |
| S2 | Xvfb without a WM fakes hover bugs | Process note, carried: all pointer tests this round that click ran under Mutter. |

Also re-run:

| scenario | result |
|---|---|
| bare Xvfb, compositor lost after 8 switches, compositor arriving after 6 | 20/20 switches clean in each (no black, 0 % unpainted); nothing mapped after leaving. The loss handler now keys on the parked set, not GTK opacity. |
| 12 size-changing switches, Mutter on Xvfb (strip sampled every ~3 ms) | **0 of 12** with an empty stretch; 778b58a had 3 of 12. New panel first seen 43–97 ms after the `xdotool` call (median ~62), measured with the soak loading the machine. `xdotool`'s own spawn is included, so this is an upper bound. Design's new acceptance (median 50, worst 70 ms) is theirs to judge. |
| whole-tree PSS (shell + WebKit + core) after 7 switches | **179 MB** (one web process, 92 MB) vs **318 MB** at 778b58a (four, 48–68 MB each). |

## MUST-FIX

1. **A web-process crash leaves a dock that looks alive and is dead, with
   no recovery.** With related views, one crash takes all four views.
   - Repro: SIGKILL the single `WebKitWebProcess` of a running `ncd
     --mock`.
     - No new web process starts.
     - The rail keeps its last frame, pixel for pixel: coloured lamps and
       readouts, frozen.
     - Hovering Docker 14 s later opens nothing (no panel has an input
       shape).
     - The shell stays up, and neither watchdog heartbeat notices,
       because both run in the shell.
   - At `778b58a` (a process per view), killing the rail's process gives
     the same frozen rail. Killing a panel's process would lose only that
     panel. So the missing recovery is not new, but sharing widens it:
     any crash, in any view, now freezes everything.
   - Design calls a dock that silently stops updating the worst failure it
     can have. Web-process crashes are not hypothetical on this class of
     machine: the kernel OOM killer picks the largest process, and
     WebKitGTK is known to crash on some NVIDIA/DMA-BUF setups.
   - No code handles `web-process-terminated` (`overlay.rs` has none; wry
     does not handle it).
   - Fix direction:
     - Connect `web-process-terminated` on each WebKitWebView. Reload
       every view, since they share the process, re-prime the panels, and
       log the reason (crash or exceeded memory).
     - Bound the retries (e.g. back off after 3 in a minute) and say so
       on the rail if it gives up.
   - Acceptance: after `kill -9` of the web process, the rail redraws with
     live data within a few seconds, and a hover opens a panel.

## SHOULD-FIX

1. **The pressing window still reports a stale timeout 150 s after a core
   restart.**
   - `onReset` clears `running`, but `core.act`'s pending promise keeps
     its `RESULT_TIMEOUT_MS` timer (`core.svelte.ts`).
   - 150 s later that window marks the pill **failed · ncd-core did not
     answer**. That happens even if the action has since been run again
     and succeeded.
   - Settle or drop pending actions on reset, quietly.
2. **`x11::set_visible` can leave the server grabbed on an error path.**
   - `grab_server()?` is followed by `?` on each property request, which
     returns before `ungrab_server()`.
   - The errors are connection-level only, so this is unlikely. Still, a
     leaked server grab freezes every client on the display. Ungrab and
     flush in all paths.
   - Nobody reads that connection's events, so an asynchronous
     `BadWindow` (a destroyed overlay) sits unread. Harmless at this
     volume.

## Process note

- **Mutter-on-Xvfb captures need a window behind the dock.**
  - With no desktop window, XGetImage returns the windows' own contents and
    ignores `_NET_WM_WINDOW_OPACITY`: an `xclock` set to opacity 0 still
    shows in full, and parked panels appear drawn.
  - With a desktop-type window behind them, the same probes read correctly
    (the xclock vanishes; parked panels are invisible).
  - Picom reads correctly either way.
- The freeze soak (≥45 min, Mutter, gdb as parent, periodic right-clicks
  under a foreign keyboard grab, watching for `WATCHDOG` lines) is running
  on `9a9b2e9` and is reported separately.
- The scratch mock config my earlier runs toggled (`readouts`) was removed.
  It was mine: `mock-research-m2`.

VERDICT: FAIL
