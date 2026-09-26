// Package config owns config.json: the dock's placement and look, and later
// every widget's options. The core is its only writer; the interface changes
// it by sending a JSON merge patch.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

type Config struct {
	Dock    Dock    `json:"dock"`
	Widgets Widgets `json:"widgets"`
}

// Widgets holds each widget's own options.
type Widgets struct {
	ComfyUI   ComfyUI   `json:"comfyui"`
	Power     Power     `json:"power"`
	Tailscale Tailscale `json:"tailscale"`
}

// ComfyUI is the server the ComfyUI widget watches.
type ComfyUI struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// Power is the tariff and the estimates every Power figure derives from
// (docs/rewrite/specs/power.md).
type Power struct {
	PricePerKWh     float64 `json:"price_per_kwh"`
	Currency        string  `json:"currency"`
	OverheadWatts   float64 `json:"overhead_watts"`   // board, fans, PSU losses
	PeripheralWatts float64 `json:"peripheral_watts"` // once, when any USB peripheral exists
	PollInterval    int     `json:"poll_interval"`    // seconds
	LogInterval     string  `json:"log_interval"`     // off | hourly | daily | weekly | monthly
	// Time of use: peak_rate inside peak_hours ("6-13,15-22": a ≤ hour < b),
	// off_peak_rate otherwise, weekends off-peak unless tou_weekend_offpeak
	// is false. A missing or zero rate falls back to price_per_kwh.
	TOU               bool     `json:"tou"`
	PeakRate          *float64 `json:"peak_rate"`
	OffPeakRate       *float64 `json:"off_peak_rate"`
	PeakHours         string   `json:"peak_hours"`
	TOUWeekendOffPeak bool     `json:"tou_weekend_offpeak"`
}

type Tailscale struct {
	// Watch pins the peers whose absence matters, by host name. Null (the
	// default) watches every peer seen online since the core started; an
	// empty list watches none.
	Watch *[]string `json:"watch"`
}

type Dock struct {
	Orientation string `json:"orientation"` // "vertical" | "horizontal"
	Density     string `json:"density"`     // "compact" | "normal" | "large"
	Readouts    bool   `json:"readouts"`
	// Position is where the shell last left the rail. Nil means "not placed
	// yet": the shell picks.
	Position *Position `json:"position,omitempty"`
}

// Position survives resolution and scale changes: the monitor by name, the
// edge the rail was snapped to and how far along it its centre sat (0..1 of
// the work area), with the physical top-left corner as the fallback.
type Position struct {
	Monitor string   `json:"monitor,omitempty"`
	Edge    string   `json:"edge,omitempty"` // "left" | "right" | "top" | "bottom" | "" (free)
	Along   *float64 `json:"along,omitempty"`
	X       int      `json:"x"`
	Y       int      `json:"y"`
}

func Default() Config {
	return Config{
		Dock: Dock{Orientation: "vertical", Density: "normal", Readouts: true},
		Widgets: Widgets{
			ComfyUI: ComfyUI{Host: "127.0.0.1", Port: 8188},
			Power: Power{
				PricePerKWh: 0.30, Currency: "EUR", OverheadWatts: 30, PeripheralWatts: 5,
				PollInterval: 2, LogInterval: "hourly", PeakHours: "6-13,15-22", TOUWeekendOffPeak: true,
			},
		},
	}
}

func (c Config) validate() error {
	switch c.Dock.Orientation {
	case "vertical", "horizontal":
	default:
		return fmt.Errorf("dock.orientation must be vertical or horizontal, not %q", c.Dock.Orientation)
	}
	switch c.Dock.Density {
	case "compact", "normal", "large":
	default:
		return fmt.Errorf("dock.density must be compact, normal or large, not %q", c.Dock.Density)
	}
	if p := c.Dock.Position; p != nil {
		switch p.Edge {
		case "", "left", "right", "top", "bottom":
		default:
			return fmt.Errorf("dock.position.edge must be left, right, top, bottom or empty, not %q", p.Edge)
		}
		if p.Along != nil && (*p.Along < 0 || *p.Along > 1) {
			return fmt.Errorf("dock.position.along must be between 0 and 1")
		}
	}
	if err := c.Widgets.ComfyUI.validate(); err != nil {
		return err
	}
	if err := c.Widgets.Power.validate(); err != nil {
		return err
	}
	if w := c.Widgets.Tailscale.Watch; w != nil {
		for _, host := range *w {
			if strings.TrimSpace(host) == "" || len(host) > 253 {
				return fmt.Errorf("widgets.tailscale.watch holds host names, not %q", host)
			}
		}
	}
	return nil
}

func (c ComfyUI) validate() error {
	h := c.Host
	if h == "" || len(h) > 253 || strings.ContainsAny(h, " /?#@\t") {
		return fmt.Errorf("widgets.comfyui.host must be a host name or address, not %q", h)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("widgets.comfyui.port must be between 1 and 65535, not %d", c.Port)
	}
	return nil
}

