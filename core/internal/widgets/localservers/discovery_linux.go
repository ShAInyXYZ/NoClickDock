//go:build linux

package localservers

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type socket struct {
	inode, address string
	port           int
}

// Kernel tables only: never connect to a port or change a server during discovery.
func sockets() ([]socket, error) {
	var out []socket
	for _, name := range []string{"tcp", "tcp6"} {
		b, err := os.ReadFile("/proc/net/" + name)
		if err != nil {
			if name == "tcp6" && os.IsNotExist(err) {
				continue
			}
			return nil, errors.New("Cannot read Linux listening sockets")
		}
		out = append(out, parseSockets(string(b))...)
	}
	return out, nil
}

func parseSockets(text string) []socket {
	var out []socket
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || f[3] != "0A" {
			continue
		}
		addr, port, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		b, err := hex.DecodeString(addr)
		if err != nil || (len(b) != 4 && len(b) != 16) {
			continue
		}
		// /proc writes each native 32-bit word in host byte order (Linux x86/arm LE).
		for i := 0; i < len(b); i += 4 {
			b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
		}
		ip := net.IP(b)
		if !ip.IsLoopback() && !ip.IsUnspecified() {
			continue
		}
		p, err := strconv.ParseInt(port, 16, 32)
		if err != nil || p == 0 {
			continue
		}
		host := ip.String()
		if ip.IsUnspecified() {
			if len(b) == 4 {
				host = "127.0.0.1"
			} else {
				host = "::1"
			}
		}
		out = append(out, socket{f[9], host, int(p)})
	}
	return out
}

func readProcess(pid int) (process, error) {
	p := process{PID: pid}
	dir := fmt.Sprintf("/proc/%d", pid)
	b, err := os.ReadFile(dir + "/stat")
	if err != nil {
		return p, err
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return p, errors.New("Invalid process identity")
	}
	f := strings.Fields(string(b[end+1:]))
	if len(f) < 20 {
		return p, errors.New("Invalid process identity")
	}
	p.Start = f[19]
	p.PPID, _ = strconv.Atoi(f[1])
	p.State = f[0]
	var u unix.Stat_t
	if err = unix.Stat(dir, &u); err != nil {
		return p, err
	}
	p.UID = int(u.Uid)
	p.Exe, err = os.Readlink(dir + "/exe")
	if err != nil {
		return p, err
	}
	p.Cwd, err = os.Readlink(dir + "/cwd")
	if err != nil {
		return p, err
	}
	b, err = os.ReadFile(dir + "/cmdline")
	if err != nil {
		return p, err
	}
	p.Args = strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	b, _ = os.ReadFile(dir + "/cgroup")
	p.Cgroup = string(b)
	return p, nil
}

func identity(p process) string { return fmt.Sprintf("%d:%s", p.PID, p.Start) }
func sameProcess(a, b process) bool {
	return a.PID == b.PID && a.Start == b.Start && a.UID == b.UID && a.Exe == b.Exe && a.Cwd == b.Cwd && a.Cgroup == b.Cgroup && slices.Equal(a.Args, b.Args)
}

func processSockets(pid int, byInode map[string]socket) []socket {
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		return nil
	}
	var out []socket
	seen := map[string]bool{}
	for _, e := range entries {
		target, _ := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, e.Name()))
		if !strings.HasPrefix(target, "socket:[") {
			continue
		}
		inode := strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")
		if s, ok := byInode[inode]; ok && !seen[inode] {
			out = append(out, s)
			seen[inode] = true
		}
	}
	return out
}

