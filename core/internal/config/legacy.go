package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// LegacyDir is where the Python widgets kept their files, on Linux only.
func LegacyDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "status-widgets"), nil
}

// MigrateLegacy brings the Python Tailscale widget's pinned peers over, once:
// only while config.json does not exist yet (the first run of NoClickDock
// 2), and only when the old file really holds a "watch" list — the Python
// runtime overwrote that same file with its registration, so most copies
// hold nothing to migrate. The old file is only read.
func (s *Store) MigrateLegacy(legacyDir string) (bool, error) {
	if _, err := os.Stat(s.path); !errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	data, err := os.ReadFile(filepath.Join(legacyDir, "tailscale.json"))
	if err != nil {
		return false, nil // nothing to migrate
	}
	var old struct {
		Watch *[]any `json:"watch"`
	}
	if json.Unmarshal(data, &old) != nil || old.Watch == nil {
		return false, nil
	}
	watch := []string{}
	for _, w := range *old.Watch {
		if host, ok := w.(string); ok && host != "" {
			watch = append(watch, host)
		}
	}
	patch, err := json.Marshal(map[string]any{"widgets": map[string]any{"tailscale": map[string]any{"watch": watch}}})
	if err != nil {
		return false, err
	}
	if _, err := s.Patch(patch); err != nil {
		return false, err
	}
	return true, nil
}
