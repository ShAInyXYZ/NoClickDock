package docker

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// container is one container as the panel needs it, from `docker inspect`
// and `docker stats`.
type container struct {
	Name    string
	State   string // running paused restarting exited dead created removing
	Exit    int
	HasExit bool // no exit code while running, paused or restarting
	OOM     bool
	// Finished is the raw stop time: the log-line cache is keyed on it.
	Finished string
	Restarts int
	Health   string // starting healthy unhealthy, only while running
	Image    string
	Project  string // compose project; "" = standalone
	Service  string
	Policy   string // restart policy; "" for none
	Ports    []string

	// UpH is hours since start while running-ish, AgeH hours since the
	// stop otherwise; -1 is unknown.
	UpH, AgeH float64
	Crashed   bool

	CPU  float64 // %
	Mem  string  // "492M"
	MemB float64 // exact bytes, for totals
	Log  string  // last log line, for broken ones
}

// runningish is what docker reports no exit code for.
func (c *container) runningish() bool {
	switch c.State {
	case "running", "paused", "restarting":
		return true
	}
	return false
}

// stopCodes mean "was told to stop": clean, SIGTERM (docker stop), SIGKILL
// (docker stop's timeout, docker kill) — the last only when the kernel's OOM
// killer was not the one sending it.
func stopCode(code int) bool { return code == 0 || code == 143 || code == 137 }

// inspected is the part of `docker inspect`'s JSON the panel reads. JSON,
// not a --format template: a label holding a tab or a newline cannot split
// a record in two.
type inspected struct {
	Name         string
	RestartCount int
	State        struct {
		Status     string
		ExitCode   int
		OOMKilled  bool
		StartedAt  string
		FinishedAt string
		Health     *struct{ Status string }
	}
	Config *struct {
		Image  string
		Labels map[string]string
	}
	HostConfig *struct {
		RestartPolicy struct{ Name string }
	}
	NetworkSettings *struct {
		Ports map[string][]struct {
			HostIp   string
			HostPort string
		}
	}
}

// parseInspect reads `docker inspect <ids…>` as of now.
func parseInspect(data []byte, now time.Time) ([]*container, error) {
	var raw []inspected
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("docker inspect: %w", err)
	}
	out := make([]*container, 0, len(raw))
	for _, r := range raw {
		c := &container{
			Name:     strings.TrimPrefix(r.Name, "/"),
			State:    strings.ToLower(r.State.Status),
			OOM:      r.State.OOMKilled,
			Finished: r.State.FinishedAt,
			Restarts: r.RestartCount,
			UpH:      -1,
			AgeH:     -1,
		}
		if !c.runningish() {
			c.Exit, c.HasExit = r.State.ExitCode, true
		}
		if c.State == "running" && r.State.Health != nil {
			c.Health = r.State.Health.Status
		}
		if r.Config != nil {
			c.Image = r.Config.Image
			c.Project = r.Config.Labels["com.docker.compose.project"]
			c.Service = r.Config.Labels["com.docker.compose.service"]
		}
		if r.HostConfig != nil {
			if p := r.HostConfig.RestartPolicy.Name; p != "no" {
				c.Policy = p
			}
		}
		if r.NetworkSettings != nil {
			c.Ports = ports(r.NetworkSettings.Ports)
		}
		if c.runningish() {
			c.UpH = hoursSince(r.State.StartedAt, now)
		} else {
			c.AgeH = hoursSince(r.State.FinishedAt, now)
		}
		c.Crashed = (c.State == "exited" || c.State == "dead") && (c.OOM || !stopCode(c.Exit))
		out = append(out, c)
	}
	return out, nil
}

// ports lists each published container port by its first binding, as short
// as it can be said: ":8080" (same port, every interface), ":8080->80",
// "127.0.0.1:8108" (bound to one address).
func ports(m map[string][]struct{ HostIp, HostPort string }) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys) // docker's own JSON order
	var out []string
	for _, cport := range keys {
		binds := m[cport]
		if len(binds) == 0 {
			continue
		}
		b := binds[0]
		host := b.HostIp
		if host == "0.0.0.0" || host == "::" {
			host = ""
		}
		cp, _, _ := strings.Cut(cport, "/")
		switch {
		case host != "":
			out = append(out, host+":"+b.HostPort)
		case b.HostPort == cp:
			out = append(out, ":"+b.HostPort)
		default:
			out = append(out, ":"+b.HostPort+"->"+cp)
		}
	}
	return out
}

// hoursSince reads a docker timestamp; -1 for the zero time (year 1: never)
// or anything unreadable.
func hoursSince(iso string, now time.Time) float64 {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(iso))
	if err != nil || t.Year() < 2000 {
		return -1
	}
	return max(0, now.Sub(t).Hours())
}

// usage is one running container's line of `docker stats`.
type usage struct {
	cpu  float64
	mem  string
	memB float64
}

// parseStats reads `docker stats --no-stream --format
// "{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}"`: "api\t7.12%\t492.1MiB / 125.1GiB".
func parseStats(out string) map[string]usage {
	res := map[string]usage{}
	for _, line := range strings.Split(out, "\n") {
		p := strings.Split(line, "\t")
		if len(p) < 3 {
			continue
		}
		cpu, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(p[1]), "%"), 64)
		if err != nil {
			cpu = 0
		}
		used, _, _ := strings.Cut(p[2], "/")
		res[p[0]] = usage{cpu: cpu, mem: fmtMem(used), memB: memExact(used)}
	}
	return res
}

