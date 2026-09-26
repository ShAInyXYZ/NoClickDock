package power

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sensors"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

// Temperature tones start where the thermal alerts do, so a toned cell,
// the lamp, the label and the toast always agree (DESIGN A20).
func coreTone(c float64) model.Tone { return tempTone(c, coreAlertC, 85) }
func cpuTone(c float64) model.Tone  { return tempTone(c, cpuAlertC, 95) }
func vramTone(c float64) model.Tone { return tempTone(c, vramAlertC, 94) }

func tempTone(c, warnAt, critAbove float64) model.Tone {
	switch {
	case c > critAbove:
		return model.ToneCrit
	case c >= warnAt:
		return model.ToneWarn
	}
	return model.ToneNone
}

// useTone: RAM and VRAM warn at 90 %, crit at 97 % (Q4). CPU load carries
// none: load is not a fault.
func useTone(pct float64) model.Tone {
	switch {
	case pct >= 97:
		return model.ToneCrit
	case pct >= 90:
		return model.ToneWarn
	}
	return model.ToneNone
}

func temp(v *float64, tone func(float64) model.Tone) model.Cell {
	if v == nil {
		return model.Cell{}
	}
	return model.Cell{Text: fmt.Sprintf("%.0f°", *v), Tone: tone(*v)}
}

func render(r reading) model.Snapshot {
	s := model.Snapshot{
		Width:  640,
		Menu:   []model.MenuItem{{ID: "open-report", Label: "Open power report"}},
		Glance: model.Glance{Text: "—"},
	}
	cur := r.tariff.currency

	// State: a thermal alert, else the load. Crit when its reading is.
	lv := level(r.total)
	switch {
	case r.total == nil:
		s.State, s.StateLabel = model.StateIdle, "no readings"
	default:
		s.State, s.StateLabel = model.StateOK, fmt.Sprintf("%s · %.0fW", lv, *r.total)
		s.Glance = model.Glance{Text: fmt.Sprintf("%.0fW", *r.total)}
	}
	alerts := append([]alert(nil), r.alerts...)
	sort.SliceStable(alerts, func(i, j int) bool { return severity(alerts[i].tone) > severity(alerts[j].tone) })
	var top model.Blocks
	for i, a := range alerts {
		tone := a.tone
		if tone == model.ToneNone {
			tone = model.ToneWarn
		}
		if i == 0 {
			s.State, s.StateLabel = model.State(tone), a.label
			s.Glance.Tone = tone
		}
		s.Issues = append(s.Issues, model.Issue{ID: a.id, Text: a.text, Tone: tone})
		// Problems first (A19): what the header says, before anything else.
		top = append(top, model.Alert{Title: a.text, Tone: tone})
	}

	// The tariff every figure derives from, on the panel.
	head := model.Stats{}
	if r.total != nil {
		head.Items = append(head.Items,
			model.Stat{Key: "avg", Value: fmt.Sprintf("%.0fW", r.avg)},
			model.Stat{Key: "now", Value: fmt.Sprintf("%s %s/h", money(*r.total/1000*r.tariff.rate(r.now)), cur)})
	}
	head.Items = append(head.Items, model.Stat{Key: "rate", Value: fmt.Sprintf("%s %s/kWh", rateText(r.tariff.rate(r.now)), cur)})
	if name, next := r.tariff.window(r.now); name != "" && !next.IsZero() {
		head.Items = append(head.Items, model.Stat{Key: name + " until", Value: sys.Clock(next, r.now)})
	}

	s.Pages = []model.Page{{ID: "main", Blocks: append(top,
		head,
		model.Split{Columns: []model.Blocks{left(r), right(r)}},
	)}}

	s.Footer = "logging " + r.logging
	if r.cpuEstimated {
		s.Footer += " · CPU estimated"
	}
	return s
}

