// Package disk watches filesystem pressure — local disks, ZFS pools and
// network shares — with the reclaimable space Docker is sitting on. A full
// root filesystem breaks builds, package installs and image pulls in
// confusing ways; this is the warning before that. Parity:
// docs/rewrite/specs/disk.md, and decision Q6 in ARCHITECTURE.md.
package disk

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

// Thresholds are on the used fraction. Disks misbehave well before 100 %:
// filesystems fragment and some reserve blocks for root only.
const (
	warnFrac = 0.85
	critFrac = 0.92

	analyzer = "baobab"
)

// Kind is where a filesystem's space comes from; each has its own section.
type Kind int

const (
	Local Kind = iota
	Pool
	Network
)

var sections = []struct {
	kind  Kind
	title string
}{{Local, "Filesystems"}, {Pool, "Pools"}, {Network, "Network"}}

// Filesystem is one mounted filesystem's capacity, in bytes. Used and Free
// do not add up to Total: the difference is reserved for root.
type Filesystem struct {
	Mount  string // where it is mounted: "/", `C:\`
	Label  string // what the panel calls it: "/", "C: System"
	FSType string
	Kind   Kind
	Total  uint64
	Used   uint64
	Free   uint64
	// Unresponsive is a share that did not answer in time: it has no numbers.
	Unresponsive bool
}

func (f Filesystem) frac() float64 { return float64(f.Used) / float64(f.Total) }

type Widget struct {
	probe prober

	mu         sync.Mutex
	docker     *dockerSpace
	dockerAt   time.Time
	dockerBusy bool
	refresh    func()
}

func New() *Widget { return &Widget{} }

func (*Widget) Meta() hub.Meta {
	return hub.Meta{ID: "disk", Title: "Disk", Order: 40, PollSecs: 30}
}

// SetRefresh lets the background docker query ask for a poll when it lands.
func (w *Widget) SetRefresh(f func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.refresh = f
}

func (w *Widget) Collect(ctx context.Context) (model.Snapshot, error) {
	fss, err := w.filesystems()
	if err != nil {
		return model.Snapshot{}, err
	}
	var menu []model.MenuItem
	if sys.Has(analyzer) {
		menu = append(menu, model.MenuItem{ID: "open-analyzer", Label: "Open Disk Usage Analyzer"})
	}
	return build(fss, w.dockerSpace(), menu), nil
}

func (*Widget) Act(_ context.Context, action string, _ map[string]string) (string, error) {
	if action != "open-analyzer" {
		return "", fmt.Errorf("disk has no action %q", action)
	}
	if err := sys.Start(analyzer); err != nil {
		return "", fmt.Errorf("could not open Disk Usage Analyzer: %w", err)
	}
	return "opened Disk Usage Analyzer", nil
}

func sortFilesystems(fss []Filesystem) {
	sort.SliceStable(fss, func(i, j int) bool {
		if (fss[i].Mount == "/") != (fss[j].Mount == "/") {
			return fss[i].Mount == "/"
		}
		return fss[i].Mount < fss[j].Mount
	})
}

// build turns measurements into the snapshot. It is pure, so the panel can be
// tested from fixtures and the mock draws with the same code as the real one.
func build(fss []Filesystem, docker *dockerSpace, menu []model.MenuItem) model.Snapshot {
	var measured, silent []Filesystem
	for _, f := range fss {
		switch {
		case f.Unresponsive:
			silent = append(silent, f)
		case f.Total > 0:
			measured = append(measured, f)
		}
	}
	s := model.Snapshot{Menu: menu, Width: 400}

	var blocks model.Blocks
	if len(measured)+len(silent) == 0 {
		s.State, s.StateLabel = model.StateIdle, "no filesystems"
		blocks = model.Blocks{
			model.Section{Title: "Filesystems"},
			model.Note{Text: "No filesystems found", Tone: model.ToneIdle},
		}
	} else {
		s.State, s.StateLabel, s.Glance = summarize(measured, silent)
		blocks = sectionsFor(measured, silent)
		s.Issues = issues(measured, silent)
	}
	if docker != nil {
		blocks = append(blocks, docker.blocks()...)
	}
	s.Pages = []model.Page{{ID: "filesystems", Blocks: blocks}}
	return s
}

