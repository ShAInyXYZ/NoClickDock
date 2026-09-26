package disk

import (
	"fmt"
	"syscall"
	"testing"
)

func TestOnlyTransportErrorsMeanNotResponding(t *testing.T) {
	for err, want := range map[error]bool{
		syscall.EHOSTDOWN:                        true,
		fmt.Errorf("statfs: %w", syscall.ESTALE): true,
		syscall.ETIMEDOUT:                        true,
		syscall.EACCES:                           false,
		syscall.EPERM:                            false,
		fmt.Errorf("stat: %w", syscall.ENOENT):   false,
		nil:                                      false,
	} {
		if got := unreachable(err); got != want {
			t.Errorf("unreachable(%v) = %v, want %v", err, got, want)
		}
	}
}
