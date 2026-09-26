package docker

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

// The fixtures are an invented host; every name in them is made up.
var now = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// host is the fixtures as one collect: inspect, stats and df.
func host(t *testing.T) *data {
	t.Helper()
	cs, err := parseInspect([]byte(fixture(t, "inspect.json")), now)
	if err != nil {
		t.Fatal(err)
	}
	stats := parseStats(fixture(t, "stats.txt"))
	for _, c := range cs {
		if u, ok := stats[c.Name]; ok {
			c.CPU, c.Mem, c.MemB = u.cpu, u.mem, u.memB
		}
	}
	return &data{
		available: true, all: cs, units: group(cs), portainer: portainer(cs),
		sys: &system{engine: "27.3.1", compose: "2.29.7", dangling: 7, df: parseDF(fixture(t, "df.txt"))},
	}
}

func byName(cs []*container) map[string]*container {
	m := map[string]*container{}
	for _, c := range cs {
		m[c.Name] = c
	}
	return m
}

func TestInspectReadsEveryField(t *testing.T) {
	cs := byName(host(t).all)
	if len(cs) != 15 {
		t.Fatalf("%d containers, want 15", len(cs))
	}

	gw := cs["api-gateway"]
	if gw.State != "running" || gw.HasExit || gw.Health != "healthy" || gw.Policy != "unless-stopped" || gw.Image != "traefik:v3" {
		t.Errorf("api-gateway: %+v", gw)
	}
	if gw.UpH < 41.9 || gw.UpH > 42.1 || gw.AgeH != -1 {
		t.Errorf("api-gateway up %.2fh age %.2f, want 42h and unknown", gw.UpH, gw.AgeH)
	}
	// First binding only; every-interface binds are blank; unpublished ports skipped.
	if !reflect.DeepEqual(gw.Ports, []string{":8080"}) {
		t.Errorf("api-gateway ports %q", gw.Ports)
	}
	if got := cs["ledger-db"].Ports; !reflect.DeepEqual(got, []string{"127.0.0.1:5432"}) {
		t.Errorf("ledger-db ports %q", got)
	}
	if got := cs["media-proxy"].Ports; !reflect.DeepEqual(got, []string{":8443->443"}) {
		t.Errorf("media-proxy ports %q", got)
	}
	if got := cs["shop-web"].Ports; !reflect.DeepEqual(got, []string{":8081->80"}) {
		t.Errorf("shop-web ports %q", got)
	}

	api := cs["ledger-api"]
	if api.Project != "ledger" || api.Service != "api" || api.Health != "unhealthy" {
		t.Errorf("ledger-api: %+v", api)
	}
	// A label with a tab and a newline cannot split a record.
	if cs["ledger-db"].Image != "postgres:16" {
		t.Errorf("ledger-db image %q", cs["ledger-db"].Image)
	}

	// No exit code while restarting; no health unless running.
	if cr := cs["cron-runner"]; cr.HasExit || cr.Health != "" || cr.Restarts != 14 || cr.Crashed {
		t.Errorf("cron-runner: %+v", cr)
	}
	if cs["media-proxy"].HasExit {
		t.Error("a paused container has no exit code")
	}
	if cs["scratch"].AgeH != -1 || cs["scratch"].Crashed {
		t.Errorf("scratch (created, zero FinishedAt): %+v", cs["scratch"])
	}
	if cs["api-gateway"].Policy != "unless-stopped" || cs["docs-preview"].Policy != "" {
		t.Error(`restart policy "no" reads as none`)
	}
}

