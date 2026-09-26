// Package claude watches Anthropic's status page, the plan's limits and the
// local token spend. Until its collector is ported (M4) it only has a mock.
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

func (Widget) Mock(time.Time) model.Snapshot {
	ok := model.ToneOK
	return model.Snapshot{
		State:      model.StateOK,
		StateLabel: "operational",
		Glance:     model.Glance{Text: "62%"}, // the highest limit
		Width:      380,
		Pages: []model.Page{{ID: "main", Blocks: model.Blocks{
			model.Section{Title: "Plan limits", Meta: "max"},
			model.Meter{Label: "Session", Frac: 0.54, Value: "54%", Note: "resets 14:00", Tone: ok},
			model.Meter{Label: "Week · all models", Frac: 0.31, Value: "31%", Note: "resets Thu 09:00", Tone: ok},
			model.Meter{Label: "Week · Opus", Frac: 0.62, Value: "62%", Note: "resets Thu 09:00", Tone: ok},
			model.Section{Title: "Tokens"},
			model.Stats{Items: []model.Stat{{Key: "1h", Value: "212K"}, {Key: "24h", Value: "3.4M"}, {Key: "7d", Value: "18.9M"}}},
			model.Section{Title: "Services", Meta: "all operational"},
			model.Row{Name: "claude.ai", Value: "operational", LED: ok},
			model.Row{Name: "Claude API", Value: "operational", LED: ok},
			model.Row{Name: "Claude Code", Value: "operational", LED: ok},
		}}},
		Menu: []model.MenuItem{{ID: "open-status", Label: "Open status page"}},
	}
}
