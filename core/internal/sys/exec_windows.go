package sys

import (
	"os/exec"
	"syscall"
)

// createNoWindow keeps a console program from opening a console window when
// the core itself has none (it is linked with -H=windowsgui).
const createNoWindow = 0x08000000

// prepare hides the console. A timeout kills only the direct child here; a
// Job object would take its children too, if M7 finds any left behind.
func prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// stop kills the direct child (a Job object would take its children too,
// if M7 finds any left behind).
func stop(cmd *exec.Cmd) error { return cmd.Process.Kill() }

// detach leaves a launched GUI program alone: it has its own window, and it
// is not a child of the dock's lifetime.
func detach(cmd *exec.Cmd) {
	const detachedProcess = 0x00000008
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess}
}
