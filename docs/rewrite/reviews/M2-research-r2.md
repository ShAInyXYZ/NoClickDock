# M2 — research review, round 2

Reviewed: `13f1995`, `3c69860`, `42fe857`, `82c8f12` (after
[M2-research-r1.md](M2-research-r1.md)). All tests ran on a clean
`git archive 82c8f12` tree, on a private Xvfb `:98` — with `picom --config
/dev/null --backend xrender` where a compositor matters — with
`--instance research-m2` and `DBUS_SESSION_BUS_ADDRESS` pointing at a
nonexistent bus, so no tray reached the real session. For comparisons, the
single-panel build `fde7211` ran under the same conditions.

## r1 items

| r1 | item | verified |
|---|---|---|
| MUST 1 | no AppIndicator library → abort | **Fixed.** Same repro (library hidden by a user-namespace bind mount): the shell keeps running and logs `ncd: no tray: neither libayatana-appindicator3 nor libappindicator3 is installed`. |
| S1 | 1×1 input pixel on parked windows | **Fixed** (`overlay::pass_through`, an empty GDK input shape). At a parked panel's corner pixel the pointer now reaches the desktop; the `fde7211` build's parked panel and toast still took it there. |
| S2 | toast starvation / backlog | Done (`toast.rs`): the newest 3 waiting per widget, 30 s staleness, and a stack that receives no new items while another widget waits. Checked by reading the code; the rotation reasoning holds for the orderings I traced. |
| S3 | `placing` counter drift | Done: a 400 ms deadline replaces the counter. |
| S4 | `_GTK_WORKAREAS` not notified | Done: x11rb watcher on its own connection and thread, hands off to the main thread, 150 ms coalescing (a port of HubVoice's). Not runtime-tested: Xvfb has no window manager whose work areas GDK would trust. |
| S5 | Windows display/work-area observer | Still `ScaleFactorChanged` only; stays an M7 item. |
| S6 | URL prefix test | Done: `url` crate, http/https only, host required, no userinfo, no control characters or spaces; unit test covers `file:`, `javascript:`, userinfo, whitespace, `\n`, BEL, `ftp:`, `smb:`, empty. |
| S7 | toasts while the dock is hidden | Done: they wait (`toast::set_dock_hidden`). |
| S8 | mock details | Done: GPU0 core `71°C` toned, display estimates 48/43 W, no `resets —`. |
| — | `XInitThreads` | Called first in `run()` via `libloading("libX11.so.6")`, before tao opens a display. Release builds log the first off-main-thread call once. |

Regression checks at `82c8f12`: `gofmt` clean; `go vet` linux + windows;
`go test -race ./...` (9 packages) ok; `svelte-check` 0/0; `cargo test --lib`
8 passed; `cargo check --target x86_64-pc-windows-msvc` ok after a forced
recheck.

## The two-panel swap (`82c8f12`)

What I checked, under picom:

| scenario | result |
|---|---|
| human-speed switching, 20 dial changes 400 ms apart, share of the showing panel still unpainted | 0 of 20 |
| a rapid sweep (25 dial changes, 20–50 ms each) before every rest, 12 rests | every resting panel fully painted |
| snapshot storm: a stand-in core re-emits Docker every 50 ms, its height toggling | 1 of 20 switches had 7 % unpainted, landing on Docker while the *visible* window itself was resizing — the pre-existing resize case, not the swap |
| core killed with SIGKILL mid-run | respawned once, all later switches painted, no errors |
| a tab switch on the incoming panel | not possible: it is parked with an empty input shape until shown |
| events | `panel-open` / `panel-close` / `panel-reveal` are `emit_to` one window, and each page listens with `target: own label`; `core` / `core-link` stay broadcast (both pages need the state) |
| memory (idle PSS, whole tree) | 279 MB with four webviews vs 235 MB with three (+44 MB) |

The ordering logic is sound:
- the target is always the window that is not the front, so the window
  fading out is never the one drawn in;
- `seq` guards stale layout and paint reports on both sides;
- the page never reports a paint for a superseded `seq`;
- `sized` (200 ms) and `painted` (100 ms) have fallbacks, so a page that
  runs no frames cannot wedge the swap;
- the fade timer only parks a window that is not the front.

## MUST-FIX

1. **A widget's panel state lives in whichever window last drew it, so it
   comes and goes with the swap parity.** `app/src/routes/panel/+page.svelte`
   keeps the selected page per widget in a page-local `pages` record, and
   `app/src/lib/actions.svelte.ts` keeps armed, running and failed per page.
   Each panel window is its own page, and the shell draws the next widget in
   whichever window is not showing (`hover.rs` `open`: `st.target = 1 -
   st.front`).
   - Reproduced under picom: select Docker's **Stopped** tab. Reopen Docker
     after one other dial: it opens on Stopped (same window). Reopen it after
     two other dials: it opens on **Now** (the other window).
   - This regresses dock-core §4 ("the selected page per widget persists
     across polls and across close/open"), which the single-window build met.
   - The same split breaks M2-a. A failed action is shown inline only in the
     window that ran it, while the shell drops that widget's toast while its
     panel is open. After an even number of intermediate switches within
     10 s, the failure is shown nowhere.
   - A still-running action's "busy" state is also missing in the other
     window, so its button can be pressed again (the hub does not reject a
     duplicate in flight).

   Correct: per-widget panel state follows the widget, not the window.
   - The shell keeps each widget's selected page (a `panel_page(widget, page)`
     command) and sends it in `panel-open`.
   - Failures and in-flight actions are known to both pages: the core's
     `result` carries `widget`, `action` and `args` (it has the request), and
     the page that sends an action broadcasts `running` through the shell, or
     the shell tracks it.
   - Arming may stay per window: losing an arm is the safe direction.
   - Add a test that draws a widget, switches twice, and expects the same page.

## SHOULD-FIX

1. **Without a compositor, the outgoing panel covers the incoming one.**
   Parking relies on client-side alpha (GTK paints an RGBA toplevel at
   opacity 0 itself). With no compositor, those pixels are opaque black, and
   the parked window stays mapped and stacked above.
   - On Xvfb with no compositor: 10 of 20 switches showed part of the new
     panel covered, and the covered share matched the old window's overlap
     exactly (e.g. 69 %). The single-panel build showed 0 of 20.
   - Parked windows also leave black rectangles over the desktop there.
   - ARCHITECTURE makes a compositor a requirement, so this is outside the
     target. The cheap guard: when `!gdk::Screen::is_composited()` (and on
     `composited-changed`), hide instead of park.
2. **Windows: the swap assumes the hidden window renders.** `overlay::prime`
   is Linux-only, and WebView2 typically pauses rendering for a hidden
   controller. The incoming panel's `sized`/`painted` then fall back to their
   timeouts (up to ~300 ms per switch) and may show an unpainted page. Verify
   in M7. If confirmed, keep the incoming window shown off-screen (or at
   near-zero alpha via `WS_EX_LAYERED`) rather than hidden.
3. **Snapshot mid-swap** (minor): a snapshot that changes the incoming
   panel's height between `panel_layout` and `panel_painted` can let the
   earlier paint report (same `seq`) reveal the window at the new size before
   that size has painted. Not observed beyond the storm case above. Counting
   the size in the paint report (`panel_painted(seq, w, h)`, shown only when
   it matches the last `panel_layout`) would close it.

## Process note

Every run in this round used `:98`, the unique instance name, and a
nonexistent session bus. Only my own PIDs were stopped, and the scratch
configs these runs created were removed. A `picom` running with
`--config /dev/null` belongs to another agent and was left alone.

VERDICT: FAIL
