package sensors

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// CPUUsage is utilisation between two successive readings: the first one
// has nothing to compare with and says nothing.
type CPUUsage struct {
	mu          sync.Mutex
	idle, total uint64
	primed      bool
}

// Read is the load since the previous Read, in percent; nil on the first
// call or when the counters cannot be read.
func (c *CPUUsage) Read() *float64 {
	idle, total, ok := cpuTimes()
	if !ok {
		return nil
	}
	return c.next(idle, total)
}

func (c *CPUUsage) next(idle, total uint64) *float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	prevIdle, prevTotal, primed := c.idle, c.total, c.primed
	c.idle, c.total, c.primed = idle, total, true
	if !primed || total <= prevTotal || idle < prevIdle {
		return nil
	}
	pct := (1 - float64(idle-prevIdle)/float64(total-prevTotal)) * 100
	pct = max(0, min(100, pct))
	return &pct
}

// ParseProcStat reads /proc/stat's first line: idle is idle + iowait.
func ParseProcStat(data string) (idle, total uint64, ok bool) {
	line, _, _ := strings.Cut(data, "\n")
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0, false
	}
	var vals []uint64
	for _, s := range f[1:] {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		vals = append(vals, v)
	}
	// guest and guest_nice are already inside user and nice.
	if len(vals) > 8 {
		vals = vals[:8]
	}
	for _, v := range vals {
		total += v
	}
	idle = vals[3]
	if len(vals) > 4 {
		idle += vals[4]
	}
	return idle, total, true
}

// Memory is the RAM in use: total minus what is available.
type Memory struct {
	UsedKB, TotalKB uint64
}

func (m Memory) Pct() float64 {
	if m.TotalKB == 0 {
		return 0
	}
	return float64(m.UsedKB) / float64(m.TotalKB) * 100
}

// ParseMeminfo reads /proc/meminfo.
func ParseMeminfo(data string) (Memory, bool) {
	var total, avail uint64
	var haveTotal, haveAvail bool
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			total, haveTotal = v, true
		case "MemAvailable:":
			avail, haveAvail = v, true
		}
	}
	if !haveTotal || !haveAvail || total == 0 || avail > total {
		return Memory{}, false
	}
	return Memory{UsedKB: total - avail, TotalKB: total}, true
}

// HwmonRoot is where Linux lists its hardware monitors.
const HwmonRoot = "/sys/class/hwmon"

// CPUTempInput finds the CPU package temperature under a hwmon tree: the
// known CPU drivers' die reading (Tctl, Tdie, Package id 0, or an
// unlabelled first input) first, then any input labelled Package, Tdie or
// Tctl. "" when there is none.
func CPUTempInput(root string) string {
	hwmons, _ := filepath.Glob(filepath.Join(root, "hwmon*"))
	sort.Strings(hwmons)
	var labelled []string
	for _, h := range hwmons {
		name := readTrim(filepath.Join(h, "name"))
		if name == "" {
			continue
		}
		inputs, _ := filepath.Glob(filepath.Join(h, "temp*_input"))
		sort.Strings(inputs)
		known := name == "k10temp" || name == "zenpower" || name == "coretemp"
		for _, in := range inputs {
			label := readTrim(strings.TrimSuffix(in, "_input") + "_label")
			switch {
			case known && (label == "Tdie" || label == "Tctl" || label == "Package id 0" || label == ""):
				return in
			case known,
				strings.HasPrefix(label, "Package"), strings.HasPrefix(label, "Tdie"), strings.HasPrefix(label, "Tctl"):
				labelled = append(labelled, in)
			}
		}
	}
	if len(labelled) > 0 {
		return labelled[0]
	}
	return ""
}

// Milli reads a hwmon millidegree file as °C; nil when it cannot.
func Milli(path string) *float64 {
	if path == "" {
		return nil
	}
	v, err := strconv.ParseFloat(readTrim(path), 64)
	if err != nil {
		return nil
	}
	v /= 1000
	return &v
}

func readTrim(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
