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
	return []hub.Collector{disk.New()}
}

// Mocks is every widget, each inventing its data.
func Mocks() []hub.Collector {
	return []hub.Collector{
		hub.Mocked(claude.Widget{}),
		hub.Mocked(codex.Widget{}),
		hub.Mocked(comfyui.Widget{}),
		hub.Mocked(disk.New()),
		hub.Mocked(docker.Widget{}),
		hub.Mocked(power.Widget{}),
		hub.Mocked(tailscale.Widget{}),
	}
}
