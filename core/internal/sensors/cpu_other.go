//go:build !linux && !windows

package sensors

func cpuTimes() (idle, total uint64, ok bool) { return 0, 0, false }

// ReadMemory is not implemented here.
func ReadMemory() (Memory, bool) { return Memory{}, false }
