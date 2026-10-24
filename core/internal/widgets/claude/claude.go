// Package claude watches Anthropic's status page, plan limits from Claude
// Code's local quota cache, and token spend from its transcripts. Parity:
// docs/rewrite/specs/claude.md.
//
// The status page is public; quota observations and transcripts are local.
// The core never decodes, stores or shows a credential.
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
	limitsEvery = 30 * time.Second
	usageEvery  = 2 * time.Minute
	// A "Refresh now" asks the slow sources again only if they are older
	// than this, so hammering it cannot rescan files over and over.
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
	limitsAt    time.Time // when Claude Code observed the rows, not file read time
	limitsAsked time.Time // when the cache was last read
	limitsNote  string
	limitsBusy  bool
	usage       *usage
	usageAt     time.Time
	usageBusy   bool

	// The last page that answered: an unreachable poll keeps its issues,
	// so recovery does not raise every open incident again, and service
	// recoveries are compared against it.
	comps  map[string]string
	issues []model.Issue

	// The last two snapshots' states were raised by a limit (A20).
	raised, wasRaised bool
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
	now := time.Now()

	w.mu.Lock()
	w.dueLimits()
	w.dueUsage()
	p := poll{page: page, limits: w.limits, limitsAt: w.limitsAt, usage: w.usage, now: now, limitsNote: w.limitsNote, reading: w.limitsBusy && w.limitsAt.IsZero()}
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
	raised := model.RaiseToWorst(&s)
	w.mu.Lock()
	w.wasRaised, w.raised = w.raised, raised
	w.mu.Unlock()
	return s, nil
}

// dueLimits reads a bounded local file on the widget cadence. Transient
// read errors keep the previous observation until it expires. Called with mu held.
func (w *Widget) dueLimits() {
	if !w.limitsDue(time.Now()) {
		return
	}
	w.limitsBusy, w.limitsAsked = true, time.Now()
	go func() {
		reading := readLimits(limitsPath(), time.Now())
		w.mu.Lock()
		w.limitsBusy = false
		w.limitsNote = reading.note
		if !reading.keep {
			w.limits, w.limitsAt = reading.rows, reading.at
		}
		refresh := w.refresh
		w.mu.Unlock()
		if refresh != nil {
			refresh()
		}
	}()
}

// limitsDue: thirty seconds since the cache was read — or a window has reset
// since the rows were read, which is asked about again at once (no faster
// than a refresh may): until then it can only be said in words. Called
// with mu held.
func (w *Widget) limitsDue(now time.Time) bool {
	if w.limitsBusy {
		return false
	}
	if w.limitsAsked.IsZero() || now.Sub(w.limitsAsked) >= limitsEvery {
		return true
	}
	for _, l := range w.limits {
		if lapsed(l, now) && w.limitsAt.Before(l.Resets) && now.Sub(w.limitsAsked) > forceAfter {
			return true
		}
	}
	return false
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
