package tailscale

import (
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

// Mock invents a tailnet for --mock, drawn by the real widget's render.
type Mock struct{}

func (Mock) Meta() hub.Meta { return meta }

// Mock is an invented tailnet with one watched peer that has dropped off.
// Every host name and address here is made up (100.64.0.0/10 is
// Tailscale's own range).
func (Mock) Mock(now time.Time) model.Snapshot {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	day := 24 * time.Hour
	on := func(host, os string, active, direct bool, relay string, rate float64) *peer {
		return &peer{Host: host, id: "nodekey:" + host, name: host, Online: true, Active: active, OS: os, Direct: direct, Relay: relay,
			Rate: rate, HasRate: rate > 0, Last: now}
	}
	off := func(host string, last time.Time) *peer {
		return &peer{Host: host, id: "nodekey:" + host, name: host, OS: "linux", Last: last}
	}
	exitFra := on("exit-fra", "linux", false, true, "", 0)
	exitFra.ExitOption = true
	exitAms := off("exit-ams", ago(9*day))
	exitAms.ExitOption = true
	oldVM := off("old-vm", ago(140*day))
	oldVM.KeyExpired = true
	peers := []*peer{
		on("atlas", "linux", true, true, "", 1.2*(1<<20)),
		on("build-agent", "linux", true, false, "fra", 340*(1<<10)),
		on("laptop", "macOS", false, true, "", 0),
		on("phone", "iOS", false, true, "", 0),
		exitFra, exitAms, oldVM,
		off("fileserver", ago(12*time.Minute)),
		off("old-desktop", ago(12*day)),
		off("test-vm", ago(21*day)),
		off("pi-garage", ago(60*day)),
	}
	for _, p := range peers {
		switch p.Host {
		case "atlas", "build-agent", "fileserver", "laptop":
			p.Watched = true
		}
	}
	return render(&poll{
		available: true, pinned: true, now: now,
		st: &status{
			Backend: "Running", Self: "workstation", SelfOnline: true,
			IP: "100.101.1.7", DNS: "workstation.tail0c4fe.ts.net", Version: "1.80.2",
			ExpiryDays: 143, HasExpiry: true, Peers: peers,
		},
		exposure: &exposure{serve: []served{{port: 443, target: "localhost:3000"}}},
	})
}

// Variants: the daemon not running.
func (Mock) Variants(time.Time) map[string]model.Snapshot {
	return map[string]model.Snapshot{"unavailable": render(&poll{})}
}
