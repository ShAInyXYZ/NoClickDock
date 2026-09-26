package power

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"time"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows/registry"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/sensors"
)

// Windows (M7 reviews the rest): GPUs through nvidia-smi.exe; the CPU
// package's power and temperature only from LibreHardwareMonitor's WMI
// namespace (or OpenHardwareMonitor's), queried natively — never by
// spawning PowerShell every poll. Without it the CPU is estimated and the
// panel says so. Displays, USB devices and drives: M7.

const lhmRetry = time.Minute

var lhm struct {
	sync.Mutex
	at, retry   time.Time
	power, temp *float64
}

// readLHM returns the CPU package's power and temperature, queried at most
// once a poll; an absent namespace is looked for again a minute later.
func readLHM(now time.Time) (power, temp *float64) {
	lhm.Lock()
	defer lhm.Unlock()
	if now.Before(lhm.retry) {
		return nil, nil
	}
	if now.Sub(lhm.at) < time.Second {
		return lhm.power, lhm.temp
	}
	p, t, err := queryLHM()
	lhm.at = now
	if err != nil {
		lhm.retry = now.Add(lhmRetry)
		lhm.power, lhm.temp = nil, nil
		return nil, nil
	}
	lhm.power, lhm.temp = p, t
	return p, t
}

func queryLHM() (power, temp *float64, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		var oleErr *ole.OleError
		if !errors.As(err, &oleErr) || oleErr.Code() != 1 { // S_FALSE: already initialised
			return nil, nil, err
		}
	}
	defer ole.CoUninitialize()
	unknown, err := oleutil.CreateObject("WbemScripting.SWbemLocator")
	if err != nil {
		return nil, nil, err
	}
	defer unknown.Release()
	locator, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return nil, nil, err
	}
	defer locator.Release()
	for _, ns := range []string{`root\LibreHardwareMonitor`, `root\OpenHardwareMonitor`} {
		svcRaw, err := oleutil.CallMethod(locator, "ConnectServer", nil, ns)
		if err != nil {
			continue
		}
		svc := svcRaw.ToIDispatch()
		power, temp, err = cpuSensors(svc)
		svc.Release()
		if err == nil && (power != nil || temp != nil) {
			return power, temp, nil
		}
	}
	return nil, nil, errors.New("no LibreHardwareMonitor WMI namespace")
}

func cpuSensors(svc *ole.IDispatch) (power, temp *float64, err error) {
	resRaw, err := oleutil.CallMethod(svc, "ExecQuery",
		"SELECT Name, SensorType, Value, Parent FROM Sensor WHERE SensorType='Power' OR SensorType='Temperature'")
	if err != nil {
		return nil, nil, err
	}
	res := resRaw.ToIDispatch()
	defer res.Release()
	countVar, err := oleutil.GetProperty(res, "Count")
	if err != nil {
		return nil, nil, err
	}
	count := int(countVar.Val)
	tempRank := 99
	for i := 0; i < count; i++ {
		itemRaw, err := oleutil.CallMethod(res, "ItemIndex", i)
		if err != nil {
			continue
		}
		item := itemRaw.ToIDispatch()
		prop := func(name string) *ole.VARIANT {
			v, err := oleutil.GetProperty(item, name)
			if err != nil {
				return nil
			}
			return v
		}
		name, kind, parent, value := prop("Name"), prop("SensorType"), prop("Parent"), prop("Value")
		if name != nil && kind != nil && parent != nil && value != nil && strings.Contains(strings.ToLower(parent.ToString()), "cpu") {
			v, ok := toFloat(value.Value())
			switch n := name.ToString(); {
			case !ok:
			case kind.ToString() == "Power" && (n == "CPU Package" || n == "Package"):
				power = &v
			case kind.ToString() == "Temperature":
				for rank, want := range []string{"CPU Package", "Core (Tctl/Tdie)", "Core (Tctl)", "Package"} {
					if n == want && rank < tempRank {
						tempRank, temp = rank, &v
					}
				}
			}
		}
		item.Release()
	}
	return power, temp, nil
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float32:
		return float64(x), true
	case float64:
		return x, true
	case int32:
		return float64(x), true
	}
	return 0, false
}

type cpuPower struct {
	reason string
}

func (c *cpuPower) read(now time.Time) *float64 {
	p, _ := readLHM(now)
	c.reason = ""
	if p == nil {
		c.reason = "LibreHardwareMonitor is not running"
	}
	return p
}

func (c *cpuPower) primed() bool { return false }

func readGPUs(ctx context.Context) []sensors.GPU {
	gpus, _ := sensors.NvidiaSMI(ctx)
	return gpus
}

var errNoTool = errors.New("not installed")

// No VRAM junction temperatures on Windows (say nothing).
func readVRAMTemps(context.Context) (map[string]float64, error) { return nil, errNoTool }

func cpuTempInput() string { return "lhm" }

func readCPUTemp(string) *float64 {
	_, t := readLHM(time.Now())
	return t
}

func readMemory() (sensors.Memory, bool) { return sensors.ReadMemory() }

type nvme struct {
	Model string
	Temp  *float64
}

func (n nvme) readTemp() *float64 { return nil }

func readStatic(context.Context) staticInfo {
	var s staticInfo
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE); err == nil {
		s.CPUModel, _, _ = k.GetStringValue("ProcessorNameString")
		s.CPUModel = strings.TrimSpace(s.CPUModel)
		k.Close()
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\BIOS`, registry.QUERY_VALUE); err == nil {
		s.Board, _, _ = k.GetStringValue("BaseBoardProduct")
		k.Close()
	}
	return s
}

func cpuPowerNote(string) string { return "CPU package power needs LibreHardwareMonitor on Windows" }
