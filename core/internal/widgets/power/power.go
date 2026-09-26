// Package power measures what the machine draws and what it costs. Until its
// collector is ported (M5) it only has a mock.
package power

import (
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

type Widget struct{}

func (Widget) Meta() hub.Meta {
	return hub.Meta{ID: "power", Title: "Power", Order: 60, PollSecs: 2}
}

func (Widget) Mock(time.Time) model.Snapshot {
	ok := model.ToneOK
	return model.Snapshot{
		State:      model.StateOK,
		StateLabel: "412 W",
		Glance:     model.Glance{Text: "412W"},
		Width:      400,
		Pages: []model.Page{{ID: "main", Blocks: model.Blocks{
			model.Section{Title: "Draw", Meta: "412 W"},
			model.Row{Name: "CPU package", Sub: "RAPL", Value: "86 W", Mono: false},
			model.Meter{Label: "GPU 0", Frac: 318.0 / 450, Value: "318/450W", Note: "core 71°C · vram 84°C", Tone: ok},
			model.Row{Name: "Drives", Sub: "3 NVMe, 1 SATA · estimate", Value: "11 W"},
			model.Row{Name: "Board, fans, RAM", Sub: "estimate", Value: "38 W"},
			model.Row{Name: "Displays", Sub: "2 · estimate", Value: "52 W"},
			model.Section{Title: "Today"},
			model.Stats{Items: []model.Stat{{Key: "kWh", Value: "4.82"}, {Key: "cost", Value: "€1.21"}, {Key: "month", Value: "€28.40"}}},
			model.Note{Text: "Peak tariff until 22:00, then €0.18/kWh"},
		}}},
		Menu: []model.MenuItem{{ID: "open-report", Label: "Open power report"}},
	}
}