// summarize reads the fullest filesystem: a full share drives the dial like
// a local disk, and a share that stopped answering is at least a warning.
func summarize(measured, silent []Filesystem) (model.State, string, model.Glance) {
	state, label := model.StateOK, ""
	var glance model.Glance
	if len(measured) > 0 {
		worst, roomiest := measured[0], measured[0]
		for _, f := range measured {
			if f.frac() > worst.frac() {
				worst = f
			}
			if f.Free < roomiest.Free {
				roomiest = f
			}
		}
		switch sys.RatioTone(worst.frac(), warnFrac, critFrac) {
		case model.ToneCrit:
			state = model.StateCrit
		case model.ToneWarn:
			state = model.StateWarn
		}
		// Healthy: the tightest margin is the number worth knowing.
		label = sys.FmtBytes(float64(roomiest.Free)) + " free"
		if state != model.StateOK {
			label = fmt.Sprintf("%s %d%% full", worst.Label, percent(worst.frac()))
		}
		glance = model.Glance{Text: fmt.Sprintf("%d%%", percent(worst.frac())), Tone: problemTone(worst.frac())}
	}
	if len(silent) > 0 && state == model.StateOK {
		state, label = model.StateWarn, silent[0].Label+" not responding"
		glance.Tone = model.ToneWarn
	}
	return state, label, glance
}

// sectionsFor spells out every filesystem, grouped by where its space comes
// from: used / total with the free space, because a bare "85 %" leaves you
// asking 85 % of what, and how much room that actually leaves.
func sectionsFor(measured, silent []Filesystem) model.Blocks {
	var out model.Blocks
	for _, sec := range sections {
		var group model.Blocks
		n := 0
		for _, f := range measured {
			if f.Kind != sec.kind {
				continue
			}
			n++
			frac := f.frac()
			group = append(group, model.Meter{
				Label: f.Label,
				Mono:  true,
				Frac:  sys.Clamp01(frac),
				Value: sys.FmtBytes(float64(f.Free)) + " free",
				Note: fmt.Sprintf("%s used of %s · %d%% · %s",
					sys.FmtBytes(float64(f.Used)), sys.FmtBytes(float64(f.Total)), percent(frac), f.FSType),
				Tone: sys.RatioTone(frac, warnFrac, critFrac),
			})
		}
		for _, f := range silent {
			if f.Kind != sec.kind {
				continue
			}
			n++
			group = append(group, model.Row{
				Name: f.Label, Mono: true, Sub: f.FSType,
				Value: "not responding", Tone: model.ToneWarn, LED: model.ToneWarn,
			})
		}
		if n == 0 {
			continue
		}
		meta := fmt.Sprint(n)
		if len(out) == 0 {
			meta += fmt.Sprintf(" · warn %d · crit %d", percent(warnFrac), percent(critFrac))
		}
		out = append(out, model.Section{Title: sec.title, Meta: meta})
		out = append(out, group...)
	}
	return out
}

// issues are the filesystems worth a toast when they first cross a line.
func issues(measured, silent []Filesystem) []model.Issue {
	var out []model.Issue
	for _, f := range measured {
		if frac := f.frac(); frac >= warnFrac {
			out = append(out, model.Issue{
				ID:   f.Mount,
				Text: fmt.Sprintf("%s: %d%% full · %s left", f.Label, percent(frac), sys.FmtBytes(float64(f.Free))),
				Tone: problemTone(frac),
			})
		}
	}
	for _, f := range silent {
		out = append(out, model.Issue{ID: "silent " + f.Mount, Text: f.Label + " is not responding", Tone: model.ToneWarn})
	}
	return out
}

// problemTone colours only what needs attention; a healthy readout is quiet.
func problemTone(frac float64) model.Tone {
	if t := sys.RatioTone(frac, warnFrac, critFrac); t != model.ToneOK {
		return t
	}
	return model.ToneNone
}

// percent rounds half to even, as Python's "%.0f" did in the old panel.
func percent(frac float64) int { return int(math.RoundToEven(frac * 100)) }
