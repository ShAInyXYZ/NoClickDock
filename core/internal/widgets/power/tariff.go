package power

import (
	"fmt"
	"math"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/config"
)

// tariff is what a kWh costs, flat or by time of use. Every cost figure on
// the panel, in the log and in the report derives from it — and one figure
// never mixes a flat rate with time-of-use ones (the Python widget priced
// today's logged hours at the weekly blend and the hour in progress at the
// live rate, and added them).
type tariff struct {
	currency       string
	flat           float64
	tou            bool
	peak, off      float64
	hours          []config.Hours
	weekendOffPeak bool
}

func newTariff(c config.Power) tariff {
	t := tariff{currency: c.Currency, flat: c.PricePerKWh, tou: c.TOU, weekendOffPeak: c.TOUWeekendOffPeak}
	t.peak, t.off = t.flat, t.flat
	if c.PeakRate != nil && *c.PeakRate > 0 {
		t.peak = *c.PeakRate
	}
	if c.OffPeakRate != nil && *c.OffPeakRate > 0 {
		t.off = *c.OffPeakRate
	}
	t.hours, _ = config.ParseHours(c.PeakHours)
	return t
}

// inPeak: a time-of-use peak hour (a ≤ hour < b), weekends off-peak unless
// the tariff says otherwise.
func (t tariff) inPeak(at time.Time) bool {
	if !t.tou {
		return false
	}
	if t.weekendOffPeak && (at.Weekday() == time.Saturday || at.Weekday() == time.Sunday) {
		return false
	}
	for _, h := range t.hours {
		if h.From <= at.Hour() && at.Hour() < h.To {
			return true
		}
	}
	return false
}

// rate is the price of a kWh at a moment.
func (t tariff) rate(at time.Time) float64 {
	if !t.tou {
		return t.flat
	}
	if t.inPeak(at) {
		return t.peak
	}
	return t.off
}

// window is "peak" or "off-peak" now and when that changes: the first
// whole hour within the week whose peak-ness differs. Flat: "".
func (t tariff) window(now time.Time) (string, time.Time) {
	if !t.tou {
		return "", time.Time{}
	}
	peak := t.inPeak(now)
	name := "off-peak"
	if peak {
		name = "peak"
	}
	probe := now.Truncate(time.Hour)
	for i := 1; i <= 24*7; i++ {
		at := probe.Add(time.Duration(i) * time.Hour)
		if t.inPeak(at) != peak {
			return name, at
		}
	}
	return name, time.Time{}
}

// blended is the flat equivalent for projections: peak and off-peak hours
// in a week.
func (t tariff) blended() float64 {
	if !t.tou {
		return t.flat
	}
	days := 7.0
	if t.weekendOffPeak {
		days = 5
	}
	sum := 0
	for _, h := range t.hours {
		sum += max(0, h.To-h.From)
	}
	peakH := days * float64(sum)
	return (peakH*t.peak + (168-peakH)*t.off) / 168
}

// monthly is what a steady draw costs in 30 days, at the blended rate.
func (t tariff) monthly(watts float64) float64 {
	return watts * 24 * 30 / 1000 * t.blended()
}

// rateText: no decimals from 10 (JPY, HUF), else two unless the rate
// needs three (1.30, 0.29, 0.185).
func rateText(r float64) string {
	switch {
	case r >= 10:
		return fmt.Sprintf("%.0f", r)
	case math.Round(r*100) == math.Round(r*1000)/10:
		return fmt.Sprintf("%.2f", r)
	}
	return fmt.Sprintf("%.3f", r)
}

// money is a sum in the tariff's currency: whole units from 10.
func money(v float64) string {
	if v >= 10 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.2f", v)
}
