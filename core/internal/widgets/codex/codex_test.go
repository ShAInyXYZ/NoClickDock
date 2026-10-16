package codex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/statuspage"
)

// Every fixture is invented: ids, models' names aside, and all content.
var now = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

func ts(ago time.Duration) string { return now.Add(-ago).UTC().Format("2006-01-02T15:04:05.000Z") }

func sessionMeta(at, id, parent string) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"session_meta","payload":{"id":%q,"session_id":%q,"parent_thread_id":%q,"cwd":"/home/someone/example","originator":"codex_cli"}}`, at, id, id, parent)
}

func turnContext(at, model string) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"turn_context","payload":{"turn_id":"t","cwd":"/home/someone/example","model":%q}}`, at, model)
}

// tokens is a token_count record: the turn's usage and, when limitID is not
// "", the account's rate limits at that moment.
func tokens(at string, total int64, limitID string, primary, secondary string) string {
	rl := "null"
	if limitID != "" {
		rl = fmt.Sprintf(`{"limit_id":%q,"limit_name":null,"primary":%s,"secondary":%s,"plan_type":"plus"}`, limitID, primary, secondary)
	}
	info := "null"
	if total > 0 {
		info = fmt.Sprintf(`{"total_token_usage":{"total_tokens":%d},"last_token_usage":{"input_tokens":1,"output_tokens":1,"total_tokens":%d}}`, total*10, total)
	}
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":%s,"rate_limits":%s}}`, at, info, rl)
}

func win(pct float64, minutes string, resets time.Time) string {
	r := "null"
	if !resets.IsZero() {
		r = fmt.Sprint(resets.Unix())
	}
	return fmt.Sprintf(`{"used_percent":%v,"window_minutes":%s,"resets_at":%s}`, pct, minutes, r)
}

