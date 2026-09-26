package power

import (
	"bufio"
	"encoding/csv"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// header is the CSV's columns, byte-compatible with the Python widget's
// log: the report and years of history depend on it.
var header = []string{
	"timestamp", "avg_total_w", "avg_cpu_w", "avg_gpu_w", "avg_cpu_temp", "avg_gpu_temp",
	"avg_cpu_pct", "avg_ram_pct", "samples", "kwh_period", "cost_period",
}

// A gap longer than this between two polls is a suspend or a stall: no
// energy is counted for it (the Python widget charged a full nominal hour
// whatever happened).
const maxGap = 60 * time.Second

// sample is one poll's contribution to the log.
type sample struct {
	at               time.Time
	total, cpu, gpu  float64 // W; cpu 0 when not measured (as the Python log)
	cpuTemp, gpuTemp *float64
	cpuPct, ramPct   *float64
}

// period accumulates one log row.
type period struct {
	start, end      time.Time
	n               int
	total, cpu, gpu float64
	temps           [4]struct {
		sum float64
		n   int
	} // cpu temp, gpu temp, cpu %, ram %
	wh, cost float64
}

// hour is one clock hour's energy, in memory, for Today.
type hour struct {
	wh, cost, sumW float64
	n              int
}

// logger writes one CSV row per period, aligned to the clock (the first
// one partial: kWh from the time actually elapsed), and keeps today's
// hours in memory. It writes only to its directory — ~/Documents/PM-Log in
// production, a scratch directory under --dev, --mock and --once.
type logger struct {
	dir   string
	last  time.Time
	p     period
	hours map[time.Time]*hour
	// wrote is told a row went to disk (the report is rebuilt then).
	wrote func()
}

func newLogger(dir string) *logger { return &logger{dir: dir, hours: map[time.Time]*hour{}} }

// periodEnd: the next whole hour, midnight, Monday or first of the month.
func periodEnd(t time.Time, interval string) time.Time {
	y, m, d := t.Date()
	loc := t.Location()
	switch interval {
	case "daily":
		return time.Date(y, m, d+1, 0, 0, 0, 0, loc)
	case "weekly":
		days := (8 - int(t.Weekday())) % 7
		if days == 0 {
			days = 7
		}
		return time.Date(y, m, d+days, 0, 0, 0, 0, loc)
	case "monthly":
		return time.Date(y, m+1, 1, 0, 0, 0, 0, loc)
	}
	return time.Date(y, m, d, t.Hour()+1, 0, 0, 0, loc)
}

func hourOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, t.Hour(), 0, 0, 0, t.Location())
}

// add counts a poll: its energy since the previous one at the rate in
// effect now, into the period and the hour; a period that has ended is
// written first.
func (l *logger) add(s sample, interval string, t tariff) error {
	var err error
	if interval != "off" && !l.p.end.IsZero() && !s.at.Before(l.p.end) {
		err = l.flush(t)
	}
	if interval == "off" {
		l.p = period{}
	} else if l.p.end.IsZero() {
		l.p = period{start: s.at, end: periodEnd(s.at, interval)}
	}
	dt := 0.0
	if !l.last.IsZero() {
		if gap := s.at.Sub(l.last); gap > 0 && gap <= maxGap {
			dt = gap.Hours()
		}
	}
	l.last = s.at
	wh := s.total * dt
	cost := wh / 1000 * t.rate(s.at)

	if interval != "off" {
		p := &l.p
		p.n++
		p.total += s.total
		p.cpu += s.cpu
		p.gpu += s.gpu
		for i, v := range []*float64{s.cpuTemp, s.gpuTemp, s.cpuPct, s.ramPct} {
			if v != nil && *v != 0 { // the Python log averaged the truthy ones
				p.temps[i].sum += *v
				p.temps[i].n++
			}
		}
		p.wh += wh
		p.cost += cost
	}

	h := hourOf(s.at)
	acc := l.hours[h]
	if acc == nil {
		acc = &hour{}
		l.hours[h] = acc
		for k := range l.hours { // today and yesterday are enough
			if s.at.Sub(k) > 48*time.Hour {
				delete(l.hours, k)
			}
		}
	}
	acc.wh += wh
	acc.cost += cost
	acc.sumW += s.total
	acc.n++
	return err
}

// flush writes the period's row, stamped with the end of the period it
// covers (the report prices a row by the hour before its timestamp).
func (l *logger) flush(t tariff) error {
	p := l.p
	l.p = period{}
	if p.n == 0 || l.dir == "" {
		return nil
	}
	avg := func(i int) float64 {
		if p.temps[i].n == 0 {
			return 0
		}
		return p.temps[i].sum / float64(p.temps[i].n)
	}
	n := float64(p.n)
	row := []string{
		p.end.Format("2006-01-02 15:04:05"),
		f2(p.total / n), f2(p.cpu / n), f2(p.gpu / n),
		f2(avg(0)), f2(avg(1)), f2(avg(2)), f2(avg(3)),
		strconv.Itoa(p.n), f2(p.wh / 1000), f2(p.cost),
	}
	if err := appendRow(l.dir, p.end, row); err != nil {
		return err
	}
	if l.wrote != nil {
		l.wrote()
	}
	return nil
}

