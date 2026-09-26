// Package sys is the core's only way to touch the machine outside Go's own
// APIs: running the CLIs widgets watch, opening programs, and the formatting
// every widget shares.
package sys

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Run executes a command and returns its stdout. Every command the core runs
// goes through here: widgets shell out to CLIs they do not control, so a hang
// must end in a timeout rather than a frozen widget, and on Windows no
// console window may flash.
//
// A non-zero exit returns what the command printed together with the error,
// because some tools report useful output and fail anyway.
func Run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	prepare(cmd)
	// A grandchild holding the pipes open must not outlive the timeout.
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return stdout.String(), fmt.Errorf("%s timed out after %s", name, timeout)
		}
		if msg := firstLine(stderr.String()); msg != "" {
			return stdout.String(), fmt.Errorf("%s: %s", name, msg)
		}
		return stdout.String(), fmt.Errorf("%s: %w", name, err)
	}
	return stdout.String(), nil
}

// Start launches a program for the user (an "Open …" menu item) and does not
// wait for it. It is detached so it outlives the core.
func Start(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap it; the exit status belongs to the user
	return nil
}

// Has reports whether a program is on PATH.
func Has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