func write(t *testing.T, path string, mtime time.Time, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// rollouts builds an invented ~/.codex.
func rollouts(t *testing.T) (sessions, archived string) {
	home := t.TempDir()
	sessions, archived = filepath.Join(home, "sessions"), filepath.Join(home, "archived_sessions")
	in5h := now.Add(4 * time.Hour)
	inWeek := now.Add(80 * time.Hour)
	day := filepath.Join(sessions, "2026", "09", "26")

	write(t, filepath.Join(day, "rollout-a.jsonl"), now,
		sessionMeta(ts(3*time.Hour), "sess-a", ""),
		turnContext(ts(3*time.Hour), "gpt-5-codex"),
		tokens(ts(2*time.Hour), 1000, "codex", win(12, "300", in5h), win(30, "10080", inWeek)),
		tokens(ts(50*time.Minute), 400, "", "", ""),
		turnContext(ts(40*time.Minute), "gpt-5-2025-08-07"), // the model changes
		// The newest reading by instant: ".9Z" (900 ms) is later than ".15Z"
		// (150 ms), though it sorts first as text.
		tokens("2026-09-26T11:30:00.15Z", 200, "codex", win(44, "300", in5h), win(31, "10080", inWeek)),
		tokens("2026-09-26T11:30:00.9Z", 100, "codex", win(47, "300", in5h), win(31, "10080", inWeek)),
		// A second allowance that never moves.
		tokens(ts(20*time.Minute), 0, "codex-mini", win(0, "300", in5h), win(0, "10080", inWeek)),
	)
	// A resumed session rewrote three turns under one timestamp: dropped.
	write(t, filepath.Join(day, "rollout-b.jsonl"), now,
		sessionMeta(ts(30*time.Minute), "sess-b", ""),
		turnContext(ts(30*time.Minute), "gpt-5-codex"),
		tokens(ts(10*time.Minute), 7000, "", "", ""),
		tokens(ts(10*time.Minute), 8000, "", "", ""),
		tokens(ts(10*time.Minute), 9000, "", "", ""),
		tokens(ts(5*time.Minute), 50, "", "", ""),
	)
	// A fork replays its parent's turns at the moment of the fork: dropped;
	// the fork's own later turn counts.
	fork := ts(15 * time.Minute)
	write(t, filepath.Join(day, "rollout-c.jsonl"), now,
		sessionMeta(fork, "sess-c", "sess-a"),
		turnContext(fork, "gpt-5-codex"),
		tokens(fork, 1000, "", "", ""),
		tokens(ts(4*time.Minute), 30, "", "", ""),
	)
	// A resume that replays a single turn, in a new file (dropped: it sits
	// in the second of the session_meta), then a turn of its own.
	resume := ts(45 * time.Minute)
	write(t, filepath.Join(day, "rollout-f.jsonl"), now,
		sessionMeta(resume, "sess-f", ""),
		turnContext(resume, "gpt-5-codex"),
		tokens(resume, 777, "", "", ""),
		tokens(ts(6*time.Minute), 11, "", "", ""),
	)
	// A resume appended to its own file: a second session_meta, one
	// replayed turn in its second (dropped), then new work.
	again := ts(40 * time.Minute)
	write(t, filepath.Join(day, "rollout-g.jsonl"), now,
		sessionMeta(ts(50*time.Minute), "sess-g", ""),
		turnContext(ts(50*time.Minute), "gpt-5-codex"),
		tokens(ts(48*time.Minute), 3, "", "", ""),
		sessionMeta(again, "sess-g", ""),
		tokens(again, 555, "", "", ""),
		tokens(ts(35*time.Minute), 2, "", "", ""),
	)
	// Archived, three days old: 7 d only; windows of 2 days, 45 minutes
	// and an unknown length, in another allowance.
	write(t, filepath.Join(archived, "rollout-d.jsonl"), now.Add(-72*time.Hour),
		sessionMeta(ts(73*time.Hour), "sess-d", ""),
		tokens(ts(72*time.Hour), 5000, "other", win(5, "2880", now.Add(24*time.Hour)), win(9, "45", now.Add(-time.Hour))),
		tokens(ts(71*time.Hour), 5, "odd", win(3, "null", time.Time{}), "null"),
	)
	// Untouched for 8 days: never opened.
	write(t, filepath.Join(archived, "rollout-e.jsonl"), now.Add(-8*24*time.Hour),
		sessionMeta(ts(8*24*time.Hour), "sess-e", ""),
		tokens(ts(time.Hour), 999_999, "stale", win(99, "300", in5h), "null"),
	)
	return sessions, archived
}

func TestRollouts(t *testing.T) {
	sc := newScanner(rollouts(t))
	sp := sc.scan(now)
	// 1 h: 400 (a) + 200 + 100 + 50 (b) + 30 (c) + 11 (f) + 3 + 2 (g) = 796;
	// 24 h: + 1000 = 1796; 7 d: + 5000 + 5 = 6801.
	if sp.H1 != 796 || sp.H24 != 1796 || sp.D7 != 6801 {
		t.Errorf("1h %d 24h %d 7d %d, want 796 1796 6801", sp.H1, sp.H24, sp.D7)
	}
	if sp.Sessions != 5 {
		t.Errorf("sessions %d, want a, b, c, f, g", sp.Sessions)
	}
	wantModels := []modelSpend{{"gpt-5-codex", 1496}, {"gpt-5-2025-08-07", 300}}
	if !reflect.DeepEqual(sp.Models, wantModels) {
		t.Errorf("models %+v", sp.Models)
	}
	if sp.Plan != "plus" {
		t.Errorf("plan %q", sp.Plan)
	}
	var got []string
	for _, g := range sp.Groups {
		var rows []string
		for _, r := range g.Rows {
			rows = append(rows, fmt.Sprintf("%s %.0f", windowLabel(r.Minutes), r.Pct))
		}
		got = append(got, fmt.Sprintf("%s idle=%v [%s]", g.ID, g.Idle, strings.Join(rows, ", ")))
	}
	want := []string{
		"codex idle=false [5h 47, week 31]", // the .9Z reading, the newest
		"odd idle=false [limit 3]",          // read at 71 h, newer than other's 72 h
		"other idle=false [45m 9, 2d 5]",
		"codex-mini idle=true [5h 0, week 0]",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("groups:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !sp.Seen.Equal(time.Date(2026, 9, 26, 11, 40, 0, 0, time.UTC)) {
		t.Errorf("limits seen %v (codex-mini's reading at 11:40 is the newest)", sp.Seen)
	}
}

func TestWindowLabels(t *testing.T) {
	for m, want := range map[int]string{300: "5h", 10080: "week", 2880: "2d", 45: "45m", -1: "limit", 60: "1h", 1440: "24h"} {
		if got := windowLabel(m); got != want {
			t.Errorf("windowLabel(%d) = %q, want %q", m, got, want)
		}
	}
}

func rowsOf(blocks model.Blocks) []string {
	var out []string
	for _, b := range blocks {
		switch v := b.(type) {
		case model.Section:
			out = append(out, "# "+v.Title+" "+v.Meta+" "+string(v.Tone))
		case model.Row:
			out = append(out, "- "+v.Name+" | "+v.Value+" | "+string(v.Tone))
		case model.Limit:
			out = append(out, fmt.Sprintf("= %s %.0f %s %s", v.Label, v.Pct, v.Resets, v.Tone))
		case model.Stats:
			var s []string
			for _, i := range v.Items {
				s = append(s, i.Key+" "+i.Value)
			}
			out = append(out, "  "+strings.Join(s, " · "))
		case model.Chips:
			var s []string
			for _, c := range v.Items {
				s = append(s, c.Label)
			}
			out = append(out, "  ["+strings.Join(s, ", ")+"]")
		case model.Alert:
			out = append(out, "! "+v.Title+" | "+v.Meta+" | "+v.Body)
		case model.Note:
			out = append(out, "  "+v.Text+" "+string(v.Tone))
		}
	}
	return out
}

func TestCollectAndPanel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file := map[string]string{"/api/v2/status.json": "status.json", "/api/v2/components.json": "components.json", "/api/v2/incidents.json": "incidents.json"}[r.URL.Path]
		if file == "" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join("testdata", file))
	}))
	defer srv.Close()
	w := New()
	w.src.Base = srv.URL
	w.spendAt = time.Now() // the spend below, not a scan
	sp := newScanner(rollouts(t)).scan(now)
	w.spend = &sp

	s, err := w.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.State != model.StateWarn || s.StateLabel != "2 services degraded" {
		t.Errorf("state %s %q", s.State, s.StateLabel)
	}
	var ids []string
	for _, is := range s.Issues {
		ids = append(ids, is.ID+" / "+is.Text)
	}
	if !reflect.DeepEqual(ids, []string{
		"comp:VS Code extension:partial_outage / VS Code partial outage",
		"comp:Image Generation:degraded_performance / Image Generation degraded",
		"inc:inc-a / Increased latency in the VS Code extension",
	}) {
		t.Errorf("issues (resolved and postmortem incidents left out):\n%s", strings.Join(ids, "\n"))
	}

	// The panel, rendered at the fixtures' instant.
	s = render(poll{page: w.src.Fetch(context.Background()), spend: &sp, now: now})
	loc := now.Local()
	want := []string{
		"# Degraded 2 warn",
		"- VS Code | partial outage | warn",
		"- Image Generation | degraded | warn",
		"# Active incidents 1 warn",
		"! Increased latency in the VS Code extension | identified · 25m | We have identified the cause and are rolling out a fix.",
		"# Usage plus plan ",
		"= codex 5h 47 resets " + loc.Add(4*time.Hour).Format("15:04") + " ",
		"= codex week 31 resets " + loc.Add(80*time.Hour).Format("Mon 15:04") + " ",
		// A window whose reset has passed reads 0, 'reset … ago' (not P's 9 %).
		"= odd limit 3  ",
		"= other 45m 0 reset 1h ago ",
		"= other 2d 5 resets " + loc.Add(24*time.Hour).Format("Mon 15:04") + " ",
		"  codex-mini · never reports usage idle",
		"  1h 796 · 24h 2K · 7d 7K · sessions 5",
		"  gpt-5-codex 1K · gpt-5 300",
		"# Services 8 of 9 ",
		"  [Codex API, Codex Web, Desktop app, CLI, Responses, Completions, Login, Conversations]",
	}
	if got := rowsOf(s.Pages[0].Blocks); !reflect.DeepEqual(got, want) {
		t.Errorf("panel:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The glance: the highest live, unlapsed window (the 9 % reset already).
	if s.Glance.Text != "47%" || s.Footer != "limits as of 20m ago" {
		t.Errorf("glance %+v footer %q", s.Glance, s.Footer)
	}
}

func TestNothingLocalAndUnreachable(t *testing.T) {
	s := render(poll{page: statuspage.Page{Indicator: "unknown"}, spend: &spend{}, now: now})
	got := rowsOf(s.Pages[0].Blocks)
	want := []string{
		"# Usage  ", "  No Codex sessions in the last 7 days idle",
		"# Services  ", "  status.openai.com did not answer idle",
	}
	if !reflect.DeepEqual(got, want) || s.State != model.StateIdle || s.StateLabel != "status page unreachable" || s.Glance.Text != "—" {
		t.Errorf("empty: %q %s %q %q", got, s.State, s.StateLabel, s.Glance.Text)
	}
	if sp := newScanner(filepath.Join(t.TempDir(), "absent")).scan(now); sp.Available || len(sp.Groups) != 0 {
		t.Error("no rollouts, nothing")
	}
}
