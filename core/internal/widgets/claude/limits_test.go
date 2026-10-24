package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

func quotaFixture(at time.Time) string {
	return fmt.Sprintf(`{
		"oauthAccount":{"accountUuid":"invented-account"},
		"unrelatedSecret":"NEVER-RENDER-THIS",
		"cachedUsageUtilization":{"fetchedAtMs":%d,"accountUuid":"invented-account",
		"utilization":{
			"five_hour":{"utilization":0,"resets_at":%q},
			"seven_day":{"utilization":81.5,"resets_at":%q},
			"seven_day_opus":null,
			"seven_day_sonnet":{"utilization":12,"resets_at":null},
			"future_field":{"secret":"NEVER-RENDER-THIS"}
		}}
	}`, at.UnixMilli(), at.Add(3*time.Hour).Format(time.RFC3339Nano), at.Add(72*time.Hour).Format(time.RFC3339Nano))
}

func TestNumericQuotaCache(t *testing.T) {
	r := parseLimits([]byte(quotaFixture(now.Add(-4*time.Minute))), now)
	if r.keep || r.note != "" || !r.at.Equal(now.Add(-4*time.Minute)) {
		t.Fatalf("reading: %+v", r)
	}
	want := []limit{
		{Label: "Session", Pct: 0, Resets: now.Add(3*time.Hour - 4*time.Minute)},
		{Label: "Week", Pct: 81.5, Resets: now.Add(72*time.Hour - 4*time.Minute)},
		{Label: "Week · Sonnet", Pct: 12},
	}
	if !reflect.DeepEqual(r.rows, want) {
		t.Fatalf("rows: %+v", r.rows)
	}
	s := render(poll{limits: r.rows, limitsAt: r.at, now: now})
	if s.Glance.Text != "82%" || s.Footer != "limits as of 4m ago" {
		t.Fatalf("glance/footer: %+v / %q", s.Glance, s.Footer)
	}
	for _, b := range s.Pages[0].Blocks {
		if strings.Contains(fmt.Sprint(b), "NEVER-RENDER-THIS") {
			t.Fatal("unrelated config data escaped")
		}
	}
}

func TestQuotaCacheInvalidAndUnavailable(t *testing.T) {
	for _, tt := range []struct {
		name, data string
		keep       bool
	}{
		{"missing", `{}`, false},
		{"partial write", `{"cachedUsageUtilization":`, true},
		{"stale", quotaFixture(now.Add(-time.Hour - time.Second)), false},
		{"future", quotaFixture(now.Add(time.Second)), false},
		{"different login", strings.Replace(quotaFixture(now), `"oauthAccount":{"accountUuid":"invented-account"}`, `"oauthAccount":{"accountUuid":"other"}`, 1), false},
		{"logged out", strings.Replace(quotaFixture(now), `"oauthAccount":{"accountUuid":"invented-account"},`, ``, 1), false},
		{"no timestamp", `{"cachedUsageUtilization":{"utilization":{"five_hour":{"utilization":25}}}}`, false},
		{"API account without windows", fmt.Sprintf(`{"cachedUsageUtilization":{"fetchedAtMs":%d,"utilization":{}}}`, now.UnixMilli()), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := parseLimits([]byte(tt.data), now)
			if len(r.rows) != 0 || r.note == "" || r.keep != tt.keep {
				t.Fatalf("unexpected reading: %+v", r)
			}
			s := render(poll{now: now, limitsNote: r.note, usage: &usage{Available: true, H24: 1234}})
			if s.Glance.Text != "1K" || !strings.Contains(strings.Join(rows(s.Pages[0].Blocks), "\n"), r.note) {
				t.Fatal("missing limits must explain the problem even with token spend")
			}
		})
	}
	for _, invalid := range []string{`null`, `{"utilization":null}`, `{"utilization":-1}`, `{"utilization":101}`, `{"utilization":"50"}`, `{"utilization":50,"resets_at":"tomorrow"}`} {
		data := fmt.Sprintf(`{"cachedUsageUtilization":{"fetchedAtMs":%d,"utilization":{"five_hour":%s,"seven_day":{"utilization":25}}}}`, now.UnixMilli(), invalid)
		r := parseLimits([]byte(data), now)
		if len(r.rows) != 1 || r.rows[0].Label != "Week" {
			t.Errorf("invalid optional window affected valid sibling: %s: %+v", invalid, r)
		}
	}
}

