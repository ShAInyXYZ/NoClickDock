// Package codex watches OpenAI's status page, the plan's rate-limit windows
// and the local token spend. Until its collector is ported (M4) it only has
// a mock.
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

func (Widget) Mock(time.Time) model.Snapshot {
	ok, warn := model.ToneOK, model.ToneWarn
	return model.Snapshot{
		State:      model.StateWarn,
		StateLabel: "5h window 86%",
		Glance:     model.Glance{Text: "86%", Tone: warn},
		Width:      380,
		Pages: []model.Page{{ID: "main", Blocks: model.Blocks{
			model.Section{Title: "Rate limits", Meta: "pro"},
			model.Meter{Label: "5h window", Frac: 0.86, Value: "86%", Note: "resets 16:40", Tone: warn},
			model.Meter{Label: "Weekly", Frac: 0.41, Value: "41%", Note: "resets Mon 02:10", Tone: ok},
			model.Section{Title: "Tokens"},
			model.Stats{Items: []model.Stat{{Key: "1h", Value: "96K"}, {Key: "24h", Value: "1.9M"}, {Key: "7d", Value: "11.2M"}}},
			model.Section{Title: "Services", Meta: "1 degraded", Tone: warn},
			model.Alert{
				Title: "Codex CLI", Meta: "degraded · since 11:02", Tone: warn,
				Body: "Elevated latency on some requests. We are investigating.",
			},
			model.Row{Name: "API", Value: "operational", LED: ok},
			model.Row{Name: "ChatGPT", Value: "operational", LED: ok},
		}}},
		Menu: []model.MenuItem{{ID: "open-status", Label: "Open status page"}},
	}
}
