package power

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/sensors"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

const (
	raplDir  = "/sys/class/powercap/intel-rapl:0"
	vramTool = "/usr/local/bin/gddr6-oneshot"
)

// cpuPower reads the package's RAPL counter (Intel, and AMD Zen as
// package-0): watts from the energy used since the previous poll. Stock
// kernels since 5.10 let only root read it (PLATYPUS); then the CPU is
// estimated from its load, and the panel says why.
type cpuPower struct {
	r      rapl
	reason string // why there is no reading, in words
}

func (c *cpuPower) read(now time.Time) *float64 {
	data, err := os.ReadFile(filepath.Join(raplDir, "energy_uj"))
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrPermission):
			c.reason = "only root may read the CPU's RAPL counter on this kernel"
		default:
			c.reason = "this CPU has no RAPL counter"
		}
		return nil
	}
	energy, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		c.reason = "the CPU's RAPL counter is unreadable"
		return nil
	}
	if c.r.max == 0 {
		if m, err := os.ReadFile(filepath.Join(raplDir, "max_energy_range_uj")); err == nil {
			c.r.max, _ = strconv.ParseUint(strings.TrimSpace(string(m)), 10, 64)
		}
	}
	c.reason = ""
	return c.r.next(energy, float64(now.UnixNano())/1e9)
}

// primed: the counter has a first reading to measure from.
func (c *cpuPower) primed() bool { return c.r.primed }

// readGPUs: NVIDIA through nvidia-smi, else AMD through rocm-smi.
func readGPUs(ctx context.Context) []sensors.GPU {
	if gpus, err := sensors.NvidiaSMI(ctx); err == nil && len(gpus) > 0 {
		return gpus
	}
	if !sys.Has("rocm-smi") {
		return nil
	}
	out, err := sys.Run(ctx, 5*time.Second, "rocm-smi", "--showpower", "--showtemp", "--showmeminfo", "vram", "--json")
	if err != nil {
		return nil
	}
	return parseRocmSMI(out)
}

// readVRAMTemps asks gddr6-oneshot, when installed, through `sudo -n`:
// never a password prompt (it runs under a NOPASSWD rule for exactly that
// binary, or fails at once).
func readVRAMTemps(ctx context.Context) (map[string]float64, error) {
	if _, err := os.Stat(vramTool); err != nil {
		return nil, errNoTool
	}
	out, err := sys.Run(ctx, 5*time.Second, "sudo", "-n", vramTool)
	if err != nil {
		return nil, err
	}
	return parseGDDR6(out), nil
}

var errNoTool = errors.New("not installed")

func cpuTempInput() string { return sensors.CPUTempInput(sensors.HwmonRoot) }

func readCPUTemp(input string) *float64 { return sensors.Milli(input) }

func readMemory() (sensors.Memory, bool) { return sensors.ReadMemory() }

// nvme is one drive, as its hwmon names it.
type nvme struct {
	Model string
	Temp  *float64
	path  string // the hwmon directory
}

func findNVMe(root string) []nvme {
	dirs, _ := filepath.Glob(filepath.Join(root, "hwmon*"))
	sort.Strings(dirs)
	var out []nvme
	for _, d := range dirs {
		if readTrim(filepath.Join(d, "name")) != "nvme" {
			continue
		}
		model := readTrim(filepath.Join(d, "device", "model"))
		if model == "" {
			model = "NVMe"
		}
		out = append(out, nvme{Model: model, path: d})
	}
	return out
}

func (n nvme) readTemp() *float64 { return sensors.Milli(filepath.Join(n.path, "temp1_input")) }

// readStatic is what changes rarely: re-read every five minutes, for
// hot-plugged monitors and USB devices.
func readStatic(ctx context.Context) staticInfo {
	var s staticInfo
	s.CPUModel = cpuModel()
	s.Board = readTrim("/sys/devices/virtual/dmi/id/board_name")
	if s.Board == "" {
		if out, err := sys.Run(ctx, 3*time.Second, "sudo", "-n", "dmidecode", "-t", "baseboard"); err == nil {
			s.Board = parseBaseboard(out)
		}
	}
	if out, err := sys.Run(ctx, 3*time.Second, "sudo", "-n", "dmidecode", "-t", "memory"); err == nil {
		s.RAM = parseDmidecodeMemory(out)
	}
	if os.Getenv("DISPLAY") != "" && sys.Has("xrandr") {
		if out, err := sys.Run(ctx, 5*time.Second, "xrandr", "--verbose", "--prop"); err == nil {
			s.Displays = parseXrandr(out)
			addSysfsEDIDs(s.Displays)
		}
	}
	if sys.Has("lsusb") {
		if out, err := sys.Run(ctx, 5*time.Second, "lsusb"); err == nil {
			s.USB = parseLsusb(out)
		}
	}
	s.NVMe = findNVMe(sensors.HwmonRoot)
	return s
}

// addSysfsEDIDs fills an output xrandr printed no EDID for from the DRM
// connector of the same name ("DP-1" is card0-DP-1), when there is one —
// matched by name, never by order.
func addSysfsEDIDs(displays []display) {
	for i := range displays {
		d := &displays[i]
		if d.Maker != "" {
			continue
		}
		matches, _ := filepath.Glob("/sys/class/drm/card*-" + d.Port + "/edid")
		for _, m := range matches {
			if raw, err := os.ReadFile(m); err == nil {
				if e, ok := parseEDID(raw); ok {
					d.Maker, d.Model, d.Inches, d.EstW = e.maker, e.model, e.inches, e.estW
					break
				}
			}
		}
	}
}

func cpuModel() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if key, value, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(key) == "model name" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func readTrim(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// cpuPowerNote is said on the panel when the CPU is estimated.
func cpuPowerNote(reason string) string {
	if reason == "" {
		return "CPU power is estimated from its load"
	}
	return "CPU power is estimated from its load: " + reason
}
