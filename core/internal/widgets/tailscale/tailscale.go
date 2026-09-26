// Package tailscale watches tailscaled, this node and every peer. Until its
// collector is ported (M3) it only has a mock.
package tailscale

import (
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

type Widget struct{}

func (Widget) Meta() hub.Meta {
	return hub.Meta{ID: "tailscale", Title: "Tailscale", Order: 70, PollSecs: 30}
}

// Mock is an invented tailnet: every name and address here is made up.
func (Widget) Mock(time.Time) model.Snapshot {
	ok, idle := model.ToneOK, model.ToneIdle
	return model.Snapshot{
		State:      model.StateOK,
		StateLabel: "connected",
		Glance:     model.Glance{Text: "5/7"},
		Pages: []model.Page{{ID: "main", Blocks: model.Blocks{
			model.Section{Title: "This node", Meta: "1.80.2"},
			model.Row{Name: "workstation", Sub: "workstation.example-tailnet.ts.net", Value: "100.101.1.7", Mono: true},
			model.Section{Title: "Online", Meta: "5"},
			model.Row{Name: "atlas", Value: "direct · 2 ms", LED: ok, Mono: true},
			model.Row{Name: "build-agent", Value: "relay fra · 38 ms", LED: ok, Mono: true},
			model.Row{Name: "fileserver", Value: "direct · 4 ms", LED: ok, Mono: true},
			model.Row{Name: "phone", Value: "direct · 21 ms", LED: ok, Mono: true},
			model.Row{Name: "laptop", Value: "relay ams · 44 ms", LED: ok, Mono: true},
			model.Section{Title: "Dormant", Meta: "2"},
			model.Row{Name: "old-desktop", Value: "seen 12d ago", LED: idle, Mono: true},
			model.Row{Name: "test-vm", Value: "key expired", LED: idle, Tone: model.ToneWarn, Mono: true},
		}}},
		Menu: []model.MenuItem{
			{ID: "copy-ip", Label: "Copy address"},
			{ID: "copy-dns", Label: "Copy MagicDNS name"},
		},
	}
}
