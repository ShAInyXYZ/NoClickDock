package claude

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
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

// Every fixture is invented: no real prompt, project, path or incident.
var now = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

func TestLimitsFromTheCLI(t *testing.T) {
	out, err := os.ReadFile("testdata/usage.txt")
	if err != nil {
		t.Fatal(err)
	}
	rows := parseLimits(string(out), now)
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s %.1f %s", r.Label, r.Pct, r.Resets.UTC().Format(time.RFC3339)))
	}
	want := []string{
		`Session 46.0 2026-09-26T18:30:00Z`,
		`Week 78.0 2026-09-28T09:00:00Z`,
		`Week · Fable 23.5 2026-09-28T09:00:00Z`,
		`Week · Opus 0.0 0001-01-01T00:00:00Z`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("limits:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if parseLimits("Weekly limit: 78% used · resets soon\nnothing here", now) != nil {
		t.Error("changed wording must not match")
	}

	// Reset times: with and without minutes or a date, the year rolling over.
	for raw, want := range map[string]string{
		"Sep 26, 6:30pm (Etc/UTC)": "2026-09-26T18:30:00Z",
		"Sep 27, 3pm (Etc/UTC)":    "2026-09-27T15:00:00Z",
		"11:15pm (Etc/UTC)":        "2026-09-26T23:15:00Z",
		"9am (Etc/UTC)":            "2026-09-27T09:00:00Z", // already past today
	} {
		got, ok := parseReset(raw, now)
		if !ok || got.UTC().Format(time.RFC3339) != want {
			t.Errorf("parseReset(%q) = %v %v, want %s", raw, got, ok, want)
		}
	}
	december := time.Date(2026, 12, 30, 12, 0, 0, 0, time.UTC)
	if got, _ := parseReset("Jan 2, 9am (Etc/UTC)", december); got.Year() != 2027 {
		t.Errorf("January read in December is next year: %v", got)
	}
	rows = parseLimits("Current session: 5% used · resets whenever it likes", now)
	if len(rows) != 1 || rows[0].Pct != 5 || !rows[0].Resets.IsZero() {
		t.Errorf("an unreadable reset is left out, the percentage kept: %+v", rows)
	}
}