func f2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// appendRow adds a line to <dir>/<YYYY-MM>/<YYYY-MM-DD>.csv — the header
// first when the file is new — in one write, synced.
func appendRow(dir string, at time.Time, row []string) error {
	path := filepath.Join(dir, at.Format("2006-01"), at.Format("2006-01-02")+".csv")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	var b strings.Builder
	if info, err := f.Stat(); err == nil && info.Size() == 0 {
		b.WriteString(strings.Join(header, ",") + "\r\n")
	}
	b.WriteString(strings.Join(row, ",") + "\r\n") // Python's csv module ends lines so
	if _, err := f.WriteString(b.String()); err != nil {
		return err
	}
	return f.Sync()
}

// logRow is one CSV row as read back.
type logRow struct {
	ts            time.Time
	w, cpuW, gpuW float64
	cpuT, gpuT    float64
	kwh           float64
	samples       int
}

// readRows reads the given files (missing ones are skipped), in time order;
// a row that does not parse is left out, as the report does.
func readRows(paths []string) []logRow {
	var rows []logRow
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		r := csv.NewReader(bufio.NewReader(f))
		r.FieldsPerRecord = -1
		records, err := r.ReadAll()
		f.Close()
		if err != nil || len(records) < 2 {
			continue
		}
		col := map[string]int{}
		for i, name := range records[0] {
			col[strings.TrimSpace(name)] = i
		}
		get := func(rec []string, name string) (string, bool) {
			i, ok := col[name]
			if !ok || i >= len(rec) {
				return "", false
			}
			return strings.TrimSpace(rec[i]), true
		}
		num := func(rec []string, name string) float64 { // missing or empty: 0
			s, _ := get(rec, name)
			v, _ := strconv.ParseFloat(s, 64)
			return v
		}
		for _, rec := range records[1:] {
			tsText, ok1 := get(rec, "timestamp")
			wText, ok2 := get(rec, "avg_total_w")
			kText, ok3 := get(rec, "kwh_period")
			if !ok1 || !ok2 || !ok3 {
				continue
			}
			ts, err1 := time.ParseInLocation("2006-01-02 15:04:05", tsText, time.Local)
			w, err2 := strconv.ParseFloat(wText, 64)
			k, err3 := strconv.ParseFloat(kText, 64)
			if err1 != nil || err2 != nil || err3 != nil || math.IsNaN(w) || math.IsNaN(k) {
				continue
			}
			rows = append(rows, logRow{
				ts: ts, w: w, kwh: k,
				cpuW: num(rec, "avg_cpu_w"), gpuW: num(rec, "avg_gpu_w"),
				cpuT: num(rec, "avg_cpu_temp"), gpuT: num(rec, "avg_gpu_temp"),
				samples: int(num(rec, "samples")),
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ts.Before(rows[j].ts) })
	return rows
}

// logFiles is every log file under dir (<dir>/20*/*.csv), sorted.
func logFiles(dir string) []string {
	files, _ := filepath.Glob(filepath.Join(dir, "20*", "*.csv"))
	sort.Strings(files)
	return files
}

func dayFile(dir string, day time.Time) string {
	return filepath.Join(dir, day.Format("2006-01"), day.Format("2006-01-02")+".csv")
}

// today is the day so far: 24 clock-hour slots — hours this core measured
// from memory, earlier ones from the log's hourly rows — every hour priced
// at the rate it was in (never a blend with a live rate).
type today struct {
	kwh, cost float64
	watts     []float64 // average W per hour up to the current one; NaN unknown
	has       bool
}

func (l *logger) today(now time.Time, t tariff) today {
	var out today
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	type slot struct {
		kwh, cost, w float64
		ok           bool
	}
	slots := make([]slot, now.Hour()+1)
	// Rows close the hour before their timestamp.
	for _, r := range readRows([]string{dayFile(l.dir, now.AddDate(0, 0, -1)), dayFile(l.dir, now)}) {
		mid := r.ts.Add(-30 * time.Minute)
		if mid.Before(start) || !mid.Before(now) {
			continue
		}
		h := mid.Hour()
		slots[h] = slot{kwh: r.kwh, cost: r.kwh * t.rate(mid), w: r.w, ok: true}
	}
	for h := range slots {
		if acc := l.hours[start.Add(time.Duration(h)*time.Hour)]; acc != nil && acc.n > 0 {
			slots[h] = slot{kwh: acc.wh / 1000, cost: acc.cost, w: acc.sumW / float64(acc.n), ok: true}
		}
	}
	for _, s := range slots {
		if s.ok {
			out.kwh += s.kwh
			out.cost += s.cost
			out.watts = append(out.watts, s.w)
			out.has = true
		} else {
			out.watts = append(out.watts, math.NaN())
		}
	}
	return out
}

var errNoRows = errors.New("no log rows")
