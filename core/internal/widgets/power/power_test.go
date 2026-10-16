package power

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/config"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sensors"
)

// 21 September 2026 is a Monday; 06–13 and 15–22 are peak.
func mon(h, m, s int) time.Time { return time.Date(2026, 9, 21, h, m, s, 0, time.Local) }

func TestTheLogWritesOneAlignedRowAnHour(t *testing.T) {
	dir := t.TempDir()
	l := newLogger(dir)
	rows := 0
	l.wrote = func() { rows++ }
	tou := tariffTOU()
	temp, pct := 60.0, 20.0
	// A poll every 30 s from 12:00 to 13:00 at 400 W.
	for at := mon(12, 0, 0); !at.After(mon(13, 0, 0)); at = at.Add(30 * time.Second) {
		if err := l.add(sample{at: at, total: 400, cpu: 100, gpu: 250, cpuTemp: &temp, cpuPct: &pct}, "hourly", tou); err != nil {
			t.Fatal(err)
		}
	}
	if rows != 1 {
		t.Fatalf("%d rows written, want 1", rows)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "2026-09", "2026-09-21.csv"))
	if err != nil {
		t.Fatal(err)
	}
	// 120 polls; 119 × 30 s at 400 W = 0.40 kWh, all peak at 0.40 = 0.16.
	want := "timestamp,avg_total_w,avg_cpu_w,avg_gpu_w,avg_cpu_temp,avg_gpu_temp,avg_cpu_pct,avg_ram_pct,samples,kwh_period,cost_period\r\n" +
		"2026-09-21 13:00:00,400.00,100.00,250.00,60.00,0.00,20.00,0.00,120,0.40,0.16\r\n"
	if string(raw) != want {
		t.Errorf("csv:\n%q\nwant:\n%q", raw, want)
	}

	// A suspend: the gap counts no energy, and the next row is the hour
	// the machine was on again, stamped at that hour's end.
	for _, at := range []time.Time{mon(16, 20, 0), mon(16, 20, 30), mon(17, 0, 0)} {
		l.add(sample{at: at, total: 200}, "hourly", tou)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "2026-09", "2026-09-21.csv"))
	lines := strings.Split(strings.TrimSpace(string(raw)), "\r\n")
	// 12:59:30 → 13:00 and 16:20 → 16:20:30 count; the 3 h 20 m gap does not.
	if len(lines) != 4 || !strings.HasPrefix(lines[2], "2026-09-21 14:00:00,400.00,") || !strings.HasPrefix(lines[3], "2026-09-21 17:00:00,200.00,") {
		t.Fatalf("rows after a suspend:\n%s", strings.Join(lines, "\n"))
	}
	if f := strings.Split(lines[3], ","); f[8] != "2" || f[9] != "0.00" {
		t.Errorf("16:20 → 16:20:30 at 200 W is 1.7 Wh: %s", lines[3])
	}

	// Logging off writes nothing more.
	l.add(sample{at: mon(18, 30, 0), total: 100}, "off", tou)
	l.add(sample{at: mon(19, 30, 0), total: 100}, "off", tou)
	if rows != 3 {
		t.Errorf("rows %d", rows)
	}
	if got := periodEnd(mon(23, 10, 0), "daily"); !got.Equal(time.Date(2026, 9, 22, 0, 0, 0, 0, time.Local)) {
		t.Errorf("daily %v", got)
	}
	if got := periodEnd(mon(10, 0, 0), "weekly"); !got.Equal(time.Date(2026, 9, 28, 0, 0, 0, 0, time.Local)) {
		t.Errorf("weekly %v", got)
	}
	if got := periodEnd(mon(10, 0, 0), "monthly"); !got.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)) {
		t.Errorf("monthly %v", got)
	}
}

func TestTodayPricesEveryHourAtItsRate(t *testing.T) {
	dir := t.TempDir()
	// The log: yesterday's last hour (not today), and 08:00–09:00.
	appendRow(dir, mon(0, 0, 0), strings.Split("2026-09-21 00:00:00,500.00,0,0,0,0,0,0,1800,0.50,0.10", ","))
	appendRow(dir, mon(9, 0, 0), strings.Split("2026-09-21 09:00:00,250.00,0,0,0,0,0,0,1800,0.25,0.07", ","))
	l := newLogger(dir)
	tou := tariffTOU()
	// This core ran 10:30–10:31 at 600 W.
	for at := mon(10, 30, 0); !at.After(mon(10, 31, 0)); at = at.Add(30 * time.Second) {
		l.add(sample{at: at, total: 600}, "hourly", tou)
	}
	d := l.today(mon(10, 31, 0), tou)
	if !d.has || len(d.watts) != 11 {
		t.Fatalf("%+v", d)
	}
	for h, w := range d.watts {
		switch h {
		case 8:
			if w != 250 {
				t.Errorf("08:00 %v", w)
			}
		case 10:
			if w != 600 {
				t.Errorf("10:00 %v", w)
			}
		default:
			if !math.IsNaN(w) {
				t.Errorf("%02d:00 was not measured: %v", h, w)
			}
		}
	}
	// 0.25 kWh at peak (08:30) + 10 Wh at peak.
	if got := fmt.Sprintf("%.4f %.4f", d.kwh, d.cost); got != "0.2600 0.1040" {
		t.Errorf("kwh cost %s", got)
	}
}