// line is one transcript line as Claude Code writes it (invented content).
func line(typ, ts, session, msgID, reqID, uuid, model string, in, out, cacheW, cacheR int) string {
	return fmt.Sprintf(`{"type":%q,"timestamp":%q,"sessionId":%q,"requestId":%q,"uuid":%q,"message":{"id":%q,"model":%q,"role":"assistant","content":[{"type":"text","text":"(invented)"}],"usage":{"input_tokens":%d,"output_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d}}}`,
		typ, ts, session, reqID, uuid, msgID, model, in, out, cacheW, cacheR)
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

// projects builds an invented ~/.claude/projects.
func projects(t *testing.T) string {
	root := t.TempDir()
	ts := func(ago time.Duration) string { return now.Add(-ago).Format(time.RFC3339Nano) }
	proj := filepath.Join(root, "-home-someone-example-project")
	write(t, filepath.Join(proj, "s1.jsonl"), now,
		`{"type":"user","timestamp":"`+ts(40*time.Minute)+`","message":{"role":"user","content":"(invented)"}}`,
		// One streamed message written as three lines: counted once, with
		// the line that has the most output.
		line("assistant", ts(30*time.Minute), "s1", "msg_a", "req_a", "u1", "claude-opus-5", 100, 10, 0, 0),
		line("assistant", ts(30*time.Minute), "s1", "msg_a", "req_a", "u2", "claude-opus-5", 100, 50, 0, 0),
		line("assistant", ts(30*time.Minute), "s1", "msg_a", "req_a", "u3", "claude-opus-5", 100, 50, 0, 0),
		// Another message, 3 h ago, cache counted.
		line("assistant", ts(3*time.Hour), "s1", "msg_b", "req_b", "u4", "claude-sonnet-5", 1000, 200, 300, 500),
		`{"type":"assistant","usage": not json`,
		// A line over 64 KB.
		strings.Replace(line("assistant", ts(2*time.Hour), "s1", "msg_c", "req_c", "u5", "claude-opus-5", 7, 3, 0, 0),
			`(invented)`, strings.Repeat("x", 70_000), 1),
		// <synthetic> counts in the totals, never in the model split.
		line("assistant", ts(10*time.Minute), "s1", "msg_d", "req_d", "u6", "<synthetic>", 0, 5, 0, 0),
		// No message id or request id: known by its uuid.
		line("assistant", ts(5*time.Hour), "s1", "", "", "u7", "claude-haiku-4-5-20251001", 20, 0, 0, 0),
		line("assistant", ts(5*time.Hour), "s1", "", "", "u7", "claude-haiku-4-5-20251001", 20, 0, 0, 0),
		// Three days ago: 7 d only.
		line("assistant", ts(72*time.Hour), "s0", "msg_e", "req_e", "u8", "claude-opus-5", 5000, 0, 0, 0),
	)
	// A subagent's transcript, one level deeper, and a resumed session that
	// copied msg_b into its own file.
	write(t, filepath.Join(proj, "s1", "subagents", "agent-1.jsonl"), now,
		line("assistant", ts(20*time.Minute), "s1", "msg_f", "req_f", "u9", "claude-sonnet-5", 400, 100, 0, 0))
	write(t, filepath.Join(proj, "s2.jsonl"), now,
		// A /usage first, then work: the session counts.
		`{"type":"user","timestamp":"`+ts(4*time.Hour)+`","sessionId":"s2","message":{"role":"user","content":"<command-name>/usage</command-name>"}}`,
		`{"type":"user","timestamp":"`+ts(4*time.Hour)+`","sessionId":"s2","message":{"role":"user","content":"(invented)"}}`,
		line("assistant", ts(3*time.Hour), "s2", "msg_b", "req_b", "u10", "claude-sonnet-5", 1000, 200, 300, 500),
		line("assistant", ts(90*time.Minute), "s2", "msg_g", "req_g", "u11", "claude-opus-5", 60, 40, 0, 0))
	// Limits readings: sessions whose only prompt is /usage (each reading
	// by an older CLI, or by the Python widget, left one). Nothing in them
	// counts, not even a reply that carries usage; a tool result or the
	// CLI's own caveat is not a prompt.
	home := filepath.Join(root, "-home-someone")
	write(t, filepath.Join(home, "r1.jsonl"), now,
		`{"type":"user","isMeta":true,"timestamp":"`+ts(15*time.Minute)+`","sessionId":"r1","message":{"role":"user","content":"<local-command-caveat>(invented)</local-command-caveat>"}}`,
		`{"type":"user","timestamp":"`+ts(15*time.Minute)+`","sessionId":"r1","message":{"role":"user","content":"<command-name>/usage</command-name>\n<command-message>usage</command-message>"}}`,
		`{"type":"system","subtype":"local_command","timestamp":"`+ts(15*time.Minute)+`","sessionId":"r1","content":"<local-command-stdout>(invented)</local-command-stdout>"}`,
		line("assistant", ts(15*time.Minute), "r1", "msg_r", "req_r", "u20", "<synthetic>", 0, 7, 0, 0))
	write(t, filepath.Join(home, "r2.jsonl"), now,
		`{"type":"user","timestamp":"`+ts(15*time.Minute)+`","sessionId":"r2","message":{"role":"user","content":[{"type":"text","text":"/usage"}]}}`,
		`{"type":"user","timestamp":"`+ts(15*time.Minute)+`","sessionId":"r2","message":{"role":"user","content":[{"type":"tool_result","content":"(invented)"}]}}`,
		line("assistant", ts(15*time.Minute), "r2", "msg_s", "req_s", "u21", "claude-opus-5", 9, 9, 0, 0))
	// A file untouched for 8 days is not opened.
	write(t, filepath.Join(proj, "old.jsonl"), now.Add(-8*24*time.Hour),
		line("assistant", ts(time.Hour), "s9", "msg_z", "req_z", "u12", "claude-opus-5", 999_999, 0, 0, 0))
	return root
}

func TestSpendCountsEachMessageOnce(t *testing.T) {
	sc := newScanner(projects(t))
	u := sc.scan(now)
	// 1 h: msg_a 150 + msg_d 5 + msg_f 500 = 655
	// 24 h: + msg_b 2000 + msg_c 10 + u7 20 + msg_g 100 = 2785
	// 7 d: + msg_e 5000 = 7785
	if u.H1 != 655 || u.H24 != 2785 || u.D7 != 7785 {
		t.Errorf("1h %d 24h %d 7d %d, want 655 2785 7785", u.H1, u.H24, u.D7)
	}
	if u.Sessions != 2 || !u.Available {
		t.Errorf("sessions %d (want s1, s2; never the /usage readings)", u.Sessions)
	}
	want := []spend{{"claude-sonnet-5", 2500}, {"claude-opus-5", 260}, {"claude-haiku-4-5-20251001", 20}}
	if !reflect.DeepEqual(u.Models, want) {
		t.Errorf("models %+v\nwant   %+v", u.Models, want)
	}

	// A second scan reads only what changed.
	before := len(sc.files)
	path := filepath.Join(sc.root, "-home-someone-example-project", "s2.jsonl")
	parsed := sc.files[path]
	u2 := sc.scan(now)
	if !reflect.DeepEqual(u2, u) || sc.files[path] != parsed || len(sc.files) != before {
		t.Error("an unchanged file is not parsed again")
	}
	write(t, path, now.Add(time.Second),
		line("assistant", now.Add(-time.Minute).Format(time.RFC3339Nano), "s2", "msg_h", "req_h", "u13", "claude-opus-5", 1, 1, 0, 0))
	if u3 := sc.scan(now); u3.H1 != 657 || sc.files[path] == parsed {
		t.Errorf("a changed file is parsed again: 1h %d", u3.H1)
	}
	if u := newScanner(filepath.Join(t.TempDir(), "absent")).scan(now); u.Available || u.D7 != 0 {
		t.Error("no transcripts, nothing available")
	}
}

func TestNames(t *testing.T) {
	for id, want := range map[string]string{
		"claude-opus-5": "opus 5", "claude-haiku-4-5-20251001": "haiku 4.5",
		"us.anthropic.claude-sonnet-6-20270101": "sonnet 6", "anthropic.claude-fable-5": "fable 5", "": "unknown",
		"claude-opus-5-5": "opus 5.5", "claude-haiku-4-6-20270101": "haiku 4.6", "claude-3-7-sonnet": "3 7 sonnet",
	} {
		if got := shortModel(id); got != want {
			t.Errorf("shortModel(%q) = %q, want %q", id, got, want)
		}
	}
	for name, want := range map[string]string{
		"Claude Console (platform.claude.com)": "Console", "Claude API (api.anthropic.com)": "Claude API",
		"Claude Code": "Claude Code", "claude.ai": "claude.ai", "New Thing (new.example.com)": "New Thing",
	} {
		if got := short(name); got != want {
			t.Errorf("short(%q) = %q, want %q", name, got, want)
		}
	}
}

// statusServer serves the fixtures; "down" answers nothing.
func statusServer(t *testing.T, down *bool) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if *down {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("no user agent")
		}
		file := map[string]string{
			"/api/v2/status.json": "status.json", "/api/v2/components.json": "components.json",
			"/api/v2/incidents/unresolved.json": "unresolved.json",
		}[r.URL.Path]
		if file == "" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join("testdata", file))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCollectAndToasts(t *testing.T) {
	down := false
	srv := statusServer(t, &down)
	w := New()
	w.src.Base = srv.URL
	w.scanner = newScanner(projects(t))
	w.limitsBusy = true // no CLI here
	w.usageAt = time.Now()
	w.usage = &usage{H24: 1234, Available: true}

	s, err := w.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.State != model.StateWarn || s.StateLabel != "Console degraded" {
		t.Errorf("state %s %q", s.State, s.StateLabel)
	}
	var ids []string
	for _, is := range s.Issues {
		ids = append(ids, is.ID+" / "+is.Text+" / "+string(is.Tone))
	}
	want := []string{
		"comp:Claude Console (platform.claude.com):degraded_performance / Console degraded / warn",
		"comp:Claude Cowork:under_maintenance / Cowork maintenance / info",
		"inc:inv01 / Elevated errors on Console / warn",
	}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("issues:\n%s", strings.Join(ids, "\n"))
	}
	if s.Glance.Text != "1K" {
		t.Errorf("no limits: the glance is the day's tokens, got %+v", s.Glance)
	}

	// Unreachable: idle, and the issues it knew are kept, so recovery does
	// not raise every incident again.
	down = true
	s, _ = w.Collect(context.Background())
	if s.State != model.StateIdle || s.StateLabel != "status page unreachable" || len(s.Issues) != 3 {
		t.Errorf("unreachable: %s %q %d issues", s.State, s.StateLabel, len(s.Issues))
	}
	if got := w.Transition(model.StateWarn, model.StateIdle, s); got != "Status page unreachable" {
		t.Errorf("transition %q", got)
	}

	// Console recovers: said once, as a notice.
	down = false
	w.comps["Claude Console (platform.claude.com)"] = "degraded_performance"
	orig, _ := os.ReadFile("testdata/components.json")
	fixed := strings.Replace(string(orig), `"degraded_performance"`, `"operational"`, 1)
	srv.Config.Handler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/components.json" {
			rw.Write([]byte(fixed))
			return
		}
		http.ServeFile(rw, r, filepath.Join("testdata", map[string]string{"/api/v2/status.json": "status.json", "/api/v2/incidents/unresolved.json": "unresolved.json"}[r.URL.Path]))
	})
	s, _ = w.Collect(context.Background())
	if len(s.Notices) != 1 || s.Notices[0].Text != "Console operational" {
		t.Errorf("recovery notice: %+v", s.Notices)
	}
}

