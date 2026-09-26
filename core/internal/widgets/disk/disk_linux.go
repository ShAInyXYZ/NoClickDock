package disk

import (
	"os"
	"syscall"
)

type usage struct {
	dev               uint64
	total, used, free uint64
}

// filesystems measures every mounted filesystem worth watching, once each:
// a bind mount or btrfs subvolume shares its device with one already listed
// and is skipped. Network shares are measured against a deadline, and one
// that does not answer is reported as such rather than stalling the poll.
func (w *Widget) filesystems() ([]Filesystem, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seen := map[uint64]bool{}
	var out []Filesystem
	for _, m := range parseMounts(f) {
		fs := Filesystem{Mount: m.point, Label: m.point, Device: m.device, FSType: m.fstype, Kind: m.kind}
		if m.kind == Pool {
			fs.Label = m.device // the dataset: tank/media
		}
		var u usage
		if m.kind == Network {
			var answered bool
			u, err, answered = probe(&w.probe, m.point, networkTimeout, func() (usage, error) { return measure(m.point) })
			if !answered {
				fs.Unresponsive = true
				out = append(out, fs)
				continue
			}
		} else {
			u, err = measure(m.point)
		}
		if err != nil || seen[u.dev] {
			continue
		}
		seen[u.dev] = true
		fs.Total, fs.Used, fs.Free = u.total, u.used, u.free
		out = append(out, fs)
	}
	sortFilesystems(out)
	return out, nil
}

// measure uses the same arithmetic as Python's shutil.disk_usage: free is
// what an ordinary user can still write, used includes the root reserve.
func measure(point string) (usage, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(point, &st); err != nil {
		return usage{}, err
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(point, &fs); err != nil {
		return usage{}, err
	}
	block := uint64(fs.Frsize)
	if block == 0 {
		block = uint64(fs.Bsize)
	}
	return usage{
		dev:   st.Dev,
		total: fs.Blocks * block,
		used:  (fs.Blocks - fs.Bfree) * block,
		free:  fs.Bavail * block,
	}, nil
}
