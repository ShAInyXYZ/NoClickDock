// Package codex watches OpenAI's status page, the plan's rate-limit windows
// and the local token spend. Parity: docs/rewrite/specs/codex.md. Until its
// collector is ported (M4) it only has a mock.
package codex

import (
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

type Widget struct{}

func (Widget) Meta() hub.Meta {
	return hub.Meta{ID: "codex", Title: "Codex", Order: 20, PollSecs: 30}
}

// Mock is two limit buckets, one of which never moves, and one Codex
// surface in partial outage. The incident text is made up.
func (Widget) Mock(time.Time) model.Snapshot {
	ok, warn, idle := model.ToneOK, model.ToneWarn, model.ToneIdle
	return model.Snapshot{
		State:      model.StateWarn,
		StateLabel: "Partially Degraded Service",
		Glance:     model.Glance{Text: "47%"},
		Width:      440,
		Pages: []model.Page{{ID: "main", Blocks: model.Blocks{
			model.Section{Title: "Usage", Meta: "plus plan"},
			model.Note{Text: "codex · 12m ago"},
			model.Limit{Label: "5h", Pct: 47, Resets: "resets 16:40"},
			model.Limit{Label: "week", Pct: 31, Resets: "resets Sep 29, 9:12am"},
			model.Note{Text: "codex-mini · 3d ago · never reports usage", Tone: idle},
			model.Limit{Label: "5h", Pct: 0},
			model.Limit{Label: "week", Pct: 0},
			model.Stats{Items: []model.Stat{
				{Key: "1h", Value: "96K"}, {Key: "24h", Value: "2.3M"}, {Key: "7d", Value: "11.2M"}, {Key: "sessions", Value: "4"},
			}},
			model.Chips{Columns: 2, Items: []model.Chip{{Label: "gpt-5-codex", Value: "2.1M"}, {Label: "gpt-5", Value: "180K"}}},
			model.Section{Title: "Degraded", Meta: "1", Tone: warn},
			model.Row{Name: "vs code", Value: "partial outage", Tone: warn, LED: warn},
			model.Section{Title: "Services", Meta: "8 of 9"},
			model.Chips{Columns: 3, Items: []model.Chip{
				{Label: "codex api", Tone: ok}, {Label: "codex web", Tone: ok}, {Label: "desktop app", Tone: ok},
				{Label: "cli", Tone: ok}, {Label: "responses", Tone: ok}, {Label: "completions", Tone: ok},
				{Label: "login", Tone: ok}, {Label: "conversations", Tone: ok},
			}},
			model.Section{Title: "Active incidents", Meta: "1", Tone: warn},
			model.Alert{
				Title: "Increased latency in the VS Code extension", Meta: "identified · 25m", Tone: warn,
				Body: "We have identified the cause of slow responses for some users and are rolling out a fix.",
			},
		}}},
		Footer: "limits as of 12m ago",
		Menu:   []model.MenuItem{{ID: "open-status", Label: "Open status page", URL: "https://status.openai.com"}},
		Issues: []model.Issue{
			{ID: "comp:VS Code extension:partial_outage", Text: "vs code: partial outage", Tone: warn},
			{ID: "inc:mock-2", Text: "Increased latency in the VS Code extension", Tone: warn},
		},
	}
}
