package tailscale

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

// The fixtures are an invented tailnet; every name and address in them is
// made up.
var now = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

func load(t *testing.T, name string, at time.Time) *status {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	st, err := parseStatus(b, at)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func peers(st *status) map[string]*peer {
	m := map[string]*peer{}
	for _, p := range st.Peers {
		m[p.Host] = p
	}
	return m
}

func pin(hosts ...string) func() *[]string {
	return func() *[]string { return &hosts }
}

func TestParseStatus(t *testing.T) {
	st := load(t, "status-running.json", now)
	if st.Backend != "Running" || st.Self != "workstation" || !st.SelfOnline {
		t.Errorf("self: %+v", st)
	}
	if st.IP != "100.64.0.10" || st.DNS != "workstation.tail0c4fe.ts.net" {
		t.Errorf("ip %q dns %q (IPv4 first, trailing dot dropped)", st.IP, st.DNS)
	}
	if st.Version != "1.80.2" || !st.Update {
		t.Errorf("version %q update %v", st.Version, st.Update)
	}
	if !st.HasExpiry || st.ExpiryDays != 3 {
		t.Errorf("key expiry %v %.2f", st.HasExpiry, st.ExpiryDays)
	}
	if !reflect.DeepEqual(st.Health, []string{"Some peers are advertising routes but --accept-routes is false"}) {
		t.Errorf("health %q (empty lines dropped)", st.Health)
	}
	if !st.ExitOffered || !reflect.DeepEqual(st.Routes, []string{"10.10.0.0/16"}) || st.ExitNode != "exit-fra" {
		t.Errorf("exit %v routes %q exit node %q", st.ExitOffered, st.Routes, st.ExitNode)
	}

	ps := peers(st)
	if len(ps) != 9 {
		t.Fatalf("%d peers", len(ps))
	}
	// One-digit fractional seconds.
	if want := time.Date(2026, 9, 26, 11, 48, 0, 1e8, time.UTC); !ps["fileserver"].Last.Equal(want) {
		t.Errorf("fileserver last seen %v", ps["fileserver"].Last)
	}
	if !ps["pi-garage"].Last.IsZero() {
		t.Error("the zero LastSeen is unknown")
	}
	if !ps["old-vm"].KeyExpired || ps["old-desktop"].KeyExpired {
		t.Error("key expiry is compared with now")
	}
	if ps["build-agent"].Direct || ps["build-agent"].Relay != "fra" || !ps["atlas"].Direct {
		t.Error("direct means a current address")
	}
	if ps["phone"] == nil || ps["phone"].name != "phone" || !strings.HasPrefix(ps["phone"].id, "nodekey:") {
		t.Error("peers are named by MagicDNS (Phone → phone) and known by node key")
	}

	if st := load(t, "status-stopped.json", now); st.Backend != "Stopped" || st.SelfOnline || len(st.Peers) != 0 || st.IP != "100.64.0.10" {
		t.Errorf("stopped: %+v", st)
	}
	if _, err := parseStatus([]byte("tailscaled is not running"), now); err == nil {
		t.Error("not JSON is unreachable")
	}
	if _, err := parseStatus([]byte("{}"), now); err == nil {
		t.Error("JSON without a backend state is unreachable")
	}
}

func TestState(t *testing.T) {
	for _, c := range []struct {
		name   string
		file   string
		watch  func() *[]string
		mutate func(*status)
		state  model.State
		label  string
		glance string
	}{
		{"a watched peer offline", "status-running.json", pin("fileserver", "atlas"), nil, model.StateWarn, "fileserver offline", "1/2"},
		{"two watched peers offline", "status-running.json", pin("fileserver", "pi-garage", "atlas"), nil, model.StateWarn, "2 peers offline", "1/3"},
		{"health", "status-running.json", pin("atlas"), nil, model.StateWarn, "1 health warning", "1/1"},
		{"key expiring alone", "status-running.json", pin("atlas"), func(st *status) { st.Health = nil }, model.StateWarn, "key expires in 3d", "3d"},
		{"all well", "status-running.json", pin("atlas", "PHONE"), func(st *status) { st.Health, st.ExpiryDays = nil, 90 }, model.StateOK, "2 peers up", "2/2"},
		{"one up", "status-running.json", pin("atlas"), func(st *status) { st.Health, st.HasExpiry = nil, false }, model.StateOK, "1 peer up", "1/1"},
		{"self offline", "status-running.json", pin(), func(st *status) { st.SelfOnline = false }, model.StateCrit, "this node offline", "off"},
		{"stopped", "status-stopped.json", nil, nil, model.StateCrit, "stopped", "off"},
		{"logged out", "status-needslogin.json", nil, nil, model.StateCrit, "needs login", "off"},
	} {
		st := load(t, c.file, now)
		if c.mutate != nil {
			c.mutate(st)
		}
		w := New(c.watch)
		p := &poll{available: true, st: st, now: now, exposure: &exposure{}}
		p.pinned = w.observe(st, now)
		s := render(p)
		if s.State != c.state || s.StateLabel != c.label || s.Glance.Text != c.glance {
			t.Errorf("%s: %s %q %q, want %s %q %q", c.name, s.State, s.StateLabel, s.Glance.Text, c.state, c.label, c.glance)
		}
		if (c.state == model.StateWarn || c.state == model.StateCrit) && s.Glance.Tone != c.state.Tone() {
			t.Errorf("%s: glance tone %q", c.name, s.Glance.Tone)
		}
	}
}

func TestIssuesInOrder(t *testing.T) {
	st := load(t, "status-running.json", now)
	w := New(pin("fileserver", "pi-garage"))
	w.observe(st, now)
	s := render(&poll{available: true, st: st, now: now})
	var got []string
	for _, is := range s.Issues {
		got = append(got, is.ID+" / "+is.Text+" / "+string(is.Tone))
	}
	want := []string{
		"down:fileserver / fileserver is offline / warn",
		"down:pi-garage / pi-garage is offline / warn",
		"health:Some peers are advertising routes but --accept-routes is false / Some peers are advertising routes but --accept-routes is false / warn",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("issues:\n%s", strings.Join(got, "\n"))
	}
	st = load(t, "status-needslogin.json", now)
	New(nil).observe(st, now)
	s = render(&poll{available: true, st: st, now: now})
	if s.Issues[0].ID != "backend:NeedsLogin" || s.Issues[0].Tone != model.ToneCrit || s.Issues[0].Text != "Tailscale needs a login" {
		t.Errorf("backend issue %+v", s.Issues[0])
	}
}

func TestAutoWatchGrowsOnlyFromPeersSeenOnline(t *testing.T) {
	w := New(nil)
	st := load(t, "status-running.json", now)
	if pinned := w.observe(st, now); pinned {
		t.Error("no list is not pinned")
	}
	ps := peers(st)
	if !ps["atlas"].Watched || ps["fileserver"].Watched || ps["pi-garage"].Watched {
		t.Error("watched: online ones only; the dormant at start never")
	}
	// A poll later atlas has dropped off: it stays watched, and is news.
	st = load(t, "status-running.json", now.Add(30*time.Second))
	peers(st)["atlas"].Online = false
	w.observe(st, now.Add(30*time.Second))
	if !peers(st)["atlas"].Watched {
		t.Error("a peer that blips offline stays watched")
	}
	if s := render(&poll{available: true, st: st, now: now}); s.StateLabel != "atlas offline" {
		t.Errorf("label %q", s.StateLabel)
	}

	// An empty list watches nothing — not the same as none.
	st = load(t, "status-running.json", now)
	w = New(pin())
	if pinned := w.observe(st, now); !pinned {
		t.Error("an empty list is pinned")
	}
	for _, p := range st.Peers {
		if p.Watched {
			t.Errorf("%s watched by an empty list", p.Host)
		}
	}
}

func TestRateBetweenPollsAndCounterReset(t *testing.T) {
	w := New(nil)
	st := load(t, "status-running.json", now)
	w.observe(st, now)
	if peers(st)["atlas"].HasRate {
		t.Error("no rate on first sight")
	}

	later := now.Add(10 * time.Second)
	st = load(t, "status-running.json", later)
	atlas, agent := peers(st)["atlas"], peers(st)["build-agent"]
	atlas.rx += 10 << 20 // 10 MiB in, 2 MiB out over 10 s
	atlas.tx += 2 << 20
	agent.rx = 100 // restarted: the counter went backwards
	w.observe(st, later)
	if !atlas.HasRate || atlas.Rate != 1.2*(1<<20) {
		t.Errorf("atlas rate %v %.0f", atlas.HasRate, atlas.Rate)
	}
	if agent.HasRate {
		t.Error("a reset counter gives no rate, never a negative one")
	}
	if got := fmtRate(atlas.Rate); got != "1.2 MB/s" {
		t.Errorf("fmtRate %q", got)
	}
	for bps, want := range map[float64]string{0: "", 1000: "", 340 * 1024: "340.0 KB/s", 3 << 30: "3.0 GB/s"} {
		if got := fmtRate(bps); got != want {
			t.Errorf("fmtRate(%v) = %q, want %q", bps, got, want)
		}
	}
}

func TestServeAndFunnel(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	e, err := parseServe(read("serve-tailnet.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := []served{{443, "127.0.0.1:3000"}, {2222, "127.0.0.1:22"}, {8443, "localhost:8080"}}
	if !reflect.DeepEqual(e.serve, want) || len(e.funnel) != 0 {
		t.Errorf("tailnet serve: %+v", e)
	}
	e, _ = parseServe(read("serve-funnel.json"))
	if !reflect.DeepEqual(e.funnel, []string{"workstation.tail0c4fe.ts.net:443"}) {
		t.Errorf("funnel: %+v", e)
	}
	for _, none := range []string{"", "{}\n", "  "} {
		if e, err := parseServe(none); err != nil || len(e.serve)+len(e.funnel) != 0 {
			t.Errorf("%q: %+v %v", none, e, err)
		}
	}
	if _, err := parseServe("No serve config"); err == nil {
		t.Error("text is not a config")
	}

	// A serve or funnel run in a terminal (no --bg) lives under Foreground:
	// a public funnel must never read as nothing exposed (research M3 r1).
	e, _ = parseServe(read("serve-foreground-funnel.json"))
	if !reflect.DeepEqual(e.funnel, []string{"workstation.tail0c4fe.ts.net:443"}) || !e.session ||
		!reflect.DeepEqual(e.serve, []served{{443, "127.0.0.1:5173"}}) {
		t.Errorf("foreground funnel: %+v", e)
	}
	e, _ = parseServe(read("serve-mixed.json"))
	if !reflect.DeepEqual(e.serve, []served{{8443, "localhost:8080"}, {10000, "127.0.0.1:3000"}}) || !e.session || len(e.funnel) != 0 {
		t.Errorf("background + foreground: %+v", e)
	}
	e, _ = parseServe(read("serve-services.json"))
	if !reflect.DeepEqual(e.services, []string{"svc:wiki"}) || e.unknown {
		t.Errorf("services: %+v", e)
	}
	e, _ = parseServe(`{"Future":{"x":{"Port":443}}}`)
	if !e.unknown {
		t.Errorf("an unknown shape is unknown, not nothing: %+v", e)
	}
	e, _ = parseServe(`{"ETag":"abc"}`)
	if e.unknown || len(e.serve) != 0 {
		t.Errorf("nothing but an etag is nothing: %+v", e)
	}
	e, _ = parseServe(`{"AllowIngress":{"old.tail0c4fe.ts.net:443":true},"TCP":{"443":{"HTTPS":true}}}`)
	if len(e.funnel) != 1 {
		t.Errorf("an older client's AllowIngress is a funnel: %+v", e)
	}
}

func TestExposureRows(t *testing.T) {
	st := load(t, "status-running.json", now)
	New(pin()).observe(st, now)
	rows := func(file string) []string {
		b, _ := os.ReadFile(filepath.Join("testdata", file))
		e, err := parseServe(string(b))
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rowsOf(render(&poll{available: true, st: st, now: now, pinned: true, exposure: e}).Pages[0].Blocks) {
			if strings.HasPrefix(r, "- Funnel") || strings.HasPrefix(r, "- Serve") || strings.HasPrefix(r, "- Services") {
				out = append(out, r)
			}
		}
		return out
	}
	if got := rows("serve-foreground-funnel.json"); !reflect.DeepEqual(got, []string{
		"- Funnel | :443 · public internet · session | crit crit",
		"- Serve | :443 → 127.0.0.1:5173 · session | warn warn",
	}) {
		t.Errorf("foreground funnel rows: %q", got)
	}
	if got := rows("serve-services.json"); !reflect.DeepEqual(got, []string{"- Services | svc:wiki | warn warn"}) {
		t.Errorf("service rows: %q", got)
	}
}

func TestPeersWithOneHostnameStayApart(t *testing.T) {
	status := func(rxA, rxB int64) []byte {
		return []byte(fmt.Sprintf(`{"BackendState":"Running","Self":{"HostName":"workstation","Online":true},"Peer":{
			"nodekey:aa":{"HostName":"ubuntu","DNSName":"ubuntu.tail0c4fe.ts.net.","Online":true,"RxBytes":%d},
			"nodekey:bb":{"HostName":"ubuntu","DNSName":"ubuntu-1.tail0c4fe.ts.net.","Online":true,"RxBytes":%d}}}`, rxA, rxB))
	}
	w := New(pin("ubuntu-1"))
	st, err := parseStatus(status(1000, 5_000_000), now)
	if err != nil {
		t.Fatal(err)
	}
	w.observe(st, now)
	st, _ = parseStatus(status(1000+10*1024*10, 5_000_000), now.Add(10*time.Second))
	w.observe(st, now.Add(10*time.Second))
	ps := peers(st)
	if ps["ubuntu"].Rate != 10*1024 || ps["ubuntu-1"].Rate != 0 {
		t.Errorf("rates mixed: %v / %v", ps["ubuntu"].Rate, ps["ubuntu-1"].Rate)
	}
	if ps["ubuntu"].Watched || !ps["ubuntu-1"].Watched {
		t.Error("a pin names one of the two by its MagicDNS name")
	}
}

func rowsOf(blocks model.Blocks) []string {
	var got []string
	for _, b := range blocks {
		switch v := b.(type) {
		case model.Section:
			got = append(got, "# "+v.Title+" "+v.Meta+" "+string(v.Tone))
		case model.Row:
			got = append(got, "- "+v.Name+" | "+v.Value+" | "+string(v.LED)+" "+string(v.Tone))
		case model.Note:
			got = append(got, "  "+v.Text+" "+string(v.Tone))
		case model.Chips:
			for _, c := range v.Items {
				got = append(got, "  ["+c.Label+" "+c.Value+" "+string(c.Tone)+"]")
			}
		}
	}
	return got
}

func TestPanel(t *testing.T) {
	st := load(t, "status-running.json", now)
	w := New(pin("fileserver", "atlas", "build-agent"))
	w.observe(st, now)
	e, _ := parseServe(`{"TCP":{"443":{"HTTPS":true}},"Web":{"workstation.tail0c4fe.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:3000"}}}},"AllowFunnel":{"workstation.tail0c4fe.ts.net:443":true}}`)
	s := render(&poll{available: true, st: st, now: now, pinned: true, exposure: e})
	// Problems first: the watched peer that is offline leads (A19).
	want := []string{
		"# Watched · offline 1 warn",
		"- fileserver | 11m ago | warn warn", // 11 min 59.9 s
		"# Health  crit",
		"  Some peers are advertising routes but --accept-routes is false warn",
		"# This node  ",
		"- workstation | running | ok ",
		"- Address | 100.64.0.10 |  ",
		"- MagicDNS | workstation.tail0c4fe.ts.net |  ",
		"- Version | 1.80.2 · update available | warn warn",
		"- Key expires | 3d | warn warn",
		"- Exit node | exit-fra | warn warn",
		"# Exposure  ",
		"- Funnel | :443 · public internet | crit crit",
		"- Serve | :443 → 127.0.0.1:3000 | warn warn",
		"- Exit node | offered by this node | warn warn",
		"- Route advertised | 10.10.0.0/16 | warn warn",
		"- Exit nodes available | exit-fra |  ",
		"- Route via router | 192.168.50.0/24 |  ",
		"# Online 5 ",
		"- atlas | linux · direct | ok ",
		"- build-agent | linux · relay fra | warn warn",
		"- exit-fra | linux · direct | ok ",
		"- phone | iOS · relay ams | warn warn",
		"- router | linux · direct | ok ",
		"# Dormant 3 ",
		"  [old-desktop 12d ago idle]",
		"  [old-vm key expired warn]",
		"  [pi-garage - idle]",
	}
	got := rowsOf(s.Pages[0].Blocks)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("panel:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !reflect.DeepEqual(s.Menu, []model.MenuItem{
		{ID: "copy-ip", Label: "Copy address  100.64.0.10", Copy: "100.64.0.10"},
		{ID: "copy-dns", Label: "Copy MagicDNS name", Copy: "workstation.tail0c4fe.ts.net"},
	}) {
		t.Errorf("menu %+v", s.Menu)
	}

	// Automatic watching says how to pin; nothing exposed is said aloud;
	// an unanswered serve status is not "nothing".
	w = New(nil)
	w.observe(st, now)
	got = rowsOf(render(&poll{available: true, st: st, now: now, exposure: &exposure{}}).Pages[0].Blocks)
	if got[len(got)-1] != "  Pin peers in Settings → Tailscale " {
		t.Errorf("last %q", got[len(got)-1])
	}
	if !contains(got, "- Serve / funnel | nothing exposed | ok ") {
		t.Error("nothing exposed is said out loud")
	}
	got = rowsOf(render(&poll{available: true, st: st, now: now}).Pages[0].Blocks)
	if !contains(got, "- Serve / funnel | unknown | idle ") {
		t.Error("an unanswered serve status is unknown")
	}

	st = load(t, "status-stopped.json", now)
	got = rowsOf(render(&poll{available: true, st: st, now: now, pinned: true}).Pages[0].Blocks)
	if got[len(got)-1] != "  No peers in this tailnet idle" || got[0] != "# This node  " {
		t.Errorf("stopped: this node leads, no peers last: %q", got)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestUnavailable(t *testing.T) {
	for _, c := range []struct {
		p     poll
		label string
		text  string
	}{
		{poll{}, "tailscaled unreachable", "tailscaled is not running on this machine. Start it with"},
		{poll{windows: true}, "Tailscale not running", "Tailscale is not running. Start the Tailscale app."},
		{poll{missing: true}, "not installed", "tailscale is not installed on this machine."},
	} {
		s := render(&c.p)
		if s.State != model.StateIdle || s.StateLabel != c.label || s.Glance.Text != "off" {
			t.Errorf("%+v: %s %q", c.p, s.State, s.StateLabel)
		}
		if e, ok := s.Pages[0].Blocks[0].(model.Empty); !ok || e.Text != c.text {
			t.Errorf("%+v: %+v", c.p, s.Pages[0].Blocks)
		}
	}
}
