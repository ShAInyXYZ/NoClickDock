package power

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The report: every logged row, re-priced at the hour it covers with the
// current tariff, gathered into one self-contained page. A port of the
// Python widget's generate_report.py — same report_data.json, same numbers
// (tested against the Python on a fixture log) — with its template, which
// only renders. Written next to the log.

//go:embed report_template.html
var reportTemplate string

// The page's fonts travel inside it, as data URLs: a report opened from
// Documents never falls back to whatever the system has (design M5 r1).
//
//go:embed fonts/*.woff2
var reportFonts embed.FS

var reportPage = template.Must(template.New("report").Parse(withFonts(reportTemplate)))

// withFonts turns each url("fonts/<file>") into a data: URL of that file.
func withFonts(page string) string {
	files, _ := reportFonts.ReadDir("fonts")
	for _, f := range files {
		data, err := reportFonts.ReadFile("fonts/" + f.Name())
		if err != nil {
			continue
		}
		page = strings.ReplaceAll(page, `url("fonts/`+f.Name()+`")`,
			`url("data:font/woff2;base64,`+base64.StdEncoding.EncodeToString(data)+`")`)
	}
	return page
}

const (
	gapHours   = 3 // a hole in the log longer than this is a gap
	recentDays = 30
	fullDay    = 20 // rows: an honest comparison needs whole days
)

type reportData struct {
	Cfg        reportCfg     `json:"cfg"`
	Period     reportPeriod  `json:"period"`
	Stats      reportStats   `json:"stats"`
	Recent     reportRecent  `json:"recent"`
	Daily      []reportDay   `json:"daily"`
	Rolling    []float64     `json:"rolling"`
	Monthly    []reportMonth `json:"monthly"`
	Hourly     []float64     `json:"hourly"`
	HourlyMax  []float64     `json:"hourlyMax"`
	HourlyPeak []bool        `json:"hourlyPeak"`
	Weekday    []float64     `json:"weekday"`
	Heat       [][]*float64  `json:"heat"`
	Context    [][3]string   `json:"context"`
}

type reportCfg struct {
	Currency   string  `json:"currency"`
	TOU        bool    `json:"tou"`
	Flat       float64 `json:"flat"`
	Peak       float64 `json:"peak"`
	Off        float64 `json:"off"`
	PeakHours  string  `json:"peak_hours"`
	WeekendOff bool    `json:"weekend_off"`
}

type reportGap struct {
	From  string  `json:"from"`
	To    string  `json:"to"`
	Hours float64 `json:"hours"`
}

type reportPeriod struct {
	First     string      `json:"first"`
	Last      string      `json:"last"`
	NDays     int         `json:"nDays"`
	FullDays  int         `json:"fullDays"`
	NRecords  int         `json:"nRecords"`
	Samples   int         `json:"samples"`
	Coverage  float64     `json:"coverage"`
	Gaps      []reportGap `json:"gaps"`
	NGaps     int         `json:"nGaps"`
	Generated string      `json:"generated"`
}

type reportStats struct {
	TotalKwh     float64 `json:"totalKwh"`
	TotalCost    float64 `json:"totalCost"`
	EffRate      float64 `json:"effRate"`
	AvgDayKwh    float64 `json:"avgDayKwh"`
	MedianDayKwh float64 `json:"medianDayKwh"`
	AvgDayCost   float64 `json:"avgDayCost"`
	CV           float64 `json:"cv"`
	AvgW         float64 `json:"avgW"`
	MaxW         float64 `json:"maxW"`
	MinW         float64 `json:"minW"`
	WorstDay     string  `json:"worstDay"`
	WorstKwh     float64 `json:"worstKwh"`
	WorstCost    float64 `json:"worstCost"`
	BestDay      string  `json:"bestDay"`
	BestKwh      float64 `json:"bestKwh"`
	PeakShare    float64 `json:"peakShare"`
	PeakKwh      float64 `json:"peakKwh"`
	PeakCost     float64 `json:"peakCost"`
	OffKwh       float64 `json:"offKwh"`
	OffCost      float64 `json:"offCost"`
	GPUShare     float64 `json:"gpuShare"`
	CPUShare     float64 `json:"cpuShare"`
	OtherShare   float64 `json:"otherShare"`
	BusiestHour  int     `json:"busiestHour"`
	QuietestHour int     `json:"quietestHour"`
	Drift        float64 `json:"drift"`
	GPUTmax      float64 `json:"gpuTmax"`
	CPUTmax      float64 `json:"cpuTmax"`
}

