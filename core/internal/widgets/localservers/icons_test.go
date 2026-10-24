package localservers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func tinyIcon(t *testing.T) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 16, 16))
	im.Set(0, 0, color.RGBA{R: 200, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func iconServer(raw string) server {
	u, _ := url.Parse(raw)
	port, _ := strconv.Atoi(u.Port())
	return server{Key: "123:1", Name: "sample-project", Dir: "/projects/sample", Running: true, Dev: true, Endpoints: []endpoint{{Port: port, Host: u.Hostname()}}}
}
func TestFaviconLinkFallbackAndLocalBoundary(t *testing.T) {
	png := tinyIcon(t)
	var mode atomic.Int32
	var remoteHits, linkedHits, fallbackHits atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { remoteHits.Add(1); w.Write(png) }))
	defer remote.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credentials sent")
		}
		switch r.URL.Path {
		case "/":
			switch mode.Load() {
			case 0:
				w.Write([]byte(`<!-- <link rel="icon" href="/comment"> --><script>const x='<link rel="icon" href="/script">'</script><BASE href="/assets/"><LINK HREF='brand.png?v=1&amp;n=2' REL='shortcut ICON'>`))
			case 1:
				w.Write([]byte(`<link rel="icon" href="` + remote.URL + `/remote">`))
			case 2:
				w.Write([]byte(`<link rel="icon" href="/redirect">`))
			case 3:
				w.Write([]byte(`<link rel="icon" href="/bad">`))
			}
		case "/assets/brand.png":
			linkedHits.Add(1)
			if r.URL.Query().Get("n") != "2" {
				t.Error("HTML entity not decoded")
			}
			w.Write(png)
		case "/redirect":
			http.Redirect(w, r, remote.URL+"/redirected", http.StatusFound)
		case "/bad":
			w.Write([]byte(`<html>not an image</html>`))
		case "/favicon.ico":
			fallbackHits.Add(1)
			w.Write(png)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	verify := func(server) error { return nil }
	for i := 0; i < 4; i++ {
		mode.Store(int32(i))
		if data := fetchIcon(context.Background(), iconServer(srv.URL), verify); !strings.HasPrefix(data, "data:image/png;base64,") {
			t.Fatalf("mode %d: no favicon", i)
		}
	}
	if remoteHits.Load() != 0 || linkedHits.Load() != 1 || fallbackHits.Load() != 3 {
		t.Fatalf("requests remote=%d linked=%d fallback=%d", remoteHits.Load(), linkedHits.Load(), fallbackHits.Load())
	}
}
func TestIconReadsRejectReusedProcessAndOversizedResponses(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write(bytes.Repeat([]byte("x"), pageBytes+1))
	}))
	defer srv.Close()
	s := iconServer(srv.URL)
	if got := fetchIcon(context.Background(), s, func(server) error { return errors.New("identity changed") }); got != "" || hits.Load() != 0 {
		t.Fatal("read from reused process")
	}
	if got := fetchIcon(context.Background(), s, func(server) error { return nil }); got != "" {
		t.Fatal("oversized data accepted")
	}
	png := tinyIcon(t)
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(`<link rel="icon" href="/i.png">`))
		} else {
			w.Write(png)
		}
	}))
	defer imageServer.Close()
	var checks atomic.Int32
	got := fetchIcon(context.Background(), iconServer(imageServer.URL), func(server) error {
		if checks.Add(1) > 2 {
			return errors.New("reused after download")
		}
		return nil
	})
	if got != "" {
		t.Fatal("identity changed during download but icon was accepted")
	}
}
func TestIconFormatsAndSVGIsolation(t *testing.T) {
	for _, bad := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		`<svg><foreignObject><div>html</div></foreignObject></svg>`,
		`<svg><image href="http://outside.test/leak"/></svg>`,
		`<svg><path fill="url(http://outside.test/leak)"/></svg>`,
		`<!DOCTYPE svg SYSTEM "file:///etc/passwd"><svg/>`,
		`<svg/><svg/>`,
	} {
		if iconData([]byte(bad)) != "" {
			t.Fatalf("unsafe SVG accepted: %s", bad)
		}
	}
	data := iconData([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" onload="bad()"><style>bad</style><path d="M0 0h24v24z"/></svg>`))
	if data != "" {
		t.Fatal("style element accepted")
	}
	data = iconData([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" onload="bad()"><path style="fill:url(http://outside.test)" fill="#ff0000" d="M0 0h24v24z"/></svg>`))
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(data, "data:image/svg+xml;base64,"))
	if err != nil || !bytes.Contains(raw, []byte("#ff0000")) || bytes.Contains(raw, []byte("bad")) || bytes.Contains(raw, []byte("outside")) || bytes.Contains(raw, []byte("onload")) {
		t.Fatalf("SVG was not reduced to inert geometry: %s", raw)
	}
	png := tinyIcon(t)
	ico := make([]byte, 22)
	copy(ico, []byte{0, 0, 1, 0, 1, 0})
	ico[6] = 16
	ico[7] = 16
	binary.LittleEndian.PutUint32(ico[14:18], uint32(len(png)))
	binary.LittleEndian.PutUint32(ico[18:22], 22)
	ico = append(ico, png...)
	if !strings.HasPrefix(iconData(ico), "data:image/x-icon;") {
		t.Fatal("PNG-in-ICO rejected")
	}
	if iconData(ico[:23]) != "" {
		t.Fatal("truncated ICO accepted")
	}
	for name, want := range map[string]string{"studio": "ST", "sample-project": "SP", "@scope/app": "SA", "": "?", "项目": "项目"} {
		if got := projectInitials(name); got != want {
			t.Errorf("%s = %s; want %s", name, got, want)
		}
	}
}
func TestIconCacheIsBackgroundBoundedAndIdentityScoped(t *testing.T) {
	c := newIconCache()
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	done := make(chan struct{}, 4)
	var calls atomic.Int32
	c.fetch = func(ctx context.Context, s server, verify func(server) error) string {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			return "image"
		case <-ctx.Done():
			return ""
		}
	}
	verify := func(server) error { return nil }
	s := iconServer("http://127.0.0.1:43210")
	decorate := func(rows []server) { c.decorate(rows, verify, func() { done <- struct{}{} }) }
	before := time.Now()
	rows := []server{s}
	decorate(rows)
	if time.Since(before) > 100*time.Millisecond {
		t.Fatal("cache blocks scans")
	}
	<-started
	decorate(rows)
	if calls.Load() != 1 {
		t.Fatal("in-flight fetch duplicated")
	}
	close(release)
	<-done
	decorate(rows)
	if rows[0].Icon != "image" || calls.Load() != 1 {
		t.Fatal("cache miss")
	}
	reused := s
	reused.Key = "456:2"
	fresh := []server{reused}
	decorate(fresh)
	if fresh[0].Icon != "" {
		t.Fatal("port reuse inherited another process's icon")
	}
	<-done
	c.mu.Lock()
	e := c.entries[iconKey(s)]
	e.expires = time.Now().Add(-time.Second)
	c.entries[iconKey(s)] = e
	c.mu.Unlock()
	decorate(rows)
	<-done
	if calls.Load() != 3 {
		t.Fatal("expired cache was not refreshed")
	}
	// Failed lookups are cached too, so an API without a favicon is not polled.
	c.fetch = func(context.Context, server, func(server) error) string { return "" }
	other := s
	other.Key = "789:3"
	negative := []server{other}
	decorate(negative)
	<-done
	c.mu.Lock()
	entry := c.entries[iconKey(other)]
	c.mu.Unlock()
	if entry.data != "" || !entry.expires.After(time.Now()) {
		t.Fatal("missing icon was not cached")
	}
}

func TestIconQueueLimitsConcurrencyAndMemory(t *testing.T) {
	c := newIconCache()
	var active, peak atomic.Int32
	done := make(chan struct{}, 16)
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	c.fetch = func(context.Context, server, func(server) error) string {
		n := active.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		active.Add(-1)
		return ""
	}
	var rows []server
	for i := 0; i < 40; i++ {
		s := iconServer("http://127.0.0.1:43210")
		s.Key = strconv.Itoa(i)
		rows = append(rows, s)
	}
	c.decorate(rows, func(server) error { return nil }, func() { done <- struct{}{} })
	<-started
	<-started
	c.mu.Lock()
	queued := len(c.entries)
	c.mu.Unlock()
	if queued != 16 || peak.Load() != 2 {
		t.Fatalf("queued=%d active=%d", queued, peak.Load())
	}
	close(release)
	for i := 0; i < 16; i++ {
		<-done
	}
	c.mu.Lock()
	for i := 0; i < iconCacheSize; i++ {
		c.entries[strconv.Itoa(i)] = iconEntry{expires: time.Now()}
	}
	// Reset to exactly capacity before asking for a new identity.
	for k := range c.entries {
		if strings.Contains(k, "\x00") {
			delete(c.entries, k)
		}
	}
	c.mu.Unlock()
	s := iconServer("http://127.0.0.1:43211")
	c.decorate([]server{s}, func(server) error { return nil }, func() { done <- struct{}{} })
	<-done
	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n != iconCacheSize {
		t.Fatalf("cache grew to %d", n)
	}
}
