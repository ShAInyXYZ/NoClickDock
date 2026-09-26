// Package sensors reads the machine's own hardware for the widgets that show
// it (ComfyUI's telemetry, Power's draw): NVIDIA cards through nvidia-smi,
// the CPU's temperature, load and the memory in use. Every reading a device
// does not report is nil — never a zero.
package sensors

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

// GPU is one NVIDIA card as nvidia-smi reports it.
type GPU struct {
	Index     int
	Name      string
	Power     *float64 // W
	Limit     *float64 // W
	Temp      *float64 // °C, core
	VRAMUsed  *float64 // MiB
	VRAMTotal *float64 // MiB
	Util      *float64 // %
	Fan       *float64 // %
	Bus       string   // "11:00.0", as gddr6-oneshot prints it
}

// nvidiaQuery is every field both widgets use, in one call.
const nvidiaQuery = "index,power.draw,power.limit,temperature.gpu,memory.used,memory.total,name,utilization.gpu,fan.speed,pci.bus_id"

// NvidiaSMI asks nvidia-smi about every card. No tool, no card or a
// failure is an empty list and the error.
func NvidiaSMI(ctx context.Context) ([]GPU, error) {
	path := nvidiaSMIPath()
	if path == "" {
		return nil, errNoTool
	}
	out, err := sys.Run(ctx, 5*time.Second, path, "--query-gpu="+nvidiaQuery, "--format=csv,noheader,nounits")
	if err != nil {
		return nil, err
	}
	return ParseNvidiaSMI(out), nil
}

type toolError string

func (e toolError) Error() string { return string(e) }

const errNoTool = toolError("nvidia-smi is not installed")

// nvidiaSMIPath: on PATH, else where the Windows driver puts it.
func nvidiaSMIPath() string {
	var fallbacks []string
	if runtime.GOOS == "windows" {
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			fallbacks = append(fallbacks, filepath.Join(pf, "NVIDIA Corporation", "NVSMI", "nvidia-smi.exe"))
		}
		if root := os.Getenv("SystemRoot"); root != "" {
			fallbacks = append(fallbacks, filepath.Join(root, "System32", "nvidia-smi.exe"))
		}
	}
	return sys.Find("nvidia-smi", fallbacks...)
}

// ParseNvidiaSMI reads one CSV line per card; "[N/A]" (a sensor the card
// lacks) and anything unreadable stay nil.
func ParseNvidiaSMI(out string) []GPU {
	var gpus []GPU
	for _, line := range strings.Split(out, "\n") {
		p := strings.Split(line, ",")
		for i := range p {
			p[i] = strings.TrimSpace(p[i])
		}
		if len(p) < 7 {
			continue
		}
		idx, err := strconv.Atoi(p[0])
		if err != nil {
			continue
		}
		g := GPU{
			Index: idx, Name: p[6],
			Power: num(p[1]), Limit: num(p[2]), Temp: num(p[3]),
			VRAMUsed: num(p[4]), VRAMTotal: num(p[5]),
		}
		if len(p) > 7 {
			g.Util = num(p[7])
		}
		if len(p) > 8 {
			g.Fan = num(p[8])
		}
		// "00000000:11:00.0" → "11:00.0"
		if len(p) > 9 {
			if _, bus, ok := strings.Cut(strings.ToLower(p[9]), ":"); ok {
				g.Bus = bus
			}
		}
		gpus = append(gpus, g)
	}
	return gpus
}

func num(s string) *float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	return &v
}

// ShortGPUName drops the vendor words every card repeats.
func ShortGPUName(name string) string {
	for _, prefix := range []string{"NVIDIA GeForce ", "NVIDIA ", "AMD Radeon ", "AMD ", "Intel(R) "} {
		if strings.HasPrefix(name, prefix) {
			return name[len(prefix):]
		}
	}
	return name
}
