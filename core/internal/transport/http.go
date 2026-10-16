package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
)

// DevOrigins are the Vite dev servers allowed to read the development API:
// the executor's, the design reviewer's and the research reviewer's. Vite
// binds 127.0.0.1, so each is allowed under both of its names.
var DevOrigins = []string{
	"http://localhost:1420", "http://127.0.0.1:1420",
	"http://localhost:1430", "http://127.0.0.1:1430",
	"http://localhost:1440", "http://127.0.0.1:1440",
}

const keepAlive = 15 * time.Second

// HTTP serves the development API on 127.0.0.1:port until ctx ends:
//
//	GET  /v1/state     hello and every snapshot, as a JSON array of messages
//	GET  /v1/events    the same messages as server-sent events, then live ones
//	GET  /v1/variants  the mocks' alternative panels, for the gallery
//	POST /v1/send      one UI → core message
//
// It exists only under --dev and --mock. Production has no listening port,
// so no web page and no local process can reach the widgets' actions.
func HTTP(ctx context.Context, h *hub.Hub, port int) error {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	srv := &http.Server{
		Addr:              addr,
		Handler:           Handler(ctx, h, addr),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Printf("dev API on http://%s (origins: Vite on 1420, 1430, 1440)", addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Handler is the development API behind its guards. addr is the host:port
// the server answers to.
func Handler(ctx context.Context, h *hub.Hub, addr string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonArray(h.State()))
	})
	mux.HandleFunc("GET /v1/variants", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(h.Variants())
	})
	mux.HandleFunc("GET /v1/events", func(w http.ResponseWriter, r *http.Request) {
		events(ctx, h, w, r)
	})
	mux.HandleFunc("POST /v1/send", func(w http.ResponseWriter, r *http.Request) {
		// Requiring JSON makes every cross-origin POST a preflighted one, so
		// a page on another origin cannot fire a plain form at the actions.
		if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
			http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxLine))
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		if err := h.Handle(ctx, body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	return guard(mux, addr)
}

// guard admits only requests addressed to this server by name (a DNS
// rebinding page carries its own host) and, when a browser says where it
// comes from, only from the Vite dev origins.
func guard(next http.Handler, addr string) http.Handler {
	_, port, _ := net.SplitHostPort(addr)
	hosts := map[string]bool{"127.0.0.1:" + port: true, "localhost:" + port: true}
	origins := map[string]bool{}
	for _, o := range DevOrigins {
		origins[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[r.Host] {
			http.Error(w, "unexpected Host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if !origins[origin] {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// events streams every message as an SSE event named after its type. A
// client that reconnects gets the full state again, so EventSource's own
// retry is all the recovery the interface needs.
func events(ctx context.Context, h *hub.Hub, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")

	state, sub := h.Subscribe()
	defer h.Unsubscribe(sub)
	for _, msg := range state {
		writeEvent(w, msg)
	}
	flusher.Flush()

	tick := time.NewTicker(keepAlive)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.Context().Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case msg, ok := <-sub.C:
			if !ok {
				return // fell behind: the client reconnects and starts over
			}
			writeEvent(w, msg)
			flusher.Flush()
		}
	}
}

func writeEvent(w io.Writer, msg []byte) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", messageType(msg), msg)
}

// messageType reads "type" without decoding the whole message: every
// message the hub builds starts with it.
func messageType(msg []byte) string {
	const prefix = `{"type":"`
	if len(msg) > len(prefix) && string(msg[:len(prefix)]) == prefix {
		rest := msg[len(prefix):]
		for i, c := range rest {
			if c == '"' {
				return string(rest[:i])
			}
		}
	}
	return "message"
}

func jsonArray(msgs [][]byte) []byte {
	out := []byte{'['}
	for i, m := range msgs {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, m...)
	}
	return append(out, ']')
}
