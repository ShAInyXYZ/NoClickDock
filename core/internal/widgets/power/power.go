// Package power measures what the machine draws and what it costs: the
// CPU package (RAPL), every GPU, drives, board, displays and USB, priced by
// a flat or time-of-use tariff, logged hourly and gathered into a report.
// Parity: docs/rewrite/specs/power.md.
package power

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/config"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sensors"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

const (
	staticEvery = 5 * time.Minute
	vramEvery   = 5 * time.Second
	vramBackoff = time.Minute // after any failure: a hiccup must not blind it
	avgSamples  = 1800        // ≈ 1 h at 2 s
	donutEvery  = time.Minute
	levelHold   = 60 * time.Second // a new level must hold this long to be announced (Q3)
	nvmeWatts   = 5
)

// Thermal alerts: a GPU's memory junction, else its core; the CPU.
const (
	vramAlertC = 90
	coreAlertC = 80
	cpuAlertC  = 85
)

type Widget struct {
	cfg    func() config.Power
	logDir string

	mu      sync.Mutex
	refresh func()

	cpu       cpuPower
	usage     sensors.CPUUsage
	tempInput string
	firstPoll bool

	static     staticInfo
	staticAt   time.Time
	staticBusy bool

	vram      map[string]float64
	vramAt    time.Time
	vramRetry time.Time
	vramBusy  bool

	totals []float64 // the last avgSamples totals

	donutAcc []parts
	donutAt  time.Time
	donut    *parts // the last minute's average; nil until one has closed

	level, candidate string
	candidateSince   time.Time

	log        *logger
	reportBusy bool
}

// staticInfo is what changes rarely.
type staticInfo struct {
	CPUModel, Board, RAM string
	Displays             []display
	USB                  []string
	NVMe                 []nvme
}

// parts is the draw by component, for the donut.
type parts struct {
	cpu, gpu, nvme, periph, total float64
}

// New watches this machine. logDir is where the hourly CSV and the report
// go: ~/Documents/PM-Log in production only (the caller decides).
func New(cfg func() config.Power, logDir string) *Widget {
	w := &Widget{cfg: cfg, logDir: logDir, firstPoll: true, log: newLogger(logDir)}
	w.log.wrote = w.rebuildReport
	return w
}

func (w *Widget) Meta() hub.Meta {
	return hub.Meta{ID: "power", Title: "Power", Order: 60, PollSecs: max(1, w.cfg().PollInterval)}
}

func (w *Widget) SetRefresh(f func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.refresh = f
}

func (w *Widget) poke() {
	w.mu.Lock()
	refresh := w.refresh
	w.mu.Unlock()
	if refresh != nil {
		refresh()
	}
}

// gpu is a card with its memory junction temperature, when known.
type gpu struct {
	sensors.GPU
	VRAMTemp *float64
}

// reading is one poll, everything the panel says derived from it.
type reading struct {
	now     time.Time
	os      string
	tariff  tariff
	logging string

	cpuW         *float64 // measured, or estimated (then cpuEstimated)
	cpuEstimated bool
	cpuNote      string
	cpuTemp      *float64
	cpuUsage     *float64
	mem          *sensors.Memory
	gpus         []gpu
	nvme         []nvme
	static       staticInfo

	overhead, periph, gpuW, nvmeW float64
	total                         *float64
	avg                           float64
	monitors                      float64
	donut                         parts
	today                         today
	alerts                        []alert
}

type alert struct {
	id, label, text string
	tone            model.Tone
}

func (w *Widget) Collect(ctx context.Context) (model.Snapshot, error) {
	cfg := w.cfg()
	t := newTariff(cfg)
	now := time.Now()
	w.dueStatic()
	w.dueVRAM()

	r := reading{now: now, os: runtime.GOOS, tariff: t, logging: cfg.LogInterval, overhead: cfg.OverheadWatts}
	r.cpuW = w.cpu.read(now)
	if r.cpuW == nil && w.firstPoll && w.cpu.primed() {
		// The first poll measures over half a second rather than show an
		// estimate for two (only once: never a sleep in every poll).
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
		}
		r.cpuW = w.cpu.read(time.Now())
	}
	r.cpuUsage = w.usage.Read()
	if w.firstPoll && r.cpuUsage == nil {
		r.cpuUsage = w.usage.Read()
	}
	w.firstPoll = false
	if w.tempInput == "" {
		w.tempInput = cpuTempInput()
	}
	r.cpuTemp = readCPUTemp(w.tempInput)
	if m, ok := readMemory(); ok {
		r.mem = &m
	}

	w.mu.Lock()
	r.static = w.static
	vram := w.vram
	w.mu.Unlock()
	for _, g := range readGPUs(ctx) {
		c := gpu{GPU: g}
		if v, ok := vram[g.Bus]; ok && g.Bus != "" {
			c.VRAMTemp = &v
		}
		r.gpus = append(r.gpus, c)
	}
	for _, n := range r.static.NVMe {
		n.Temp = n.readTemp()
		r.nvme = append(r.nvme, n)
	}
	derive(&r, cfg, w.cpu.reason)

	w.mu.Lock()
	defer w.mu.Unlock()
	if r.total != nil {
		w.totals = append(w.totals, *r.total)
		if len(w.totals) > avgSamples {
			w.totals = w.totals[len(w.totals)-avgSamples:]
		}
	}
	r.avg = mean(w.totals)
	r.donut = w.donutAverage(r, now)

	// The log: every poll, whatever the panel shows.
	if r.total != nil {
		s := sample{at: now, total: *r.total, gpu: r.gpuW, cpuTemp: r.cpuTemp, cpuPct: r.cpuUsage}
		if r.cpuW != nil && !r.cpuEstimated {
			s.cpu = *r.cpuW
		}
		for _, g := range r.gpus {
			if g.Temp != nil && (s.gpuTemp == nil || *g.Temp > *s.gpuTemp) {
				v := *g.Temp
				s.gpuTemp = &v
			}
		}
		if r.mem != nil {
			pct := r.mem.Pct()
			s.ramPct = &pct
		}
		if err := w.log.add(s, cfg.LogInterval, t); err != nil {
			log.Printf("power log: %v", err)
		}
	}
	r.today = w.log.today(now, t)

	s := render(r)
	if n := w.levelNotice(r, now); n != "" {
		s.Notices = []model.Issue{{Text: n, Tone: model.ToneInfo}}
	}
	return s, nil
}