func TestLevelToastsWaitAMinute(t *testing.T) {
	w := &Widget{}
	r := func(watts float64) reading { return reading{total: &watts} }
	at := mon(12, 0, 0)
	steps := []struct {
		dt    time.Duration
		watts float64
		want  string
	}{
		{0, 180, ""},                // the first reading is not news
		{2 * time.Second, 260, ""},  // high load, not yet held
		{4 * time.Second, 240, ""},  // back to normal: nothing
		{6 * time.Second, 270, ""},  // high again: the minute starts over
		{50 * time.Second, 280, ""}, // 44 s held
		{67 * time.Second, 290, "High load at 290W"},
		{70 * time.Second, 300, ""}, // said once
	}
	for _, s := range steps {
		if got := w.levelNotice(r(s.watts), at.Add(s.dt)); got != s.want {
			t.Errorf("+%s %vW: %q, want %q", s.dt, s.watts, got, s.want)
		}
	}
}

func panelRows(blocks model.Blocks) []string {
	var out []string
	for _, b := range blocks {
		switch v := b.(type) {
		case model.Section:
			out = append(out, "# "+v.Title+" "+v.Meta)
		case model.Alert:
			out = append(out, "! "+v.Title+" "+string(v.Tone))
		case model.Stats:
			var s []string
			for _, i := range v.Items {
				s = append(s, i.Key+" "+i.Value)
			}
			out = append(out, "  "+strings.Join(s, " · "))
		case model.Table:
			var h []string
			for _, c := range v.Columns {
				h = append(h, c.Label)
			}
			out = append(out, "| "+strings.Join(h, " | "))
			for _, r := range v.Rows {
				if r.Group != "" {
					out = append(out, "|# "+r.Group+" "+r.Meta)
					continue
				}
				var c []string
				for _, cell := range r.Cells {
					if cell.Tone != "" {
						c = append(c, cell.Text+"!"+string(cell.Tone))
					} else {
						c = append(c, cell.Text)
					}
				}
				out = append(out, "| "+strings.Join(c, " | "))
			}
		case model.Note:
			out = append(out, "  ("+v.Text+")")
		case model.Donut:
			var s []string
			for _, g := range v.Segments {
				s = append(s, g.Label+" "+g.Display)
			}
			out = append(out, "  donut "+v.Center+" "+v.Unit+": "+strings.Join(s, ", "))
		case model.Bars:
			out = append(out, fmt.Sprintf("  bars %d", len(v.Values)))
		case model.Meter:
			out = append(out, fmt.Sprintf("  %s %s %s", v.Label, v.Value, v.Tone))
		case model.Split:
			for i, col := range v.Columns {
				out = append(out, fmt.Sprintf("-- column %d", i+1))
				out = append(out, panelRows(col)...)
			}
		}
	}
	return out
}

// steady is the mock without its breathing, derived afresh.
func steady(now time.Time) reading {
	r := mockReading(now)
	cpu, g0 := 96.0, 318.0
	r.cpuW, r.gpus[0].Power = &cpu, &g0
	r.gpuW, r.nvmeW, r.periph, r.total, r.alerts, r.monitors = 0, 0, 0, nil, nil, 0
	derive(&r, testCfg(), "")
	return r
}