type reportRecent struct {
	Days      int     `json:"days"`
	Kwh       float64 `json:"kwh"`
	Cost      float64 `json:"cost"`
	AvgDayKwh float64 `json:"avgDayKwh"`
	AvgW      float64 `json:"avgW"`
	Trend     float64 `json:"trend"`
	YearKwh   float64 `json:"yearKwh"`
	YearCost  float64 `json:"yearCost"`
	MonthCost float64 `json:"monthCost"`
}

type reportDay struct {
	D     string  `json:"d"`
	Kwh   float64 `json:"kwh"`
	Cost  float64 `json:"cost"`
	MaxW  float64 `json:"maxw"`
	Hours int     `json:"hours"`
	Peak  float64 `json:"peak"`
}

type reportMonth struct {
	M       string   `json:"m"`
	Kwh     float64  `json:"kwh"`
	Cost    float64  `json:"cost"`
	Days    int      `json:"days"`
	Avg     float64  `json:"avg"`
	GPU     float64  `json:"gpu"`
	CPU     float64  `json:"cpu"`
	Other   float64  `json:"other"`
	Peak    float64  `json:"peak"`
	AvgW    float64  `json:"avgW"`
	GPUT    *float64 `json:"gpuT"`
	GPUTmax *float64 `json:"gpuTmax"`
	CPUT    *float64 `json:"cpuT"`
	CPUTmax *float64 `json:"cpuTmax"`
}

// priced is a row with its hour's price and its split.
type priced struct {
	logRow
	mid                      time.Time
	peak                     bool
	cost                     float64
	gpuKwh, cpuKwh, otherKwh float64
}

// round is Python's round(x, n): correctly rounded, ties to even.
func round(v float64, n int) float64 {
	r, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', n, 64), 64)
	return r
}

// mean is Python's statistics.mean: the exact sum of the floats, divided,
// rounded once. A tie like 177.5 then rounds the way the Python's did.
func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sum := new(big.Rat)
	for _, x := range v {
		sum.Add(sum, new(big.Rat).SetFloat64(x))
	}
	f, _ := sum.Quo(sum, new(big.Rat).SetInt64(int64(len(v)))).Float64()
	return f
}

