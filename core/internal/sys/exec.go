// Package sys is the core's only way to touch the machine outside Go's own
// APIs: running the CLIs widgets watch, opening programs, and the formatting
// every widget shares.
package sys

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	stdout, _, err := Output(ctx, timeout, name, args...)
	return stdout, err
}

// Output is Run keeping stderr apart: `docker logs` replays a container's
// stderr on its own, and a failed command's last word is often there.
func Output(ctx context.Context, timeout time.Duration, name string, args ...string) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	prepare(cmd)
	// A grandchild holding the pipes open must not outlive the timeout.
	cmd.WaitDelay = time.Second
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return out.String(), errOut.String(), fmt.Errorf("%s timed out after %s", filepath.Base(name), timeout)
		}
		if msg := firstLine(errOut.String()); msg != "" {
			return out.String(), errOut.String(), fmt.Errorf("%s: %s", filepath.Base(name), msg)
		}
		return out.String(), errOut.String(), fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	return out.String(), errOut.String(), nil
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

// Find locates a program: on PATH first, then at the given absolute paths —
// Windows installers do not always put themselves on the PATH a GUI-launched
// process inherits. "" when it is nowhere.
func Find(name string, fallbacks ...string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, p := range fallbacks {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