func TestPanel(t *testing.T) {
	now := mon(10, 15, 0)
	s := render(steady(now))
	if s.State != model.StateWarn || s.StateLabel != "GPU0 VRAM 92°C" || s.Glance.Tone != model.ToneWarn {
		t.Errorf("state %q %q glance %+v", s.State, s.StateLabel, s.Glance)
	}
	if len(s.Issues) != 1 || s.Issues[0].ID != "gpu:0" || s.Issues[0].Text != "GPU0 RTX 4090 memory junction is at 92°C" {
		t.Errorf("issues %+v", s.Issues)
	}
	got := panelRows(s.Pages[0].Blocks)
	want := []string{
		"! GPU0 RTX 4090 memory junction is at 92°C warn",
		"  avg 402W · now 0.16 EUR/h · rate 0.34 EUR/kWh · peak until 13:00",
		"-- column 1",
		"| Draw |  | W / cap | core | vram",
		"| CPU | Ryzen 9 7950X | 96 | 61° | ",
		"| GPU0 | RTX 4090 | 318/450 | 71° | 92°!warn",
		"| GPU1 | RTX 3060 | 21/170 | 41° | 46°",
		"| Board | X670E board | ~30 |  | ",
		"|# Storage 2",
		"| NVMe | 990 PRO 2 TB | ~5 | 43° | ",
		"| NVMe | SN850X 1 TB | ~5 | 38° | ",
		"|# Displays own PSU",
		"| DP-0 | 27″ 2560×1440 | ~48 |  | ",
		"| HDMI-0 | 24″ 1920×1080 | ~43 |  | ",
		"|# USB ",
		"| USB | 3 devices | ~5 |  | ",
		"  (keyboard · mouse receiver · audio interface)",
		"-- column 2",
		"# Cost 346 kWh / mo",
		"  donut 93 EUR / mo: GPU 66 EUR, CPU 19 EUR, Board 6 EUR, NVMe 2 EUR, USB 1 EUR",
		"# Today ",
		"  today 4.1 kWh · cost 1.23 EUR",
		"  bars 11",
		"# Utilization ",
		"  CPU 22% ",
		"  RAM 41/125G ",
		"  VRAM0 22.1/24G warn",
		"  VRAM1 0.9/12G ",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("panel:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if s.Footer != "logging hourly" {
		t.Errorf("footer %q", s.Footer)
	}
	if n := len([]rune(s.Glance.Text)); n > 5 {
		t.Errorf("glance %q", s.Glance.Text)
	}
}

// A crit reading raises the state to crit (A20): the lamp, the label and
// the toast agree with the red cell.
func TestACritTemperatureIsCrit(t *testing.T) {
	r := mockReading(mon(10, 15, 0))
	hot := 88.0
	r.gpus[1].Temp = &hot
	r.gpuW, r.nvmeW, r.periph, r.total, r.alerts, r.monitors = 0, 0, 0, nil, nil, 0
	derive(&r, testCfg(), "")
	s := render(r)
	if s.State != model.StateCrit || s.StateLabel != "GPU1 core 88°C" || s.Issues[0].Tone != model.ToneCrit {
		t.Errorf("state %q %q issues %+v", s.State, s.StateLabel, s.Issues)
	}
	if model.RaiseToWorst(&s) {
		t.Error("the writer's own state is already the worst signal")
	}
}

// Estimated CPU: in the table with "~", in the donut, said in words; with
// no reading at all, words instead of zeros.
func TestEstimatesAndNothing(t *testing.T) {
	usage := 50.0
	r := reading{now: mon(10, 0, 0), os: "linux", tariff: tariffTOU(), logging: "hourly", overhead: 30, cpuUsage: &usage,
		gpus: []gpu{{GPU: sensors.GPU{Index: 0, Name: "NVIDIA GeForce RTX 3060"}}}}
	derive(&r, testCfg(), "only root may read the CPU's RAPL counter on this kernel")
	if !r.cpuEstimated || *r.cpuW != 105 || *r.total != 135 {
		t.Fatalf("estimate %v total %v", *r.cpuW, *r.total)
	}
	r.donut = parts{cpu: *r.cpuW, total: *r.total}
	s := render(r)
	got := strings.Join(panelRows(s.Pages[0].Blocks), "\n")
	for _, want := range []string{
		"| CPU | — | ~105 | ",
		"| GPU | RTX 3060 | — | ", // a card without a power reading is not 0 W
		"(CPU power is estimated from its load: only root may read the CPU's RAPL counter on this kernel)",
		"CPU 21 EUR", // the estimate is in the donut
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if s.Footer != "logging hourly · CPU estimated" || s.StateLabel != "normal · 135W" {
		t.Errorf("footer %q label %q", s.Footer, s.StateLabel)
	}
	none := render(reading{now: mon(10, 0, 0), tariff: tariffTOU(), logging: "off"})
	if none.State != model.StateIdle || none.StateLabel != "no readings" || none.Glance.Text != "—" {
		t.Errorf("no readings: %q %q", none.StateLabel, none.Glance.Text)
	}
}

func testCfg() config.Power { return config.Default().Widgets.Power }

// Design M5 r1: the alert names the card's worse sensor (a tie goes to the
// core), so the cell, the lamp, the label and the toast agree.
func TestTheWorseSensorIsNamed(t *testing.T) {
	for _, x := range []struct {
		core, vram float64
		label      string
		state      model.State
	}{
		{92, 92, "GPU0 core 92°C", model.StateCrit}, // core crit (> 85), VRAM warn (< 94)
		{75, 96, "GPU0 VRAM 96°C", model.StateCrit}, // VRAM crit, core fine
		{82, 91, "GPU0 core 82°C", model.StateWarn}, // both warn: the core
		{60, 91, "GPU0 VRAM 91°C", model.StateWarn},
	} {
		r := steady(mon(10, 15, 0))
		r.gpus[0].Temp, r.gpus[0].VRAMTemp = &x.core, &x.vram
		r.gpuW, r.nvmeW, r.periph, r.total, r.alerts, r.monitors = 0, 0, 0, nil, nil, 0
		derive(&r, testCfg(), "")
		s := render(r)
		if s.StateLabel != x.label || s.State != x.state || s.Issues[0].Tone != model.Tone(x.state) {
			t.Errorf("core %v vram %v: %q %q %+v", x.core, x.vram, s.StateLabel, s.State, s.Issues)
		}
	}
}
