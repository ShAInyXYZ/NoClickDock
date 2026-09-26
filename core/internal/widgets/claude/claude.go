// Package claude watches Anthropic's status page, the plan's limits (from
// the Claude Code CLI, the only authoritative percent-of-limit) and the
// local token spend (from Claude Code's transcripts). Parity:
// docs/rewrite/specs/claude.md.
//
// The status page is public; the CLI keeps its own login; the transcripts
// are local files. The core never reads, stores or shows a credential.
package claude

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/statuspage"
)

var meta = hub.Meta{ID: "claude", Title: "Claude", Order: 10, PollSecs: 30}

const (
	limitsEvery   = 5 * time.Minute
	limitsTimeout = 45 * time.Second
	usageEvery    = 2 * time.Minute
	// A "Refresh now" asks the slow sources again only if they are older
	// than this, so hammering it cannot spawn the CLI over and over.
	forceAfter = 10 * time.Second
)

type Widget struct {
	src     statuspage.Source
	scanner *scanner

	mu      sync.Mutex
	refresh func()

	// Kept across polls and across a "Refresh now": a refresh never blanks
	// the limits or the usage (the Python widget did).
	limits      []limit
	limitsAt    time.Time // when the rows shown were read
	limitsAsked time.Time // when the CLI was last asked
	limitsBusy  bool
	usage       *usage
	usageAt     time.Time
	usageBusy   bool

	// The last page that answered: an unreachable poll keeps its issues,
	// so recovery does not raise every open incident again, and service
	// recoveries are compared against it.
	comps  map[string]string
	issues []model.Issue
}

func New() *Widget {
	return &Widget{
		src: statuspage.Source{
			Base: statusPage, UserAgent: "noclickdock/2 (+status)",
			Incidents: "/api/v2/incidents/unresolved.json",
			Client:    &http.Client{Timeout: 30 * time.Second},
		},
		scanner: newScanner(projectsDir()),
	}
}

func (*Widget) Meta() hub.Meta { return meta }

func (w *Widget) SetRefresh(f func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.refresh = f
}

// Force makes the limits and the usage due again, unless just read.
func (w *Widget) Force() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if time.Since(w.limitsAsked) > forceAfter {
		w.limitsAsked = time.Time{}
	}
	if time.Since(w.usageAt) > forceAfter {
		w.usageAt = time.Time{}
	}
}

func (w *Widget) Transition(_, to model.State, s model.Snapshot) string {
	return statuspage.Transition(to, s.StateLabel)
}

func (w *Widget) Collect(ctx context.Context) (model.Snapshot, error) {
	page := w.src.Fetch(ctx)
	now := time.Now()

	w.mu.Lock()
	w.dueLimits()
	w.dueUsage()
	p := poll{page: page, limits: w.limits, limitsAt: w.limitsAt, usage: w.usage, now: now}
	var notices []model.Issue
	if page.Reached {
		comps := map[string]string{}
		for _, c := range page.Components {
			comps[c.Name] = c.Status
			// Back to operational: said once (dock-core §5.4, claude only).
			if was, ok := w.comps[c.Name]; ok && was != "operational" && c.Status == "operational" {
				notices = append(notices, model.Issue{Text: short(c.Name) + " operational", Tone: model.ToneOK})
			}
		}
		w.comps, w.issues = comps, statuspage.Issues(page, short)
	}
	issues := w.issues
	w.mu.Unlock()

	s := render(p)
	s.Issues, s.Notices = issues, notices
	return s, nil
}

// dueLimits starts the CLI when its reading is five minutes old. An empty
// answer keeps the rows it had. Called with mu held.
func (w *Widget) dueLimits() {
	if w.limitsBusy || (!w.limitsAsked.IsZero() && time.Since(w.limitsAsked) < limitsEvery) {
		return
	}
	w.limitsBusy, w.limitsAsked = true, time.Now()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), limitsTimeout+5*time.Second)
		defer cancel()
		rows := readLimits(ctx, time.Now())
		w.mu.Lock()
		w.limitsBusy = false
		if len(rows) > 0 { // nothing read keeps the rows it had
			w.limits, w.limitsAt = rows, time.Now()
		}
		refresh := w.refresh
		w.mu.Unlock()
		if refresh != nil && len(rows) > 0 {
			refresh()
		}
	}()
}

// dueUsage rescans the transcripts every two minutes, in the background:
// thousands of files, only the changed ones parsed. Called with mu held.
func (w *Widget) dueUsage() {
	if w.usageBusy || (!w.usageAt.IsZero() && time.Since(w.usageAt) < usageEvery) {
		return
	}
	w.usageBusy = true
	go func() {
		u := w.scanner.scan(time.Now()) // only this goroutine touches the scanner
		w.mu.Lock()
		w.usage, w.usageAt, w.usageBusy = &u, time.Now(), false
		refresh := w.refresh
		w.mu.Unlock()
		if refresh != nil {
			refresh()
		}
	}()
}
