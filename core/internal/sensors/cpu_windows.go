package sensors

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
)

// cpuTimes: GetSystemTimes' kernel time already includes idle time.
func cpuTimes() (idle, total uint64, ok bool) {
	var idleFT, kernelFT, userFT syscall.Filetime
	r, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleFT)), uintptr(unsafe.Pointer(&kernelFT)), uintptr(unsafe.Pointer(&userFT)))
	if r == 0 {
		return 0, 0, false
	}
	ticks := func(ft syscall.Filetime) uint64 { return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime) }
	return ticks(idleFT), ticks(kernelFT) + ticks(userFT), true
}

type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

// ReadMemory is GlobalMemoryStatusEx's view of the RAM in use.
func ReadMemory() (Memory, bool) {
	m := memoryStatusEx{length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 || m.totalPhys == 0 || m.availPhys > m.totalPhys {
		return Memory{}, false
	}
	return Memory{UsedKB: (m.totalPhys - m.availPhys) / 1024, TotalKB: m.totalPhys / 1024}, true
}
