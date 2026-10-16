package comfyui

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/config"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sensors"
)

// Every fixture is invented: no real machine, prompt or address.

func load(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// server answers /queue and /system_stats from fixtures, switchable.
type server struct {
	mu           sync.Mutex
	queue, stats string
	down         bool
}

func (s *server) set(queue, stats string, down bool) {
	s.mu.Lock()
	s.queue, s.stats, s.down = queue, stats, down
	s.mu.Unlock()
}

func (s *server) start(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		queue, stats, down := s.queue, s.stats, s.down
		s.mu.Unlock()
		if down {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("user agent %q", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/queue":
			w.Write(load(t, queue))
		case "/system_stats":
			w.Write(load(t, stats))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func endpointOf(t *testing.T, srv *httptest.Server) config.ComfyUI {
	u, _ := url.Parse(srv.URL)
	host, port, _ := net.SplitHostPort(u.Host)
	p, _ := strconv.Atoi(port)
	return config.ComfyUI{Host: host, Port: p}
}

// rows flattens a page into one line per block, for comparing panels.
func rows(blocks model.Blocks) []string {
	var out []string
	for _, b := range blocks {
		switch v := b.(type) {
		case model.Section:
			out = append(out, "# "+v.Title+" "+v.Meta)
		case model.Meter:
			out = append(out, fmt.Sprintf("  %s %s %s", v.Label, v.Value, v.Tone))
		case model.Stats:
			var parts []string
			for _, it := range v.Items {
				parts = append(parts, it.Key+" "+it.Value)
			}
			out = append(out, "  "+strings.Join(parts, " · "))
		case model.Row:
			out = append(out, "  "+v.Name+" = "+v.Value)
		case model.Note:
			out = append(out, "  ("+v.Text+")")
		case model.Empty:
			out = append(out, "  ["+v.Text+" "+strings.Join(v.Commands, " ")+"]")
		}
	}
	return out
}

func TestDevicesAreReadFromTheResponse(t *testing.T) {
	srv := (&server{queue: "queue-idle.json", stats: "stats-two.json"}).start(t)
	sys, devices, err := fetchStats(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if sys.Torch != "2.7.1" || sys.Python != "3.11.9" || sys.ComfyUI != "0.3.60" {
		t.Errorf("versions %+v", sys)
	}
	if len(devices) != 2 {
		t.Fatalf("%d devices", len(devices))
	}
	a, b := devices[0], devices[1]
	if a.Label != "cuda:0" || a.Name != "NVIDIA GeForce RTX 3060" || a.Allocator != "native" || a.Index != 0 ||
		a.VRAMUsed != 12884901888-644245094 || a.TorchUsed != 8589934592-2147483648 {
		t.Errorf("device 0 %+v", a)
	}
	if b.Index != 1 || b.Label != "cuda:1" || b.TorchTotal != 0 {
		t.Errorf("a missing index is its position: %+v", b)
	}
	_, one, _ := fetchStats(context.Background(), srv.Client(), (&server{stats: "stats-one.json"}).start(t).URL)
	if one[0].Allocator != "cudaMallocAsync" || one[0].TorchTotal != 0 {
		t.Errorf("one %+v", one[0])
	}
	sys, cpu, _ := fetchStats(context.Background(), srv.Client(), (&server{stats: "stats-cpu.json"}).start(t).URL)
	if cpu[0].Label != "cpu" || cpu[0].Name != "cpu" || cpu[0].Allocator != "" || sys.Torch != "2.8.0" {
		t.Errorf("cpu %+v %+v", cpu[0], sys)
	}
	if baseURL("::1", 8188) != "http://[::1]:8188" || baseURL("127.0.0.1", 8000) != "http://127.0.0.1:8000" {
		t.Error("an IPv6 host is bracketed")
	}
}

func TestTelemetryFindsItsCard(t *testing.T) {
	g := func(i int, name string, temp float64) sensors.GPU {
		return sensors.GPU{Index: i, Name: name, Temp: &temp}
	}
	dev := func(i int, name string) device { return device{Index: i, Name: name, Type: "cuda"} }
	temps := func(ds []device) []string {
		var out []string
		for _, d := range ds {
			if d.Temp == nil {
				out = append(out, "-")
			} else {
				out = append(out, fmt.Sprint(*d.Temp))
			}
		}
		return out
	}
	// Same order: by index.
	got := merge([]device{dev(0, "NVIDIA GeForce RTX 4090"), dev(1, "NVIDIA GeForce RTX 3060")},
		[]sensors.GPU{g(0, "NVIDIA GeForce RTX 4090", 70), g(1, "NVIDIA GeForce RTX 3060", 40)})
	if !reflect.DeepEqual(temps(got), []string{"70", "40"}) {
		t.Errorf("by index %v", temps(got))
	}
	// Torch put the fast card first, nvidia-smi the PCI order: by name.
	got = merge([]device{dev(0, "NVIDIA GeForce RTX 4090"), dev(1, "NVIDIA GeForce RTX 3060")},
		[]sensors.GPU{g(0, "NVIDIA GeForce RTX 3060", 40), g(1, "NVIDIA GeForce RTX 4090", 70)})
	if !reflect.DeepEqual(temps(got), []string{"70", "40"}) {
		t.Errorf("by name %v", temps(got))
	}
	// Lists of different lengths that disagree: no telemetry rather than
	// another card's.
	got = merge([]device{dev(0, "NVIDIA GeForce RTX 4090")},
		[]sensors.GPU{g(0, "NVIDIA GeForce RTX 3060", 40), g(1, "NVIDIA GeForce RTX 4090", 70)})
	if !reflect.DeepEqual(temps(got), []string{"-"}) {
		t.Errorf("unsure %v", temps(got))
	}
	// A CPU device never gets a card's.
	got = merge([]device{{Index: 0, Name: "cpu", Type: "cpu"}}, []sensors.GPU{g(0, "cpu", 40)})
	if got[0].Temp != nil {
		t.Error("cpu device")
	}
}

func TestLocalEndpoints(t *testing.T) {
	r := resolver{
		lookup: func(_ context.Context, host string) ([]net.IP, error) {
			switch host {
			case "workstation.lan":
				return []net.IP{net.ParseIP("192.0.2.10")}, nil
			case "render-box.example":
				return []net.IP{net.ParseIP("198.51.100.7")}, nil
			case "loopback.example":
				return []net.IP{net.ParseIP("127.0.1.1")}, nil
			}
			return nil, fmt.Errorf("no such host")
		},
		own: func() []net.IP { return []net.IP{net.ParseIP("192.0.2.10"), net.ParseIP("fe80::1")} },
	}
	for host, want := range map[string]bool{
		"localhost": true, "0.0.0.0": true, "::1": true, "[::1]": true, "127.0.0.1": true,
		"192.0.2.10": true, "workstation.lan": true, "loopback.example": true,
		"render-box.example": false, "198.51.100.7": false, "unknown.example": false, "": false,
	} {
		if got := r.isLocal(context.Background(), host); got != want {
			t.Errorf("%q local = %v, want %v", host, got, want)
		}
	}
}

func TestPanels(t *testing.T) {
	gib := float64(1 << 30)
	one := device{Label: "cuda:0", Name: "NVIDIA GeForce RTX 4090", Type: "cuda", Allocator: "cudaMallocAsync",
		VRAMTotal: 24 * gib, VRAMUsed: 21 * gib, Temp: f(71), Util: f(87), Power: f(312)}
	base := reading{base: "http://127.0.0.1:8188", online: true, os: "linux",
		system: system{ComfyUI: "0.3.60", Torch: "2.8.0", Python: "3.12.3", RAMTotal: 64 * gib, RAMFree: 32 * gib}}

	r := base
	r.queue, r.devices, r.progress = queue{Running: 1, Pending: 2}, []device{one}, f(0.37)
	r.sensed, r.cpuLoad = true, f(23) // no CPU temperature: no temp
	s := render(r)
	if s.State != model.StateBusy || s.StateLabel != "generating · 37%" || s.Glance.Text != "37%" || s.Progress == nil {
		t.Errorf("generating: %q %q", s.StateLabel, s.Glance.Text)
	}
	want := []string{
		"  running 1 · pending 2",
		"# GPU · RTX 4090 cudaMallocAsync",
		"  VRAM 21.0 GB / 24.0 GB (88%) warn",
		"  load 87% · temp 71°C · power 312W",
		"# CPU ",
		"  load 23%",
		"# System ",
		"  RAM 32.0 GB / 64.0 GB ",
		"  comfyui 0.3.60 · torch 2.8.0 · python 3.12.3",
		"  Endpoint = http://127.0.0.1:8188",
	}
	if got := rows(s.Pages[0].Blocks); !reflect.DeepEqual(got, want) {
		t.Errorf("generating panel\n got %q\nwant %q", got, want)
	}

	// No progress event reached us (the browser queued it): indeterminate.
	r.progress = nil
	if s := render(r); s.StateLabel != "generating" || s.Glance.Text != "run" || s.Progress != nil {
		t.Errorf("no progress: %q %q", s.StateLabel, s.Glance.Text)
	}

	// Two devices, one with torch's share, remote (no sensors): labels,
	// a crit tone, no CPU section.
	r = base
	r.queue = queue{Pending: 3}
	r.devices = []device{
		{Label: "cuda:0", Name: "NVIDIA GeForce RTX 3060", Type: "cuda", Allocator: "native", VRAMTotal: 12 * gib, VRAMUsed: 11.5 * gib, TorchTotal: 8 * gib, TorchUsed: 7.9 * gib},
		{Label: "cuda:1", Name: "NVIDIA GeForce RTX 4090", Type: "cuda", Allocator: "native", VRAMTotal: 24 * gib, VRAMUsed: 1 * gib},
	}
	s = render(r)
	if s.StateLabel != "queued · 3 pending" || s.Glance.Text != "q 3" {
		t.Errorf("queued: %q %q", s.StateLabel, s.Glance.Text)
	}
	want = []string{
		"  running 0 · pending 3",
		"# CUDA:0 · RTX 3060 native",
		"  VRAM 11.5 GB / 12.0 GB (96%) crit",
		"  Torch VRAM 7.9 GB / 8.0 GB ",
		"# CUDA:1 · RTX 4090 native",
		"  VRAM 1.0 GB / 24.0 GB (4%) ",
		"# System ",
		"  RAM 32.0 GB / 64.0 GB ",
		"  comfyui 0.3.60 · torch 2.8.0 · python 3.12.3",
		"  Endpoint = http://127.0.0.1:8188",
	}
	if got := rows(s.Pages[0].Blocks); !reflect.DeepEqual(got, want) {
		t.Errorf("two devices\n got %q\nwant %q", got, want)
	}

	// Idle: device 0's VRAM on the dial.
	r.queue = queue{}
	if s := render(r); s.State != model.StateOK || s.StateLabel != "idle" || s.Glance.Text != "96%" {
		t.Errorf("idle: %q %q", s.StateLabel, s.Glance.Text)
	}

	// Offline: the sentence alone.
	s = render(reading{base: "http://[::1]:8188"})
	if got := rows(s.Pages[0].Blocks); !reflect.DeepEqual(got, []string{"  [ComfyUI is not answering at http://[::1]:8188]"}) ||
		s.State != model.StateIdle || s.StateLabel != "offline" || s.Glance.Text != "off" {
		t.Errorf("offline: %q %q %q", got, s.StateLabel, s.Glance.Text)
	}

	// Windows says why there is no CPU temperature.
	r = base
	r.os, r.sensed, r.cpuLoad = "windows", true, f(12)
	got := rows(render(r).Pages[0].Blocks)
	if got[1] != "# CPU " || got[2] != "  load 12%" || got[3] != "  (CPU temperature needs a sensor driver on Windows)" {
		t.Errorf("windows cpu %q", got)
	}
}

func TestCollectAndTransitions(t *testing.T) {
	fake := &server{queue: "queue-idle.json", stats: "stats-one.json"}
	srv := fake.start(t)
	ep := endpointOf(t, srv)
	w := New(func() config.ComfyUI { return ep })
	w.net = resolver{lookup: func(context.Context, string) ([]net.IP, error) { return nil, nil }, own: func() []net.IP { return nil }}
	defer w.Close()
	ctx := context.Background()

	step := func(queue string, down bool) (model.Snapshot, string) {
		t.Helper()
		fake.set(queue, "stats-one.json", down)
		s, err := w.Collect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return s, w.Transition("", s.State, s)
	}
	if s, _ := step("queue-idle.json", false); s.StateLabel != "idle" || s.Glance.Text != "38%" {
		t.Fatalf("first: %q %q", s.StateLabel, s.Glance.Text)
	}
	if _, msg := step("queue-running.json", false); msg != "Generating…" {
		t.Errorf("start: %q", msg)
	}
	if _, msg := step("queue-idle.json", false); msg != "Generation complete" {
		t.Errorf("done: %q", msg)
	}
	if s, msg := step("queue-pending.json", false); msg != "Queued, 1 pending" || s.StateLabel != "queued · 1 pending" {
		t.Errorf("queued: %q %q", msg, s.StateLabel)
	}
	if _, msg := step("queue-idle.json", false); msg != "" {
		t.Errorf("a queue that drains unseen says nothing: %q", msg)
	}
	if s, msg := step("queue-idle.json", true); msg != "ComfyUI is offline" || s.StateLabel != "offline" {
		t.Errorf("offline: %q %q", msg, s.StateLabel)
	}
	if _, msg := step("queue-idle.json", false); msg != "ComfyUI is online" {
		t.Errorf("online: %q", msg)
	}
}

// A ComfyUI stand-in speaking the websocket: it counts connections and the
// client ids they used, and plays frames on demand.
type wsServer struct {
	mu    sync.Mutex
	ids   []string
	conns chan *websocket.Conn
}

func (s *wsServer) start(t *testing.T) *httptest.Server {
	s.conns = make(chan *websocket.Conn, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		s.mu.Lock()
		s.ids = append(s.ids, r.URL.Query().Get("clientId"))
		s.mu.Unlock()
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		s.conns <- c
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWebsocketFrames(t *testing.T) {
	ws := &wsServer{}
	srv := ws.start(t)
	ep := endpointOf(t, srv)
	statuses := make(chan struct{}, 8)
	l := newListener(func() { statuses <- struct{}{} })
	defer l.stop()
	l.follow(ep.Host, ep.Port)
	var conn *websocket.Conn
	select {
	case conn = <-ws.conns:
	case <-time.After(3 * time.Second):
		t.Fatal("no connection")
	}
	ctx := context.Background()
	send := func(frame string) {
		t.Helper()
		if err := conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
			t.Fatal(err)
		}
	}

	// A preview image (binary) and a ping are taken in stride.
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 200_000)); err != nil {
		t.Fatal(err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	go func() { _, _, _ = conn.Read(ctx) }() // the server must read to see the pong
	if err := conn.Ping(pingCtx); err != nil {
		t.Errorf("ping: %v", err)
	}
	cancel()

	send(`{"type":"status","data":{"status":{"exec_info":{"queue_remaining":2}},"sid":"x"}}`)
	select {
	case <-statuses:
	case <-time.After(3 * time.Second):
		t.Fatal("a status event asks for a poll")
	}
	send(`{"type":"status","data":{"status":{"exec_info":{"queue_remaining":2}}}}`)
	send(`{"type":"progress","data":{"value":5,"max":20,"prompt_id":"p","node":"3"}}`)
	eventually(t, "progress 25%", func() bool { p := l.Progress(); return p != nil && *p == 0.25 })
	if len(statuses) != 0 {
		t.Error("an unchanged queue does not ask again")
	}
	send(`{"type":"progress","data":{"value":20,"max":20}}`)
	eventually(t, "a finished node is indeterminate", func() bool { return l.Progress() == nil })
	send(`{"type":"progress","data":{"value":7,"max":10}}`)
	eventually(t, "progress 70%", func() bool { p := l.Progress(); return p != nil && *p == 0.7 })
	send(`{"type":"executing","data":{"node":"8","prompt_id":"p"}}`)
	send(`not json`)
	time.Sleep(20 * time.Millisecond)
	if l.Progress() == nil {
		t.Error("a node starting keeps the progress")
	}
	send(`{"type":"executing","data":{"node":null,"prompt_id":"p"}}`)
	eventually(t, "executing null clears", func() bool { return l.Progress() == nil })

	// A dropped socket comes back, under the same id of our own.
	conn.CloseNow()
	select {
	case <-ws.conns:
	case <-time.After(reconnectEvery + 3*time.Second):
		t.Fatal("no reconnection")
	}
	ws.mu.Lock()
	ids := append([]string{}, ws.ids...)
	ws.mu.Unlock()
	if len(ids) < 2 || ids[0] != ids[1] || ids[0] != l.id || len(ids[0]) != 36 {
		t.Errorf("client ids %q", ids)
	}
}

// Settings' Test button: the address asked, and what answered there.
func TestProbeSaysWhatWasAsked(t *testing.T) {
	srv := (&server{queue: "queue-running.json", stats: "stats-two.json"}).start(t)
	ep := endpointOf(t, srv)
	w := New(func() config.ComfyUI { return config.ComfyUI{Host: "127.0.0.1", Port: 1} })
	defer w.Close()
	ctx := context.Background()
	msg, _, err := w.Ask(ctx, "probe", map[string]string{"host": ep.Host, "port": fmt.Sprint(ep.Port)})
	want := fmt.Sprintf("http://%s:%d answered · ComfyUI 0.3.60 · 2 devices (RTX 3060, RTX 4090) · generating, 2 pending", ep.Host, ep.Port)
	if err != nil || msg != want {
		t.Errorf("probe: %q %v\nwant %q", msg, err, want)
	}
	// A closed port on this machine: refused, and the address is named.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := l.Addr().(*net.TCPAddr).Port
	l.Close()
	_, _, err = w.Ask(ctx, "probe", map[string]string{"host": "127.0.0.1", "port": fmt.Sprint(closed)})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("http://127.0.0.1:%d did not answer: connection refused", closed)) {
		t.Errorf("closed port: %v", err)
	}
	// Not ComfyUI: a server with no /queue.
	other := httptest.NewServer(http.NotFoundHandler())
	defer other.Close()
	u, _ := url.Parse(other.URL)
	h, p, _ := net.SplitHostPort(u.Host)
	if _, _, err := w.Ask(ctx, "probe", map[string]string{"host": h, "port": p}); err == nil || !strings.Contains(err.Error(), "HTTP 404 Not Found on /queue — is it ComfyUI?") {
		t.Errorf("not ComfyUI: %v", err)
	}
	for _, bad := range []map[string]string{{"host": "http://x", "port": "8188"}, {"host": "x", "port": "0"}, {"host": "", "port": "1"}} {
		if _, _, err := w.Ask(ctx, "probe", bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}