// left is one table — draw, storage, displays, USB — so every name and
// number lines up under one set of columns.
func left(r reading) model.Blocks {
	vramCol := false
	for _, g := range r.gpus {
		vramCol = vramCol || g.VRAMTemp != nil
	}
	cols := []model.Column{
		{Label: "Draw", Align: "left", Mono: true},
		{Label: "", Align: "left"},
		{Label: "W / cap", Align: "right", Mono: true},
		{Label: "core", Align: "right", Mono: true},
	}
	if vramCol {
		cols = append(cols, model.Column{Label: "vram", Align: "right", Mono: true})
	}
	row := func(cells ...model.Cell) model.TableRow {
		for len(cells) < len(cols) {
			cells = append(cells, model.Cell{})
		}
		return model.TableRow{Cells: cells[:len(cols)]}
	}
	txt := func(s string) model.Cell { return model.Cell{Text: s} }
	dash := func(s string) string {
		if s == "" {
			return "—"
		}
		return s
	}

	var rows []model.TableRow
	cpuW := "—"
	if r.cpuW != nil {
		cpuW = fmt.Sprintf("%.0f", *r.cpuW)
		if r.cpuEstimated {
			cpuW = "~" + cpuW
		}
	}
	rows = append(rows, row(txt("CPU"), txt(dash(r.static.CPUModel)), txt(cpuW), temp(r.cpuTemp, cpuTone)))
	for _, g := range r.gpus {
		w := "—"
		if g.Power != nil {
			w = fmt.Sprintf("%.0f", *g.Power)
			if g.Limit != nil && *g.Limit > 0 {
				w += fmt.Sprintf("/%.0f", *g.Limit)
			}
		}
		rows = append(rows, row(txt(gpuTag(g, len(r.gpus))), txt(sensors.ShortGPUName(g.Name)), txt(w), temp(g.Temp, coreTone), temp(g.VRAMTemp, vramTone)))
	}
	rows = append(rows, row(txt("Board"), txt(dash(r.static.Board)), txt(fmt.Sprintf("~%.0f", r.overhead))))
	if len(r.nvme) > 0 {
		rows = append(rows, model.Group("Storage", fmt.Sprint(len(r.nvme))))
		for _, n := range r.nvme {
			rows = append(rows, row(txt("NVMe"), txt(n.Model), txt(fmt.Sprintf("~%d", nvmeWatts)), temp(n.Temp, func(float64) model.Tone { return model.ToneNone })))
		}
	}
	if len(r.static.Displays) > 0 {
		rows = append(rows, model.Group("Displays", "own PSU"))
		for _, d := range r.static.Displays {
			// Size and mode, as the column has room for; the model name
			// only when the EDID gave no size.
			var name []string
			switch {
			case d.Inches > 0:
				name = append(name, fmt.Sprintf("%.0f″", d.Inches))
			case d.Model != "":
				name = append(name, d.Model)
			case d.Maker != "":
				name = append(name, d.Maker)
			}
			name = append(name, strings.ReplaceAll(d.Res, "x", "×"))
			w := "—"
			if d.EstW > 0 {
				w = fmt.Sprintf("~%.0f", d.EstW)
			}
			rows = append(rows, row(txt(d.Port), txt(strings.Join(name, " ")), txt(w)))
		}
	}
	out := model.Blocks{}
	if len(r.static.USB) > 0 {
		rows = append(rows, model.Group("USB", ""))
		rows = append(rows, row(txt("USB"), txt(fmt.Sprintf("%d %s", len(r.static.USB), plural(len(r.static.USB), "device", "devices"))), txt(fmt.Sprintf("~%.0f", r.periph))))
	}
	out = append(out, model.Table{Columns: cols, Rows: rows})
	if len(r.static.USB) > 0 {
		out = append(out, model.Note{Text: strings.Join(r.static.USB, " · ")})
	}
	if r.cpuEstimated {
		note := r.cpuNote
		if r.os == "windows" {
			note = "CPU package power needs LibreHardwareMonitor on Windows"
		}
		out = append(out, model.Note{Text: note, Tone: model.ToneIdle})
	}
	return out
}

// right: the month's cost by component, today, and what is in use.
func right(r reading) model.Blocks {
	cur := r.tariff.currency
	var out model.Blocks
	d := r.donut
	if d.total > 0 {
		segs := []model.Segment{}
		for _, p := range []struct {
			label string
			w     float64
		}{{"CPU", d.cpu}, {"GPU", d.gpu}, {"Board", r.overhead}, {"NVMe", d.nvme}, {"USB", d.periph}} {
			if p.w > 0 {
				segs = append(segs, model.Segment{Label: p.label, Value: p.w, Display: fmt.Sprintf("%.0f %s", r.tariff.monthly(p.w), cur)})
			}
		}
		sort.SliceStable(segs, func(i, j int) bool { return segs[i].Value > segs[j].Value })
		out = append(out,
			model.Section{Title: "Cost", Meta: fmt.Sprintf("%.0f kWh / mo", d.total*24*30/1000)},
			model.Donut{Segments: segs, Center: fmt.Sprintf("%.0f", r.tariff.monthly(d.total)), Unit: cur + " / mo"},
		)
	}
	if t := r.today; t.has {
		hl := len(t.watts) - 1
		out = append(out,
			model.Section{Title: "Today"},
			model.Stats{Items: []model.Stat{{Key: "today", Value: fmt.Sprintf("%.1f kWh", t.kwh)}, {Key: "cost", Value: money(t.cost) + " " + cur}}},
			model.Bars{Values: t.watts, Highlight: &hl, Unit: "W"},
		)
	}
	var meters model.Blocks
	if r.cpuUsage != nil {
		meters = append(meters, model.Meter{Label: "CPU", Frac: clamp(*r.cpuUsage / 100), Value: fmt.Sprintf("%.0f%%", *r.cpuUsage)})
	}
	if m := r.mem; m != nil {
		gib := func(kb uint64) float64 { return float64(kb) / 1048576 }
		meters = append(meters, model.Meter{Label: "RAM", Frac: clamp(m.Pct() / 100), Tone: useTone(m.Pct()),
			Value: fmt.Sprintf("%.0f/%.0fG", gib(m.UsedKB), gib(m.TotalKB))})
	}
	for _, g := range r.gpus {
		if g.VRAMTotal == nil || *g.VRAMTotal <= 0 || g.VRAMUsed == nil {
			continue
		}
		pct := *g.VRAMUsed / *g.VRAMTotal * 100
		label := "VRAM"
		if len(r.gpus) > 1 {
			label = fmt.Sprintf("VRAM%d", g.Index)
		}
		meters = append(meters, model.Meter{Label: label, Frac: clamp(pct / 100), Tone: useTone(pct),
			Value: fmt.Sprintf("%.1f/%.0fG", *g.VRAMUsed/1024, *g.VRAMTotal/1024)})
	}
	if len(meters) > 0 {
		out = append(out, model.Section{Title: "Utilization"})
		out = append(out, meters...)
	}
	return out
}

func clamp(f float64) float64 { return max(0, min(1, f)) }

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