func TestExitCodesThatAreStopsAreNotCrashes(t *testing.T) {
	cs := byName(host(t).all)
	for name, crashed := range map[string]bool{
		"ledger-worker": true,  // exit 1
		"thumbnailer":   true,  // 137 with OOMKilled
		"docs-preview":  false, // 143: docker stop
		"batch-job":     false, // 137 without OOM: stop timeout or kill
		"legacy-cron":   false, // 0
		"scratch":       false, // never started
	} {
		if cs[name].Crashed != crashed {
			t.Errorf("%s crashed = %v, want %v", name, cs[name].Crashed, crashed)
		}
	}
	if !cs["thumbnailer"].OOM || cs["thumbnailer"].AgeH != 3 {
		t.Errorf("thumbnailer: %+v", cs["thumbnailer"])
	}
}

func TestUnitsGroupStacksAndSumExactBytes(t *testing.T) {
	d := host(t)
	var names []string
	units := map[string]*unit{}
	for _, u := range d.units {
		names = append(names, u.Name)
		units[u.Name] = u
	}
	want := []string{"api-gateway", "batch-job", "cron-runner", "docs-preview", "ledger", "legacy-cron",
		"media-proxy", "portainer", "scratch", "shopfront", "thumbnailer"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("units %q\nwant  %q", names, want)
	}
	ledger := units["ledger"]
	if !ledger.Stack || len(ledger.Members) != 3 || len(ledger.Running) != 2 || len(ledger.Bad) != 1 || len(ledger.Crashed) != 1 {
		t.Errorf("ledger: members %d running %d bad %d crashed %d", len(ledger.Members), len(ledger.Running), len(ledger.Bad), len(ledger.Crashed))
	}
	if ledger.AgeH != 2 {
		t.Errorf("ledger age %.1fh, want 2 (its worker's stop)", ledger.AgeH)
	}
	// 492.1 MiB + 1.21 GiB, from the exact numbers.
	if want := 492.1*(1<<20) + 1.21*(1<<30); ledger.Mem < want-1 || ledger.Mem > want+1 {
		t.Errorf("ledger mem %.0f, want %.0f", ledger.Mem, want)
	}
	if ledger.CPU < 8.41 || ledger.CPU > 8.43 {
		t.Errorf("ledger cpu %.2f", ledger.CPU)
	}
	if units["media-proxy"].Running[0].State != "paused" {
		t.Error("a paused container counts as running")
	}
	if len(units["cron-runner"].Running) != 0 || len(units["cron-runner"].Bad) != 1 {
		t.Error("a restarting container is bad, not running")
	}
}

func TestStateAndIssues(t *testing.T) {
	s := render(host(t), pending{})
	if s.State != model.StateCrit || s.StateLabel != "2 containers unhealthy" {
		t.Errorf("state %s %q", s.State, s.StateLabel)
	}
	if s.Glance.Text != "7" || s.Glance.Tone != model.ToneCrit {
		t.Errorf("glance %+v, want 7 running (paused counts), crit", s.Glance)
	}
	var ids []string
	for _, is := range s.Issues {
		ids = append(ids, is.ID+" / "+is.Text)
	}
	want := []string{"restarting:cron-runner / cron-runner is restart-looping", "unhealthy:ledger-api / ledger-api is unhealthy"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("issues %q", ids)
	}
	if s.Footer != "engine 27.3 · compose 2.29" {
		t.Errorf("footer %q", s.Footer)
	}
	if len(s.Menu) != 1 || s.Menu[0].URL != "https://localhost:9443" {
		t.Errorf("menu %+v", s.Menu)
	}

	// Crashes alone are news for a day: warn, and never an issue (toast).
	d := host(t)
	var calm []*container
	for _, c := range d.all {
		if c.Name != "ledger-api" && c.Name != "cron-runner" {
			calm = append(calm, c)
		}
	}
	d.all, d.units = calm, group(calm)
	s = render(d, pending{})
	if s.State != model.StateWarn || s.StateLabel != "2 crashed today" || len(s.Issues) != 0 {
		t.Errorf("state %s %q issues %d", s.State, s.StateLabel, len(s.Issues))
	}
}