func rows(blocks model.Blocks) []string {
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
			out = append(out, "  "+v.Text)
		case model.Empty:
			out = append(out, "  ("+v.Text+")")
		}
	}
	return out
}

func TestPanelProblemsFirst(t *testing.T) {
	page := statuspage.Page{Reached: true, Indicator: "minor", Description: "Minor Service Outage",
		Components: []statuspage.Component{{Name: "claude.ai", Status: "operational"}, {Name: "Claude Console (platform.claude.com)", Status: "degraded_performance"}},
		Incidents: []statuspage.Incident{{ID: "i1", Name: "Elevated errors on Console", Status: "investigating", Impact: "minor",
			Latest: statuspage.Update{Body: "We are looking into it.", CreatedAt: now.Add(-12 * time.Minute)}}}}
	s := render(poll{page: page, now: now, limitsAt: now.Add(-4 * time.Minute),
		limits: []limit{{Label: "Session", Pct: 46, Resets: now.Add(6 * time.Hour)}, {Label: "Week", Pct: 91, Resets: now.Add(50 * time.Hour)}},
		usage:  &usage{H1: 348_000, H24: 1_210_000, D7: 9_800_000, Sessions: 6, Available: true, Models: []spend{{"claude-opus-5", 1_100_000}}}})
	loc := now.Local()
	want := []string{
		"# Degraded 1 warn",
		"- Console | degraded | warn",
		"# Active incidents 1 warn",
		"! Elevated errors on Console | investigating · 12m | We are looking into it.",
		"# Usage  ",
		"= Session 46 resets " + loc.Add(6*time.Hour).Format("15:04") + " ",
		"= Week 91 resets " + loc.Add(50*time.Hour).Format("Mon 15:04") + " crit",
		"  1h 348K · 24h 1.2M · 7d 9.8M · sessions 6",
		"  opus 5 1.1M",
		"# Services 1 operational ",
		"  [claude.ai]",
	}
	if got := rows(s.Pages[0].Blocks); !reflect.DeepEqual(got, want) {
		t.Errorf("panel:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if s.Glance.Text != "91%" || s.Glance.Tone != model.ToneCrit || s.Footer != "limits as of 4m ago" {
		t.Errorf("glance %+v footer %q", s.Glance, s.Footer)
	}

	// Nothing ran this week, all well: the sentence, no zeros.
	s = render(poll{page: statuspage.Page{Reached: true, Indicator: "none", Components: page.Components[:1]}, now: now, usage: &usage{}})
	if got := rows(s.Pages[0].Blocks); !reflect.DeepEqual(got, []string{
		"# Usage  ", "  (No Claude Code sessions on this machine in the last 7 days.)", "# Services 1 operational ", "  [claude.ai]",
	}) || s.StateLabel != "operational" || s.Glance.Text != "—" {
		t.Errorf("empty: %q %q %q", got, s.StateLabel, s.Glance.Text)
	}
}

func TestStatusPageFetchIsPartial(t *testing.T) {
	// Components fail after the status answered: the indicator is kept.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/status.json" {
			w.Write([]byte(`{"status":{"indicator":"major","description":"Partial System Outage"}}`))
			return
		}
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	p := statuspage.Source{Base: srv.URL, UserAgent: "test", Incidents: "/api/v2/incidents/unresolved.json"}.Fetch(context.Background())
	if !p.Reached || p.Indicator != "major" || len(p.Components) != 0 || statuspage.State(p) != model.StateCrit {
		t.Errorf("partial: %+v", p)
	}
}