// derive: the PC's total through its own PSU (displays have theirs), its
// level and the thermal alerts.
func derive(r *reading, cfg config.Power, reason string) {
	for _, g := range r.gpus {
		if g.Power != nil {
			r.gpuW += *g.Power
		}
	}
	r.nvmeW = float64(nvmeWatts * len(r.nvme))
	if len(r.static.USB) > 0 {
		r.periph = cfg.PeripheralWatts // once, whatever the count
	}
	rest := r.gpuW + r.overhead + r.nvmeW + r.periph
	switch {
	case r.cpuW != nil:
	case r.cpuUsage != nil:
		est := 10 + *r.cpuUsage/100*190
		r.cpuW, r.cpuEstimated = &est, true
		r.cpuNote = cpuPowerNote(reason)
	}
	if r.cpuW != nil || len(r.gpus) > 0 || r.cpuUsage != nil {
		total := rest
		if r.cpuW != nil {
			total += *r.cpuW
		}
		r.total = &total
	}
	for _, d := range r.static.Displays {
		r.monitors += d.EstW
	}
	for _, g := range r.gpus {
		tag := gpuTag(g, len(r.gpus))
		name := sensors.ShortGPUName(g.Name)
		// One alert per card, naming its worse sensor (a tie goes to the
		// core), so the cell, the lamp, the label and the toast agree
		// (design M5 r1).
		var core, vram model.Tone
		if g.Temp != nil {
			core = coreTone(*g.Temp)
		}
		if g.VRAMTemp != nil {
			vram = vramTone(*g.VRAMTemp)
		}
		switch {
		case core != model.ToneNone && severity(core) >= severity(vram):
			r.alerts = append(r.alerts, alert{
				id: fmt.Sprintf("gpu:%d", g.Index), label: fmt.Sprintf("%s core %.0f°C", tag, *g.Temp),
				text: fmt.Sprintf("%s %s core is at %.0f°C", tag, name, *g.Temp), tone: core,
			})
		case vram != model.ToneNone:
			r.alerts = append(r.alerts, alert{
				id: fmt.Sprintf("gpu:%d", g.Index), label: fmt.Sprintf("%s VRAM %.0f°C", tag, *g.VRAMTemp),
				text: fmt.Sprintf("%s %s memory junction is at %.0f°C", tag, name, *g.VRAMTemp), tone: vram,
			})
		}
	}
	if r.cpuTemp != nil {
		if tone := cpuTone(*r.cpuTemp); tone != model.ToneNone {
			r.alerts = append(r.alerts, alert{id: "cpu", label: fmt.Sprintf("CPU %.0f°C", *r.cpuTemp), text: fmt.Sprintf("CPU is at %.0f°C", *r.cpuTemp), tone: tone})
		}
	}
}

func severity(t model.Tone) int {
	switch t {
	case model.ToneCrit:
		return 2
	case model.ToneWarn:
		return 1
	}
	return 0
}

// gpuTag is "GPU" when there is one card, "GPU0", "GPU1"… when several.
func gpuTag(g gpu, count int) string {
	if count == 1 {
		return "GPU"
	}
	return fmt.Sprintf("GPU%d", g.Index)
}

// level is the Python widget's four words, by total watts.
func level(total *float64) string {
	switch {
	case total == nil:
		return ""
	case *total < 100:
		return "idle"
	case *total < 250:
		return "normal"
	case *total < 500:
		return "high load"
	}
	return "full send"
}

