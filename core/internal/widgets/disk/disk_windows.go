package disk

import (
	"errors"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Windows has no /proc/mounts: the filesystems are the drive letters, and
// only fixed and network drives count — an empty card reader is not a disk
// running out of space.
var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	procGetLogicalDrives  = kernel32.NewProc("GetLogicalDrives")
	procGetDriveTypeW     = kernel32.NewProc("GetDriveTypeW")
	procGetVolumeInfoW    = kernel32.NewProc("GetVolumeInformationW")
	procGetDiskFreeSpaceW = kernel32.NewProc("GetDiskFreeSpaceExW")
)

const (
	driveFixed  = 3
	driveRemote = 4
	// A local drive that takes longer than this is dropped, as the Python
	// widget did; a mapped drive gets the network timeout and says so.
	driveTimeout = 3 * time.Second
)

type volume struct {
	name, fstype      string
	total, used, free uint64
}

func (w *Widget) filesystems() ([]Filesystem, error) {
	mask, _, err := procGetLogicalDrives.Call()
	if mask == 0 {
		return nil, errors.Join(errors.New("GetLogicalDrives failed"), err)
	}
	var out []Filesystem
	for i := 0; i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		path, err := syscall.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		kind, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(path)))
		if kind != driveFixed && kind != driveRemote {
			continue
		}
		fs := Filesystem{Mount: root, Label: root[:2], Kind: Local}
		if kind == driveRemote {
			fs.Kind, fs.FSType = Network, "network"
		}
		timeout := driveTimeout
		if fs.Kind == Network {
			timeout = networkTimeout
		}
		v, err, answered := probe(&w.probe, root, timeout, func() (volume, error) { return query(path) })
		if !answered {
			// A share that does not answer is worth saying; a local drive
			// that does not answer is dropped, as the Python widget did.
			if fs.Kind == Network {
				fs.Unresponsive = true
				out = append(out, fs)
			}
			continue
		}
		if err != nil {
			continue
		}
		if v.name != "" {
			fs.Label += " " + v.name
		}
		if v.fstype != "" {
			fs.FSType = v.fstype
		} else if fs.FSType == "" {
			fs.FSType = "disk"
		}
		fs.Total, fs.Used, fs.Free = v.total, v.used, v.free
		out = append(out, fs)
	}
	sortFilesystems(out)
	return out, nil
}

// query reads a volume's label, file system and space. Used is total minus
// the volume's free bytes, as Python's shutil.disk_usage computes it.
func query(path *uint16) (volume, error) {
	var name, fsName [256]uint16
	procGetVolumeInfoW.Call(uintptr(unsafe.Pointer(path)),
		uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)), 0, 0, 0,
		uintptr(unsafe.Pointer(&fsName[0])), uintptr(len(fsName)))
	var callerFree, total, totalFree uint64
	ok, _, err := procGetDiskFreeSpaceW.Call(uintptr(unsafe.Pointer(path)),
		uintptr(unsafe.Pointer(&callerFree)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&totalFree)))
	if ok == 0 {
		return volume{}, err
	}
	return volume{
		name:   syscall.UTF16ToString(name[:]),
		fstype: strings.ToLower(syscall.UTF16ToString(fsName[:])),
		total:  total,
		used:   total - totalFree,
		free:   totalFree,
	}, nil
}
