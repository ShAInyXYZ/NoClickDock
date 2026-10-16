# M3 — research review, round 2

Reviewed: `e625c2f` (after [M3-research-r1.md](M3-research-r1.md)), on a
clean `git archive e625c2f` tree.

- Regression checks: `gofmt` clean; `go vet` on linux and windows;
  `go test -race ./...` ok (9 packages).
- `--once tailscale` against this machine (read-only) now labels its health
  warning `1 health warning` (16 characters).
- The fixtures and widget sources contain none of this machine's tailnet
  names, hostnames or addresses. I checked mechanically against its live
  status, and no values were copied here.

## r1 items

| r1 | item | verified |
|---|---|---|
| MUST 1 | foreground serve/funnel invisible | **Fixed.** Details below. |
| S1 | health label 40 characters | **Fixed.** It reads `<n> health warning(s)`; the messages lead the panel and still go to toasts. |
| S2 | peers keyed by lower-cased `HostName` | **Rates fixed:** they are keyed by node key, and "seen online" too (`TestPeersWithOneHostnameStayApart`). **Pins regressed.** See MUST-FIX 1. |
| S3 | duplicate in-flight actions | **Done in the core.** The hub refuses a second identical action while one runs; the Docker widget refuses any action on a unit it is already acting on. See SHOULD-FIX 1 for how the interface shows a refusal. |

**MUST 1 in detail.** My r1 probe, re-run against the real code:
`{"Foreground":{"b3a1c9":{"TCP":{"443":{"HTTPS":true}},"Web":{…:443 → 127.0.0.1:8080},"AllowFunnel":{"node.example.ts.net:443":true}}}}`
now gives:
- `Funnel | :443 · public internet · session | crit`;
- `Serve | :443 → 127.0.0.1:8080 · session | warn`.

More shapes I probed (all with invented names):

| input | rows |
|---|---|
| foreground tailnet-only serve | `Serve :8443 → 127.0.0.1:5000 · session` (warn), no funnel |
| two foreground sessions, each a funnel | both ports under Funnel (crit) |
| background serve + foreground funnel | Funnel `:8443` (crit); Serve `:443, :8443` |
| funnel over a raw `TCPForward` | Funnel `:2222` (crit) + Serve `:2222 → 127.0.0.1:22` |
| a `Web` entry with no `/` handler | Serve `:443` |
| `AllowFunnel` all `false`, `{"Foreground":{}}`, `null` | nothing exposed (correct) |
| `AllowIngress` (older clients), `Services` (`svc:`), an `ETag` alone, an unknown top-level key | funnel, Services row, nothing exposed, `unknown` (the executor's fixtures and tests) |

## MUST-FIX

1. **A pinned peer whose hostname is not its MagicDNS label is no longer
   watched.**
   - `core/internal/widgets/tailscale/parse.go:143-146` names a peer by the
     first label of its `DNSName` when it has one.
   - `tailscale.go:116` matches pins only against that name:
     `p.Watched = pins[p.name]`.
   - The watch list's names are lower-cased `HostName`s. That covers
     entries migrated from the Python widget (which matched `HostName`),
     entries typed by hand, and the spec (`specs/tailscale.md` §Watch list,
     and line 35). For any host whose name is not DNS-safe, the two differ.
     Examples: spaces, as macOS and phone device names often have;
     apostrophes; underscores; dots. On this machine 1 of 6 peers is such a
     host.
   - Probe (invented names): a peer `HostName "Office PC"`, `DNSName
     "office-pc.example.ts.net."`, offline, with the legacy pin `"office
     pc"`. It renders as `office-pc`, `watched=false`, and the state is **ok
     "0 peers up"** instead of warn "office pc offline".
   - A peer the user asked to watch goes offline in silence. That is the
     one job of the pin.
   - Fix: a pin matches the MagicDNS label **or** the lower-cased
     `HostName` (the r1 suggestion's fallback). Keep the node key for rates
     and "seen online". Add the probe above as a test.

## SHOULD-FIX

1. **A refused duplicate shows as the original action failing.**
   - The hub answers the second press with a `result` that names the same
     widget, action and args (`hub.go`, `NewResult(req, false, "that is
     still running")`).
   - Both panel pages key running/failed on exactly that, so they clear
     `running` and show **failed · that is still running** while the first
     action is still going. The pill becomes pressable again.
   - The first action's later success does not clear that `failed`: only
     a new start does, so the pill says `failed` for up to 10 s after a
     success.
   - `act()` also raises a crit toast for the refusal.
   - The interface already blocks a same-key press in either window, so
     this is reached by a dial-menu item with the same action, or by a
     race.
   - Fix: send the refusal without widget, action and args (or with a
     `busy` flag the pages ignore), and give it no toast, or at most an
     `ok`-toned one.
2. **Unknown shapes inside a foreground session read as nothing exposed.**
   - The `unknown` check (`parse.go:258-271`) looks only at top-level keys.
     `{"Foreground":{"s1":{"Future":{…}}}}` renders "nothing exposed" with
     an `ok` lamp.
   - Apply the same rule to each `Foreground` entry (and to `Services`
     values if they are ever read).
3. **Nits:**
   - `· session` is added to every exposure row when any part is
     foreground. A background serve next to a foreground funnel reads
     `:443, :8443 · session`, but only `:8443` ends with the terminal.
   - Funnel ports are sorted as `host:port` strings (`:10000, :443`), while
     serve ports are sorted numerically.
   - `<host> offline` (`render.go:54`) is not cut to the 28-character label
     budget (dock-core §11). A MagicDNS label can be 63 characters.

VERDICT: FAIL
