//go:build !windows

package sys

import (
	"os/exec"
	"syscall"
)

// prepare puts the command in a process group of its own and makes a
// timeout kill the whole group: `docker compose` and friends fork helpers
// that would otherwise outlive the deadline.
func prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	dieWithParent(cmd.SysProcAttr)
	cmd.Cancel = func() error { return stop(cmd) }
}

// stop kills the command's whole process group.
func stop(cmd *exec.Cmd) error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }

// detach starts a launched program in its own session, so quitting the dock
// (or a signal to its process group) does not take the user's window with it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