func (p Power) validate() error {
	positive := func(key string, v float64) error {
		if v < 0 || v > 1e6 {
			return fmt.Errorf("widgets.power.%s must be a positive number, not %v", key, v)
		}
		return nil
	}
	for _, f := range []struct {
		key string
		v   float64
	}{{"price_per_kwh", p.PricePerKWh}, {"overhead_watts", p.OverheadWatts}, {"peripheral_watts", p.PeripheralWatts}} {
		if err := positive(f.key, f.v); err != nil {
			return err
		}
	}
	for key, v := range map[string]*float64{"peak_rate": p.PeakRate, "off_peak_rate": p.OffPeakRate} {
		if v != nil {
			if err := positive(key, *v); err != nil {
				return err
			}
		}
	}
	if n := len([]rune(p.Currency)); n > 5 {
		return fmt.Errorf("widgets.power.currency is at most 5 characters, not %q", p.Currency)
	}
	if p.PollInterval < 1 || p.PollInterval > 60 {
		return fmt.Errorf("widgets.power.poll_interval must be 1 to 60 seconds, not %d", p.PollInterval)
	}
	switch p.LogInterval {
	case "off", "hourly", "daily", "weekly", "monthly":
	default:
		return fmt.Errorf("widgets.power.log_interval must be off, hourly, daily, weekly or monthly, not %q", p.LogInterval)
	}
	if _, err := ParseHours(p.PeakHours); err != nil {
		return fmt.Errorf("widgets.power.peak_hours: %w", err)
	}
	return nil
}

// Hours is one peak window: a ≤ hour < b.
type Hours struct{ From, To int }

// ParseHours reads "6-13,15-22". Unlike the Python widget, which skipped a
// range it could not read, a malformed one is refused when it is saved.
func ParseHours(spec string) ([]Hours, error) {
	var out []Hours
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		a, b, ok := strings.Cut(part, "-")
		from, err1 := strconv.Atoi(strings.TrimSpace(a))
		to, err2 := strconv.Atoi(strings.TrimSpace(b))
		if !ok || err1 != nil || err2 != nil || from < 0 || to > 24 || from >= to {
			return nil, fmt.Errorf("%q is not an hour range like 6-13", part)
		}
		out = append(out, Hours{from, to})
	}
	return out, nil
}

// Dir is where config.json lives: ~/.config/noclickdock on Linux,
// %APPDATA%\NoClickDock on Windows.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(base, "NoClickDock"), nil
	}
	return filepath.Join(base, "noclickdock"), nil
}

// Store is the loaded config and the file it is saved to.
type Store struct {
	path string

	mu  sync.Mutex
	cfg Config
}

// Open loads dir/config.json, or starts from the defaults when there is none.
// Nothing is written until the first change. A file that cannot be read is
// kept aside as config.json.bad before defaults are used, so a broken edit is
// never silently lost.
func Open(dir string) (*Store, error) {
	s := &Store{path: filepath.Join(dir, "config.json"), cfg: Default()}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	cfg, err := decode(data)
	if err != nil {
		_ = os.WriteFile(s.path+".bad", data, 0o600)
		return s, fmt.Errorf("%s: %w (kept as config.json.bad, using defaults)", s.path, err)
	}
	s.cfg = cfg
	return s, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Patch applies an RFC 7396 merge patch, validates the result and saves it.
// An invalid patch changes nothing.
func (s *Store) Patch(patch json.RawMessage) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var p any
	if err := json.Unmarshal(patch, &p); err != nil {
		return s.cfg, fmt.Errorf("config patch: %w", err)
	}
	current, err := toTree(s.cfg)
	if err != nil {
		return s.cfg, err
	}
	merged, err := json.Marshal(mergePatch(current, p))
	if err != nil {
		return s.cfg, err
	}
	next, err := decode(merged)
	if err != nil {
		return s.cfg, err
	}
	if err := s.save(next); err != nil {
		return s.cfg, err
	}
	s.cfg = next
	return next, nil
}

// decode reads a config over the defaults, so a file written by an older
// version keeps working, and refuses keys it does not know: a typo in a patch
// is an error, not a silently ignored setting.
func decode(data []byte) (Config, error) {
	cfg := Default()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Default(), err
	}
	return cfg, cfg.validate()
}

// save writes through a temporary file and a rename, so a crash mid-write
// leaves the previous config intact.
func (s *Store) save(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

func toTree(v any) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var tree any
	return tree, json.Unmarshal(data, &tree)
}

// mergePatch is RFC 7396: objects merge key by key, null deletes, anything
// else replaces.
func mergePatch(target, patch any) any {
	p, ok := patch.(map[string]any)
	if !ok {
		return patch
	}
	t, ok := target.(map[string]any)
	if !ok {
		t = map[string]any{}
	}
	for k, v := range p {
		if v == nil {
			delete(t, k)
			continue
		}
		t[k] = mergePatch(t[k], v)
	}
	return t
}
