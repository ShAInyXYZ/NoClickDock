package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Legacy is where the Python generation kept its settings (Linux). Every
// file is only read, never written or removed.
type Legacy struct {
	StatusWidgets string // ~/.config/status-widgets: bar.conf, tailscale.json
	PowerMonitor  string // ~/.config/power-monitor: config.json
	Autostart     string // ~/.config/autostart: which widgets were installed, ComfyUI's endpoint
}

// LegacyDirs are the Python generation's places for this user.
func LegacyDirs() (Legacy, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return Legacy{}, err
	}
	return Legacy{
		StatusWidgets: filepath.Join(base, "status-widgets"),
		PowerMonitor:  filepath.Join(base, "power-monitor"),
		Autostart:     filepath.Join(base, "autostart"),
	}, nil
}

// LegacyDir is where the Python widgets kept their files, on Linux only.
func LegacyDir() (string, error) {
	l, err := LegacyDirs()
	return l.StatusWidgets, err
}

// autostartIDs are the Python generation's autostart entries, by widget.
var autostartIDs = map[string]string{
	"claude": "claude-status-checker", "codex": "codex-status-checker", "comfyui": "comfyui-status-checker",
	"disk": "disk-status-checker", "docker": "docker-status-checker", "power": "power-monitor",
	"tailscale": "tailscale-status-checker",
}

// Migrate brings the Python generation's settings over, once: only while
// config.json does not exist yet (the first run of NoClickDock 2). What is
// missing or unreadable is skipped, a value the config refuses is left
// out, and the rest still comes over. It says what it brought.
//
//   - bar.conf: the dock's orientation, density (s/m/l) and position;
//   - tailscale.json: the watch list, only if it holds one (the Python
//     runtime overwrote that file with its registration);
//   - power-monitor/config.json: the tariff, estimates and intervals;
//   - autostart: a widget the user never installed starts disabled (when
//     none was, all start), and ComfyUI's --host/--port.
func (s *Store) Migrate(l Legacy) ([]string, error) {
	if _, err := os.Stat(s.path); !errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var brought []string
	try := func(what string, patch map[string]any) {
		if len(patch) == 0 {
			return
		}
		raw, err := json.Marshal(patch)
		if err != nil {
			return
		}
		if _, err := s.Patch(raw); err == nil {
			brought = append(brought, what)
		}
	}
	if dock := legacyDock(l.StatusWidgets); dock != nil {
		try("the dock's orientation, density and position", map[string]any{"dock": dock})
	}
	if watch := legacyWatch(l.StatusWidgets); watch != nil {
		try("the Tailscale watch list", map[string]any{"widgets": map[string]any{"tailscale": map[string]any{"watch": watch}}})
	}
	// Key by key: one value the config refuses does not lose the others.
	power := legacyPower(l.PowerMonitor)
	keys := 0
	for k, v := range power {
		before := len(brought)
		try(k, map[string]any{"widgets": map[string]any{"power": map[string]any{k: v}}})
		if len(brought) > before {
			brought = brought[:before]
			keys++
		}
	}
	if keys > 0 {
		brought = append(brought, fmt.Sprintf("%d Power settings", keys))
	}
	if installed, host, port := legacyAutostart(l.Autostart); installed != nil {
		widgets := map[string]any{}
		for id := range autostartIDs {
			widgets[id] = map[string]any{"enabled": installed[id]}
		}
		try("which widgets run", map[string]any{"widgets": widgets})
		if host != "" || port != 0 {
			endpoint := map[string]any{}
			if host != "" {
				endpoint["host"] = host
			}
			if port != 0 {
				endpoint["port"] = port
			}
			try("the ComfyUI endpoint", map[string]any{"widgets": map[string]any{"comfyui": endpoint}})
		}
	}
	return brought, nil
}

// MigrateLegacy is Migrate with only the status-widgets directory (the
// Tailscale watch list and bar.conf).
func (s *Store) MigrateLegacy(statusWidgets string) (bool, error) {
	brought, err := s.Migrate(Legacy{StatusWidgets: statusWidgets})
	return len(brought) > 0, err
}

