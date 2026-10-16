//go:build !linux && !windows

package power

import (
	"context"
	"errors"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/sensors"
)

// Elsewhere only NVIDIA cards are read; the CPU is estimated.

type cpuPower struct{ reason string }

func (c *cpuPower) read(time.Time) *float64 {
	c.reason = "no CPU power reading on this system"
	return nil
}
func (c *cpuPower) primed() bool { return false }

func readGPUs(ctx context.Context) []sensors.GPU {
	gpus, _ := sensors.NvidiaSMI(ctx)
	return gpus
}

var errNoTool = errors.New("not installed")

func readVRAMTemps(context.Context) (map[string]float64, error) { return nil, errNoTool }
func cpuTempInput() string                                      { return "" }
func readCPUTemp(string) *float64                               { return nil }
func readMemory() (sensors.Memory, bool)                        { return sensors.ReadMemory() }

type nvme struct {
	Model string
	Temp  *float64
}

func (n nvme) readTemp() *float64 { return nil }

func readStatic(context.Context) staticInfo { return staticInfo{} }

func cpuPowerNote(reason string) string { return "CPU power is estimated from its load: " + reason }
