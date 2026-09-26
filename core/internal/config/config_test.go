package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenWithoutFileUsesDefaultsAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); got != Default() {
		t.Fatalf("got %+v, want defaults", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); !os.IsNotExist(err) {
		t.Fatal("opening must not create config.json")
	}
}

func TestPatchMergesValidatesAndPersists(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)

	cfg, err := s.Patch(json.RawMessage(`{"dock":{"orientation":"horizontal","position":{"monitor":"DP-9","edge":"left","along":0.5,"x":40,"y":2300}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Dock.Orientation != "horizontal" || cfg.Dock.Density != "normal" || !cfg.Dock.Readouts {
		t.Fatalf("patch lost or clobbered fields: %+v", cfg.Dock)
	}
	if p := cfg.Dock.Position; p == nil || p.X != 40 || p.Y != 2300 || p.Monitor != "DP-9" || p.Edge != "left" || *p.Along != 0.5 {
		t.Fatalf("position = %+v", p)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Get().Dock; got.Orientation != "horizontal" || got.Position == nil {
		t.Fatalf("not persisted: %+v", got)
	}

	// null removes a key: the shell forgets the position and places anew.
	cfg, err = s.Patch(json.RawMessage(`{"dock":{"position":null}}`))
	if err != nil || cfg.Dock.Position != nil {
		t.Fatalf("null did not delete: %+v, %v", cfg.Dock.Position, err)
	}
}

func TestInvalidPatchChangesNothing(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	for _, patch := range []string{
		`{"dock":{"orientation":"diagonal"}}`,
		`{"dock":{"orientaton":"horizontal"}}`, // a typo is an error, not ignored
		`{"dock":{"readouts":"yes"}}`,
		`{"dock":{"position":{"x":1,"y":2,"edge":"middle"}}}`,
		`{"dock":{"position":{"x":1,"y":2,"along":1.5}}}`,
		`not json`,
	} {
		if _, err := s.Patch(json.RawMessage(patch)); err == nil {
			t.Errorf("patch %s was accepted", patch)
		}
	}
	if s.Get() != Default() {
		t.Fatalf("a rejected patch changed the config: %+v", s.Get())
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); !os.IsNotExist(err) {
		t.Fatal("a rejected patch wrote the file")
	}
}

func TestBrokenFileIsKeptAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte(`{"dock":`), 0o600)

	s, err := Open(dir)
	if err == nil || !strings.Contains(err.Error(), "config.json.bad") {
		t.Fatalf("want an error naming the backup, got %v", err)
	}
	if s.Get() != Default() {
		t.Fatal("a broken file must fall back to defaults")
	}
	if data, _ := os.ReadFile(path + ".bad"); string(data) != `{"dock":` {
		t.Fatalf("backup = %q", data)
	}
}

func TestWidgetOptionsAreValidated(t *testing.T) {
	s, _ := Open(t.TempDir())
	cfg := s.Get()
	if c := cfg.Widgets.ComfyUI; c.Host != "127.0.0.1" || c.Port != 8188 {
		t.Errorf("comfyui defaults %+v", c)
	}
	if p := cfg.Widgets.Power; p.PricePerKWh != 0.30 || p.Currency != "EUR" || p.LogInterval != "hourly" || p.PeakHours != "6-13,15-22" || !p.TOUWeekendOffPeak || p.PeakRate != nil {
		t.Errorf("power defaults %+v", p)
	}
	for _, bad := range []string{
		`{"widgets":{"comfyui":{"port":0}}}`,
		`{"widgets":{"comfyui":{"host":"http://example"}}}`,
		`{"widgets":{"comfyui":{"host":""}}}`,
		`{"widgets":{"power":{"log_interval":"yearly"}}}`,
		`{"widgets":{"power":{"peak_hours":"13-6"}}}`,
		`{"widgets":{"power":{"peak_hours":"6to13"}}}`,
		`{"widgets":{"power":{"currency":"EURSSS"}}}`,
		`{"widgets":{"power":{"price_per_kwh":-1}}}`,
		`{"widgets":{"power":{"poll_interval":0}}}`,
		`{"widgets":{"power":{"peak_rate":-0.1}}}`,
	} {
		if _, err := s.Patch(json.RawMessage(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	cfg, err := s.Patch(json.RawMessage(`{"widgets":{"comfyui":{"host":"::1","port":8000},"power":{"tou":true,"peak_rate":0.4,"peak_hours":"7-9, 17-21"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Widgets.ComfyUI.Port != 8000 || !cfg.Widgets.Power.TOU || *cfg.Widgets.Power.PeakRate != 0.4 || cfg.Widgets.Power.Currency != "EUR" {
		t.Errorf("patched %+v", cfg.Widgets)
	}
	if h, _ := ParseHours("7-9, 17-21"); len(h) != 2 || h[1] != (Hours{17, 21}) {
		t.Errorf("hours %+v", h)
	}
}
