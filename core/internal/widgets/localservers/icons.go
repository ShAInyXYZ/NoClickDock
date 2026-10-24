package localservers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

const iconBytes = 64 << 10
const pageBytes = 512 << 10
const iconCacheSize = 128

// In-memory, identity-bound cache. Collect only schedules work; two background
// jobs at most read HTML/icons. Errors, URLs and HTML never enter snapshots/logs.
type iconEntry struct {
	data    string
	expires time.Time
	busy    bool
}
type iconCache struct {
	mu      sync.Mutex
	entries map[string]iconEntry
	slots   chan struct{}
	pending chan struct{}
	fetch   func(context.Context, server, func(server) error) string
}

func newIconCache() *iconCache {
	return &iconCache{entries: map[string]iconEntry{}, slots: make(chan struct{}, 2), pending: make(chan struct{}, 16), fetch: fetchIcon}
}
func iconKey(s server) string {
	return s.Key + "\x00" + s.Dir + "\x00" + s.Endpoints[0].URL()
}
func (c *iconCache) decorate(rows []server, verify func(server) error, refresh func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for i := range rows {
		s := &rows[i]
		if !s.Dev || len(s.Endpoints) == 0 {
			continue
		}
		key := iconKey(*s)
		entry, exists := c.entries[key]
		s.Icon = entry.data
		if !s.Running || entry.busy || exists && now.Before(entry.expires) {
			continue
		}
		select {
		case c.pending <- struct{}{}:
		default:
			continue
		}
		if !exists && len(c.entries) >= iconCacheSize {
			var oldest string
			for k, e := range c.entries {
				if !e.busy && (oldest == "" || e.expires.Before(c.entries[oldest].expires)) {
					oldest = k
				}
			}
			if oldest == "" {
				<-c.pending
				continue
			}
			delete(c.entries, oldest)
		}
		entry.busy = true
		c.entries[key] = entry
		// Copy private process slices only as immutable identity data; never serialize.
		go c.load(key, *s, verify, refresh)
	}
}
func (c *iconCache) load(key string, s server, verify func(server) error, refresh func()) {
	c.slots <- struct{}{}
	defer func() { <-c.slots }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	data := c.fetch(ctx, s, verify)
	ttl := 10 * time.Minute
	if data == "" {
		ttl = 2 * time.Minute
	}
	c.mu.Lock()
	c.entries[key] = iconEntry{data: data, expires: time.Now().Add(ttl)}
	<-c.pending
	c.mu.Unlock()
	if refresh != nil {
		refresh()
	}
}

// Match the discovered numeric loopback origin, allowing localhost spelling
// without resolving DNS. Never follow a link/redirect to another port or host.
func localIconURL(origin *url.URL, u *url.URL) bool {
	if u == nil || u.User != nil || u.Scheme != origin.Scheme || u.Port() != origin.Port() {
		return false
	}
	return strings.EqualFold(u.Hostname(), "localhost") || u.Hostname() == origin.Hostname()
}
func fetchIcon(ctx context.Context, s server, verify func(server) error) string {
	for _, ep := range s.Endpoints {
		if ctx.Err() != nil {
			break
		}
		ip := net.ParseIP(ep.Host)
		if ip == nil || !ip.IsLoopback() {
			continue
		}
		origin, _ := url.Parse(ep.URL())
		target := s
		target.Endpoints = []endpoint{ep}
		// A shared supervisor blocks process controls, not passive page reads.
		// Verify still reclassifies the live executable and rejects infrastructure.
		target.Protected = ""
		transport := &http.Transport{
			Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 16 << 10,
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				if err := verify(target); err != nil {
					return nil, err
				}
				return (&net.Dialer{Timeout: 700 * time.Millisecond}).DialContext(ctx, network, origin.Host)
			},
		}
		client := &http.Client{Transport: transport, Timeout: time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 || !localIconURL(origin, req.URL) {
				return errors.New("nonlocal icon redirect")
			}
			return nil
		}}
		read := func(u *url.URL, limit int) ([]byte, *url.URL) {
			if !localIconURL(origin, u) {
				return nil, u
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
			if err != nil {
				return nil, u
			}
			req.Header.Set("User-Agent", "NoClickDock/favicon")
			resp, err := client.Do(req)
			if err != nil {
				return nil, u
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return nil, resp.Request.URL
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit+1)))
			if err != nil || len(body) > limit {
				return nil, resp.Request.URL
			}
			return body, resp.Request.URL
		}
		body, pageURL := read(origin, pageBytes)
		links := iconLinks(body, pageURL)
		links = append(links, origin.ResolveReference(&url.URL{Path: "/favicon.ico"}))
		seen := map[string]bool{}
		for _, u := range links {
			if !localIconURL(origin, u) || seen[u.String()] {
				continue
			}
			seen[u.String()] = true
			b, _ := read(u, iconBytes)
			if data := iconData(b); data != "" && verify(target) == nil {
				transport.CloseIdleConnections()
				return data
			}
		}
		transport.CloseIdleConnections()
	}
	return ""
}

