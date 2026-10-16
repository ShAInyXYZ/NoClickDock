package comfyui

import (
	"fmt"
	"strings"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sensors"
)

// reading is one poll of the server, and of this machine when it is the
// server.
type reading struct {
	base, os string
	online   bool
	local    bool
	queue    queue
	system   system
	devices  []device
	progress *float64 // the running node's steps, when an event said so

	sensed           bool // local sensors were read
	cpuTemp, cpuLoad *float64
}

func (r reading) phase() string {
	switch {
	case !r.online:
		return "offline"
	case r.queue.Running > 0:
		return "generating"
	case r.queue.Pending > 0:
		return "queued"
	}
	return "idle"
}

// fmtBytes is ComfyUI's own: binary, one decimal, a two-letter unit.
func fmtBytes(b float64) string {
	for _, unit := range []string{"B", "KB", "MB", "GB", "TB"} {
		if b < 1024 && b > -1024 {
			return fmt.Sprintf("%.1f %s", b, unit)
		}
		b /= 1024
	}
	return fmt.Sprintf("%.1f PB", b)
}

// vramTone: ComfyUI fills VRAM by design, so only a nearly full card is
// worth a colour (ARCHITECTURE Q5).
func vramTone(frac float64) model.Tone {
	switch {
	case frac >= 0.95:
		return model.ToneCrit
	case frac >= 0.85:
		return model.ToneWarn
	}
	return model.ToneNone
}

func render(r reading) model.Snapshot {
	s := model.Snapshot{
		Width: 400,
		Menu:  []model.MenuItem{{ID: "open-ui", Label: "Open ComfyUI", URL: r.base}},
	}
	if !r.online {
		// Unknown is never zero, and nothing twice: the sentence alone.
		s.State, s.StateLabel = model.StateIdle, "offline"
		s.Glance = model.Glance{Text: "off", Tone: model.ToneIdle}
		s.Pages = []model.Page{{ID: "main", Blocks: model.Blocks{
			model.Empty{Text: "ComfyUI is not answering at", Commands: []string{r.base}},
		}}}
		return s
	}

	switch r.phase() {
	case "generating":
		s.State, s.StateLabel, s.Glance = model.StateBusy, "generating", model.Glance{Text: "run"}
		if p := r.progress; p != nil {
			pct := int(*p * 100)
			s.StateLabel = fmt.Sprintf("generating · %d%%", pct)
			s.Glance.Text = fmt.Sprintf("%d%%", pct)
			s.Progress = p
		}
	case "queued":
		s.State, s.StateLabel = model.StateBusy, fmt.Sprintf("queued · %d pending", r.queue.Pending)
		s.Glance = model.Glance{Text: fmt.Sprintf("q %d", min(r.queue.Pending, 999))}
	default:
		s.State, s.StateLabel, s.Glance = model.StateOK, "idle", model.Glance{Text: "—"}
		if len(r.devices) > 0 && r.devices[0].VRAMTotal > 0 {
			d := r.devices[0]
			s.Glance.Text = fmt.Sprintf("%.0f%%", d.VRAMUsed/d.VRAMTotal*100)
		}
	}

	blocks := model.Blocks{model.Stats{Items: []model.Stat{
		{Key: "running", Value: fmt.Sprint(r.queue.Running)}, {Key: "pending", Value: fmt.Sprint(r.queue.Pending)},
	}}}
	for _, d := range r.devices {
		blocks = append(blocks, deviceBlocks(d, len(r.devices))...)
	}
	if r.sensed {
		blocks = append(blocks, cpuBlocks(r)...)
	}
	blocks = append(blocks, model.Section{Title: "System"})
	if r.system.RAMTotal > 0 {
		used := max(0, r.system.RAMTotal-r.system.RAMFree)
		blocks = append(blocks, model.Meter{
			Label: "RAM", Frac: min(1, used/r.system.RAMTotal),
			Value: fmtBytes(used) + " / " + fmtBytes(r.system.RAMTotal),
		})
	}
	var versions []model.Stat
	for _, v := range []model.Stat{{Key: "comfyui", Value: r.system.ComfyUI}, {Key: "torch", Value: r.system.Torch}, {Key: "python", Value: r.system.Python}} {
		if v.Value != "" {
			versions = append(versions, v)
		}
	}
	if len(versions) > 0 {
		blocks = append(blocks, model.Stats{Items: versions})
	}
	blocks = append(blocks, model.Row{Name: "Endpoint", Value: r.base})
	s.Pages = []model.Page{{ID: "main", Blocks: blocks}}
	return s
}

// deviceBlocks: one section per device, in the server's order and naming.
func deviceBlocks(d device, count int) model.Blocks {
	name := sensors.ShortGPUName(d.Name)
	var title string
	switch {
	case count > 1:
		title = strings.ToUpper(d.Label) + " · " + name
	case d.Type == "cpu":
		title = "Device · " + name
	default:
		title = "GPU · " + name
	}
	if d.Label == d.Name && count > 1 { // no torch device string to add
		title = name
	}
	out := model.Blocks{model.Section{Title: title, Meta: d.Allocator}}
	if d.VRAMTotal > 0 {
		frac := min(1, d.VRAMUsed/d.VRAMTotal)
		out = append(out, model.Meter{
			Label: "VRAM", Frac: frac, Tone: vramTone(frac),
			Value: fmt.Sprintf("%s / %s (%.0f%%)", fmtBytes(d.VRAMUsed), fmtBytes(d.VRAMTotal), frac*100),
		})
	}
	// Torch's share is what its allocator reserved: full is its normal
	// state, so it carries no tone. cudaMallocAsync reports 0 — no row.
	if d.TorchTotal > 0 {
		out = append(out, model.Meter{
			Label: "Torch VRAM", Frac: min(1, d.TorchUsed/d.TorchTotal),
			Value: fmtBytes(d.TorchUsed) + " / " + fmtBytes(d.TorchTotal),
		})
	}
	var tele []model.Stat
	add := func(key, format string, v *float64) {
		if v != nil {
			tele = append(tele, model.Stat{Key: key, Value: fmt.Sprintf(format, *v)})
		}
	}
	add("load", "%.0f%%", d.Util)
	add("temp", "%.0f°C", d.Temp)
	add("power", "%.0fW", d.Power)
	add("fan", "%.0f%%", d.Fan)
	if len(tele) > 0 {
		out = append(out, model.Stats{Items: tele})
	}
	return out
}

// cpuBlocks: this machine's CPU, only when it is the server. The RAM is
// the System section's meter (nothing twice).
func cpuBlocks(r reading) model.Blocks {
	var items []model.Stat
	if r.cpuLoad != nil {
		items = append(items, model.Stat{Key: "load", Value: fmt.Sprintf("%.0f%%", *r.cpuLoad)})
	}
	if r.cpuTemp != nil {
		items = append(items, model.Stat{Key: "temp", Value: fmt.Sprintf("%.0f°C", *r.cpuTemp)})
	}
	out := model.Blocks{model.Section{Title: "CPU"}}
	if len(items) > 0 {
		out = append(out, model.Stats{Items: items})
	}
	if r.os == "windows" {
		out = append(out, model.Note{Text: "CPU temperature needs a sensor driver on Windows", Tone: model.ToneIdle})
	}
	if len(out) == 1 {
		return nil
	}
	return out
}
