// Package docker watches every container, grouped as compose stacks, and
// can start, stop, restart, remove and prune — every one of them asked
// twice. The value is the exception, not the count: the dial stays quiet
// while everything is up and healthy. Parity: docs/rewrite/specs/docker.md.
//
// Everything goes through the docker CLI rather than the socket, so the
// user's current context (DOCKER_HOST, Docker Desktop's) is honoured.
package docker

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

var meta = hub.Meta{ID: "docker", Title: "Docker", Order: 50, PollSecs: 20}

const (
	psTimeout      = 20 * time.Second
	inspectTimeout = 20 * time.Second
	// Over Docker Desktop's VM stats can take longer than this; the rows
	// then go without memory and CPU rather than the poll failing.
	statsTimeout = 8 * time.Second
	logTimeout   = 4 * time.Second
	logFetches   = 8 // last-log-line lookups per poll
	logEntries   = 200

	// The System page walks the whole graph: ask rarely, never on the
	// poll's own time, and again after a prune.
	systemEvery  = 5 * time.Minute
	systemBudget = 50 * time.Second
)

type Widget struct {
	mu      sync.Mutex
	refresh func()
	last    *data // what the panel shows: actions are checked against it
	pending pending
	logs    map[logKey]*logLine
	used    uint64 // the log cache's clock
	sys     *system
	sysAt   time.Time
	sysBusy bool
	compose *bool // docker compose v2 present, asked once per run
}

func New() *Widget {
	return &Widget{pending: pending{}, logs: map[logKey]*logLine{}}
}

func (*Widget) Meta() hub.Meta { return meta }

// SetRefresh lets the background System query and a starting action ask
// for a poll.
func (w *Widget) SetRefresh(f func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.refresh = f
}

func (w *Widget) poke() {
	w.mu.Lock()
	f := w.refresh
	w.mu.Unlock()
	if f != nil {
		f()
	}
}

// binary is docker on PATH, or where Docker Desktop installs it on Windows
// (not always on the PATH a GUI-launched process inherits).
func binary() string {
	var fallbacks []string
	if pf := os.Getenv("ProgramFiles"); runtime.GOOS == "windows" && pf != "" {
		fallbacks = append(fallbacks, filepath.Join(pf, "Docker", "Docker", "resources", "bin", "docker.exe"))
	}
	return sys.Find("docker", fallbacks...)
}

func (w *Widget) Collect(ctx context.Context) (model.Snapshot, error) {
	d, err := w.collect(ctx, binary())
	if err != nil {
		return model.Snapshot{}, err
	}
	w.mu.Lock()
	w.last = d
	p := maps.Clone(w.pending)
	w.mu.Unlock()
	return render(d, p), nil
}

func (w *Widget) collect(ctx context.Context, bin string) (*data, error) {
	d := &data{windows: runtime.GOOS == "windows"}
	if bin == "" {
		d.missing = true
		return d, nil
	}
	out, err := sys.Run(ctx, psTimeout, bin, "ps", "-aq")
	if err != nil {
		return d, nil // the daemon is not there: that is the panel's news
	}
	d.available = true
	now := time.Now()
	if ids := strings.Fields(out); len(ids) > 0 {
		// A container removed between ps and inspect fails the command
		// but not the others' records.
		js, err := sys.Run(ctx, inspectTimeout, bin, append([]string{"inspect"}, ids...)...)
		cs, perr := parseInspect([]byte(js), now)
		if perr != nil {
			if err != nil {
				return nil, err
			}
			return nil, perr
		}
		d.all = cs
	}
	w.usage(ctx, bin, d.all)
	w.fetchLogs(ctx, bin, d.all)
	d.units = group(d.all)
	d.portainer = portainer(d.all)
	d.sys = w.system(bin)
	return d, nil
}

// usage adds docker stats' CPU and memory to running containers.
func (w *Widget) usage(ctx context.Context, bin string, cs []*container) {
	running := false
	for _, c := range cs {
		running = running || c.State == "running"
	}
	if !running {
		return
	}
	out, err := sys.Run(ctx, statsTimeout, bin, "stats", "--no-stream", "--format", "{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}")
	if err != nil {
		return
	}
	stats := parseStats(out)
	for _, c := range cs {
		if u, ok := stats[c.Name]; ok {
			c.CPU, c.Mem, c.MemB = u.cpu, u.mem, u.memB
		}
	}
}

