// Package comfyui watches a ComfyUI server: its queue, its devices and, when
// the server runs on this machine, their temperatures and load. Parity:
// docs/rewrite/specs/comfyui.md.
package comfyui

import (
	"context"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/config"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sensors"
)

var meta = hub.Meta{ID: "comfyui", Title: "ComfyUI", Order: 30, PollSecs: 2}

const sensorsEvery = 5 * time.Second

type Widget struct {
	endpoint func() config.ComfyUI
	client   *http.Client
	net      resolver
	ws       *listener

	mu      sync.Mutex
	refresh func()

	// Per endpoint: a change starts over (offline, local re-evaluated,
	// sensors forgotten, the websocket reconnected).
	base  string
	local bool

	sensorsAt time.Time
	gpus      []sensors.GPU
	cpuTemp   *float64
	cpuLoad   *float64
	cpuUsage  sensors.CPUUsage
	tempInput string

	phase, prevPhase string // "offline" | "idle" | "queued" | "generating"
}

func New(endpoint func() config.ComfyUI) *Widget {
	w := &Widget{endpoint: endpoint, client: &http.Client{}, net: netResolver}
	w.ws = newListener(func() {
		w.mu.Lock()
		refresh := w.refresh
		w.mu.Unlock()
		if refresh != nil {
			refresh()
		}
	})
	return w
}

func (*Widget) Meta() hub.Meta { return meta }

func (w *Widget) SetRefresh(f func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.refresh = f
}

func (w *Widget) Collect(ctx context.Context) (model.Snapshot, error) {
	ep := w.endpoint()
	base := baseURL(ep.Host, ep.Port)

	w.mu.Lock()
	changed, local := base != w.base, w.local
	w.mu.Unlock()
	if changed { // Collect never runs concurrently with itself
		local = w.net.isLocal(ctx, ep.Host) // a lookup: not under the lock
		w.mu.Lock()
		w.base, w.local = base, local
		w.sensorsAt, w.gpus, w.cpuTemp, w.cpuLoad = time.Time{}, nil, nil, nil
		w.cpuUsage, w.tempInput = sensors.CPUUsage{}, ""
		w.phase = "offline"
		w.mu.Unlock()
		w.ws.follow(ep.Host, ep.Port)
	}

	r := reading{base: base, local: local, os: runtime.GOOS}
	q, err := fetchQueue(ctx, w.client, base)
	if err == nil {
		r.system, r.devices, err = fetchStats(ctx, w.client, base)
	}
	r.online, r.queue = err == nil, q

	if r.online && local {
		w.readSensors(ctx)
		w.mu.Lock()
		r.devices = merge(r.devices, w.gpus)
		r.cpuTemp, r.cpuLoad, r.sensed = w.cpuTemp, w.cpuLoad, true
		w.mu.Unlock()
	}
	if r.online && r.queue.Running > 0 {
		r.progress = w.ws.Progress()
	} else {
		w.ws.clear()
	}

	w.mu.Lock()
	w.prevPhase, w.phase = w.phase, r.phase()
	w.mu.Unlock()
	return render(r), nil
}

// readSensors refreshes the local telemetry every five seconds; the queue
// keeps its two.
func (w *Widget) readSensors(ctx context.Context) {
	w.mu.Lock()
	due := time.Since(w.sensorsAt) >= sensorsEvery
	if due {
		w.sensorsAt = time.Now()
	}
	w.mu.Unlock()
	if !due {
		return
	}
	gpus, _ := sensors.NvidiaSMI(ctx)
	load := w.cpuUsage.Read()
	var temp *float64
	if runtime.GOOS == "linux" {
		if w.tempInput == "" {
			w.tempInput = sensors.CPUTempInput(sensors.HwmonRoot)
		}
		temp = sensors.Milli(w.tempInput)
	}
	w.mu.Lock()
	w.gpus, w.cpuLoad, w.cpuTemp = gpus, load, temp
	w.mu.Unlock()
}

// merge puts nvidia-smi's telemetry on ComfyUI's devices by index. Torch's
// CUDA order (fastest first by default) can differ from nvidia-smi's PCI
// order: when both lists are as long and the names disagree at an index,
// the card is found by name instead; otherwise that device gets none.
func merge(devices []device, gpus []sensors.GPU) []device {
	byIndex := map[int]sensors.GPU{}
	for _, g := range gpus {
		byIndex[g.Index] = g
	}
	cuda := 0
	for _, d := range devices {
		if d.Type == "cuda" {
			cuda++
		}
	}
	used := map[int]bool{}
	for i := range devices {
		d := &devices[i]
		if d.Type != "cuda" {
			continue
		}
		g, ok := byIndex[d.Index]
		if !ok || !sameCard(d.Name, g.Name) {
			ok = false
			if cuda == len(gpus) {
				g, ok = byName(gpus, d.Name, used)
			}
		}
		if !ok {
			continue
		}
		used[g.Index] = true
		d.Temp, d.Util, d.Power, d.Fan = g.Temp, g.Util, g.Power, g.Fan
	}
	return devices
}

func sameCard(a, b string) bool {
	return strings.EqualFold(sensors.ShortGPUName(strings.TrimSpace(a)), sensors.ShortGPUName(strings.TrimSpace(b)))
}

// byName is the one unused card of that name, if exactly one.
func byName(gpus []sensors.GPU, name string, used map[int]bool) (sensors.GPU, bool) {
	var found []sensors.GPU
	for _, g := range gpus {
		if !used[g.Index] && sameCard(name, g.Name) {
			found = append(found, g)
		}
	}
	if len(found) == 1 {
		return found[0], true
	}
	return sensors.GPU{}, false
}

// Transition words the job's life as the Python widget did. A queue that
// drains without a job having been seen running says nothing.
func (w *Widget) Transition(from, to model.State, s model.Snapshot) string {
	w.mu.Lock()
	prev, phase := w.prevPhase, w.phase
	w.mu.Unlock()
	return transition(prev, phase, s)
}

func transition(prev, phase string, s model.Snapshot) string {
	switch {
	case phase == "offline":
		return "ComfyUI is offline"
	case phase == "generating":
		return "Generating…"
	case phase == "queued":
		_, pending, _ := strings.Cut(s.StateLabel, " · ")
		return "Queued, " + pending
	case prev == "generating" && phase == "idle":
		return "Generation complete"
	case prev == "offline" && phase == "idle":
		return "ComfyUI is online"
	}
	return ""
}

func (w *Widget) Close() { w.ws.stop() }
