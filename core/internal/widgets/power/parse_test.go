package power

import (
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/config"
)

// Every fixture is invented: no real monitor, serial, board or device.

func load(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// fakeEDID is a base EDID block: maker id, image size in cm, a name.
func fakeEDID(maker string, hCM, vCM byte, name string) []byte {
	b := make([]byte, 128)
	copy(b, "\x00\xff\xff\xff\xff\xff\xff\x00")
	id := uint16(maker[0]-64)<<10 | uint16(maker[1]-64)<<5 | uint16(maker[2]-64)
	b[8], b[9] = byte(id>>8), byte(id)
	b[21], b[22] = hCM, vCM
	d := b[72:90] // the second descriptor: the name
	d[3] = 0xfc
	copy(d[5:], []byte(fmt.Sprintf("%-13s", name+"\n")))
	return b
}

func xrandrBlock(b []byte) string {
	h := hex.EncodeToString(b)
	var out strings.Builder
	out.WriteString("\tEDID: \n")
	for i := 0; i < len(h); i += 32 {
		out.WriteString("\t\t" + h[i:i+32] + "\n")
	}
	return out.String()
}

func TestEDID(t *testing.T) {
	e, ok := parseEDID(fakeEDID("NCD", 60, 34, "Studio 27"))
	// √(60² + 34²) / 2.54 = 27.15" → (27.15 × 1.1 + 2) × 1.5 = 47.8 → 48
	if !ok || e.maker != "NCD" || e.model != "Studio 27" || int(e.inches) != 27 || e.estW != 48 {
		t.Errorf("%+v %v", e, ok)
	}
	if _, ok := parseEDID([]byte("short")); ok {
		t.Error("not an EDID")
	}
	noSize := fakeEDID("ABC", 0, 0, "Projector")
	if e, _ := parseEDID(noSize); e.estW != 0 || e.inches != 0 || e.model != "Projector" {
		t.Errorf("no size, no estimate: %+v", e)
	}
}

// Each output gets its own EDID — never paired by order (the Python bug).
func TestXrandrPairsEachOutputWithItsOwnEDID(t *testing.T) {
	out := "Screen 0: minimum 8 x 8, current 4480 x 1440, maximum 32767 x 32767\n" +
		"HDMI-0 connected 1920x1080+2560+180 (0x1d2) normal (normal left inverted right x axis y axis) 527mm x 296mm\n" +
		"\tIdentifier: 0x1b6\n" + xrandrBlock(fakeEDID("XYZ", 53, 30, "Office 24")) +
		"\tBorderDimensions: 4 \n\t\tsupported: 4\n" +
		"  1920x1080 (0x1d2) 148.500MHz +HSync +VSync *current +preferred\n" +
		"DP-0 connected primary 2560x1440+0+0 (0x1c8) normal (normal left inverted right x axis y axis) 597mm x 336mm\n" +
		xrandrBlock(fakeEDID("NCD", 60, 34, "Studio 27")) +
		"  2560x1440 (0x1c8) 241.500MHz +HSync -VSync *current +preferred\n" +
		"DP-1 disconnected (normal left inverted right x axis y axis)\n" +
		// A BMC's virtual output: connected, no active mode.
		"VGA-1 connected (normal left inverted right x axis y axis)\n" + xrandrBlock(fakeEDID("BMC", 0, 0, "Virtual")) +
		// An output that prints no EDID.
		"DP-2 connected 1280x1024+4480+0 (0x1f0) normal\n"
	got := parseXrandr(out)
	if len(got) != 3 {
		t.Fatalf("%d displays: %+v", len(got), got)
	}
	if d := got[0]; d.Port != "HDMI-0" || d.Res != "1920x1080" || d.Model != "Office 24" || d.EstW != 43 {
		t.Errorf("HDMI-0 %+v", d)
	}
	if d := got[1]; d.Port != "DP-0" || d.Res != "2560x1440" || d.Model != "Studio 27" || d.EstW != 48 {
		t.Errorf("DP-0 %+v", d)
	}
	if d := got[2]; d.Port != "DP-2" || d.Model != "" || d.EstW != 0 {
		t.Errorf("DP-2 %+v", d)
	}
}

func TestLsusbDmidecodeGDDR6RocmSMI(t *testing.T) {
	if got := parseLsusb(load(t, "lsusb.txt")); !reflect.DeepEqual(got, []string{
		"Example Corp. Wireless Mouse Receiver", "Sample Audio Interface", "Mechanical Keyboard",
	}) {
		t.Errorf("lsusb %q", got)
	}
	if got := parseDmidecodeMemory(load(t, "dmidecode-memory.txt")); got != "2x 32 GB · DDR5 · 4800 MT/s · Examplemem" {
		t.Errorf("memory %q", got)
	}
	if got := parseBaseboard("Base Board Information\n\tManufacturer: Example\n\tProduct Name: X670E EXAMPLE\n"); got != "X670E EXAMPLE" {
		t.Errorf("board %q", got)
	}
	if got := parseGDDR6(load(t, "gddr6.txt")); !reflect.DeepEqual(got, map[string]float64{"11:00.0": 92, "2a:00.0": 64}) {
		t.Errorf("gddr6 %v", got)
	}
	gpus := parseRocmSMI(load(t, "rocm-smi.json"))
	if len(gpus) != 2 || gpus[0].Name != "Radeon Graphics" || gpus[0].Power != nil || *gpus[0].Temp != 39 {
		t.Fatalf("rocm-smi %+v", gpus)
	}
	if g := gpus[1]; g.Index != 1 || *g.Power != 35 || *g.Limit != 300 || *g.VRAMUsed != 2048 || int(*g.VRAMTotal) != 16368 || g.Util != nil {
		t.Errorf("card1 %+v", g)
	}
}

func TestRAPLAcrossAWrap(t *testing.T) {
	r := rapl{max: 262_143_328_850}
	if r.next(262_000_000_000, 100) != nil {
		t.Error("a first reading measures nothing")
	}
	if w := r.next(262_100_000_000, 102); w == nil || *w != 50 {
		t.Errorf("50 W: %v", w)
	}
	// The counter wraps: 43,328,850 µJ to the top, then 56,671,150 after it.
	if w := r.next(56_671_150, 104); w == nil || *w != 50 {
		t.Errorf("across the wrap: %v", w)
	}
	if w := r.next(56_671_151, 104.1); w == nil || *w != 50 {
		t.Errorf("too soon to measure: the last reading again, not %v", w)
	}
}

func tariffTOU() tariff {
	peak, off := 0.40, 0.20
	c := config.Default().Widgets.Power
	c.TOU, c.PeakRate, c.OffPeakRate, c.PeakHours = true, &peak, &off, "6-13,15-22"
	return newTariff(c)
}

func TestTariff(t *testing.T) {
	loc := time.UTC
	at := func(day, h, m int) time.Time { return time.Date(2026, 9, day, h, m, 0, 0, loc) } // 21 Sep 2026 is a Monday
	tou := tariffTOU()
	for _, x := range []struct {
		at   time.Time
		peak bool
	}{
		{at(21, 5, 59), false}, {at(21, 6, 0), true}, {at(21, 12, 59), true}, {at(21, 13, 0), false},
		{at(21, 15, 0), true}, {at(21, 22, 0), false}, {at(26, 10, 0), false}, // Saturday
	} {
		if got := tou.inPeak(x.at); got != x.peak {
			t.Errorf("%s peak = %v", x.at.Format("Mon 15:04"), got)
		}
	}
	if tou.rate(at(21, 7, 0)) != 0.40 || tou.rate(at(21, 14, 0)) != 0.20 {
		t.Error("rates")
	}
	if name, next := tou.window(at(21, 7, 30)); name != "peak" || !next.Equal(at(21, 13, 0)) {
		t.Errorf("window %q %v", name, next)
	}
	// Friday evening: off-peak until Monday 06:00, across the weekend.
	if name, next := tou.window(at(25, 22, 30)); name != "off-peak" || !next.Equal(at(28, 6, 0)) {
		t.Errorf("weekend window %q %v", name, next)
	}
	// Blend: 5 days × 14 peak hours = 70 of 168.
	if b := tou.blended(); fmt.Sprintf("%.4f", b) != fmt.Sprintf("%.4f", (70*0.40+98*0.20)/168) {
		t.Errorf("blend %v", b)
	}
	flat := newTariff(config.Default().Widgets.Power)
	if name, _ := flat.window(at(21, 7, 0)); name != "" || flat.rate(at(21, 7, 0)) != 0.30 || fmt.Sprintf("%.6f", flat.monthly(100)) != "21.600000" {
		t.Error("flat")
	}
	for r, want := range map[float64]string{31: "31", 1.3: "1.30", 0.29: "0.29", 0.185: "0.185"} {
		if got := rateText(r); got != want {
			t.Errorf("rateText(%v) = %q, want %q", r, got, want)
		}
	}
}
