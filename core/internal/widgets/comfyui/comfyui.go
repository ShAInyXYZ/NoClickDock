// Package comfyui watches a ComfyUI server: its queue, its devices and the
// progress of the running job. Until its collector is ported (M5) it only
// has a mock.
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

const steps = 28

// Mock is a job in progress. The step advances with the clock, so the dial's
// progress arc can be watched moving against --mock.
func (Widget) Mock(now time.Time) model.Snapshot {
	step := int(now.Unix()/2)%steps + 1
	progress := float64(step) / steps
	return model.Snapshot{
		State:      model.StateBusy,
		StateLabel: "generating",
		Glance:     model.Glance{Text: fmt.Sprintf("%d%%", int(progress*100))},
		Progress:   &progress,
		Width:      380,
		Pages: []model.Page{{ID: "main", Blocks: model.Blocks{
			model.Section{Title: "Queue", Meta: "1 running · 2 pending"},
			model.Meter{Label: "KSampler", Frac: progress, Value: fmt.Sprintf("%d/%d", step, steps), Note: "prompt 4f2a · 1024×1024", Tone: model.ToneInfo},
			model.Section{Title: "cuda:0", Meta: "24G"},
			model.Meter{Label: "VRAM", Frac: 0.71, Value: "17.0/24.0G", Tone: model.ToneOK},
			model.Meter{Label: "Torch VRAM", Frac: 0.58, Value: "13.9G", Tone: model.ToneOK},
			model.Stats{Items: []model.Stat{{Key: "load", Value: "98%"}, {Key: "temp", Value: "71°C"}, {Key: "power", Value: "318W"}, {Key: "fan", Value: "64%"}}},
			model.Section{Title: "Host"},
			model.Stats{Items: []model.Stat{{Key: "cpu", Value: "14%"}, {Key: "ram", Value: "22.1/64G"}}},
			model.Note{Text: "ComfyUI 0.3.60 · torch 2.8.0 · python 3.12", Mono: true},
		}}},
		Footer: "127.0.0.1:8188",
		Menu:   []model.MenuItem{{ID: "open", Label: "Open ComfyUI"}},
	}
}