func discover(ctx context.Context) ([]server, error) {
	ss, err := sockets()
	if err != nil {
		return nil, err
	}
	byInode := map[string]socket{}
	for _, s := range ss {
		byInode[s.inode] = s
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, errors.New("Process discovery is unavailable")
	}
	var out []server
	owned := map[string]bool{}
	ancestors := map[int]bool{}
	for pid := os.Getpid(); pid > 1; {
		if ancestors[pid] {
			break
		}
		ancestors[pid] = true
		p, e := readProcess(pid)
		if e != nil {
			break
		}
		pid = p.PPID
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		matches := processSockets(pid, byInode)
		if len(matches) == 0 {
			continue
		}
		p, err := readProcess(pid)
		if err != nil {
			continue
		}
		s := describe(p)
		s.Key = identity(p)
		s.Running = true
		s.Proc = p
		if p.UID != os.Getuid() {
			s.Protected = "Owned by another user"
			s.Dev = false
		}
		if ancestors[pid] {
			s.Protected = "NCD or its parent process"
			s.Dev = false
		}
		seenPorts := map[int]bool{}
		sort.Slice(matches, func(i, j int) bool { return matches[i].address < matches[j].address })
		for _, m := range matches {
			owned[m.inode] = true
			if !seenPorts[m.port] {
				s.Endpoints = append(s.Endpoints, endpoint{Port: m.port, Host: m.address, Inode: m.inode})
				seenPorts[m.port] = true
			}
		}
		sort.Slice(s.Endpoints, func(i, j int) bool { return s.Endpoints[i].Port < s.Endpoints[j].Port })
		// Re-read after fd enumeration so a PID reused while scanning is never attached to stale sockets.
		fresh, err := readProcess(pid)
		if err != nil || !sameProcess(p, fresh) {
			continue
		}
		current := processSockets(pid, byInode)
		valid := false
		for _, a := range current {
			for _, b := range s.Endpoints {
				valid = valid || a.inode == b.Inode
			}
		}
		if !valid {
			continue
		}
		out = append(out, s)
	}
	for _, m := range ss {
		if !owned[m.inode] {
			out = append(out, server{Key: "unknown:" + m.inode, Name: "Unknown owner", Runtime: "TCP", Running: true, Protected: "Process details are not accessible", Endpoints: []endpoint{{m.port, m.address, m.inode}}})
		}
	}
	// A socket shared by multiple processes is deliberately read-only.
	counts := map[string]int{}
	for _, s := range out {
		for _, e := range s.Endpoints {
			counts[e.Inode]++
		}
	}
	for i := range out {
		for _, e := range out[i].Endpoints {
			if counts[e.Inode] > 1 {
				out[i].Protected = "Listener is shared by multiple processes"
			}
		}
	}
	return out, nil
}

// pidfd binds the signal to a kernel process object, closing the PID-reuse gap.
// There is intentionally no kill(pid) fallback on older kernels.
func stopProcess(ctx context.Context, s server) error {
	fd, err := unix.PidfdOpen(s.Proc.PID, 0)
	if err != nil {
		return errors.New("Process exited, or this kernel does not support safe process controls")
	}
	defer unix.Close(fd)
	if err = verify(s, true); err != nil {
		return err
	}
	if err = unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0); err != nil {
		return errors.New("Could not send a graceful stop to this process")
	}
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		p, e := readProcess(s.Proc.PID)
		if e != nil || !sameProcess(p, s.Proc) || p.State == "Z" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Still running after SIGTERM; no force kill was sent")
		case <-tick.C:
		}
	}
}

func verify(s server, listener bool) error {
	p, err := readProcess(s.Proc.PID)
	if err != nil || !sameProcess(p, s.Proc) {
		return errors.New("Process exited or changed; refresh before acting")
	}
	fresh := describe(p)
	if p.UID != os.Getuid() || s.Protected != "" || fresh.Protected != "" || !fresh.Dev {
		return errors.New("This process is protected from server controls")
	}
	if p.PID == os.Getpid() {
		return errors.New("NCD cannot control itself")
	}
	if listener {
		ss, err := sockets()
		if err != nil {
			return err
		}
		byInode := map[string]socket{}
		for _, a := range ss {
			byInode[a.inode] = a
		}
		own := processSockets(p.PID, byInode)
		for _, expected := range s.Endpoints {
			found := false
			for _, actual := range own {
				found = found || actual.inode == expected.Inode && actual.port == expected.Port
			}
			if !found {
				return errors.New("Listener changed or port was reused; refresh before acting")
			}
		}
	}
	return nil
}

func unitOf(p process) string {
	for _, line := range strings.Split(p.Cgroup, "\n") {
		_, path, ok := strings.Cut(line, "::")
		if !ok || !strings.Contains(path, fmt.Sprintf("/user-%d.slice/user@%d.service/", p.UID, p.UID)) {
			continue
		}
		base := filepath.Base(path)
		if strings.HasSuffix(base, ".service") && validUnit.MatchString(base) {
			return base
		}
	}
	return ""
}

func supervised(p process) bool {
	// System services and unknown service supervisors must not be treated as
	// unmanaged terminal launches merely because only user systemd is supported.
	for _, line := range strings.Split(p.Cgroup, "\n") {
		for _, part := range strings.Split(line, "/") {
			if strings.HasSuffix(part, ".service") && !strings.HasPrefix(part, "user@") {
				return true
			}
		}
	}

	for depth := 0; depth < 8 && p.PPID > 1; depth++ {
		parent, err := readProcess(p.PPID)
		if err != nil {
			return false
		}
		name := strings.ToLower(strings.Join(parent.Args, " "))
		for _, manager := range []string{"pm2", "supervisord", "forever", "nodemon"} {
			if strings.Contains(name, manager) {
				return true
			}
		}
		p = parent
	}
	return false
}
