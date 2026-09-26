package disk

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// Pseudo-filesystems carry no meaning for capacity planning.
var skipFSTypes = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "squashfs": true, "overlay": true, "proc": true,
	"sysfs": true, "cgroup": true, "cgroup2": true, "devpts": true, "efivarfs": true,
	"autofs": true, "fuse.gvfsd-fuse": true, "fuse.portal": true, "ramfs": true,
	"mqueue": true, "hugetlbfs": true, "debugfs": true, "tracefs": true,
	"securityfs": true, "pstore": true, "bpf": true, "configfs": true,
	"fusectl": true, "binfmt_misc": true, "nsfs": true,
}

// Network shares and ZFS datasets are not backed by a /dev node, yet they
// fill up like any disk (ARCHITECTURE.md, decision Q6).
var networkFSTypes = map[string]bool{"cifs": true, "smb3": true, "nfs": true, "nfs4": true}

var skipTrees = []string{"/snap", "/var/snap", "/run", "/sys", "/proc", "/dev"}

// The zfs storage driver mounts one dataset per image layer under the
// runtime's graph directory: those are Docker's, not the user's.
var graphTrees = []string{"/var/lib/docker", "/var/lib/containers"}

type mount struct {
	device string
	point  string
	fstype string
	kind   Kind
}

// parseMounts reads /proc/mounts and keeps what can run out of space: local
// filesystems backed by a device, ZFS datasets, and network shares — never
// pseudo-filesystems or the system's own trees.
func parseMounts(r io.Reader) []mount {
	var out []mount
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 {
			continue
		}
		m := mount{device: unescape(f[0]), point: unescape(f[1]), fstype: f[2]}
		switch {
		case skipFSTypes[m.fstype] || underTree(m.point, skipTrees):
			continue
		case networkFSTypes[m.fstype]:
			m.kind = Network
		case m.fstype == "zfs":
			if underTree(m.point, graphTrees) {
				continue
			}
			m.kind = Pool
		case strings.HasPrefix(m.device, "/"):
			m.kind = Local
		default:
			continue
		}
		out = append(out, m)
	}
	return out
}

// underTree matches whole path components: /run/media is under /run,
// /snapshots is not under /snap.
func underTree(point string, trees []string) bool {
	for _, t := range trees {
		if point == t || strings.HasPrefix(point, t+"/") {
			return true
		}
	}
	return false
}

// unescape undoes the kernel's octal escapes: a mount point with a space in
// it appears in /proc/mounts as "\040".
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