func TestTheCLIIsRunWithAFixedArgv(t *testing.T) {
	none := func(string) bool { return false }
	noNode := func() string { return "" }
	name, prefix := commandWith(`C:\Users\someone\AppData\Roaming\npm\claude.cmd`, none, noNode)
	if name != "cmd.exe" || !reflect.DeepEqual(prefix, []string{"/d", "/c", `C:\Users\someone\AppData\Roaming\npm\claude.cmd`}) {
		t.Errorf("npm shim: %q %q", name, prefix)
	}
	// The shim's target, run directly: node beside it, else on PATH.
	shim := filepath.Join("npm", "claude.cmd")
	cli := filepath.Join("npm", "node_modules", "@anthropic-ai", "claude-code", "cli.js")
	has := func(set ...string) func(string) bool {
		return func(p string) bool {
			for _, s := range set {
				if p == s {
					return true
				}
			}
			return false
		}
	}
	if name, prefix := commandWith(shim, has(cli), func() string { return "node" }); name != "node" || !reflect.DeepEqual(prefix, []string{cli}) {
		t.Errorf("node + cli.js: %q %q", name, prefix)
	}
	if name, _ := commandWith(shim, has(cli, filepath.Join("npm", "node.exe")), noNode); name != filepath.Join("npm", "node.exe") {
		t.Errorf("node beside the shim: %q", name)
	}
	// A path cmd.exe would misread is never handed to it.
	if name, _ := commandWith(`C:\Users\A&B\AppData\Roaming\npm\claude.cmd`, none, noNode); name != "" {
		t.Errorf("a path with & went to cmd.exe: %q", name)
	}
	if name, prefix := command("/home/someone/.local/bin/claude"); name != "/home/someone/.local/bin/claude" || prefix != nil {
		t.Errorf("native: %q %q", name, prefix)
	}
	if name, _ := command(""); name != "" {
		t.Error("no CLI, nothing to run")
	}
}

