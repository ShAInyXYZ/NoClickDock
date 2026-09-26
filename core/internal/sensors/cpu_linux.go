package sensors

import "os"

func cpuTimes() (idle, total uint64, ok bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	return ParseProcStat(string(data))
}

// ReadMemory is /proc/meminfo's view of the RAM in use.
func ReadMemory() (Memory, bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return Memory{}, false
	}
	return ParseMeminfo(string(data))
}
