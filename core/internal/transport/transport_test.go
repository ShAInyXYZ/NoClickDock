package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/config"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

type still struct{}

func (still) Meta() hub.Meta { return hub.Meta{ID: "disk", Title: "Disk", PollSecs: 3600} }
func (still) Mock(time.Time) model.Snapshot {
	return model.Snapshot{State: model.StateOK, StateLabel: "412G free"}
}

// running starts a hub with one mocked widget and waits for its first poll.
func running(t *testing.T) (*hub.Hub, context.Context) {
	t.Helper()
	cfg, _ := config.Open(t.TempDir())
	h := hub.New("linux", cfg, []hub.Collector{hub.Mocked(still{}, false)})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for len(h.State()) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the widget never reported")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return h, ctx
}

const addr = "127.0.0.1:47810"

func request(t *testing.T, handler http.Handler, method, path, origin, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://"+addr+path, strings.NewReader(body))
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestStateIsHelloThenSnapshots(t *testing.T) {
	h, ctx := running(t)
	w := request(t, Handler(ctx, h, addr), "GET", "/v1/state", "", "")
	var msgs []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &msgs); err != nil {
		t.Fatalf("%v: %s", err, w.Body)
	}
	if len(msgs) != 2 || msgs[0]["type"] != "hello" || msgs[1]["type"] != "snapshot" {
		t.Fatalf("state = %s", w.Body)
	}
}

func TestOnlyDevOriginsAndLocalHostsAreServed(t *testing.T) {
	h, ctx := running(t)
	handler := Handler(ctx, h, addr)

	if w := request(t, handler, "GET", "/v1/state", "http://localhost:1430", ""); w.Code != 200 ||
		w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:1430" {
		t.Fatalf("design origin refused: %d %v", w.Code, w.Header())
	}
	for _, origin := range []string{"https://example.com", "tauri://localhost", "http://localhost:5173", "null"} {
		if w := request(t, handler, "GET", "/v1/state", origin, ""); w.Code != http.StatusForbidden {
			t.Errorf("origin %s got %d", origin, w.Code)
		}
	}

	rebound := httptest.NewRequest("GET", "http://evil.example:47810/v1/state", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, rebound)
	if w.Code != http.StatusForbidden {
		t.Fatalf("a foreign Host header got %d", w.Code)
	}
}

func TestSendNeedsJSON(t *testing.T) {
	h, ctx := running(t)
	handler := Handler(ctx, h, addr)

	form := httptest.NewRequest("POST", "http://"+addr+"/v1/send", strings.NewReader(`{"type":"refresh"}`))
	form.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, form)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("a text/plain POST got %d", w.Code)
	}
	if w := request(t, handler, "POST", "/v1/send", "http://localhost:1420", `{"type":"refresh"}`); w.Code != http.StatusAccepted {
		t.Fatalf("refresh got %d: %s", w.Code, w.Body)
	}
	if w := request(t, handler, "POST", "/v1/send", "", `{"type":"launch"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("an unknown message got %d", w.Code)
	}
}

func TestEventsStartWithTheFullState(t *testing.T) {
	h, ctx := running(t)
	srv := httptest.NewServer(Handler(ctx, h, "127.0.0.1:0"))
	defer srv.Close()
	// httptest listens on a random port; the guard knows it as that port.
	handler := Handler(ctx, h, strings.TrimPrefix(srv.URL, "http://"))
	srv.Config.Handler = handler

	resp, err := http.Get(srv.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	r := bufio.NewReader(resp.Body)
	var events []string
	for len(events) < 2 {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if name, ok := strings.CutPrefix(line, "event: "); ok {
			events = append(events, strings.TrimSpace(name))
		}
	}
	if events[0] != "hello" || events[1] != "snapshot" {
		t.Fatalf("events = %v", events)
	}
}

func TestStdioWritesStateAndReadsRequests(t *testing.T) {
	h, ctx := running(t)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- Stdio(ctx, h, inR, outW) }()

	out := bufio.NewScanner(outR)
	var first []string
	for len(first) < 2 && out.Scan() {
		var m map[string]any
		json.Unmarshal(out.Bytes(), &m)
		first = append(first, m["type"].(string))
	}
	if first[0] != "hello" || first[1] != "snapshot" {
		t.Fatalf("stdout began with %v", first)
	}

	io.WriteString(inW, `{"type":"config.patch","patch":{"dock":{"density":"compact"}}}`+"\n")
	for out.Scan() {
		var m map[string]any
		json.Unmarshal(out.Bytes(), &m)
		if m["type"] == "config" {
			break
		}
	}

	// The shell closing the pipe ends the core.
	inW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stdin EOF did not end the transport")
	}
}