func TestUsageIsAskedWithoutPersistence(t *testing.T) {
	if got := usageArgs(nil, true); !reflect.DeepEqual(got, []string{"-p", "/usage", "--no-session-persistence"}) {
		t.Errorf("native argv %q", got)
	}
	shim := []string{"/d", "/c", `C:\Users\someone\AppData\Roaming\npm\claude.cmd`}
	if got := usageArgs(shim, true); !reflect.DeepEqual(got, append(append([]string{}, shim...), "-p", "/usage", "--no-session-persistence")) {
		t.Errorf("shim argv %q", got)
	}
	if got := usageArgs(nil, false); !reflect.DeepEqual(got, []string{"-p", "/usage"}) {
		t.Errorf("fallback argv %q", got)
	}
	for _, a := range usageArgs(shim, true) {
		if a == "--bare" {
			t.Error("--bare skips the login")
		}
	}
}

func TestACLIThatRefusesTheFlagIsAskedOnceWithoutIt(t *testing.T) {
	answer, err := os.ReadFile("testdata/usage.txt")
	if err != nil {
		t.Fatal(err)
	}
	flagRefused.Store(false)
	defer flagRefused.Store(false)
	var calls [][]string
	old := func(_ context.Context, dir string, _ time.Duration, name string, args ...string) (string, string, error) {
		calls = append(calls, append([]string{dir, name}, args...))
		for _, a := range args {
			if a == noPersist {
				return "", "error: unknown option '--no-session-persistence'\n", fmt.Errorf("claude: error: unknown option '--no-session-persistence'")
			}
		}
		return string(answer), "", nil
	}
	if out := askUsage(context.Background(), old, "/home/someone", "claude", nil); out != string(answer) {
		t.Fatalf("the retry's answer is read: %q", out)
	}
	if !reflect.DeepEqual(calls, [][]string{
		{"/home/someone", "claude", "-p", "/usage", "--no-session-persistence"},
		{"/home/someone", "claude", "-p", "/usage"},
	}) {
		t.Errorf("calls %q", calls)
	}
	calls = nil
	askUsage(context.Background(), old, "/home/someone", "claude", nil)
	if len(calls) != 1 || len(calls[0]) != 4 {
		t.Errorf("the refusal is remembered: %q", calls)
	}

	// Any other failure is not a refusal: no retry, the flag stays.
	flagRefused.Store(false)
	calls = nil
	down := func(_ context.Context, dir string, _ time.Duration, name string, args ...string) (string, string, error) {
		calls = append(calls, args)
		return "", "", fmt.Errorf("claude timed out after 45s")
	}
	if out := askUsage(context.Background(), down, "/home/someone", "claude", nil); out != "" || len(calls) != 1 || flagRefused.Load() {
		t.Errorf("a timeout is not a refusal: %q %q", out, calls)
	}
}

