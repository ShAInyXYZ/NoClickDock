package tailscale

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// node is `tailscale status --json`'s Self or one of its peers: the part
// the panel reads.
type node struct {
	HostName       string
	DNSName        string
	OS             string
	TailscaleIPs   []string
	CurAddr        string
	Relay          string
	RxBytes        int64
	TxBytes        int64
	LastSeen       string
	Online         bool
	Active         bool
	ExitNode       bool
	ExitNodeOption bool
	PrimaryRoutes  []string
	KeyExpiry      string // absent when key expiry is disabled
}

type rawStatus struct {
	Version        string
	BackendState   string
	TailscaleIPs   []string
	MagicDNSSuffix string
	Health         []string
	ClientVersion  *struct{ RunningLatest *bool }
	Self           *node
	Peer           map[string]*node
}

// peer is one other node of the tailnet as the panel shows it.
type peer struct {
	Host       string // as the node calls itself
	id         string // lower-cased: the identity for watching and rates
	Online     bool
	Active     bool // recent traffic
	Exit       bool // our exit node
	ExitOption bool
	OS         string
	Direct     bool
	Relay      string // DERP region
	Last       time.Time
	KeyExpired bool
	Routes     []string
	rx, tx     int64

	Rate    float64 // bytes/s since the previous poll
	HasRate bool
	Watched bool
}

// status is one poll of tailscaled.
type status struct {
	Backend     string
	Self        string
	SelfOnline  bool
	IP, DNS     string
	ExitOffered bool     // this node offers itself as an exit node
	Routes      []string // subnets this node advertises
	Version     string
	Update      bool
	Health      []string
	ExpiryDays  float64
	HasExpiry   bool
	ExitNode    string
	Peers       []*peer
}

// parseStatus reads `tailscale status --json` as of now.
func parseStatus(data []byte, now time.Time) (*status, error) {
	var raw rawStatus
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("tailscale status: %w", err)
	}
	if raw.BackendState == "" {
		return nil, fmt.Errorf("tailscale status: no backend state")
	}
	self := raw.Self
	if self == nil {
		self = &node{}
	}
	st := &status{
		Backend:     raw.BackendState,
		Self:        orQ(self.HostName),
		SelfOnline:  self.Online,
		ExitOffered: self.ExitNodeOption,
		Routes:      self.PrimaryRoutes,
		Version:     strings.SplitN(raw.Version, "-", 2)[0],
		Update:      raw.ClientVersion != nil && raw.ClientVersion.RunningLatest != nil && !*raw.ClientVersion.RunningLatest,
	}
	for _, h := range raw.Health {
		if strings.TrimSpace(h) != "" {
			st.Health = append(st.Health, h)
		}
	}
	ips := self.TailscaleIPs
	if len(ips) == 0 {
		ips = raw.TailscaleIPs
	}
	for _, ip := range ips {
		if strings.Contains(ip, ".") {
			st.IP = ip
			break
		}
	}
	if st.IP == "" && len(ips) > 0 {
		st.IP = ips[0]
	}
	st.DNS = strings.TrimSuffix(self.DNSName, ".")
	if st.DNS == "" && raw.MagicDNSSuffix != "" && self.HostName != "" {
		st.DNS = strings.ToLower(self.HostName) + "." + raw.MagicDNSSuffix
	}
	if t, ok := parseTime(self.KeyExpiry); ok {
		st.ExpiryDays, st.HasExpiry = t.Sub(now).Hours()/24, true
	}

	keys := make([]string, 0, len(raw.Peer))
	for k := range raw.Peer {
		keys = append(keys, k)
	}
	sort.Strings(keys) // the order the daemon printed them in
	for _, k := range keys {
		n := raw.Peer[k]
		if n == nil {
			continue
		}
		p := &peer{
			Host: orQ(n.HostName), id: strings.ToLower(n.HostName),
			Online: n.Online, Active: n.Active, Exit: n.ExitNode, ExitOption: n.ExitNodeOption,
			OS: n.OS, Direct: n.CurAddr != "", Relay: n.Relay, Routes: n.PrimaryRoutes,
			rx: n.RxBytes, tx: n.TxBytes,
		}
		p.Last, _ = parseTime(n.LastSeen)
		if t, ok := parseTime(n.KeyExpiry); ok && t.Before(now) {
			p.KeyExpired = true
		}
		if p.Exit && st.ExitNode == "" {
			st.ExitNode = p.Host
		}
		st.Peers = append(st.Peers, p)
	}
	return st, nil
}

// parseTime reads a tailscale timestamp — one-digit fractions ("…:00.1Z")
// included; the zero time ("0001-01-01T00:00:00Z", never) is unknown.
func parseTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
	if err != nil || t.Year() < 2000 {
		return time.Time{}, false
	}
	return t, true
}

func orQ(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

// exposure is what this node publishes: tailnet-only serves and, worse,
// funnels to the public internet.
type exposure struct {
	serve  []served
	funnel []string // host:port
}

type served struct {
	port   int
	target string // what it proxies to, when known
}

// parseServe reads `tailscale serve status --json`: a ServeConfig, "{}" or
// nothing when there is none. Serve and funnel are one config; a port is a
// funnel only where AllowFunnel says so (the text output lists tailnet-only
// serves under `funnel status` too, which the Python widget took for public
// exposure).
func parseServe(out string) (*exposure, error) {
	out = strings.TrimSpace(out)
	if out == "" || out == "{}" {
		return &exposure{}, nil
	}
	var cfg struct {
		TCP map[string]struct {
			TCPForward string
		}
		Web map[string]struct {
			Handlers map[string]struct {
				Proxy, Path, Text string
			}
		}
		AllowFunnel map[string]bool
	}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		return nil, fmt.Errorf("tailscale serve status: %w", err)
	}
	e := &exposure{}
	targets := map[int]string{}
	for hostport, web := range cfg.Web {
		_, port, _ := strings.Cut(hostport, ":")
		n, err := strconv.Atoi(port)
		if err != nil {
			continue
		}
		if h, ok := web.Handlers["/"]; ok {
			targets[n] = shortTarget(h.Proxy + h.Path)
		}
	}
	for port, tcp := range cfg.TCP {
		n, err := strconv.Atoi(port)
		if err != nil {
			continue
		}
		if tcp.TCPForward != "" {
			targets[n] = tcp.TCPForward
		} else if _, ok := targets[n]; !ok {
			targets[n] = ""
		}
	}
	for n, t := range targets {
		e.serve = append(e.serve, served{port: n, target: t})
	}
	sort.Slice(e.serve, func(i, j int) bool { return e.serve[i].port < e.serve[j].port })
	for hostport, on := range cfg.AllowFunnel {
		if on {
			e.funnel = append(e.funnel, hostport)
		}
	}
	sort.Strings(e.funnel)
	return e, nil
}

// shortTarget drops the scheme: "http://127.0.0.1:6082" → "127.0.0.1:6082".
func shortTarget(t string) string {
	if _, rest, ok := strings.Cut(t, "://"); ok {
		return rest
	}
	return t
}

// fmtRate is a peer's traffic: nothing below 1 KiB/s, else "340.0 KB/s",
// "1.2 MB/s" (binary units).
func fmtRate(bps float64) string {
	for _, u := range []struct {
		unit string
		size float64
	}{{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}} {
		if bps >= u.size {
			return fmt.Sprintf("%.1f %s/s", bps/u.size, u.unit)
		}
	}
	return ""
}
