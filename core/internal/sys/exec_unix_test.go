//go:build !windows

package sys

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestATimeoutLeavesNoGrandchildBehind(t *testing.T) {
	start := time.Now()
	out, err := Run(context.Background(), 300*time.Millisecond, "sh", "-c", "sleep 30 & echo $!; wait")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Run took %s", elapsed)
	}
	pid, perr := strconv.Atoi(strings.TrimSpace(out))
	if perr != nil {
		t.Fatalf("no pid in %q", out)
	}
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the grandchild %d outlived the timeout", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRunReturnsStdoutAndStderr(t *testing.T) {
	out, err := Run(context.Background(), time.Second, "sh", "-c", "echo ok; echo broken >&2; exit 3")
	if out != "ok\n" || err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("out %q, err %v", out, err)
	}
}

func TestStopAllEndsWhatIsStillRunning(t *testing.T) {
	file := t.TempDir() + "/pid"
	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), time.Minute, "sh", "-c", "sleep 30 & echo $! > "+file+"; wait")
		done <- err
	}()
	var pid int
	deadline := time.Now().Add(3 * time.Second)
	for pid == 0 {
		if data, err := os.ReadFile(file); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if time.Now().After(deadline) {
			t.Fatal("the command never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	StopAll()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StopAll left the command running")
	}
	deadline = time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatal("its process group outlived it")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