// levelNotice announces a new level once it has held for a minute (Q3):
// at a threshold the Python widget toasted every two seconds.
func (w *Widget) levelNotice(r reading, now time.Time) string {
	lv := level(r.total)
	switch {
	case lv == "":
		return ""
	case w.level == "":
		w.level = lv // the first reading is not news
	case lv == w.level:
		w.candidate = ""
	case lv != w.candidate:
		w.candidate, w.candidateSince = lv, now
	case now.Sub(w.candidateSince) >= levelHold:
		w.level, w.candidate = lv, ""
		return fmt.Sprintf("%s at %.0fW", sentence(lv), *r.total)
	}
	return ""
}

func sentence(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}

// donutAverage holds the donut on the last full minute's average, so its
// slices do not twitch every poll; live values until a minute has closed.
func (w *Widget) donutAverage(r reading, now time.Time) parts {
	live := parts{gpu: r.gpuW, nvme: r.nvmeW, periph: r.periph}
	if r.cpuW != nil {
		live.cpu = *r.cpuW
	}
	if r.total != nil {
		live.total = *r.total
	}
	if w.donutAt.IsZero() {
		w.donutAt = now
	}
	w.donutAcc = append(w.donutAcc, live)
	if now.Sub(w.donutAt) >= donutEvery {
		var sum parts
		for _, p := range w.donutAcc {
			sum.cpu += p.cpu
			sum.gpu += p.gpu
			sum.nvme += p.nvme
			sum.periph += p.periph
			sum.total += p.total
		}
		n := float64(len(w.donutAcc))
		avg := parts{sum.cpu / n, sum.gpu / n, sum.nvme / n, sum.periph / n, sum.total / n}
		w.donut, w.donutAcc, w.donutAt = &avg, nil, now
	}
	if w.donut != nil {
		return *w.donut
	}
	return live
}

// dueStatic re-reads the machine's parts every five minutes, off the poll.
func (w *Widget) dueStatic() {
	w.mu.Lock()
	if w.staticBusy || (!w.staticAt.IsZero() && time.Since(w.staticAt) < staticEvery) {
		w.mu.Unlock()
		return
	}
	w.staticBusy, w.staticAt = true, time.Now()
	w.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s := readStatic(ctx)
		w.mu.Lock()
		w.static, w.staticBusy = s, false
		w.mu.Unlock()
		w.poke()
	}()
}

// dueVRAM asks gddr6-oneshot every five seconds, off the poll; any failure
// leaves it alone for a minute.
func (w *Widget) dueVRAM() {
	w.mu.Lock()
	now := time.Now()
	if w.vramBusy || now.Before(w.vramRetry) || (!w.vramAt.IsZero() && now.Sub(w.vramAt) < vramEvery) {
		w.mu.Unlock()
		return
	}
	w.vramBusy, w.vramAt = true, now
	w.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		temps, err := readVRAMTemps(ctx)
		w.mu.Lock()
		defer w.mu.Unlock()
		w.vramBusy = false
		if err != nil {
			w.vram = nil
			if !errors.Is(err, errNoTool) {
				w.vramRetry = time.Now().Add(vramBackoff)
			} else {
				w.vramRetry = time.Now().Add(staticEvery) // not installed: look again later
			}
			return
		}
		w.vram = temps
	}()
}

// rebuildReport runs after a log row is written, off the poll.
func (w *Widget) rebuildReport() {
	if w.reportBusy {
		return
	}
	w.reportBusy = true
	cfg, dir := w.cfg(), w.logDir
	go func() {
		if _, err := writeReport(dir, newTariff(cfg), cfg.PeakHours, time.Now()); err != nil && !errors.Is(err, errNoRows) {
			log.Printf("power report: %v", err)
		}
		w.mu.Lock()
		w.reportBusy = false
		w.mu.Unlock()
	}()
}

// Act: the menu's "Open power report" — rebuilt from the log, then opened
// by the core (never a file:// URL through the shell).
func (w *Widget) Act(_ context.Context, action string, _ map[string]string) (string, error) {
	if action != "open-report" {
		return "", fmt.Errorf("power has no action %q", action)
	}
	cfg := w.cfg()
	path, err := writeReport(w.logDir, newTariff(cfg), cfg.PeakHours, time.Now())
	if errors.Is(err, errNoRows) {
		return "", errors.New("No report yet — logging builds it")
	}
	if err != nil {
		return "", fmt.Errorf("could not build the power report: %w", err)
	}
	if err := openFile(path); err != nil {
		return "", fmt.Errorf("could not open the power report: %w", err)
	}
	return "Power report opened", nil
}

// openFile hands a local file to the desktop's opener.
func openFile(path string) error {
	if runtime.GOOS == "windows" {
		return sys.Start("rundll32.exe", "url.dll,FileProtocolHandler", path)
	}
	return sys.Start("xdg-open", path)
}

// Transition: a thermal alert toasts itself (an issue); calming down, and
// load changes of state, say nothing here (levels are notices).
func (w *Widget) Transition(_, _ model.State, _ model.Snapshot) string { return "" }