// pySum is Python's built-in sum() of floats (3.12+): Neumaier's
// compensated summation. The report's totals are sums of that kind, its
// daily and monthly ones plain additions; each is ported as it was.
func pySum(v []float64) float64 {
	s, c := 0.0, 0.0
	for _, x := range v {
		t := s + x
		if math.Abs(s) >= math.Abs(x) {
			c += (s - t) + x
		} else {
			c += (x - t) + s
		}
		s = t
	}
	if c != 0 && !math.IsInf(c, 0) && !math.IsNaN(c) {
		s += c
	}
	return s
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// pstdev is statistics.pstdev: the population variance computed exactly.
func pstdev(v []float64) float64 {
	n := new(big.Rat).SetInt64(int64(len(v)))
	m := new(big.Rat)
	for _, x := range v {
		m.Add(m, new(big.Rat).SetFloat64(x))
	}
	m.Quo(m, n)
	ss := new(big.Rat)
	for _, x := range v {
		d := new(big.Rat).Sub(new(big.Rat).SetFloat64(x), m)
		ss.Add(ss, d.Mul(d, d))
	}
	variance, _ := ss.Quo(ss, n).Float64()
	return math.Sqrt(variance)
}

// pyWeekday is Python's: Monday 0.
func pyWeekday(t time.Time) int { return (int(t.Weekday()) + 6) % 7 }

// thousands is Python's "{:,.0f}".
func thousands(v float64) string {
	s := strconv.FormatFloat(v, 'f', 0, 64)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// buildReport computes the report from rows (in time order).
func buildReport(rows []logRow, t tariff, peakHours string, now time.Time) (reportData, error) {
	if len(rows) == 0 {
		return reportData{}, errNoRows
	}
	ps := make([]priced, len(rows))
	for i, r := range rows {
		p := priced{logRow: r, mid: r.ts.Add(-30 * time.Minute)}
		p.peak = t.inPeak(p.mid)
		p.cost = r.kwh * t.rate(p.mid)
		if r.w != 0 {
			p.gpuKwh = r.kwh * (r.gpuW / r.w)
			p.cpuKwh = r.kwh * (r.cpuW / r.w)
		}
		p.otherKwh = math.Max(0, r.kwh-p.gpuKwh-p.cpuKwh)
		ps[i] = p
	}

	type day struct {
		kwh, cost, maxw, peak, gpu, cpu, other float64
		hours                                  int
	}
	daily := map[string]*day{}
	for _, p := range ps {
		k := p.ts.Format("2006-01-02")
		d := daily[k]
		if d == nil {
			d = &day{}
			daily[k] = d
		}
		d.kwh += p.kwh
		d.cost += p.cost
		d.maxw = math.Max(d.maxw, p.w)
		if p.peak {
			d.peak += p.kwh
		}
		d.gpu += p.gpuKwh
		d.cpu += p.cpuKwh
		d.other += p.otherKwh
		d.hours++
	}
	days := make([]string, 0, len(daily))
	for k := range daily {
		days = append(days, k)
	}
	sort.Strings(days)
	var full []string
	for _, d := range days {
		if daily[d].hours >= fullDay {
			full = append(full, d)
		}
	}
	kwhOf := func(ds []string) []float64 {
		out := make([]float64, len(ds))
		for i, d := range ds {
			out[i] = daily[d].kwh
		}
		return out
	}

	type month struct {
		kwh, cost, gpu, cpu, other, peak float64
		days                             map[string]bool
		gpuT, cpuT, w                    []float64
		gpuTmax, cpuTmax                 float64
	}
	monthly := map[string]*month{}
	for _, p := range ps {
		k := p.ts.Format("2006-01")
		m := monthly[k]
		if m == nil {
			m = &month{days: map[string]bool{}}
			monthly[k] = m
		}
		m.kwh += p.kwh
		m.cost += p.cost
		m.days[p.ts.Format("2006-01-02")] = true
		m.gpu += p.gpuKwh
		m.cpu += p.cpuKwh
		m.other += p.otherKwh
		if p.peak {
			m.peak += p.kwh
		}
		m.w = append(m.w, p.w)
		if p.gpuT != 0 {
			m.gpuT = append(m.gpuT, p.gpuT)
			m.gpuTmax = math.Max(m.gpuTmax, p.gpuT)
		}
		if p.cpuT != 0 {
			m.cpuT = append(m.cpuT, p.cpuT)
			m.cpuTmax = math.Max(m.cpuTmax, p.cpuT)
		}
	}

	hourly := make([][]float64, 24)
	heat := make([][][]float64, 7)
	for i := range heat {
		heat[i] = make([][]float64, 24)
	}
	for _, p := range ps {
		h := p.mid.Hour()
		hourly[h] = append(hourly[h], p.w)
		heat[pyWeekday(p.mid)][h] = append(heat[pyWeekday(p.mid)][h], p.w)
	}
	weekdayKwh := make([][]float64, 7)
	for _, d := range full {
		date, _ := time.ParseInLocation("2006-01-02", d, time.Local)
		weekdayKwh[pyWeekday(date)] = append(weekdayKwh[pyWeekday(date)], daily[d].kwh)
	}

	first, last := ps[0].ts, ps[len(ps)-1].ts
	spanH := math.Max(1, last.Sub(first).Hours())
	coverage := math.Min(1, float64(len(ps))/spanH)
	var gaps []reportGap
	for i := 1; i < len(ps); i++ {
		if hrs := ps[i].ts.Sub(ps[i-1].ts).Hours(); hrs > gapHours {
			gaps = append(gaps, reportGap{From: ps[i-1].ts.Format("2006-01-02 15:04"), To: ps[i].ts.Format("2006-01-02 15:04"), Hours: round(hrs, 1)})
		}
	}
	sort.SliceStable(gaps, func(i, j int) bool { return gaps[i].Hours > gaps[j].Hours })

	var watts, kwhs, costs, peakKwhs, peakCosts, gpus, cpus, others []float64
	samples := 0
	for _, p := range ps {
		watts = append(watts, p.w)
		kwhs = append(kwhs, p.kwh)
		costs = append(costs, p.cost)
		if p.peak {
			peakKwhs = append(peakKwhs, p.kwh)
			peakCosts = append(peakCosts, p.cost)
		}
		gpus = append(gpus, p.gpuKwh)
		cpus = append(cpus, p.cpuKwh)
		others = append(others, p.otherKwh)
		samples += p.samples
	}
	totalKwh, totalCost, peakKwh, peakCost := pySum(kwhs), pySum(costs), pySum(peakKwhs), pySum(peakCosts)
	gpuKwh, cpuKwh, otherKwh := pySum(gpus), pySum(cpus), pySum(others)
	dailyFull := kwhOf(full)
	if len(dailyFull) == 0 {
		dailyFull = kwhOf(days)
	}
	avgDay := mean(dailyFull)
	medDay := median(dailyFull)
	cv := 0.0
	if avgDay != 0 && len(dailyFull) > 1 {
		cv = pstdev(dailyFull) / avgDay
	}
	pool := full
	if len(pool) == 0 {
		pool = days
	}
	worst, best := pool[0], pool[0]
	for _, d := range pool[1:] {
		if daily[d].kwh > daily[worst].kwh {
			worst = d
		}
		if daily[d].kwh < daily[best].kwh {
			best = d
		}
	}

	cut := last.AddDate(0, 0, -recentDays)
	var recent []priced
	recentSet := map[string]bool{}
	for _, p := range ps {
		if !p.ts.Before(cut) {
			recent = append(recent, p)
			recentSet[p.ts.Format("2006-01-02")] = true
		}
	}
	var recentFull []string
	for _, d := range days {
		if recentSet[d] && daily[d].hours >= fullDay {
			recentFull = append(recentFull, d)
		}
	}
	recentAvgDay := avgDay
	if len(recentFull) > 0 {
		recentAvgDay = mean(kwhOf(recentFull))
	}
	var recentW, recentCosts, recentKwhs []float64
	for _, p := range recent {
		recentW = append(recentW, p.w)
		recentCosts = append(recentCosts, p.cost)
		recentKwhs = append(recentKwhs, p.kwh)
	}
	recentCost, recentKwh := pySum(recentCosts), pySum(recentKwhs)
	recentAvgW := mean(watts)
	if len(recent) > 0 {
		recentAvgW = mean(recentW)
	}
	trend := 0.0
	if avgDay != 0 {
		trend = (recentAvgDay - avgDay) / avgDay * 100
	}
	half := len(full) / 2
	firstHalf, secondHalf := avgDay, avgDay
	if half > 0 {
		firstHalf, secondHalf = mean(kwhOf(full[:half])), mean(kwhOf(full[half:]))
	}
	drift := 0.0
	if firstHalf != 0 {
		drift = (secondHalf - firstHalf) / firstHalf * 100
	}

	hourlyAvg, hourlyMax := make([]float64, 24), make([]float64, 24)
	for h := 0; h < 24; h++ {
		if len(hourly[h]) > 0 {
			hourlyAvg[h] = round(mean(hourly[h]), 1)
			m := hourly[h][0]
			for _, w := range hourly[h] {
				m = math.Max(m, w)
			}
			hourlyMax[h] = round(m, 1)
		}
	}
	busiest, quietest := 0, 0
	for h := 1; h < 24; h++ {
		if hourlyAvg[h] > hourlyAvg[busiest] {
			busiest = h
		}
		if hourlyAvg[h] < hourlyAvg[quietest] {
			quietest = h
		}
	}
	var roll []float64
	for i := range days {
		roll = append(roll, round(mean(kwhOf(days[max(0, i-6):i+1])), 3))
	}

	share := func(v float64) float64 {
		if totalKwh == 0 {
			return 0
		}
		return round(v/totalKwh*100, 0)
	}
	eff, perKwh := t.flat, t.flat
	if totalKwh != 0 {
		eff, perKwh = round(totalCost/totalKwh, 3), totalCost/totalKwh
	}
	maxOf := func(v []float64) float64 {
		m := v[0]
		for _, x := range v {
			m = math.Max(m, x)
		}
		return m
	}
	minOf := func(v []float64) float64 {
		m := v[0]
		for _, x := range v {
			m = math.Min(m, x)
		}
		return m
	}
	var gpuTmax, cpuTmax float64
	for _, p := range ps {
		gpuTmax = math.Max(gpuTmax, p.gpuT)
		cpuTmax = math.Max(cpuTmax, p.cpuT)
	}
	nRecent := float64(max(1, len(recentSet)))
	yearKwh := recentAvgDay * 365

	data := reportData{
		Cfg: reportCfg{Currency: t.currency, TOU: t.tou, Flat: t.flat, Peak: t.peak, Off: t.off, WeekendOff: t.weekendOffPeak},
		Period: reportPeriod{
			First: first.Format("2006-01-02"), Last: last.Format("2006-01-02"),
			NDays: len(days), FullDays: len(full), NRecords: len(ps), Samples: samples,
			Coverage: round(coverage*100, 1), Gaps: gaps[:min(len(gaps), 8)], NGaps: len(gaps),
			Generated: now.Format("2006-01-02 15:04"),
		},
		Stats: reportStats{
			TotalKwh: round(totalKwh, 1), TotalCost: round(totalCost, 2), EffRate: eff,
			AvgDayKwh: round(avgDay, 2), MedianDayKwh: round(medDay, 2), AvgDayCost: round(avgDay*perKwh, 2),
			CV: round(cv*100, 0), AvgW: round(mean(watts), 0), MaxW: round(maxOf(watts), 0), MinW: round(minOf(watts), 0),
			WorstDay: worst, WorstKwh: round(daily[worst].kwh, 2), WorstCost: round(daily[worst].cost, 2),
			BestDay: best, BestKwh: round(daily[best].kwh, 2),
			PeakShare: share(peakKwh), PeakKwh: round(peakKwh, 1), PeakCost: round(peakCost, 2),
			OffKwh: round(totalKwh-peakKwh, 1), OffCost: round(totalCost-peakCost, 2),
			GPUShare: share(gpuKwh), CPUShare: share(cpuKwh), OtherShare: share(otherKwh),
			BusiestHour: busiest, QuietestHour: quietest, Drift: round(drift, 0),
			GPUTmax: round(gpuTmax, 0), CPUTmax: round(cpuTmax, 0),
		},
		Recent: reportRecent{
			Days: recentDays, Kwh: round(recentKwh, 1), Cost: round(recentCost, 2),
			AvgDayKwh: round(recentAvgDay, 2), AvgW: round(recentAvgW, 0), Trend: round(trend, 0),
			YearKwh: round(yearKwh, 0), YearCost: round(recentCost/nRecent*365, 0), MonthCost: round(recentCost/nRecent*30.4, 2),
		},
		Rolling: roll, Hourly: hourlyAvg, HourlyMax: hourlyMax,
		Context: [][3]string{
			{"Fridge-freezer (A+ class, ~0.8 kWh/day)", fmt.Sprintf("%.1f× a fridge", recentAvgDay/0.8), "per day"},
			{"Electric kettle, 1 boil (~0.17 kWh)", fmt.Sprintf("%.0f boils", recentAvgDay/0.17), "per day"},
			{"Washing machine, 40 °C cycle (~0.8 kWh)", fmt.Sprintf("%.1f cycles", recentAvgDay/0.8), "per day"},
			{"10 W LED bulb, 5 h/evening (0.05 kWh)", fmt.Sprintf("%.0f bulbs", recentAvgDay/0.05), "per day"},
			{"Electric car, 100 km (~17 kWh)", thousands(yearKwh/17*100) + " km", "per year at this rate"},
		},
	}
	if t.tou {
		data.Cfg.PeakHours = peakHours
	}
	if data.Period.Gaps == nil {
		data.Period.Gaps = []reportGap{}
	}
	for _, d := range days {
		v := daily[d]
		data.Daily = append(data.Daily, reportDay{D: d, Kwh: round(v.kwh, 3), Cost: round(v.cost, 3), MaxW: round(v.maxw, 0), Hours: v.hours, Peak: round(v.peak, 3)})
	}
	months := make([]string, 0, len(monthly))
	for k := range monthly {
		months = append(months, k)
	}
	sort.Strings(months)
	opt := func(v float64) *float64 {
		if v == 0 {
			return nil
		}
		return &v
	}
	for _, k := range months {
		m := monthly[k]
		rm := reportMonth{
			M: k, Kwh: round(m.kwh, 1), Cost: round(m.cost, 2), Days: len(m.days),
			Avg: round(m.kwh/float64(max(1, len(m.days))), 2),
			GPU: round(m.gpu, 1), CPU: round(m.cpu, 1), Other: round(m.other, 1), Peak: round(m.peak, 1),
			AvgW: round(mean(m.w), 0), GPUTmax: opt(round(m.gpuTmax, 0)), CPUTmax: opt(round(m.cpuTmax, 0)),
		}
		if len(m.gpuT) > 0 {
			v := round(mean(m.gpuT), 0)
			rm.GPUT = &v
		}
		if len(m.cpuT) > 0 {
			v := round(mean(m.cpuT), 0)
			rm.CPUT = &v
		}
		data.Monthly = append(data.Monthly, rm)
	}
	for h := 0; h < 24; h++ {
		peak := false
		if t.tou {
			for _, r := range t.hours {
				peak = peak || (r.From <= h && h < r.To)
			}
		}
		data.HourlyPeak = append(data.HourlyPeak, peak)
	}
	for i := 0; i < 7; i++ {
		data.Weekday = append(data.Weekday, 0)
		if len(weekdayKwh[i]) > 0 {
			data.Weekday[i] = round(mean(weekdayKwh[i]), 2)
		}
		row := make([]*float64, 24)
		for h := 0; h < 24; h++ {
			if len(heat[i][h]) > 0 {
				v := round(mean(heat[i][h]), 0)
				row[h] = &v
			}
		}
		data.Heat = append(data.Heat, row)
	}
	return data, nil
}

// writeReport rebuilds report_data.json and power-consumption-report.html
// in dir from every row logged there, and returns the page's path.
func writeReport(dir string, t tariff, peakHours string, now time.Time) (string, error) {
	data, err := buildReport(readRows(logFiles(dir)), t, peakHours, now)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	if err := writeFile(filepath.Join(dir, "report_data.json"), raw); err != nil {
		return "", err
	}
	var page strings.Builder
	if err := reportPage.Execute(&page, data); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "power-consumption-report.html")
	return path, writeFile(path, []byte(page.String()))
}

// writeFile replaces a file through a temporary one and a rename: a reader
// never sees half a report.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".report-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
