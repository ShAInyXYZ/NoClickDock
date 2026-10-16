package sys

import "syscall"

// dieWithParent: the kernel kills the child if the core dies first, even by
// SIGKILL, when no exit path of ours runs. (It follows the thread that
// started the child; Go's threads outlive the goroutines that use them.)
func dieWithParent(attr *syscall.SysProcAttr) { attr.Pdeathsig = syscall.SIGKILL }