var memRE = regexp.MustCompile(`^\s*([\d.]+)\s*([KkMGT]?)i?B`)

// fmtMem shortens docker stats' used memory: "492.1MiB" → "492M", "22.21GiB"
// → "22.2G" — one decimal only for G and T below ten. Bytes and kilobytes
// say nothing on this panel: "".
func fmtMem(text string) string {
	m := memRE.FindStringSubmatch(text)
	if m == nil || m[2] == "" || m[2] == "k" || m[2] == "K" {
		return ""
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return ""
	}
	if (m[2] == "G" || m[2] == "T") && v < 10 {
		return fmt.Sprintf("%.1f%s", v, m[2])
	}
	return fmt.Sprintf("%.0f%s", v, m[2])
}

// memExact is docker stats' used memory in bytes (binary units): stack
// totals are summed from these, not from the rounded text.
func memExact(text string) float64 {
	m := memRE.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	scale := map[string]float64{"": 1, "k": 1 << 10, "K": 1 << 10, "M": 1 << 20, "G": 1 << 30, "T": 1 << 40}
	return v * scale[m[2]]
}

// fmtBytesShort is a unit's memory: "1.5G", "492M", nothing below a megabyte.
func fmtBytesShort(n float64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", n/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0fM", n/(1<<20))
	}
	return ""
}

// dfRow is one line of `docker system df`.
type dfRow struct {
	total, active string
	size, recl    float64
	pct           string
}

// parseDF reads `docker system df --format "{{.Type}}\t{{.TotalCount}}\t
// {{.Active}}\t{{.Size}}\t{{.Reclaimable}}"`, keyed by the lower-cased type:
// images, containers, local volumes, build cache.
func parseDF(out string) map[string]dfRow {
	df := map[string]dfRow{}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		p := append(strings.Split(line, "\t"), "", "", "", "")[:5]
		size, _ := sizes(p[3])
		recl, pct := sizes(p[4])
		df[strings.ToLower(p[0])] = dfRow{total: p[1], active: p[2], size: size, recl: recl, pct: pct}
	}
	return df
}

var sizeRE = regexp.MustCompile(`^\s*([\d.]+)\s*([kKMGT]?B)\s*(?:\((\d+%)\))?`)

// sizes reads a df cell: "83.54GB (17%)" → 83.54e9, "17%". Docker's sizes
// are decimal.
func sizes(text string) (float64, string) {
	m := sizeRE.FindStringSubmatch(text)
	if m == nil {
		return 0, ""
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, ""
	}
	mult := map[string]float64{"B": 1, "kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12}
	return v * mult[m[2]], m[3]
}

// fmtDec is a decimal size, as docker counts: "1.2T", "83G", "9.5G", "512M",
// "0".
func fmtDec(n float64) string {
	for _, u := range []struct {
		suffix string
		size   float64
	}{{"T", 1e12}, {"G", 1e9}, {"M", 1e6}, {"K", 1e3}} {
		if n >= u.size {
			if v := n / u.size; v < 10 {
				return fmt.Sprintf("%.1f%s", v, u.suffix)
			} else {
				return fmt.Sprintf("%.0f%s", v, u.suffix)
			}
		}
	}
	if n == 0 {
		return "0"
	}
	return fmt.Sprintf("%.0fB", n)
}

// dur is how long, in the fewest characters: "24m", "46h", "3d 18h", "12d".
func dur(h float64) string {
	switch {
	case h < 0:
		return ""
	case h < 1.0/60:
		return "<1m"
	case h < 1:
		return fmt.Sprintf("%.0fm", h*60)
	case h < 48:
		return fmt.Sprintf("%.0fh", h)
	}
	d, rest := int(h)/24, int(h)%24
	if d < 7 {
		return fmt.Sprintf("%dd %dh", d, rest)
	}
	return fmt.Sprintf("%dd", d)
}

// ago is how long ago, short enough to share a row with buttons: "24m ago",
// "46h ago", "5d ago", "3w ago", "4mo ago".
func ago(h float64) string {
	switch {
	case h < 0:
		return ""
	case h < 1.0/60:
		return "just now"
	case h < 1:
		return fmt.Sprintf("%.0fm ago", h*60)
	case h < 48:
		return fmt.Sprintf("%.0fh ago", h)
	case h < 24*14:
		return fmt.Sprintf("%.0fd ago", h/24)
	case h < 24*60:
		return fmt.Sprintf("%.0fw ago", h/168)
	}
	return fmt.Sprintf("%.0fmo ago", h/720)
}

var (
	ansiRE  = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	spaceRE = regexp.MustCompile(`\s+`)
	stampRE = regexp.MustCompile(`^\d{4}-\d\d-\d\dT[\d:.]+Z?\s*`)
)

// lastLogLine is the last thing a container wrote, cleaned for one row:
// colour codes out, whitespace collapsed, a leading timestamp dropped, 90
// characters at most.
func lastLogLine(out string) string {
	var last string
	for _, ln := range strings.Split(ansiRE.ReplaceAllString(out, ""), "\n") {
		if strings.TrimSpace(ln) != "" {
			last = ln
		}
	}
	line := strings.TrimSpace(spaceRE.ReplaceAllString(last, " "))
	line = stampRE.ReplaceAllString(line, "")
	return cut(line, 90)
}

// cut keeps the first n characters (runes, not bytes).
func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
