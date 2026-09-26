package sensors

import (
	"os"
	"path/filepath"
	"testing"
)

// Every fixture is invented.

func TestNvidiaSMIKeepsMissingSensorsMissing(t *testing.T) {
	data, err := os.ReadFile("testdata/nvidia-smi.csv")
	if err != nil {
		t.Fatal(err)
	}
	gpus := ParseNvidiaSMI(string(data))
	if len(gpus) != 2 {
		t.Fatalf("%d cards, want 2", len(gpus))
	}
	a, b := gpus[0], gpus[1]
	if a.Index != 0 || a.Name != "NVIDIA GeForce RTX 4090" || *a.Power != 318.42 || *a.Limit != 450 || *a.Temp != 71 ||
		*a.VRAMUsed != 22630 || *a.VRAMTotal != 24564 || *a.Util != 87 || *a.Fan != 45 || a.Bus != "11:00.0" {
		t.Errorf("card 0: %+v", a)
	}
	if b.Power != nil || b.Limit != nil || b.Fan != nil || *b.Temp != 41 || b.Bus != "2a:00.0" {
		t.Errorf("card 1: [N/A] must stay unknown: %+v", b)
	}
	if ShortGPUName(a.Name) != "RTX 4090" || ShortGPUName("AMD Radeon RX 7900 XTX") != "RX 7900 XTX" || ShortGPUName("Quadro X") != "Quadro X" {
		t.Error("short names")
	}
}

func TestCPUUsageNeedsTwoReadings(t *testing.T) {
	idle1, total1, ok := ParseProcStat("cpu  1000 0 500 8000 500 0 0 0 0 0\ncpu0 1 2 3 4 5\n")
	if !ok || idle1 != 8500 || total1 != 10000 {
		t.Fatalf("first: %d %d %v", idle1, total1, ok)
	}
	// guest and guest_nice (the last two) are inside user and nice already.
	idle2, total2, _ := ParseProcStat("cpu  1600 0 700 8600 600 0 0 0 50 0")
	var c CPUUsage
	if c.next(idle1, total1) != nil {
		t.Error("a first reading says nothing")
	}
	// Δtotal 1500, Δidle 700 → 53.3 %
	if got := c.next(idle2, total2); got == nil || *got < 53.3 || *got > 53.4 {
		t.Errorf("usage %v", got)
	}
	if c.next(idle2, total2) != nil {
		t.Error("no time passed, no reading")
	}
	if _, _, ok := ParseProcStat("intr 1 2 3"); ok {
		t.Error("not the cpu line")
	}
}

func TestMeminfo(t *testing.T) {
	m, ok := ParseMeminfo("MemTotal:       131072000 kB\nMemFree:         1000000 kB\nMemAvailable:   88080384 kB\n")
	if !ok || m.TotalKB != 131072000 || m.UsedKB != 131072000-88080384 || int(m.Pct()) != 32 {
		t.Errorf("%+v %v", m, ok)
	}
	if _, ok := ParseMeminfo("MemTotal: 10 kB\n"); ok {
		t.Error("no MemAvailable, no reading")
	}
}

func hwmon(t *testing.T, root, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCPUTemperatureFinder(t *testing.T) {
	root := t.TempDir()
	hwmon(t, root, "hwmon0", map[string]string{"name": "nvme", "temp1_input": "41850", "temp1_label": "Composite"})
	hwmon(t, root, "hwmon1", map[string]string{"name": "acpitz", "temp1_input": "27800"})
	hwmon(t, root, "hwmon2", map[string]string{
		"name": "k10temp", "temp1_input": "54125", "temp1_label": "Tctl", "temp3_input": "48000", "temp3_label": "Tccd1",
	})
	in := CPUTempInput(root)
	if in != filepath.Join(root, "hwmon2", "temp1_input") {
		t.Fatalf("found %q", in)
	}
	if v := Milli(in); v == nil || *v != 54.125 {
		t.Errorf("temp %v", v)
	}

	// No known driver: a sensor labelled Package still counts.
	other := t.TempDir()
	hwmon(t, other, "hwmon0", map[string]string{"name": "acpitz", "temp1_input": "27800"})
	hwmon(t, other, "hwmon3", map[string]string{"name": "cputemp", "temp2_input": "61000", "temp2_label": "Package"})
	if in := CPUTempInput(other); in != filepath.Join(other, "hwmon3", "temp2_input") {
		t.Errorf("labelled fallback: %q", in)
	}
	if CPUTempInput(t.TempDir()) != "" || Milli("") != nil {
		t.Error("nothing to find")
	}
}
