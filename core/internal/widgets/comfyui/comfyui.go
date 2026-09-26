// Package comfyui watches a ComfyUI server: its queue, its devices and the
// progress of the running job. Parity: docs/rewrite/specs/comfyui.md. Until
// its collector is ported (M5) it only has a mock.
package comfyui

import (
	"fmt"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

type Widget struct{}

func (Widget) Meta() hub.Meta {
	return hub.Meta{ID: "comfyui", Title: "ComfyUI", Order: 30, PollSecs: 2}
}

const endpoint = "http://127.0.0.1:8188"

// cycle is a live mock's rhythm: idle for the first seconds of each minute,
// then a job that runs to the end of it.
const idleSecs = 8

// Mock is a job in progress with two more queued. Live, it finishes once a
// minute, idles a few seconds, and starts again, so the progress arc and
// the "Generation complete" toast can be watched.
func (Widget) Mock(now time.Time) model.Snapshot {
	sec := now.Second()
	still := now.Equal(hub.Still)
	if sec < idleSecs && !still {
		return idle()
	}
	if still {
		sec = 30 // the still instant shows the job at 42 %
	}
	progress := float64(sec-idleSecs) / float64(60-idleSecs)
	pct := int(progress * 100)
	s := panel(fmt.Sprint(1), "2", true)
	s.State, s.StateLabel = model.StateBusy, fmt.Sprintf("generating · %d%%", pct)
	s.Glance = model.Glance{Text: fmt.Sprintf("%d%%", pct)}
	s.Progress = &progress
	return s
}

func idle() model.Snapshot {
	s := panel("0", "0", false)
	s.State, s.StateLabel = model.StateOK, "idle"
	s.Glance = model.Glance{Text: "14%"} // device 0's VRAM in use
	return s
}

func panel(running, pending string, busy bool) model.Snapshot {
	vram, load, power := 0.38, "87%", "312W"
	if !busy {
		vram, load, power = 0.14, "2%", "38W"
	}
	return model.Snapshot{
		Width: 400,
		Pages: []model.Page{{ID: "main", Blocks: model.Blocks{
			model.Stats{Items: []model.Stat{{Key: "running", Value: running}, {Key: "pending", Value: pending}}},
			model.Section{Title: "GPU · RTX 4090", Meta: "cudaMallocAsync"},
			model.Meter{Label: "VRAM", Frac: vram, Value: fmt.Sprintf("%.1f GB / 24.0 GB (%.0f%%)", vram*24, vram*100)},
			model.Stats{Items: []model.Stat{
				{Key: "load", Value: load}, {Key: "temp", Value: "71°C"}, {Key: "power", Value: power}, {Key: "fan", Value: "45%"},
			}},
			model.Section{Title: "CPU"},
			model.Stats{Items: []model.Stat{{Key: "load", Value: "23%"}, {Key: "temp", Value: "54°C"}, {Key: "ram", Value: "41%"}}},
			model.Section{Title: "System"},
			model.Meter{Label: "RAM", Frac: 51.2 / 125.7, Value: "51.2 GB / 125.7 GB"},
			model.Stats{Items: []model.Stat{{Key: "comfyui", Value: "0.3.60"}, {Key: "torch", Value: "2.8.0"}, {Key: "python", Value: "3.12.3"}}},
			model.Row{Name: "Endpoint", Value: endpoint},
		}}},
		Menu: []model.MenuItem{{ID: "open-ui", Label: "Open ComfyUI", URL: endpoint}},
	}
}

// Transition words the job's life as the Python widget did; being idle
// again after a queue drains says nothing.
func (Widget) Transition(from, to model.State, s model.Snapshot) string {
	switch {
	case to == model.StateBusy:
		return "Generating…"
	case from == model.StateBusy && to == model.StateOK:
		return "Generation complete"
	case to == model.StateIdle:
		return "ComfyUI is offline"
	case from == model.StateIdle && to == model.StateOK:
		return "ComfyUI is online"
	}
	return ""
}

// Variants: nothing answering at the endpoint.
func (Widget) Variants(time.Time) map[string]model.Snapshot {
	return map[string]model.Snapshot{
		"offline": {
			State: model.StateIdle, StateLabel: "offline",
			Glance: model.Glance{Text: "off", Tone: model.ToneIdle}, Width: 400,
			// Unknown is never zero, and nothing twice: the sentence alone.
			Pages: []model.Page{{ID: "main", Blocks: model.Blocks{
				model.Empty{Text: "ComfyUI is not answering at", Commands: []string{endpoint}},
			}}},
		},
	}
}