func TestLimitsPathAndBoundedRead(t *testing.T) {
	home := t.TempDir()
	key := "HOME"
	if runtime.GOOS == "windows" {
		key = "USERPROFILE"
	}
	t.Setenv(key, home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if limitsPath() != filepath.Join(home, ".claude.json") {
		t.Fatalf("default path: %q", limitsPath())
	}
	custom := filepath.Join(t.TempDir(), "A&B Claude profile")
	t.Setenv("CLAUDE_CONFIG_DIR", custom)
	path := filepath.Join(custom, ".claude.json")
	if limitsPath() != path {
		t.Fatalf("override path: %q", limitsPath())
	}
	write(t, path, now, quotaFixture(now))
	if r := readLimits(limitsPath(), now); len(r.rows) != 3 {
		t.Fatalf("cache: %+v", r)
	}
	if r := readLimits(filepath.Join(home, ".claude.json"), now); r.keep || r.note != limitsMissing {
		t.Fatalf("missing: %+v", r)
	}
	if r := readLimits(custom, now); !r.keep || len(r.rows) != 0 {
		t.Fatalf("non-file: %+v", r)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", maxConfigBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if r := readLimits(path, now); !r.keep || len(r.rows) != 0 {
		t.Fatal("oversized config accepted")
	}
}

func TestCacheCollectionAndAccountChange(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, ".claude.json")
	at := time.Now().Add(-4 * time.Minute).Truncate(time.Millisecond)
	write(t, path, at, quotaFixture(at))
	down := false
	srv := statusServer(t, &down)
	w := New()
	w.src.Base = srv.URL
	w.usageBusy = true // isolate the quota callback from transcript collection
	updated := make(chan struct{}, 1)
	w.SetRefresh(func() { updated <- struct{}{} })
	collect := func() {
		t.Helper()
		if _, err := w.Collect(context.Background()); err != nil {
			t.Fatal(err)
		}
		select {
		case <-updated:
		case <-time.After(3 * time.Second):
			t.Fatal("quota completion did not request a refresh")
		}
	}
	collect()
	w.mu.Lock()
	if len(w.limits) != 3 || !w.limitsAt.Equal(at) {
		t.Fatalf("collection lost the observation timestamp: %+v, %s", w.limits, w.limitsAt)
	}
	w.limitsAsked = time.Time{}
	w.mu.Unlock()
	// A partial write keeps the last reading, without making it look newer.
	write(t, path, at, `{"cachedUsageUtilization":`)
	collect()
	w.mu.Lock()
	if len(w.limits) != 3 || !w.limitsAt.Equal(at) || w.limitsNote == "" {
		t.Fatal("transient error lost or relabeled the last observation")
	}
	w.limitsAsked = time.Time{}
	w.mu.Unlock()
	// A completed logout must remove another account's bars immediately.
	write(t, path, at, `{}`)
	collect()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.limits) != 0 || w.limitsNote != limitsMissing {
		t.Fatal("logout kept old account limits")
	}
}

func TestExpiredCacheNeverRaisesTheState(t *testing.T) {
	s := render(poll{limits: []limit{{Label: "Week", Pct: 99}}, limitsAt: now.Add(-2 * time.Hour), now: now})
	model.RaiseToWorst(&s)
	if s.Glance.Text != "—" || s.State == model.StateCrit {
		t.Fatal("stale data still raises the widget")
	}
	if !strings.Contains(strings.Join(rows(s.Pages[0].Blocks), "\n"), limitsStale) {
		t.Fatal("stale cache has no explanation")
	}
}
