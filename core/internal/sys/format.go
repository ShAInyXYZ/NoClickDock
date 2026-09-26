package sys

import (
	"fmt"
	"math"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

// The shared formatting helpers, ported from shinywidget.py so every panel
// reads the same way it did.

var byteUnits = []struct {
	suffix string
	size   float64
}{{"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10}}

// FmtBytes scales a byte count to its largest sensible binary unit: 1536 →
// "1.5K", 412 GiB → "412G". One decimal only below ten, where it matters.
func FmtBytes(n float64) string {
	for _, u := range byteUnits {
		if math.Abs(n) >= u.size {
			v := n / u.size
			if v < 10 {
				return fmt.Sprintf("%.1f%s", v, u.suffix)
			}
			return fmt.Sprintf("%.0f%s", v, u.suffix)
		}
	}
	return fmt.Sprintf("%.0fB", n)
}

// FmtCount is a compact integer: 1.2M, 348K, 912.
func FmtCount(n int64) string {
	switch a := math.Abs(float64(n)); {
	case a >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case a >= 1_000:
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	}
	return fmt.Sprint(n)
}

// TimeAgo is a short humanised age: "just now", "3m ago", "2h ago", "5d ago".
func TimeAgo(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	secs := int(now.Sub(t).Seconds())
	switch {
	case secs < 60:
		return "just now"
	case secs < 3600:
		return fmt.Sprintf("%dm ago", secs/60)
	case secs < 86400:
		return fmt.Sprintf("%dh ago", secs/3600)
	}
	return fmt.Sprintf("%dd ago", secs/86400)
}

// RatioTone is the tone of a 0..1 fill: quiet until it matters.
func RatioTone(frac, warn, crit float64) model.Tone {
	switch {
	case frac >= crit:
		return model.ToneCrit
	case frac >= warn:
		return model.ToneWarn
	}
	return model.ToneOK
}

// Clamp01 bounds a fraction for a meter.
func Clamp01(f float64) float64 {
	return math.Max(0, math.Min(1, f))
}

// Clock is the one way a panel says when (DESIGN "One time format"):
// 24-hour "16:40" within the day, "Mon 09:00" beyond it, both in local
// time. The zero time is unknown: "".
func Clock(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	t, now = t.Local(), now.Local()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	return t.Format("Mon 15:04")
}
