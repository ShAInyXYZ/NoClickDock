//go:build !windows

package sys

import (
	"os/exec"
	"syscall"
)

func hideConsole(*exec.Cmd) {}

// detach starts a launched program in its own session, so quitting the dock
// (or a signal to its process group) does not take the user's window with it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
