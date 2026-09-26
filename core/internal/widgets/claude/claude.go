// Package claude watches Anthropic's status page, the plan's limits and the
// local token spend. Parity: docs/rewrite/specs/claude.md. Until its
// collector is ported (M4) it only has a mock.
package claude

import (
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

type Widget struct{}

func (Widget) Meta() hub.Meta {
	return hub.Meta{ID: "claude", Title: "Claude", Order: 10, PollSecs: 30}
}

// Mock is an invented afternoon: the weekly limit past the warning line,
// one component degraded and its incident being monitored. The incident text
// is made up.
func (Widget) Mock(time.Time) model.Snapshot {
	ok, warn := model.ToneOK, model.ToneWarn
	return model.Snapshot{
		State:      model.StateWarn,
		StateLabel: "Minor Service Outage",
		Glance:     model.Glance{Text: "78%", Tone: warn}, // the highest limit
		Width:      440,
		Pages: []model.Page{{ID: "main", Blocks: model.Blocks{
			model.Section{Title: "Usage"},
			model.Limit{Label: "Session", Pct: 46, Resets: "resets 18:30"},
			model.Limit{Label: "Week", Pct: 78, Resets: "resets Mon 09:00", Tone: warn},
			model.Limit{Label: "Week · opus", Pct: 23, Resets: "resets Mon 09:00"},
			model.Stats{Items: []model.Stat{
				{Key: "1h", Value: "348K"}, {Key: "24h", Value: "1.2M"}, {Key: "7d", Value: "9.8M"}, {Key: "sessions", Value: "6"},
			}},
			model.Chips{Columns: 3, Items: []model.Chip{
				{Label: "opus 5", Value: "1.1M"}, {Label: "sonnet 5", Value: "96K"}, {Label: "haiku 4.5", Value: "12K"},
			}},
			model.Section{Title: "Degraded", Meta: "1", Tone: warn},
			model.Row{Name: "console", Value: "degraded performance", Tone: warn, LED: warn},
			model.Section{Title: "Services", Meta: "5 operational"},
			model.Chips{Columns: 3, Items: []model.Chip{
				{Label: "claude.ai", Tone: ok}, {Label: "API", Tone: ok}, {Label: "claude code", Tone: ok},
				{Label: "cowork", Tone: ok}, {Label: "gov", Tone: ok},
			}},
			model.Section{Title: "Active incidents", Meta: "1", Tone: warn},
			model.Alert{
				Title: "Elevated errors on Console", Meta: "monitoring · 12m", Tone: warn,
				Body: "A fix has been implemented and we are monitoring the results.",
			},
		}}},
		Footer: "limits as of 4m ago",
		Menu:   []model.MenuItem{{ID: "open-status", Label: "Open status page", URL: "https://status.claude.com"}},
		Issues: []model.Issue{
			{ID: "comp:console:degraded_performance", Text: "Console has degraded performance", Tone: warn},
			{ID: "inc:mock-1", Text: "Elevated errors on Console", Tone: warn},
		},
	}
}

// Variants: a machine where Claude Code has not run this week.
func (w Widget) Variants(now time.Time) map[string]model.Snapshot {
	quiet := w.Mock(now)
	quiet.State, quiet.StateLabel = model.StateOK, "All Systems Operational"
	quiet.Glance, quiet.Footer, quiet.Issues = model.Glance{}, "", nil
	quiet.Pages = []model.Page{{ID: "main", Blocks: model.Blocks{
		model.Section{Title: "Usage"},
		model.Empty{Text: "No Claude Code sessions on this machine in the last 7 days."},
		model.Section{Title: "Services", Meta: "6 operational"},
		model.Chips{Columns: 3, Items: []model.Chip{
			{Label: "claude.ai", Tone: model.ToneOK}, {Label: "API", Tone: model.ToneOK}, {Label: "claude code", Tone: model.ToneOK},
			{Label: "console", Tone: model.ToneOK}, {Label: "cowork", Tone: model.ToneOK}, {Label: "gov", Tone: model.ToneOK},
		}},
	}}}
	return map[string]model.Snapshot{"empty": quiet}
}