// The HTML5 tokenizer skips script/comment text and handles case, attribute
// quoting and entities. Ignore external <base> elements and icon URLs later.
func iconLinks(body []byte, page *url.URL) []*url.URL {
	z := html.NewTokenizer(bytes.NewReader(body))
	z.SetMaxBuf(pageBytes)
	base := page
	var hrefs []string
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		tok := z.Token()
		if tok.Data == "body" {
			break
		}
		if tok.Data != "link" && tok.Data != "base" {
			continue
		}
		rel, href := "", ""
		for _, a := range tok.Attr {
			if a.Key == "rel" {
				rel = strings.ToLower(a.Val)
			}
			if a.Key == "href" {
				href = a.Val
			}
		}
		if tok.Data == "base" {
			if u, err := url.Parse(href); err == nil {
				u = page.ResolveReference(u)
				if localIconURL(page, u) {
					base = u
				}
			}
			continue
		}
		for _, r := range strings.Fields(rel) {
			if r == "icon" && href != "" && len(hrefs) < 3 {
				hrefs = append(hrefs, href)
				break
			}
		}
	}
	var links []*url.URL
	for _, href := range hrefs {
		if u, err := url.Parse(href); err == nil {
			links = append(links, base.ResolveReference(u))
		}
	}
	return links
}
func iconData(b []byte) string {
	if len(b) == 0 || len(b) > iconBytes {
		return ""
	}
	mime := ""
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(b)); err == nil {
		if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 512 || cfg.Height > 512 {
			return ""
		}
		mime = "image/" + format
	} else if len(b) >= 22 && bytes.Equal(b[:4], []byte{0, 0, 1, 0}) {
		n := int(binary.LittleEndian.Uint16(b[4:6]))
		if n < 1 || n > 32 || len(b) < 6+16*n {
			return ""
		}
		for i := 0; i < n; i++ {
			entry := b[6+i*16 : 6+(i+1)*16]
			length := uint64(binary.LittleEndian.Uint32(entry[8:12]))
			offset := uint64(binary.LittleEndian.Uint32(entry[12:16]))
			if length == 0 || offset < uint64(6+16*n) || offset+length > uint64(len(b)) {
				return ""
			}
		}
		mime = "image/x-icon"
	} else {
		b = safeSVG(b)
		if len(b) == 0 {
			return ""
		}
		mime = "image/svg+xml"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// Re-encode only inert SVG geometry. No scripts, CSS, text, embedded images,
// links, foreign objects, entities, or external references reach the webview.
func safeSVG(b []byte) []byte {
	tags := strings.Fields("svg g path circle ellipse rect line polyline polygon defs linearGradient radialGradient stop clipPath")
	attrs := strings.Fields("id viewBox width height x y x1 y1 x2 y2 cx cy r rx ry d points transform fill fill-rule fill-opacity opacity stroke stroke-width stroke-linecap stroke-linejoin stroke-opacity stroke-miterlimit stroke-dasharray stroke-dashoffset offset stop-color stop-opacity gradientUnits gradientTransform spreadMethod fx fy clip-path clip-rule")
	allowed := func(list []string, s string) bool {
		for _, v := range list {
			if v == s {
				return true
			}
		}
		return false
	}
	d := xml.NewDecoder(bytes.NewReader(b))
	var out bytes.Buffer
	e := xml.NewEncoder(&out)
	depth, roots := 0, 0
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		switch tok := t.(type) {
		case xml.StartElement:
			if !allowed(tags, tok.Name.Local) || (tok.Name.Space != "" && tok.Name.Space != "http://www.w3.org/2000/svg") {
				return nil
			}
			if depth == 0 {
				if tok.Name.Local != "svg" || roots != 0 {
					return nil
				}
				roots++
			}
			depth++
			if depth > 32 {
				return nil
			}
			clean := xml.StartElement{Name: xml.Name{Local: tok.Name.Local}}
			if depth == 1 {
				clean.Attr = []xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: "http://www.w3.org/2000/svg"}}
			}
			for _, a := range tok.Attr {
				if a.Name.Space != "" || !allowed(attrs, a.Name.Local) {
					continue
				}
				value := strings.ToLower(strings.TrimSpace(a.Value))
				if strings.ContainsAny(value, "\\\r\n") || strings.Contains(value, ":") {
					return nil
				}
				if strings.Contains(value, "url") && !(strings.HasPrefix(value, "url(#") && strings.HasSuffix(value, ")") && strings.Count(value, "(") == 1 && strings.Count(value, ")") == 1) {
					return nil
				}
				clean.Attr = append(clean.Attr, xml.Attr{Name: xml.Name{Local: a.Name.Local}, Value: a.Value})
			}
			if e.EncodeToken(clean) != nil {
				return nil
			}
		case xml.EndElement:
			depth--
			if e.EncodeToken(xml.EndElement{Name: xml.Name{Local: tok.Name.Local}}) != nil {
				return nil
			}
		case xml.Directive:
			return nil
		}
	}
	if roots != 1 || depth != 0 || e.Flush() != nil {
		return nil
	}
	return out.Bytes()
}
func projectInitials(name string) string {
	words := strings.FieldsFunc(safeText(name), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) == 0 {
		return "?"
	}
	first := []rune(words[0])
	out := []rune{first[0]}
	if len(words) > 1 {
		out = append(out, []rune(words[len(words)-1])[0])
	} else if len(first) > 1 {
		out = append(out, first[1])
	}
	return strings.ToUpper(string(out))
}
