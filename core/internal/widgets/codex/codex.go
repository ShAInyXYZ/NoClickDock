// Package codex watches OpenAI's status page, the plan's rate-limit windows
// and the local token spend, both read from the session rollouts Codex
// writes under ~/.codex — purely local: no API key, no account call.
// Parity: docs/rewrite/specs/codex.md.
package codex

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/statuspage"
)

var meta = hub.Meta{ID: "codex", Title: "Codex", Order: 20, PollSecs: 30}

const (
	spendEvery = 2 * time.Minute
	forceAfter = 10 * time.Second
)

type Widget struct {
	src     statuspage.Source
	scanner *scanner

	mu        sync.Mutex
	refresh   func()
	spend     *spend // kept across polls and refreshes: never blanked
	spendAt   time.Time
	spendBusy bool
	issues    []model.Issue // the last page that answered

	// The last two snapshots' states were raised by a limit (A20).
	raised, wasRaised bool
}

func New() *Widget {
	return &Widget{
		src: statuspage.Source{
			Base: statusPage, UserAgent: "noclickdock/2 (+status)",
			// incident.io's mirror has no unresolved.json: the full list,
			// filtered to the open ones.
			Incidents: "/api/v2/incidents.json",
			Client:    &http.Client{Timeout: 30 * time.Second},
		},
		scanner: newScanner(rolloutDirs()...),
	}
}

func (*Widget) Meta() hub.Meta { return meta }

func (w *Widget) SetRefresh(f func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.refresh = f
}

func (w *Widget) Force() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if time.Since(w.spendAt) > forceAfter {
		w.spendAt = time.Time{}
	}
}

// Transition words the page's changes; a state raised by a limit (A20)
// says the limit, and says nothing when the limit falls back — the page
// was fine all along.
func (w *Widget) Transition(_, to model.State, s model.Snapshot) string {
	w.mu.Lock()
	raised, was := w.raised, w.wasRaised
	w.mu.Unlock()
	if !raised && was && to == model.StateOK {
		return ""
	}
	return statuspage.Transition(to, s.StateLabel)
}

func (w *Widget) Collect(ctx context.Context) (model.Snapshot, error) {
	page := w.src.Fetch(ctx)
	w.mu.Lock()
	w.dueSpend()
	p := poll{page: page, spend: w.spend, now: time.Now()}
	if page.Reached {
		w.issues = statuspage.Issues(page, short)
	}
	issues := w.issues
	w.mu.Unlock()
	s := render(p)
	s.Issues = issues
	raised := model.RaiseToWorst(&s)
	w.mu.Lock()
	w.wasRaised, w.raised = w.raised, raised
	w.mu.Unlock()
	return s, nil
}

// dueSpend rescans the rollouts every two minutes in the background, only
// the changed files parsed. Called with mu held.
func (w *Widget) dueSpend() {
	if w.spendBusy || (!w.spendAt.IsZero() && time.Since(w.spendAt) < spendEvery) {
		return
	}
	w.spendBusy, w.spendAt = true, time.Now()
	go func() {
		sp := w.scanner.scan(time.Now()) // only this goroutine touches the scanner
		w.mu.Lock()
		w.spend, w.spendBusy = &sp, false
		refresh := w.refresh
		w.mu.Unlock()
		if refresh != nil {
			refresh()
		}
	}()
}
