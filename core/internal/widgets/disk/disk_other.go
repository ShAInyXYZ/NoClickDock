//go:build !linux && !windows

package disk

import (
	"fmt"
	"runtime"
)

func (w *Widget) filesystems() ([]Filesystem, error) {
	return nil, fmt.Errorf("measuring filesystems is not supported on %s", runtime.GOOS)
}
