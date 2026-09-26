package config

import (
	"bytes"
	"encoding/json"
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
