package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWatchIsNullAnEmptyListOrHosts(t *testing.T) {
	s, _ := Open(t.TempDir())
	if s.Get().Widgets.Tailscale.Watch != nil {
		t.Fatal("the default watches peers seen online (null)")
	}
	cfg, err := s.Patch(json.RawMessage(`{"widgets":{"tailscale":{"watch":["fileserver","build-agent"]}}}`))
	if err != nil || !reflect.DeepEqual(*cfg.Widgets.Tailscale.Watch, []string{"fileserver", "build-agent"}) {
		t.Fatalf("%v %+v", err, cfg.Widgets)
	}
	cfg, _ = s.Patch(json.RawMessage(`{"widgets":{"tailscale":{"watch":[]}}}`))
	if w := cfg.Widgets.Tailscale.Watch; w == nil || len(*w) != 0 {
		t.Fatalf("an empty list watches nothing, and is not null: %v", w)
	}
	cfg, _ = s.Patch(json.RawMessage(`{"widgets":{"tailscale":{"watch":null}}}`))
	if cfg.Widgets.Tailscale.Watch != nil {
		t.Fatal("null goes back to watching what was seen online")
	}
	if _, err := s.Patch(json.RawMessage(`{"widgets":{"tailscale":{"watch":[""]}}}`)); err == nil {
		t.Fatal("an empty host name is refused")
	}
	if _, err := s.Patch(json.RawMessage(`{"widgets":{"tailscale":{"pins":[]}}}`)); err == nil {
		t.Fatal("an unknown key is refused")
	}
}

