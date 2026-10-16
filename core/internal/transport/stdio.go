// Package transport carries the hub's messages to the interface: JSON lines
// over stdio in production, HTTP and server-sent events in development.
package transport

import (
	"bufio"
	"context"
	"io"
	"log"
	"sync"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
)

// maxLine bounds one UI → core message. Requests are a few hundred bytes;
// anything larger is a bug on the other side, not data.
const maxLine = 1 << 20

// Stdio serves the Tauri shell: requests arrive as lines on in, messages
// leave as lines on out. It returns when in reaches EOF — the shell closed
// the pipe or exited — or ctx ends.
func Stdio(ctx context.Context, h *hub.Hub, in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		writeLines(ctx, h, out)
	}()

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64<<10), maxLine)
	lines := make(chan []byte)
	go func() {
		defer close(lines)
		for scanner.Scan() {
			lines <- append([]byte(nil), scanner.Bytes()...)
		}
	}()

	var err error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case line, ok := <-lines:
			if !ok {
				err = scanner.Err()
				break loop
			}
			if len(line) == 0 {
				continue
			}
			if herr := h.Handle(ctx, line); herr != nil {
				log.Printf("stdin: %v", herr)
			}
		}
	}
	cancel()
	wg.Wait()
	return err
}

// writeLines sends the full state, then every message. If the hub drops this
// subscriber for falling behind, it starts over from the full state rather
// than leaving the interface silently stale.
func writeLines(ctx context.Context, h *hub.Hub, out io.Writer) {
	w := bufio.NewWriter(out)
	for ctx.Err() == nil {
		state, sub := h.Subscribe()
		for _, msg := range state {
			writeLine(w, msg)
		}
		if w.Flush() != nil {
			h.Unsubscribe(sub)
			return
		}
		for alive := true; alive; {
			select {
			case <-ctx.Done():
				h.Unsubscribe(sub)
				return
			case msg, ok := <-sub.C:
				if !ok {
					log.Printf("stdout fell behind; resending the full state")
					alive = false
					break
				}
				writeLine(w, msg)
				// Flush when nothing else is queued, so a burst goes out
				// together and a single message is never held back.
				if len(sub.C) == 0 && w.Flush() != nil {
					h.Unsubscribe(sub)
					return
				}
			}
		}
	}
}

func writeLine(w *bufio.Writer, msg []byte) {
	w.Write(msg)
	w.WriteByte('\n')
}
