// Package widgets lists what the dock can show.
package widgets

import (
	"path/filepath"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/config"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/claude"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/codex"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/comfyui"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/disk"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/docker"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/localservers"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/power"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/tailscale"
)

// Real is every widget that measures this machine. A widget joins this list
// when its collector reaches parity with the Python one. Options are read
// from the config on every poll, so a change applies without a restart.
//
// powerLog is where Power writes its hourly CSV and report: the user's
// ~/Documents/PM-Log only when the core runs in production; the caller
// gives development runs a scratch directory.
func Real(cfg *config.Store, powerLog string) []hub.Collector {
	watch := func() *[]string { return cfg.Get().Widgets.Tailscale.Watch }
	endpoint := func() config.ComfyUI { return cfg.Get().Widgets.ComfyUI }
	tariff := func() config.Power { return cfg.Get().Widgets.Power }
	return []hub.Collector{
		claude.New(), codex.New(), comfyui.New(endpoint), disk.New(), docker.New(), localservers.New(filepath.Dir(cfg.Path())),
		power.New(tariff, powerLog), tailscale.New(watch),
	}
}

// Mocks is every widget, each inventing its data: still (one fixed instant,
// for the gallery and README images) or live (moving with the clock, for
// reviewing the real windows and their toasts).
func Mocks(live bool) []hub.Collector {
	var out []hub.Collector
	for _, m := range []hub.Mocker{
		claude.Mock{}, codex.Mock{}, comfyui.Mock{}, disk.New(),
		docker.Mock{}, localservers.Mock{}, power.Mock{}, tailscale.Mock{},
	} {
		out = append(out, hub.Mocked(m, live))
	}
	return out
}
