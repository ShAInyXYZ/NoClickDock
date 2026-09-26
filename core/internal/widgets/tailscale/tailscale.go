// Package tailscale watches tailscaled, this node and every peer. It is
// deliberately narrow: most peers of a long-lived tailnet are dormant, and
// alerting on them is noise. What matters is whether this node is up and
// logged in, whether the peers you depend on are reachable, what this
// machine exposes, and whether an exit node is routing your traffic.
// Parity: docs/rewrite/specs/tailscale.md.
package tailscale

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

var meta = hub.Meta{ID: "tailscale", Title: "Tailscale", Order: 70, PollSecs: 30}

const (
	statusTimeout = 15 * time.Second
	serveTimeout  = 5 * time.Second
)

type Widget struct {
	// watch is the config's list of pinned peers: nil watches every peer
	// seen online since the core started.
	watch func() *[]string

	mu    sync.Mutex
	seen  map[string]bool   // peers seen online this run (lower-cased)
	prev  map[string]sample // counters at the previous poll, per peer
	known []string          // every peer in the last status, as shown
}

type sample struct {
	at     time.Time
	rx, tx int64
}

func New(watch func() *[]string) *Widget {
	if watch == nil {
		watch = func() *[]string { return nil }
	}
	return &Widget{watch: watch, seen: map[string]bool{}, prev: map[string]sample{}}
}

func (*Widget) Meta() hub.Meta { return meta }

// binary is tailscale on PATH, or where the Windows installer puts it (not
// always on the PATH a GUI-launched process inherits).
func binary() string {
	var fallbacks []string
	if pf := os.Getenv("ProgramFiles"); runtime.GOOS == "windows" && pf != "" {
		fallbacks = append(fallbacks, filepath.Join(pf, "Tailscale", "tailscale.exe"))
	}
	return sys.Find("tailscale", fallbacks...)
}

func (w *Widget) Collect(ctx context.Context) (model.Snapshot, error) {
	p := &poll{windows: runtime.GOOS == "windows", now: time.Now()}
	bin := binary()
	if bin == "" {
		p.missing = true
		return render(p), nil
	}
	// A stopped or logged-out daemon still answers in JSON (and may exit
	// non-zero): what it says is the news. No JSON is "unreachable".
	out, _ := sys.Run(ctx, statusTimeout, bin, "status", "--json")
	st, err := parseStatus([]byte(out), p.now)
	if err != nil {
		return render(p), nil
	}
	p.available, p.st = true, st
	if out, err := sys.Run(ctx, serveTimeout, bin, "serve", "status", "--json"); err == nil {
		p.exposure, _ = parseServe(out)
	}
	p.pinned = w.observe(st, p.now)
	return render(p), nil
}

// observe marks the watched peers and their rates. Every peer seen online
// is remembered for the run, pinned or not, so going back to automatic
// watching loses nothing; a peer already dormant at start is never
// watched automatically.
func (w *Widget) observe(st *status, now time.Time) (pinned bool) {
	list := w.watch()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.known = w.known[:0]
	for _, p := range st.Peers {
		w.known = append(w.known, p.Host)
		if p.Online {
			w.seen[p.id] = true
		}
		if prev, ok := w.prev[p.id]; ok {
			if dt := now.Sub(prev.at).Seconds(); dt > 0 && p.rx >= prev.rx && p.tx >= prev.tx {
				// A counter that went backwards is a reset: no rate, never a
				// negative one.
				p.Rate, p.HasRate = float64(p.rx-prev.rx+p.tx-prev.tx)/dt, true
			}
		}
		w.prev[p.id] = sample{now, p.rx, p.tx}
	}
	var pins map[string]bool
	if list != nil {
		pins = map[string]bool{}
		for _, h := range *list {
			pins[strings.ToLower(strings.TrimSpace(h))] = true
		}
	}
	for _, p := range st.Peers {
		if pins != nil {
			// A pin may name the MagicDNS label or the HostName ("Office PC",
			// label "office-pc"): legacy lists and the spec use the latter.
			p.Watched = pins[p.name] || (p.hostname != "" && pins[p.hostname])
		} else {
			p.Watched = w.seen[p.id]
		}
	}
	return list != nil
}

// Ask "peers": every peer the tailnet listed at the last poll, for
// Settings' watch-list editor to choose from.
func (w *Widget) Ask(_ context.Context, what string, _ map[string]string) (string, []string, error) {
	if what != "peers" {
		return "", nil, fmt.Errorf("Tailscale cannot answer %q", what)
	}
	w.mu.Lock()
	peers := append([]string(nil), w.known...)
	w.mu.Unlock()
	sort.Slice(peers, func(i, j int) bool { return strings.ToLower(peers[i]) < strings.ToLower(peers[j]) })
	if len(peers) == 0 {
		return "No peers seen yet: Tailscale has not answered since NoClickDock started.", nil, nil
	}
	return fmt.Sprintf("%d %s on this tailnet", len(peers), map[bool]string{true: "peer", false: "peers"}[len(peers) == 1]), peers, nil
}
