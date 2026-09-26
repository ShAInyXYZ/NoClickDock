package comfyui

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/config"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

// Mock is an invented workstation running ComfyUI, drawn by the real
// render. Live, a job runs to the end of every minute, idles a few seconds
// and starts again, so the progress arc and the "Generation complete" toast
// can be watched.
type Mock struct{}

func (Mock) Meta() hub.Meta { return meta }

const (
	mockBase = "http://127.0.0.1:8188"
	idleSecs = 8
)

func f(v float64) *float64 { return &v }

func mockReading(busy bool) reading {
	const gib = 1 << 30
	r := reading{
		base: mockBase, os: "linux", online: true, local: true, sensed: true,
		system:  system{ComfyUI: "0.3.60", Torch: "2.8.0", Python: "3.12.3", RAMTotal: 125.7 * gib, RAMFree: (125.7 - 51.2) * gib},
		cpuLoad: f(23), cpuTemp: f(54),
	}
	d := device{
		Label: "cuda:0", Name: "NVIDIA GeForce RTX 4090", Type: "cuda", Allocator: "cudaMallocAsync",
		VRAMTotal: 24 * gib, VRAMUsed: 0.14 * 24 * gib,
		Util: f(2), Temp: f(44), Power: f(38), Fan: f(30),
	}
	if busy {
		r.queue = queue{Running: 1, Pending: 2}
		d.VRAMUsed, d.Util, d.Temp, d.Power, d.Fan = 0.38*24*gib, f(87), f(71), f(312), f(45)
		r.cpuLoad = f(23)
	}
	r.devices = []device{d}
	return r
}

func (Mock) Mock(now time.Time) model.Snapshot {
	sec := now.Second()
	still := now.Equal(hub.Still)
	if sec < idleSecs && !still {
		return render(mockReading(false))
	}
	if still {
		sec = 30 // the still instant shows the job at 42 %
	}
	r := mockReading(true)
	r.progress = f(float64(sec-idleSecs) / float64(60-idleSecs))
	return render(r)
}

// Transition, for the mock: its only moves are a job starting and ending.
func (Mock) Transition(from, to model.State, s model.Snapshot) string {
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
func (Mock) Variants(time.Time) map[string]model.Snapshot {
	return map[string]model.Snapshot{"offline": render(reading{base: mockBase})}
}

// Ask "probe", in a mock session: says what it would have asked, and asks
// nothing (a mock never touches the network).
func (Mock) Ask(_ context.Context, what string, args map[string]string) (string, []string, error) {
	if what != "probe" {
		return "", nil, fmt.Errorf("ComfyUI cannot answer %q", what)
	}
	port, err := strconv.Atoi(args["port"])
	if !config.ValidHost(args["host"]) || err != nil || port < 1 || port > 65535 {
		return "", nil, fmt.Errorf("%q:%q is not an endpoint", args["host"], args["port"])
	}
	return baseURL(args["host"], port) + " answered · ComfyUI 0.3.60 · RTX 4090 · queue empty (invented: a mock asks nothing)", nil, nil
}
