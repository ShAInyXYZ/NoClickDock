package power

import (
	"math"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/config"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sensors"
)

// Mock is an invented workstation, drawn by the real render: two GPUs, the
// first with a memory junction running hot (the widget's warning), on a
// time-of-use tariff. Every model name here is illustrative. Live, the
// draw breathes a little.
type Mock struct{}

func (Mock) Meta() hub.Meta { return hub.Meta{ID: "power", Title: "Power", Order: 60, PollSecs: 2} }

// hourly is an invented day of average watts per clock hour.
var hourly = []float64{182, 176, 171, 168, 170, 174, 210, 265, 318, 344, 372, 389, 402, 356, 398, 380, 371, 366, 340, 310, 280, 240, 205, 190}

func f(v float64) *float64 { return &v }

func mockReading(now time.Time) reading {
	peak, off := 0.34, 0.22
	cfg := config.Default().Widgets.Power
	cfg.TOU, cfg.PeakRate, cfg.OffPeakRate = true, &peak, &off
	breath := 18 * math.Sin(float64(now.Unix())/7)
	r := reading{
		now: now, os: "linux", tariff: newTariff(cfg), logging: "hourly", overhead: cfg.OverheadWatts,
		cpuW: f(96 + breath/3), cpuTemp: f(61), cpuUsage: f(22),
		mem: &sensors.Memory{UsedKB: 41 * 1048576, TotalKB: 125 * 1048576},
		gpus: []gpu{
			{GPU: sensors.GPU{Index: 0, Name: "NVIDIA GeForce RTX 4090", Power: f(318 + breath), Limit: f(450), Temp: f(71),
				VRAMUsed: f(22.1 * 1024), VRAMTotal: f(24 * 1024)}, VRAMTemp: f(92)},
			{GPU: sensors.GPU{Index: 1, Name: "NVIDIA GeForce RTX 3060", Power: f(21), Limit: f(170), Temp: f(41),
				VRAMUsed: f(0.9 * 1024), VRAMTotal: f(12 * 1024)}, VRAMTemp: f(46)},
		},
		nvme: []nvme{{Model: "990 PRO 2 TB", Temp: f(43)}, {Model: "SN850X 1 TB", Temp: f(38)}},
		static: staticInfo{
			CPUModel: "Ryzen 9 7950X", Board: "X670E board",
			Displays: []display{
				{Port: "DP-0", Res: "2560x1440", Model: "Studio 27", Inches: 27, EstW: 48},
				{Port: "HDMI-0", Res: "1920x1080", Model: "Office 24", Inches: 24, EstW: 43},
			},
			USB: []string{"keyboard", "mouse receiver", "audio interface"},
		},
	}
	derive(&r, cfg, "")
	r.avg = 402
	r.donut = parts{cpu: 96, gpu: 339, nvme: 10, periph: 5, total: 480}
	h := now.Hour()
	r.today = today{kwh: 4.1, cost: 1.23, watts: append([]float64(nil), hourly[:h+1]...), has: true}
	return r
}

func (Mock) Mock(now time.Time) model.Snapshot {
	return render(mockReading(now))
}

// Variants: a machine where nothing could be read.
func (Mock) Variants(now time.Time) map[string]model.Snapshot {
	r := reading{now: now, os: "linux", tariff: newTariff(config.Default().Widgets.Power), logging: "off"}
	return map[string]model.Snapshot{"no-readings": render(r)}
}
