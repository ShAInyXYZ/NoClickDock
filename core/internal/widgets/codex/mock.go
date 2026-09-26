package codex

import (
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/statuspage"
)

// Mock invents two allowances, one of which never moves, and one Codex
// surface in partial outage, drawn by the real render. The incident text
// is made up.
type Mock struct{}

func (Mock) Meta() hub.Meta { return meta }

func (Mock) Mock(now time.Time) model.Snapshot {
	page := statuspage.Page{Reached: true, Indicator: "minor", Description: "Partially Degraded Service"}
	for _, name := range codexComponents {
		status := "operational"
		if name == "VS Code extension" {
			status = "partial_outage"
		}
		page.Components = append(page.Components, statuspage.Component{Name: name, Status: status})
	}
	page.Incidents = []statuspage.Incident{{
		ID: "mock-2", Name: "Increased latency in the VS Code extension", Status: "identified", Impact: "minor",
		Latest: statuspage.Update{Body: "We have identified the cause of slow responses for some users and are rolling out a fix.", CreatedAt: now.Add(-25 * time.Minute)},
	}}
	in := func(d time.Duration) time.Time { return now.Add(d).Truncate(time.Minute) }
	s := render(poll{page: page, now: now, spend: &spend{
		H1: 96_000, H24: 2_300_000, D7: 11_200_000, Sessions: 4, Available: true, Plan: "plus", Seen: now.Add(-12 * time.Minute),
		Models: []modelSpend{{"gpt-5-codex", 2_100_000}, {"gpt-5", 180_000}},
		Groups: []group{
			{ID: "codex", Seen: now.Add(-12 * time.Minute), Rows: []row{{Minutes: 300, Pct: 47, Resets: in(4 * time.Hour)}, {Minutes: 10080, Pct: 31, Resets: in(74 * time.Hour)}}},
			{ID: "codex-mini", Seen: now.Add(-72 * time.Hour), Idle: true, Rows: []row{{Minutes: 300}, {Minutes: 10080}}},
		},
	}})
	s.Issues = statuspage.Issues(page, short)
	return s
}