// "Refresh now" asks the slow sources again but never blanks what they
// last said (the Python widget emptied usage and limits until the next poll).
func TestRefreshKeepsLimitsAndUsage(t *testing.T) {
	down := false
	srv := statusServer(t, &down)
	w := New()
	w.src.Base = srv.URL
	w.limits, w.limitsAt, w.limitsAsked = []limit{{Label: "Week", Pct: 78}}, time.Now().Add(-time.Minute), time.Now().Add(-time.Minute)
	w.usage, w.usageAt = &usage{H24: 5000, D7: 5000, Available: true}, time.Now().Add(-time.Minute)
	w.limitsBusy, w.usageBusy = true, true // the new readings have not landed yet
	w.Force()
	if !w.limitsAsked.IsZero() || !w.usageAt.IsZero() {
		t.Error("a refresh makes both due again")
	}
	s, _ := w.Collect(context.Background())
	if s.Glance.Text != "78%" || len(rows(s.Pages[0].Blocks)) == 0 || !strings.Contains(strings.Join(rows(s.Pages[0].Blocks), "\n"), "= Week 78") {
		t.Errorf("limits blanked by a refresh: %+v", rows(s.Pages[0].Blocks))
	}
	w.limitsAsked = time.Now()
	w.Force()
	if w.limitsAsked.IsZero() {
		t.Error("a second refresh within 10 s does not run the CLI again")
	}
}

