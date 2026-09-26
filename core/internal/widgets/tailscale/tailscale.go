// Package tailscale watches tailscaled, this node and every peer. Parity:
// docs/rewrite/specs/tailscale.md. Until its collector is ported (M3) it
// only has a mock.
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

const (
	address = "100.101.1.7"
	dnsName = "workstation.tail0c4fe.ts.net"
)

// Mock is an invented tailnet with one watched peer that has dropped off.
// Every host name and address here is made up.
func (Widget) Mock(time.Time) model.Snapshot {
	ok, warn, idle := model.ToneOK, model.ToneWarn, model.ToneIdle
	return model.Snapshot{
		State:      model.StateWarn,
		StateLabel: "fileserver offline",
		Glance:     model.Glance{Text: "3/4", Tone: warn},
		Width:      420,
		Pages: []model.Page{{ID: "main", Blocks: model.Blocks{
			model.Section{Title: "This node"},
			model.Row{Name: "workstation", Value: "Running", LED: ok},
			model.Row{Name: "address", Value: address, Mono: true},
			model.Row{Name: "magicdns", Value: dnsName, Mono: true},
			model.Row{Name: "version", Value: "1.80.2"},
			model.Row{Name: "key expires", Value: "143d"},
			model.Section{Title: "Exposure"},
			model.Row{Name: "serve", Value: "https://" + dnsName + " → :3000", Tone: warn, LED: warn},
			model.Row{Name: "exit nodes available", Value: "exit-fra, exit-ams"},
			model.Section{Title: "Online", Meta: "4"},
			model.Row{Name: "atlas", Value: "linux · direct · 1.2 MB/s", LED: ok, Mono: true},
			model.Row{Name: "build-agent", Value: "linux · relay fra · 340.0 KB/s", LED: ok, Tone: warn, Mono: true},
			model.Row{Name: "laptop", Value: "macOS · direct", LED: idle, Mono: true},
			model.Row{Name: "phone", Value: "iOS · direct", LED: idle, Mono: true},
			model.Section{Title: "Watched · offline", Meta: "1", Tone: warn},
			model.Row{Name: "fileserver", Value: "12m ago", Tone: warn, LED: warn, Mono: true},
			model.Section{Title: "Dormant", Meta: "4"},
			model.Row{Name: "old-vm", Value: "key expired", Tone: warn, Mono: true},
			model.Chips{Columns: 2, Items: []model.Chip{
				{Label: "old-desktop", Tone: idle, Value: "12d ago"},
				{Label: "test-vm", Tone: idle, Value: "3w ago"},
				{Label: "pi-garage", Tone: idle, Value: "2mo ago"},
			}},
		}}},
		Menu: []model.MenuItem{
			{ID: "copy-ip", Label: "Copy address  " + address, Copy: address},
			{ID: "copy-dns", Label: "Copy MagicDNS name", Copy: dnsName},
		},
		Issues: []model.Issue{{ID: "down:fileserver", Text: "fileserver is offline", Tone: warn}},
	}
}

// Variants: the daemon not running.
func (Widget) Variants(time.Time) map[string]model.Snapshot {
	return map[string]model.Snapshot{
		"unavailable": {
			State: model.StateIdle, StateLabel: "tailscaled unreachable",
			Glance: model.Glance{Text: "off", Tone: model.ToneIdle}, Width: 420,
			Pages: []model.Page{{ID: "main", Blocks: model.Blocks{
				model.Empty{Text: "tailscaled is not running on this machine. Start it with `sudo systemctl start tailscaled`"},
			}}},
		},
	}
}
