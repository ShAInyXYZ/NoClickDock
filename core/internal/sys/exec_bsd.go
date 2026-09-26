//go:build !windows && !linux

package sys

import "syscall"

func dieWithParent(*syscall.SysProcAttr) {}