func readJSON(path string, into any) bool {
	data, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(data, into) == nil
}

// legacyDock reads bar.conf: {"orientation","size","x","y"}.
func legacyDock(dir string) map[string]any {
	if dir == "" {
		return nil
	}
	var old struct {
		Orientation string `json:"orientation"`
		Size        string `json:"size"`
	}
	var pos struct {
		X *float64 `json:"x"`
		Y *float64 `json:"y"`
	}
	path := filepath.Join(dir, "bar.conf")
	if !readJSON(path, &old) || !readJSON(path, &pos) {
		return nil
	}
	dock := map[string]any{}
	if old.Orientation == "vertical" || old.Orientation == "horizontal" {
		dock["orientation"] = old.Orientation
	}
	if density, ok := map[string]string{"s": "compact", "m": "normal", "l": "large"}[old.Size]; ok {
		dock["density"] = density
	}
	if pos.X != nil && pos.Y != nil {
		dock["position"] = map[string]any{"x": int(*pos.X), "y": int(*pos.Y)}
	}
	if len(dock) == 0 {
		return nil
	}
	return dock
}

// legacyWatch is tailscale.json's "watch" list, when it has one.
func legacyWatch(dir string) []string {
	if dir == "" {
		return nil
	}
	var old struct {
		Watch *[]any `json:"watch"`
	}
	if !readJSON(filepath.Join(dir, "tailscale.json"), &old) || old.Watch == nil {
		return nil
	}
	watch := []string{}
	for _, w := range *old.Watch {
		if host, ok := w.(string); ok && host != "" {
			watch = append(watch, host)
		}
	}
	return watch
}

// legacyPower is the power monitor's keys NoClickDock knows (the obsolete
// monitor_watts_each is left behind).
func legacyPower(dir string) map[string]any {
	if dir == "" {
		return nil
	}
	var old map[string]any
	if !readJSON(filepath.Join(dir, "config.json"), &old) {
		return nil
	}
	out := map[string]any{}
	for _, k := range []string{
		"price_per_kwh", "currency", "overhead_watts", "peripheral_watts", "poll_interval", "log_interval",
		"tou", "peak_rate", "off_peak_rate", "peak_hours", "tou_weekend_offpeak",
	} {
		v, ok := old[k]
		if !ok {
			continue
		}
		if k == "poll_interval" {
			if f, ok := v.(float64); ok {
				v = int(f) // the Python accepted 2.0
			}
		}
		if (k == "peak_rate" || k == "off_peak_rate") && v == nil {
			continue // null: the default already
		}
		out[k] = v
	}
	return out
}

// legacyAutostart: which widgets had a live autostart entry (nil when none
// had: then everything starts), and ComfyUI's --host/--port.
func legacyAutostart(dir string) (map[string]bool, string, int) {
	if dir == "" {
		return nil, "", 0
	}
	installed := map[string]bool{}
	any := false
	var host string
	var port int
	for id, file := range autostartIDs {
		entry, ok := readDesktop(filepath.Join(dir, file+".desktop"))
		if !ok || strings.EqualFold(entry["Hidden"], "true") || strings.EqualFold(entry["X-GNOME-Autostart-enabled"], "false") {
			continue
		}
		installed[id], any = true, true
		if id == "comfyui" {
			host, port = endpointArgs(entry["Exec"])
		}
	}
	if !any {
		return nil, "", 0
	}
	return installed, host, port
}

func readDesktop(path string) (map[string]string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out, true
}

// endpointArgs reads "--host H --port P" (or --host=H) from an Exec line.
func endpointArgs(exec string) (string, int) {
	f := strings.Fields(exec)
	var host string
	var port int
	for i := 0; i < len(f); i++ {
		key, value, inline := strings.Cut(f[i], "=")
		if !inline && i+1 < len(f) {
			value = f[i+1]
		}
		switch key {
		case "--host":
			if ValidHost(value) {
				host = value
			}
		case "--port":
			if p, err := strconv.Atoi(value); err == nil && p > 0 && p < 65536 {
				port = p
			}
		}
	}
	return host, port
}