// DESIGN "time only moves forward": a limit read before its reset shows no
// percentage and no reset ahead, stays out of the glance, and makes the
// CLI due again.
func TestAPassedResetIsSaidInWords(t *testing.T) {
	p := poll{
		page: statuspage.Page{Reached: true, Indicator: "none"},
		limits: []limit{
			{Label: "Session", Pct: 96, Resets: now.Add(-20 * time.Minute)},
			{Label: "Week", Pct: 40, Resets: now.Add(50 * time.Hour)},
		},
		limitsAt: now.Add(-40 * time.Minute),
		now:      now,
	}
	s := render(p)
	got := rows(s.Pages[0].Blocks)
	want := []string{"# Usage  ", "  Session · reset 20m ago · no reading since", "= Week 40 resets " + sys.Clock(now.Add(50*time.Hour), now) + " "}
	if !reflect.DeepEqual(got[:3], want) {
		t.Errorf("rows %q\nwant %q", got[:3], want)
	}
	if s.Glance.Text != "40%" || s.Glance.Tone != model.ToneNone {
		t.Errorf("glance %+v: the lapsed 96%% is not current", s.Glance)
	}
	w := &Widget{limits: p.limits, limitsAt: now.Add(-40 * time.Minute), limitsAsked: now.Add(-time.Minute)}
	if !w.limitsDue(now) {
		t.Error("a reset since the reading makes the CLI due")
	}
	w.limitsAsked = now.Add(-5 * time.Second)
	if w.limitsDue(now) {
		t.Error("but no faster than a refresh may ask")
	}
	w.limitsAsked, w.limitsAt = now.Add(-time.Minute), now.Add(-10*time.Minute) // read after the reset
	if w.limitsDue(now) {
		t.Error("a reading taken after the reset is current")
	}
}

// DESIGN A20: a plan nearly used up raises the widget however well the
// status page reads, the label names it, and its end says nothing.
func TestAFullPlanRaisesTheState(t *testing.T) {
	s := render(poll{
		page:   statuspage.Page{Reached: true, Indicator: "none"},
		limits: []limit{{Label: "Week", Pct: 92, Resets: now.Add(time.Hour)}, {Label: "Session", Pct: 10}},
		now:    now,
	})
	w := &Widget{}
	w.wasRaised, w.raised = w.raised, model.RaiseToWorst(&s)
	if s.State != model.StateCrit || s.StateLabel != "week 92%" || s.Glance.Tone != model.ToneCrit {
		t.Fatalf("state %q %q glance %+v", s.State, s.StateLabel, s.Glance)
	}
	if msg := w.Transition(model.StateOK, s.State, s); msg != "Week 92%" {
		t.Errorf("raised: %q", msg)
	}
	calm := render(poll{page: statuspage.Page{Reached: true, Indicator: "none"}, limits: []limit{{Label: "Week", Pct: 3}}, now: now})
	w.wasRaised, w.raised = w.raised, model.RaiseToWorst(&calm)
	if calm.State != model.StateOK || calm.StateLabel != "operational" {
		t.Fatalf("calm %q %q", calm.State, calm.StateLabel)
	}
	if msg := w.Transition(model.StateCrit, calm.State, calm); msg != "" {
		t.Errorf("the limit falling back is not 'All systems operational': %q", msg)
	}
}

// Before the CLI's first answer, an empty Usage is not "no plan limits".
func TestLimitsBeingRead(t *testing.T) {
	s := render(poll{page: statuspage.Page{Reached: true, Indicator: "none"}, reading: true, now: now})
	if got := rows(s.Pages[0].Blocks); len(got) < 2 || got[1] != "  Plan limits · reading…" {
		t.Errorf("rows %q", got)
	}
}