func TestUnavailable(t *testing.T) {
	for _, c := range []struct {
		d     data
		label string
		text  string
	}{
		{data{}, "daemon unreachable", "Docker is not running, or this user cannot reach its socket. Check with"},
		{data{windows: true}, "Docker Desktop not running", "Docker Desktop is not running."},
		{data{missing: true}, "not installed", "docker is not installed on this machine."},
	} {
		s := render(&c.d, pending{})
		if s.State != model.StateIdle || s.StateLabel != c.label || s.Glance.Text != "off" {
			t.Errorf("%+v: %s %q %q", c.d, s.State, s.StateLabel, s.Glance.Text)
		}
		if e, ok := s.Pages[0].Blocks[0].(model.Empty); !ok || e.Text != c.text || len(s.Pages) != 1 {
			t.Errorf("%+v: %+v", c.d, s.Pages)
		}
	}
}

func page(s model.Snapshot, id string) model.Blocks {
	for _, p := range s.Pages {
		if p.ID == id {
			return p.Blocks
		}
	}
	return nil
}

func TestNowPage(t *testing.T) {
	blocks := page(render(host(t), pending{}), "now")
	var got []string
	for _, b := range blocks {
		switch v := b.(type) {
		case model.Section:
			got = append(got, "# "+v.Title+" "+v.Meta)
		case model.Alert:
			got = append(got, "! "+v.Title+" | "+v.Meta+" | "+strings.ReplaceAll(v.Body, "\n", " / "))
		case model.Row:
			got = append(got, "- "+v.Name+" | "+v.Value+" | "+v.Sub+" | "+string(v.LED))
		case model.Note:
			got = append(got, "  "+v.Text)
		}
	}
	want := []string{
		"# Needs attention 4",
		"! cron-runner | restarting · <1m | example/cron:1 / up <1m · restarted 14×",
		"! ledger-api | unhealthy · 3m | example/ledger-api:2.4 / up 3m",
		"! ledger-worker | exit 1 · 2h | example/ledger-worker:2.4 / exit 1 · error · restart=always but not back",
		"! thumbnailer | OOM (137) · 3h | example/thumbs:0.9 / OOM-killed (137) · restart=unless-stopped but not back",
		"# Running 7",
		"- shopfront | 2/3 up · 22.2G · 2% | cache, web   down: worker | warn",
		"- ledger | 2/3 up · 1.7G · 8% | api, db   down: worker | warn",
		"- api-gateway | up 42h · 48M | traefik:v3 · :8080 · healthy | ok",
		"- portainer | up 25d · 19M | portainer/portainer-ce:2 · :8000 · :9443 | ok",
		"- media-proxy | paused | nginx:1.27 · :8443->443 | warn",
		"# Elsewhere ",
		"  5 stopped · 1 broken — see Stopped",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("now page:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, b := range blocks {
		if a, ok := b.(model.Alert); ok && len([]rune(a.Meta)) > 24 {
			t.Errorf("alert meta %q is over 24 characters", a.Meta)
		}
	}
}

func TestStoppedPage(t *testing.T) {
	d := host(t)
	byName(d.all)["thumbnailer"].Log = "Killed: memory cgroup limit reached"
	blocks := page(render(d, pending{}), "stopped")
	var got []string
	for _, b := range blocks {
		switch v := b.(type) {
		case model.Section:
			got = append(got, "# "+v.Title+" "+v.Meta+" "+string(v.Tone))
		case model.Row:
			var acts []string
			for _, a := range v.Actions {
				acts = append(acts, a.ID+"("+a.Confirm+")")
			}
			got = append(got, "- "+v.Name+" | "+v.Value+" | "+v.Sub+" | "+string(v.LED)+" | "+strings.Join(acts, " "))
		}
	}
	want := []string{
		"# Broken 1 · crashed, not touched since crit",
		"- thumbnailer | OOM · 3h ago | Killed: memory cgroup limit reached | crit | unit.start(start?) unit.remove(remove?)",
		"# Stopped 1 · this week ",
		"- docs-preview | 2d ago | stopped (SIGTERM) | idle | unit.start(start?) unit.remove(remove?)",
		"# Dormant 3 · not run in a week ",
		"- batch-job | 4w ago | killed (137, docker stop timeout or kill) | idle | unit.start(start?) unit.remove(remove?)",
		"- legacy-cron | 3mo ago | exited cleanly | idle | unit.start(start?) unit.remove(remove?)",
		"- scratch | never started | created, never started | idle | unit.start(start?) unit.remove(remove?)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stopped page:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestSystemPage(t *testing.T) {
	blocks := page(render(host(t), pending{}), "system")
	var got []string
	for _, b := range blocks {
		switch v := b.(type) {
		case model.Row:
			line := v.Name + " | " + v.Value + " | " + string(v.LED)
			for _, a := range v.Actions {
				line += " | " + a.ID + " " + a.Confirm
			}
			got = append(got, line)
		case model.Note:
			got = append(got, "  "+v.Text)
		}
	}
	want := []string{
		"Docker | 27.3.1 | ",
		"Compose | 2.29.7 | ",
		"Containers | 15 · 6 running | ",
		"Images | 46 · 7 dangling | ",
		"Volumes | 21 · 14 in use | ",
		"images | 38G of 61G | warn | prune.images free 38G?",
		"containers | 1.4G of 3.1G | idle | prune.containers remove 7?",
		"build cache | 0 of 0 | idle | prune.builder prune?",
		"volumes | 6.8G of 48G | idle",
		"  Buttons ask twice. Nothing in use is ever removed.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("system page:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	d := host(t)
	d.sys.df = map[string]dfRow{}
	if last := page(render(d, pending{}), "system"); last[len(last)-1].(model.Note).Text != "docker system df did not answer" {
		t.Errorf("no df: %+v", last[len(last)-1])
	}
}

func TestPendingHidesButtonsAndReadsBusy(t *testing.T) {
	d := host(t)
	// One member of a stack being restarted: its alert turns into a busy
	// row and the whole stack keeps its hands off.
	s := render(d, pending{"unit:ledger-api": "restarting"})
	for _, b := range page(s, "now") {
		switch v := b.(type) {
		case model.Alert:
			if v.Title == "ledger-api" {
				t.Error("a pending container is not an alert")
			}
		case model.Row:
			if v.Name == "ledger-api" && (v.Value != "restarting…" || v.Tone != model.ToneBusy || v.LED != model.ToneBusy) {
				t.Errorf("pending row %+v", v)
			}
			if v.Name == "ledger" && len(v.Actions) != 0 {
				t.Error("a stack with a member being acted on offers no buttons")
			}
		}
	}
	s = render(d, pending{"unit:ledger": "stopping", "unit:ledger-api": "stopping", "unit:ledger-db": "stopping", "unit:ledger-worker": "stopping"})
	for _, b := range page(s, "now") {
		if a, ok := b.(model.Alert); ok && a.Title == "ledger-worker" {
			t.Error("the stack's own members are busy too")
		}
	}
	// A prune is its own scope.
	s = render(d, pending{"prune:images": "pruning"})
	for _, b := range page(s, "system") {
		if r, ok := b.(model.Row); ok && r.Name == "images" && r.Value != "pruning…" {
			t.Errorf("images row %+v", r)
		}
	}
}

func TestWhy(t *testing.T) {
	for _, c := range []struct {
		c    container
		want string
	}{
		{container{State: "created"}, "created, never started"},
		{container{State: "exited", HasExit: true, Exit: 137, OOM: true}, "OOM-killed (137)"},
		{container{State: "exited", HasExit: true}, "exited cleanly"},
		{container{State: "exited", HasExit: true, Exit: 143}, "stopped (SIGTERM)"},
		{container{State: "exited", HasExit: true, Exit: 137}, "killed (137, docker stop timeout or kill)"},
		{container{State: "exited", HasExit: true, Exit: 139, Crashed: true}, "exit 139 · segfault (SIGSEGV)"},
		{container{State: "exited", HasExit: true, Exit: 42, Crashed: true, Policy: "always"}, "exit 42 · error · restart=always but not back"},
		{container{State: "exited", HasExit: true, Exit: 143, Policy: "always"}, "stopped (SIGTERM)"},
		{container{State: "removing"}, "removing"},
	} {
		if got := why(&c.c); got != c.want {
			t.Errorf("why(%+v) = %q, want %q", c.c, got, c.want)
		}
	}
}

func TestFormats(t *testing.T) {
	stats := parseStats(fixture(t, "stats.txt"))
	for name, want := range map[string]string{
		"ledger-api": "492M", "ledger-db": "1.2G", "shop-web": "22G", "shop-cache": "", "media-proxy": "",
	} {
		if got := stats[name].mem; got != want {
			t.Errorf("%s mem %q, want %q", name, got, want)
		}
	}
	if fmtMem("22.21GiB") != "22G" || fmtMem("9.87GiB") != "9.9G" || fmtMem("1.5TiB") != "1.5T" {
		t.Error("one decimal only for G and T below ten")
	}
	if stats["shop-cache"].memB != 812*1024 || stats["ledger-api"].cpu != 7.12 {
		t.Errorf("exact bytes and cpu: %+v", stats["shop-cache"])
	}
	if fmtBytesShort(1.5*(1<<30)) != "1.5G" || fmtBytesShort(492*(1<<20)) != "492M" || fmtBytesShort(900*1024) != "" {
		t.Error("fmtBytesShort")
	}

	df := parseDF(fixture(t, "df.txt"))
	if r := df["images"]; r.recl != 38.07e9 || r.pct != "62%" || r.total != "46" || r.active != "12" {
		t.Errorf("images %+v", r)
	}
	if r := df["build cache"]; r.size != 0 || r.recl != 0 {
		t.Errorf("build cache %+v", r)
	}
	if v, _ := sizes("1.2kB"); v != 1200 {
		t.Errorf("kB is decimal: %v", v)
	}
	for n, want := range map[float64]string{0: "0", 512e6: "512M", 9.5e9: "9.5G", 83e9: "83G", 1.2e12: "1.2T", 900: "900B"} {
		if got := fmtDec(n); got != want {
			t.Errorf("fmtDec(%v) = %q, want %q", n, got, want)
		}
	}

	for h, want := range map[float64]string{-1: "", 0.005: "<1m", 0.4: "24m", 46.2: "46h", 90: "3d 18h", 300: "12d"} {
		if got := dur(h); got != want {
			t.Errorf("dur(%v) = %q, want %q", h, got, want)
		}
	}
	for h, want := range map[float64]string{-1: "", 0.005: "just now", 0.4: "24m ago", 46: "46h ago", 120: "5d ago", 504: "3w ago", 2880: "4mo ago"} {
		if got := ago(h); got != want {
			t.Errorf("ago(%v) = %q, want %q", h, got, want)
		}
	}
	if short("27.3.1") != "27.3" || short("v2.29.7") != "2.29" || short("5") != "5" {
		t.Error("short versions")
	}
}

func TestLastLogLine(t *testing.T) {
	out := "starting\n\x1b[31mERROR\x1b[0m   first\n\n2026-09-26T10:00:00.123Z   \x1b[1mpanic:\x1b[0m   runtime error: index out of range [3] with length 3\n   \n"
	if got := lastLogLine(out); got != "panic: runtime error: index out of range [3] with length 3" {
		t.Errorf("%q", got)
	}
	if got := lastLogLine(strings.Repeat("é", 120)); len([]rune(got)) != 90 {
		t.Errorf("cut to %d runes", len([]rune(got)))
	}
	if lastLogLine("") != "" {
		t.Error("nothing written")
	}
}

func TestPortainer(t *testing.T) {
	c := func(ports ...string) []*container {
		return []*container{{Name: "portainer", State: "running", Ports: ports}}
	}
	for _, x := range []struct {
		ports []string
		want  string
	}{
		{[]string{":8000", ":9443"}, "https://localhost:9443"},
		{[]string{":8000", ":9000"}, "http://localhost:9000"},
		{[]string{":8000", "127.0.0.1:9001"}, "http://localhost:9001"},
		{[]string{":8000"}, ""},
		{[]string{":19443->9443"}, "http://localhost:19443"},
	} {
		if got := portainer(c(x.ports...)); got != x.want {
			t.Errorf("%q → %q, want %q", x.ports, got, x.want)
		}
	}
	if portainer([]*container{{Name: "portainer-agent", State: "running", Ports: []string{":9001"}}}) != "" {
		t.Error("only a container named exactly portainer")
	}
}

func TestPlansValidateAndPreferCompose(t *testing.T) {
	d := host(t)
	yes := true
	w := New()
	w.compose = &yes
	ctx := context.Background()

	for _, bad := range []struct{ action, arg string }{
		{"unit.stop", "no-such-stack"},
		{"unit.stop", "-rf"},
		{"container.remove", "--all"},
		{"container.stop", ""},
		{"unit.explode", "ledger"},
		{"prune.volumes", ""},
	} {
		key := strings.SplitN(bad.action, ".", 2)[0]
		if _, err := w.plan(ctx, d, bad.action, map[string]string{key: bad.arg}); err == nil {
			t.Errorf("%s %q was accepted", bad.action, bad.arg)
		}
	}

	p, err := w.plan(ctx, d, "unit.remove", map[string]string{"unit": "ledger"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.cmd, []string{"compose", "-p", "ledger", "down"}) ||
		!reflect.DeepEqual(p.fallback, []string{"rm", "--", "ledger-api", "ledger-db", "ledger-worker"}) {
		t.Errorf("stack remove: %q then %q", p.cmd, p.fallback)
	}
	if !reflect.DeepEqual(p.marks, []string{"unit:ledger", "unit:ledger-api", "unit:ledger-db", "unit:ledger-worker"}) {
		t.Errorf("marks %q", p.marks)
	}

	p, _ = w.plan(ctx, d, "unit.restart", map[string]string{"unit": "api-gateway"})
	if !reflect.DeepEqual(p.cmd, []string{"restart", "--", "api-gateway"}) || p.fallback != nil {
		t.Errorf("standalone restart: %q", p.cmd)
	}
	p, _ = w.plan(ctx, d, "unit.resume", map[string]string{"unit": "media-proxy"})
	if !reflect.DeepEqual(p.cmd, []string{"unpause", "--", "media-proxy"}) {
		t.Errorf("resume: %q", p.cmd)
	}
	p, _ = w.plan(ctx, d, "container.stop", map[string]string{"container": "ledger-api"})
	if !reflect.DeepEqual(p.cmd, []string{"stop", "--", "ledger-api"}) || p.label != "ledger-api" {
		t.Errorf("container stop: %q", p.cmd)
	}
	p, _ = w.plan(ctx, d, "prune.builder", nil)
	if !reflect.DeepEqual(p.cmd, []string{"builder", "prune", "-f"}) || p.label != "build cache" || p.marks[0] != "prune:build cache" {
		t.Errorf("prune: %+v", p)
	}

	no := false
	w.compose = &no
	p, _ = w.plan(ctx, d, "unit.stop", map[string]string{"unit": "shopfront"})
	if !reflect.DeepEqual(p.cmd, []string{"stop", "--", "shop-web", "shop-cache", "shop-worker"}) || p.fallback != nil {
		t.Errorf("stack stop without compose: %q", p.cmd)
	}
}

// fakeDocker is a docker that answers from a script, for the parts that run
// commands.
func fakeDocker(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for docker")
	}
	bin := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestRunFallsBackAndSaysWhy(t *testing.T) {
	ctx := context.Background()
	w := New()
	var pokes int
	w.SetRefresh(func() { pokes++ })

	bin := fakeDocker(t, `[ "$1" = compose ] && { echo "no such project" >&2; exit 1; }; exit 0`)
	msg, err := w.run(ctx, bin, plan{verb: "stop", label: "ledger", marks: []string{"unit:ledger"},
		cmd: []string{"compose", "-p", "ledger", "stop"}, fallback: []string{"stop", "--", "ledger-api"}})
	if err != nil || msg != "Stopped ledger" {
		t.Errorf("fallback: %q %v", msg, err)
	}
	if len(w.pending) != 0 || pokes != 1 {
		t.Errorf("pending %v after, %d pokes", w.pending, pokes)
	}

	bin = fakeDocker(t, `echo "Error response from daemon: removal of container legacy-cron is already in progress, and this line is long enough to be cut" >&2; exit 1`)
	_, err = w.run(ctx, bin, plan{verb: "remove", label: "legacy-cron", marks: []string{"unit:legacy-cron"}, cmd: []string{"rm", "--", "legacy-cron"}})
	if err == nil || err.Error() != "legacy-cron: Error response from daemon: removal of container legacy-cron is alread" {
		t.Errorf("failure: %v", err)
	}
	if pokes != 3 {
		t.Errorf("a failure refreshes itself: %d pokes", pokes)
	}

	w.sysAt = time.Now()
	if _, err := w.run(ctx, fakeDocker(t, "exit 0"), plan{verb: "prune", label: "images", prune: true, cmd: []string{"image", "prune", "-a", "-f"}}); err != nil {
		t.Fatal(err)
	}
	if !w.sysAt.IsZero() {
		t.Error("a prune makes df ask again")
	}
}

func TestLogLinesAreCachedByStopTime(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	bin := fakeDocker(t, `echo "$@" >> `+calls+`; echo "line from $5"; echo "2026-09-26T10:00:00Z last words of $5" >&2`)
	d := host(t)
	w := New()
	ctx := context.Background()
	w.fetchLogs(ctx, bin, d.all)
	cs := byName(d.all)
	if cs["thumbnailer"].Log != "last words of thumbnailer" {
		t.Errorf("log %q", cs["thumbnailer"].Log)
	}
	if cs["docs-preview"].Log != "" {
		t.Error("only broken, unhealthy or restarting containers are asked")
	}
	first, _ := os.ReadFile(calls)

	d = host(t)
	w.fetchLogs(ctx, bin, d.all)
	all, _ := os.ReadFile(calls)
	second := strings.TrimPrefix(string(all), string(first))
	// Stopped ones come from the cache; the live ones are asked again.
	if strings.Count(string(first), "\n") != 4 || strings.Count(second, "\n") != 2 ||
		!strings.Contains(second, "cron-runner") || !strings.Contains(second, "ledger-api") {
		t.Errorf("first poll:\n%ssecond poll:\n%s", first, second)
	}
	if byName(d.all)["ledger-worker"].Log != "last words of ledger-worker" {
		t.Error("a cached line is used")
	}
}

func TestLogLookupsAreCapped(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	bin := fakeDocker(t, `echo x >> `+calls+`; echo "$5"`)
	var cs []*container
	for i := range 12 {
		cs = append(cs, &container{Name: "crash-" + string(rune('a'+i)), State: "exited", Crashed: true, Finished: "t", AgeH: float64(i)})
	}
	New().fetchLogs(context.Background(), bin, cs)
	got, _ := os.ReadFile(calls)
	if n := strings.Count(string(got), "\n"); n != logFetches {
		t.Errorf("%d lookups, want %d", n, logFetches)
	}
	if cs[0].Log != "crash-a" || cs[11].Log != "" {
		t.Error("the most recent first")
	}
}
