package sys

import (
	"testing"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

// The expectations are what shinywidget.py prints for the same input.
func TestFmtBytes(t *testing.T) {
	for in, want := range map[float64]string{
		0:                "0B",
		1023:             "1023B",
		1024:             "1.0K",
		1536:             "1.5K",
		10 * 1024:        "10K",
		9.96 * (1 << 30): "10.0G",
		412 * (1 << 30):  "412G",
		1.8 * (1 << 40):  "1.8T",
		-2048:            "-2.0K",
		10.5 * 1024:      "10K", // Python rounds half to even, and so does Go
	} {
		if got := FmtBytes(in); got != want {
			t.Errorf("FmtBytes(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFmtCount(t *testing.T) {
	for in, want := range map[int64]string{
		912: "912", 1_000: "1K", 348_400: "348K", 1_234_567: "1.2M", -5_000: "-5K",
	} {
		if got := FmtCount(in); got != want {
			t.Errorf("FmtCount(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestTimeAgo(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	for ago, want := range map[time.Duration]string{
		10 * time.Second:   "just now",
		3 * time.Minute:    "3m ago",
		2 * time.Hour:      "2h ago",
		5 * 24 * time.Hour: "5d ago",
	} {
		if got := TimeAgo(now.Add(-ago), now); got != want {
			t.Errorf("TimeAgo(-%s) = %q, want %q", ago, got, want)
		}
	}
	if got := TimeAgo(time.Time{}, now); got != "" {
		t.Errorf("TimeAgo(zero) = %q, want empty", got)
	}
}

func TestRatioTone(t *testing.T) {
	for frac, want := range map[float64]model.Tone{
		0.5: model.ToneOK, 0.849: model.ToneOK, 0.85: model.ToneWarn,
		0.919: model.ToneWarn, 0.92: model.ToneCrit, 1: model.ToneCrit,
	} {
		if got := RatioTone(frac, 0.85, 0.92); got != want {
			t.Errorf("RatioTone(%v) = %q, want %q", frac, got, want)
		}
	}
}
