// Package power measures what the machine draws and what it costs. Parity:
// docs/rewrite/specs/power.md. Until its collector is ported (M5) it only
// has a mock.
package power

import (
	"fmt"
	"math"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

type Widget struct{}

func (Widget) Meta() hub.Meta {
	return hub.Meta{ID: "power", Title: "Power", Order: 60, PollSecs: 2}
}

// hourly is an invented day of average watts per clock hour, up to the
// current one.
var hourly = []float64{182, 176, 171, 168, 170, 174, 210, 265, 318, 344, 372, 389, 402, 356, 398}

// Mock is an invented workstation: two GPUs, the first with a memory
// junction running hot (the widget's warning), on a time-of-use tariff.
// Every model name here is illustrative. Live, the draw breathes a little.
func (Widget) Mock(now time.Time) model.Snapshot {
	warn := model.ToneWarn
	total := 412.0 + 18*math.Sin(float64(now.Unix())/7)
	watts := fmt.Sprintf("%.0fW", total)
	cell := func(text string) model.Cell { return model.Cell{Text: text} }
	hot := func(text string) model.Cell { return model.Cell{Text: text, Tone: warn} }
	current := len(hourly) - 1

	left := model.Blocks{
		model.Section{Title: "Power draw"},
		model.Table{
			Columns: []model.Column{
				{Label: "", Align: "left", Mono: true},
				{Label: "", Align: "left"},
				{Label: "W / cap", Align: "right", Mono: true},
				{Label: "core", Align: "right", Mono: true},
				{Label: "mem", Align: "right", Mono: true},
			},
			Rows: []model.TableRow{
				{Cells: []model.Cell{cell("CPU"), cell("Ryzen 9 7950X"), cell("96W"), cell("61°C"), cell("")}},
				{Cells: []model.Cell{cell("GPU0"), cell("RTX 4090"), cell("318/450W"), hot("71°C"), hot("92°C")}},
				{Cells: []model.Cell{cell("GPU1"), cell("RTX 3060"), cell("21/170W"), cell("41°C"), cell("46°C")}},
				{Cells: []model.Cell{cell("Board"), cell("X670E board"), cell("30W"), cell(""), cell("")}},
			},
		},
		model.Section{Title: "Storage", Meta: "2"},
		model.Table{
			Columns: []model.Column{{Align: "left", Mono: true}, {Align: "left"}, {Align: "right", Mono: true}, {Align: "right", Mono: true}},
			Rows: []model.TableRow{
				{Cells: model.Cells("NVMe", "990 PRO 2 TB", "5W", "43°C")},
				{Cells: model.Cells("NVMe", "SN850X 1 TB", "5W", "38°C")},
			},
		},
		model.Section{Title: "Displays", Meta: "2 · own PSU"},
		model.Table{
			Columns: []model.Column{{Align: "left", Mono: true}, {Align: "left"}, {Align: "right", Mono: true}},
			Rows: []model.TableRow{
				{Cells: model.Cells("DP-0", `27" 2560×1440`, "~48W")},
				{Cells: model.Cells("HDMI-0", `24" 1920×1080`, "~43W")},
			},
		},
		model.Section{Title: "USB", Meta: "3"},
		model.Row{Name: "Mechanical keyboard"},
		model.Row{Name: "Wireless mouse receiver"},
		model.Row{Name: "USB audio interface"},
	}

	highlight := current
	right := model.Blocks{
		model.Section{Title: "Cost", Meta: "297 kWh / mo"},
		model.Donut{
			Segments: []model.Segment{
				{Label: "GPU", Value: 339, Display: "61 EUR"},
				{Label: "CPU", Value: 96, Display: "17 EUR"},
				{Label: "Board", Value: 30, Display: "5 EUR"},
				{Label: "NVMe", Value: 10, Display: "2 EUR"},
				{Label: "USB", Value: 5, Display: "1 EUR"},
			},
			Center: "86",
			Unit:   "EUR / mo",
		},
		model.Section{Title: "Today"},
		model.Stats{Items: []model.Stat{{Key: "today", Value: "4.1 kWh"}, {Key: "cost", Value: "1.23 EUR"}}},
		model.Bars{Values: hourly, Highlight: &highlight, Unit: "W"},
		model.Section{Title: "Utilization"},
		model.Meter{Label: "CPU", Frac: 0.22, Value: "22%"},
		model.Meter{Label: "RAM", Frac: 41.0 / 125, Value: "41/125G"},
		model.Meter{Label: "VRAM0", Frac: 22.1 / 24, Value: "22.1/24G", Tone: warn},
		model.Meter{Label: "VRAM1", Frac: 0.9 / 12, Value: "0.9/12G"},
	}

	return model.Snapshot{
		State:      model.StateWarn,
		StateLabel: "GPU0 VRAM 92°C",
		Glance:     model.Glance{Text: watts, Tone: warn},
		Width:      640,
		Pages: []model.Page{{ID: "main", Blocks: model.Blocks{
			model.Stats{Items: []model.Stat{
				{Key: "avg", Value: "402W"}, {Key: "now", Value: fmt.Sprintf("%.2f EUR/h", total/1000*0.34)},
				{Key: "rate", Value: "0.34 EUR/kWh"}, {Key: "peak until", Value: "22:00"},
			}},
			model.Split{Columns: []model.Blocks{left, right}},
		}}},
		Footer: "logging hourly",
		Menu:   []model.MenuItem{{ID: "open-report", Label: "Open power report"}},
		Issues: []model.Issue{{ID: "GPU0 RTX 4090", Text: "GPU0 RTX 4090 memory junction is at 92°C", Tone: warn}},
	}
}