// portainer is the address of a running container named portainer: its
// HTTPS page (9443), else its HTTP one (9000), else the first published
// port that is not the edge agent's tunnel (8000, which serves no page).
func portainer(cs []*container) string {
	for _, c := range cs {
		if c.Name != "portainer" || c.State != "running" {
			continue
		}
		var published []string
		for _, p := range c.Ports {
			host, _, _ := strings.Cut(p, "->")
			published = append(published, host[strings.LastIndex(host, ":")+1:])
		}
		pick := ""
		for _, want := range []string{"9443", "9000"} {
			for _, p := range published {
				if p == want && pick == "" {
					pick = p
				}
			}
		}
		for _, p := range published {
			if pick == "" && p != "8000" {
				pick = p
			}
		}
		switch pick {
		case "":
			return ""
		case "9443":
			return "https://localhost:9443"
		}
		return "http://localhost:" + pick
	}
	return ""
}

type logKey struct{ name, finished string }

type logLine struct {
	text string
	used uint64
}

// fetchLogs gives every broken, unhealthy or restarting container the last
// line it wrote — the quickest diagnostic there is. A stopped container's
// last line cannot change, so it is cached by its stop time; a live one
// (restart-looping, unhealthy) is asked again every poll. Most recent
// first, at most eight lookups a poll, at the same time.
func (w *Widget) fetchLogs(ctx context.Context, bin string, cs []*container) {
	var want []*container
	for _, c := range cs {
		if c.Crashed || c.Health == "unhealthy" || c.State == "restarting" {
			want = append(want, c)
		}
	}
	sort.SliceStable(want, func(i, j int) bool { return want[i].AgeH < want[j].AgeH })

	key := func(c *container) (logKey, bool) {
		if c.State == "running" || c.State == "restarting" {
			return logKey{c.Name, "live"}, true
		}
		return logKey{c.Name, c.Finished}, false
	}
	var ask []*container
	w.mu.Lock()
	for _, c := range want {
		k, live := key(c)
		if e, ok := w.logs[k]; ok && (!live || len(ask) >= logFetches) {
			w.used++
			e.used = w.used
			c.Log = e.text
			continue
		}
		if len(ask) < logFetches {
			ask = append(ask, c)
		}
	}
	w.mu.Unlock()

	lines := make([]*string, len(ask))
	var wg sync.WaitGroup
	for i, c := range ask {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, errOut, err := sys.Output(ctx, logTimeout, bin, "logs", "--tail", "5", "--", c.Name)
			if err != nil && out == "" && errOut == "" {
				return // no answer: ask again next poll
			}
			line := lastLogLine(out + errOut)
			lines[i] = &line
		}()
	}
	wg.Wait()

	w.mu.Lock()
	defer w.mu.Unlock()
	for i, c := range ask {
		if lines[i] == nil {
			continue
		}
		c.Log = *lines[i]
		k, _ := key(c)
		w.used++
		w.logs[k] = &logLine{text: c.Log, used: w.used}
	}
	for len(w.logs) > logEntries { // the least recently used out, not all
		var oldest logKey
		first := true
		for k, e := range w.logs {
			if first || e.used < w.logs[oldest].used {
				oldest, first = k, false
			}
		}
		delete(w.logs, oldest)
	}
}

// system returns the last System numbers and, when they are five minutes
// old (or a prune made them wrong), asks again in the background; the
// widget is polled again when the answer lands.
func (w *Widget) system(bin string) *system {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sysBusy || (!w.sysAt.IsZero() && time.Since(w.sysAt) < systemEvery) {
		return w.sys
	}
	w.sysBusy = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), systemBudget)
		defer cancel()
		s := querySystem(ctx, bin)
		w.mu.Lock()
		w.sys, w.sysAt, w.sysBusy = s, time.Now(), false
		w.mu.Unlock()
		w.poke()
	}()
	return w.sys
}

func querySystem(ctx context.Context, bin string) *system {
	s := &system{}
	out, _ := sys.Run(ctx, 5*time.Second, bin, "version", "--format", "{{.Server.Version}}")
	s.engine = strings.TrimSpace(out)
	out, _ = sys.Run(ctx, 5*time.Second, bin, "compose", "version", "--short")
	s.compose = strings.TrimSpace(out)
	out, _ = sys.Run(ctx, 10*time.Second, bin, "images", "-f", "dangling=true", "-q")
	s.dangling = len(strings.Fields(out))
	out, _ = sys.Run(ctx, 30*time.Second, bin, "system", "df", "--format", "{{.Type}}\t{{.TotalCount}}\t{{.Active}}\t{{.Size}}\t{{.Reclaimable}}")
	s.df = parseDF(out)
	return s
}
