// Package widgets lists what the dock can show.
package widgets

import (
	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/claude"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/codex"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/comfyui"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/disk"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/docker"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/power"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets/tailscale"
)

// Real is every widget that measures this machine. A widget joins this list
// when its collector reaches parity with the Python one.
func Real() []hub.Collector {
	return []hub.Collector{disk.New(), docker.New()}
}

// Mocks is every widget, each inventing its data: still (one fixed instant,
// for the gallery and README images) or live (moving with the clock, for
// reviewing the real windows and their toasts).
func Mocks(live bool) []hub.Collector {
	var out []hub.Collector
	for _, m := range []hub.Mocker{
		claude.Widget{}, codex.Widget{}, comfyui.Widget{}, disk.New(),
		docker.Mock{}, power.Widget{}, tailscale.Widget{},
	} {
		out = append(out, hub.Mocked(m, live))
	}
	return out
}