func legacy(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tailscale.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLegacyWatchListIsMigratedOnceAndOnlyRead(t *testing.T) {
	old := `{"watch": ["FileServer", "build-agent", 7]}`
	src := legacy(t, old)
	dir := t.TempDir()
	s, _ := Open(dir)
	moved, err := s.MigrateLegacy(src)
	if err != nil || !moved {
		t.Fatalf("moved %v, %v", moved, err)
	}
	if w := s.Get().Widgets.Tailscale.Watch; w == nil || !reflect.DeepEqual(*w, []string{"FileServer", "build-agent"}) {
		t.Fatalf("watch %v", w)
	}
	if b, _ := os.ReadFile(filepath.Join(src, "tailscale.json")); !bytes.Equal(b, []byte(old)) {
		t.Fatal("the legacy file must never change")
	}

	// config.json exists now: never again, even if the old list changes.
	src2 := legacy(t, `{"watch": ["someone-else"]}`)
	s, _ = Open(dir)
	if moved, _ := s.MigrateLegacy(src2); moved {
		t.Fatal("migrated twice")
	}
}

func TestLegacyRegistrationIsNotAWatchList(t *testing.T) {
	for _, content := range []string{
		`{"pid": 4242, "name": "tailscale", "xid": 12345}`, // the Python runtime's registration
		`{"watch": "fileserver"}`,
		`not json`,
	} {
		s, _ := Open(t.TempDir())
		if moved, err := s.MigrateLegacy(legacy(t, content)); moved || err != nil {
			t.Errorf("%s: moved %v, %v", content, moved, err)
		}
		if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
			t.Errorf("%s: config.json written", content)
		}
	}
	s, _ := Open(t.TempDir())
	if moved, _ := s.MigrateLegacy(t.TempDir()); moved {
		t.Error("no legacy file, nothing moved")
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Every legacy file an invented Python install would have left.
func TestEverythingComesOverOnceAndIsOnlyRead(t *testing.T) {
	root := t.TempDir()
	l := Legacy{
		StatusWidgets: filepath.Join(root, "status-widgets"),
		PowerMonitor:  filepath.Join(root, "power-monitor"),
		Autostart:     filepath.Join(root, "autostart"),
	}
	files := map[string]string{
		filepath.Join(l.StatusWidgets, "bar.conf"):       `{"orientation": "horizontal", "size": "s", "x": 1200, "y": 40}`,
		filepath.Join(l.StatusWidgets, "tailscale.json"): `{"watch": ["fileserver"]}`,
		filepath.Join(l.PowerMonitor, "config.json"): `{"price_per_kwh": 1.3, "currency": "PLN", "overhead_watts": 45,
			"peripheral_watts": 5, "poll_interval": 2.0, "log_interval": "hourly", "tou": true, "peak_rate": 1.45,
			"off_peak_rate": null, "peak_hours": "13-6", "tou_weekend_offpeak": false, "monitor_watts_each": 30}`,
		filepath.Join(l.Autostart, "power-monitor.desktop"):            "[Desktop Entry]\nExec=/usr/bin/python3 /opt/x/power-monitor.py\n",
		filepath.Join(l.Autostart, "comfyui-status-checker.desktop"):   "[Desktop Entry]\nExec=/usr/bin/python3 /opt/x/comfyui-status-checker.py --host render-box.example --port 8000\n",
		filepath.Join(l.Autostart, "docker-status-checker.desktop"):    "[Desktop Entry]\nExec=x\nX-GNOME-Autostart-enabled=false\n",
		filepath.Join(l.Autostart, "tailscale-status-checker.desktop"): "[Desktop Entry]\nExec=x\nHidden=true\n",
		filepath.Join(l.Autostart, "unrelated.desktop"):                "[Desktop Entry]\nExec=x\n",
	}
	for path, content := range files {
		write(t, path, content)
	}
	s, _ := Open(t.TempDir())
	brought, err := s.Migrate(l)
	if err != nil || len(brought) != 5 {
		t.Fatalf("%v %q", err, brought)
	}
	c := s.Get()
	if c.Dock.Orientation != "horizontal" || c.Dock.Density != "compact" || c.Dock.Position == nil || c.Dock.Position.X != 1200 || c.Dock.Position.Y != 40 {
		t.Errorf("dock %+v %+v", c.Dock, c.Dock.Position)
	}
	if w := c.Widgets.Tailscale.Watch; w == nil || (*w)[0] != "fileserver" {
		t.Errorf("watch %v", w)
	}
	p := c.Widgets.Power
	// The malformed peak hours are left out; everything else comes over.
	if p.PricePerKWh != 1.3 || p.Currency != "PLN" || p.OverheadWatts != 45 || !p.TOU || *p.PeakRate != 1.45 ||
		p.OffPeakRate != nil || p.PeakHours != "6-13,15-22" || p.TOUWeekendOffPeak || p.PollInterval != 2 {
		t.Errorf("power %+v", p)
	}
	w := c.Widgets
	if !w.Power.Enabled || !w.ComfyUI.Enabled || w.Docker.Enabled || w.Tailscale.Enabled || w.Claude.Enabled {
		t.Errorf("enabled: power %v comfyui %v docker %v tailscale %v claude %v",
			w.Power.Enabled, w.ComfyUI.Enabled, w.Docker.Enabled, w.Tailscale.Enabled, w.Claude.Enabled)
	}
	if w.ComfyUI.Host != "render-box.example" || w.ComfyUI.Port != 8000 {
		t.Errorf("endpoint %+v", w.ComfyUI)
	}
	for path, content := range files {
		if b, _ := os.ReadFile(path); string(b) != content {
			t.Errorf("%s changed", path)
		}
	}
	if again, _ := s.Migrate(l); again != nil {
		t.Error("a second start migrates nothing")
	}

	// No legacy install at all: every widget runs.
	s2, _ := Open(t.TempDir())
	brought, _ = s2.Migrate(Legacy{StatusWidgets: t.TempDir(), PowerMonitor: t.TempDir(), Autostart: t.TempDir()})
	if len(brought) != 0 || !s2.Get().Widgets.Claude.Enabled || !s2.Get().Widgets.Docker.Enabled {
		t.Errorf("nothing to migrate: %q", brought)
	}
}

func TestFieldErrors(t *testing.T) {
	s, _ := Open(t.TempDir())
	for patch, field := range map[string]string{
		`{"widgets":{"comfyui":{"port":70000}}}`:        "widgets.comfyui.port",
		`{"widgets":{"comfyui":{"port":"8188"}}}`:       "widgets.comfyui.port",
		`{"widgets":{"power":{"peak_hours":"9-5"}}}`:    "widgets.power.peak_hours",
		`{"widgets":{"docker":{"order":1000}}}`:         "widgets.docker.order",
		`{"widgets":{"tailscale":{"watch":["", "x"]}}}`: "widgets.tailscale.watch",
		`{"widgets":{"power":{"tarif":1}}}`:             "tarif",
	} {
		_, err := s.Patch(json.RawMessage(patch))
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != field || fe.Message == "" {
			t.Errorf("%s: %v (want a field error on %s)", patch, err, field)
		}
	}
}
