package claude

import (
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/statuspage"
)

// Mock invents an afternoon for --mock, drawn by the real render: the
// weekly limit past the warning line, one component degraded and its
// incident being monitored. The incident text is made up.
type Mock struct{}

func (Mock) Meta() hub.Meta { return meta }

func mockPage(broken bool) statuspage.Page {
	p := statuspage.Page{Reached: true, Indicator: "none", Description: "All Systems Operational"}
	console := "operational"
	if broken {
		p.Indicator, p.Description, console = "minor", "Minor Service Outage", "degraded_performance"
	}
	p.Components = []statuspage.Component{
		{Name: "claude.ai", Status: "operational"},
		{Name: "Claude Console (platform.claude.com)", Status: console},
		{Name: "Claude API (api.anthropic.com)", Status: "operational"},
		{Name: "Claude Code", Status: "operational"},
		{Name: "Claude Cowork", Status: "operational"},
		{Name: "Claude for Government", Status: "operational"},
	}
	return p
}

func (Mock) Mock(now time.Time) model.Snapshot {
	page := mockPage(true)
	page.Incidents = []statuspage.Incident{{
		ID: "mock-1", Name: "Elevated errors on Console", Status: "monitoring", Impact: "minor",
		Latest: statuspage.Update{Body: "A fix has been implemented and we are monitoring the results.", CreatedAt: now.Add(-12 * time.Minute)},
	}}
	today := time.Date(now.Year(), now.Month(), now.Day(), 18, 30, 0, 0, now.Location())
	monday := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, now.Location())
	for monday.Weekday() != time.Monday || !monday.After(now) {
		monday = monday.AddDate(0, 0, 1)
	}
	s := render(poll{
		page: page, now: now, limitsAt: now.Add(-4 * time.Minute),
		limits: []limit{
			{Label: "Session", Pct: 46, Resets: today},
			{Label: "Week", Pct: 78, Resets: monday},
			{Label: "Week · Fable", Pct: 23, Resets: monday},
		},
		usage: &usage{H1: 348_000, H24: 1_210_000, D7: 9_800_000, Sessions: 6, Available: true, Models: []spend{
			{"claude-opus-5", 1_100_000}, {"claude-sonnet-5", 96_000}, {"claude-haiku-4-5-20251001", 12_000},
		}},
	})
	s.Issues = statuspage.Issues(page, short)
	return s
}

// Variants: a machine where Claude Code has not run this week, and a
// status page that does not answer.
func (Mock) Variants(now time.Time) map[string]model.Snapshot {
	return map[string]model.Snapshot{
		"empty":       render(poll{page: mockPage(false), now: now, usage: &usage{}}),
		"unreachable": render(poll{page: statuspage.Page{Indicator: "unknown"}, now: now, usage: &usage{}}),
	}
}
