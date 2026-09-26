package comfyui

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const reconnectEvery = 3 * time.Second

// listener keeps a websocket open to the server, under a client id of its
// own — never another client's: ComfyUI drops the older socket with the
// same id, which would disconnect the user's browser.
//
// ComfyUI sends executing/progress events only to the socket whose client
// id queued the prompt; the browser always sends its own, so progress
// reaches us only for prompts queued without one (API scripts). Otherwise
// the dial's arc stays indeterminate. The broadcast "status" event (the
// queue's length) makes the widget poll at once.
type listener struct {
	id       string
	onStatus func() // called when the queue's length changes

	mu       sync.Mutex
	url      string
	cancel   context.CancelFunc
	progress *float64
	queued   int // the last status event's queue_remaining, -1 unknown
}

func newListener(onStatus func()) *listener {
	return &listener{id: clientID(), onStatus: onStatus, queued: -1}
}

// clientID is a random UUID v4, ours for the life of the process.
func clientID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func wsURL(host string, port int, id string) string {
	return "ws://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/ws?clientId=" + id
}

// follow (re)connects to an endpoint; the same one is left alone.
func (l *listener) follow(host string, port int) {
	url := wsURL(host, port, l.id)
	l.mu.Lock()
	defer l.mu.Unlock()
	if url == l.url {
		return
	}
	if l.cancel != nil {
		l.cancel()
	}
	l.url, l.progress, l.queued = url, nil, -1
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	go l.run(ctx, url)
}

func (l *listener) stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
		l.cancel, l.url = nil, ""
	}
}

// Progress is the running node's steps done, when an event said so.
func (l *listener) Progress() *float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.progress
}

// clear forgets a progress the queue says is over.
func (l *listener) clear() {
	l.mu.Lock()
	l.progress = nil
	l.mu.Unlock()
}

func (l *listener) run(ctx context.Context, url string) {
	for ctx.Err() == nil {
		l.session(ctx, url)
		l.mu.Lock()
		l.progress = nil
		l.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-time.After(reconnectEvery):
		}
	}
}

func (l *listener) session(ctx context.Context, url string) {
	dial, cancel := context.WithTimeout(ctx, fetchTimeout)
	conn, _, err := websocket.Dial(dial, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"User-Agent": []string{userAgent}},
	})
	cancel()
	if err != nil {
		return
	}
	defer conn.CloseNow()
	// Preview images come as binary frames; they are skipped, but must not
	// end the session.
	conn.SetReadLimit(32 << 20)
	for {
		typ, data, err := conn.Read(ctx) // answers pings meanwhile
		if err != nil {
			return
		}
		if typ == websocket.MessageText {
			l.handle(data)
		}
	}
}

type event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (l *listener) handle(frame []byte) {
	var ev event
	if json.Unmarshal(frame, &ev) != nil {
		return
	}
	switch ev.Type {
	case "status":
		var d struct {
			Status struct {
				ExecInfo struct {
					QueueRemaining *int `json:"queue_remaining"`
				} `json:"exec_info"`
			} `json:"status"`
		}
		if json.Unmarshal(ev.Data, &d) != nil || d.Status.ExecInfo.QueueRemaining == nil {
			return
		}
		n := *d.Status.ExecInfo.QueueRemaining
		l.mu.Lock()
		changed := n != l.queued
		l.queued = n
		l.mu.Unlock()
		if changed && l.onStatus != nil {
			l.onStatus()
		}
	case "progress":
		var d struct {
			Value, Max float64
		}
		if json.Unmarshal(ev.Data, &d) != nil || d.Max <= 0 {
			return
		}
		l.mu.Lock()
		if d.Value >= d.Max {
			l.progress = nil // the node is done; the next one may not report
		} else {
			p := max(0, d.Value/d.Max)
			l.progress = &p
		}
		l.mu.Unlock()
	case "executing":
		var d struct {
			Node *string `json:"node"`
		}
		if json.Unmarshal(ev.Data, &d) == nil && d.Node == nil {
			l.clear() // the prompt is finished
		}
	case "execution_success", "execution_error", "execution_interrupted":
		l.clear()
	}
}
