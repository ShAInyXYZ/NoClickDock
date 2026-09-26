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
	Host       string // its MagicDNS name (else its hostname), as shown
	id         string // its node key: the identity for rates and "seen online"
	name       string // lower-cased Host: what the watch list names
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
		// Known by its node key: two machines may share an OS hostname
		// ("ubuntu"); their MagicDNS names are unique ("ubuntu", "ubuntu-1"),
		// so that is the name shown and matched against the watch list.
		name := strings.ToLower(n.HostName)
		if label, _, _ := strings.Cut(strings.TrimSuffix(n.DNSName, "."), "."); label != "" {
			name = label
		}
		p := &peer{
			Host: orQ(name), id: k, name: name,
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
	serve    []served
	funnel   []string // host:port
	services []string // svc:<name>, served as a Tailscale service
	// session is set when some of it runs in a terminal (foreground), not
	// in the background config: it ends when that terminal does.
	session bool
	// unknown: the config holds something this reader does not know, and
	// nothing it knows — so "nothing exposed" cannot be said.
	unknown bool
}

type served struct {
	port   int
	target string // what it proxies to, when known
}

// serveConfig is the part of tailscale's ServeConfig (ipn/serve.go) that says
// what is exposed. A `serve`/`funnel` run without --bg — the default in a
// terminal — lives under Foreground[session], itself a ServeConfig.
type serveConfig struct {
	TCP map[string]struct {
		TCPForward string
	}
	Web map[string]struct {
		Handlers map[string]struct {
			Proxy, Path, Text string
		}
	}
	AllowFunnel  map[string]bool
	AllowIngress map[string]bool // its name in older clients
	Foreground   map[string]serveConfig
	Services     map[string]json.RawMessage
}

// The keys a ServeConfig may carry that expose nothing by themselves.
var quietKeys = map[string]bool{"ETag": true}

// parseServe reads `tailscale serve status --json`: a ServeConfig, "{}" or
// nothing when there is none. Serve and funnel are one config; a port is a
// funnel only where AllowFunnel says so (the text output lists tailnet-only
// serves under `funnel status` too, which the Python widget took for public
// exposure). Background and foreground (terminal) configs are merged.
func parseServe(out string) (*exposure, error) {
	out = strings.TrimSpace(out)
	if out == "" || out == "{}" || out == "null" {
		return &exposure{}, nil
	}
	var cfg serveConfig
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		return nil, fmt.Errorf("tailscale serve status: %w", err)
	}
	e := &exposure{}
	targets := map[int]string{}
	funnels := map[string]bool{}
	merge(&cfg, targets, funnels, e)
	for _, part := range cfg.Foreground {
		if len(part.TCP)+len(part.Web)+len(part.AllowFunnel)+len(part.AllowIngress)+len(part.Services) > 0 {
			e.session = true
		}
		merge(&part, targets, funnels, e)
	}
	for n, t := range targets {
		e.serve = append(e.serve, served{port: n, target: t})
	}
	sort.Slice(e.serve, func(i, j int) bool { return e.serve[i].port < e.serve[j].port })
	for hp := range funnels {
		e.funnel = append(e.funnel, hp)
	}
	sort.Strings(e.funnel)
	sort.Strings(e.services)

	// Something there that this reader does not understand, and nothing it
	// does: say unknown rather than "nothing exposed".
	if len(e.serve)+len(e.funnel)+len(e.services) == 0 {
		var top map[string]json.RawMessage
		if json.Unmarshal([]byte(out), &top) == nil {
			for k, v := range top {
				if known[k] || quietKeys[k] {
					continue
				}
				if t := strings.TrimSpace(string(v)); t != "" && t != "null" && t != "{}" && t != "[]" && t != "false" && t != `""` {
					e.unknown = true
				}
			}
		}
	}
	return e, nil
}

var known = map[string]bool{"TCP": true, "Web": true, "AllowFunnel": true, "AllowIngress": true, "Foreground": true, "Services": true}

// merge adds one ServeConfig's ports, funnels and services.
func merge(cfg *serveConfig, targets map[int]string, funnels map[string]bool, e *exposure) {
	for hostport, web := range cfg.Web {
		n, err := strconv.Atoi(hostport[strings.LastIndex(hostport, ":")+1:])
		if err != nil {
			continue
		}
		if h, ok := web.Handlers["/"]; ok {
			targets[n] = shortTarget(h.Proxy + h.Path)
		} else if _, ok := targets[n]; !ok {
			targets[n] = ""
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
	for _, allow := range []map[string]bool{cfg.AllowFunnel, cfg.AllowIngress} {
		for hostport, on := range allow {
			if on {
				funnels[hostport] = true
			}
		}
	}
	for name := range cfg.Services {
		e.services = append(e.services, name)
	}
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
