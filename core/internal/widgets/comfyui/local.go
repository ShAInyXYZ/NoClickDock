package comfyui

import (
	"context"
	"net"
	"os"
	"strings"
	"time"
)

// resolver is what the local test needs from the network: the host's
// addresses, this machine's own addresses.
type resolver struct {
	lookup func(ctx context.Context, host string) ([]net.IP, error)
	own    func() []net.IP
}

var netResolver = resolver{
	lookup: func(ctx context.Context, host string) ([]net.IP, error) {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		ips := make([]net.IP, 0, len(addrs))
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
		return ips, err
	},
	own: ownAddresses,
}

// ownAddresses: every interface's, and whatever this machine's hostname
// resolves to.
func ownAddresses() []net.IP {
	var ips []net.IP
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				ips = append(ips, n.IP)
			}
		}
	}
	if name, err := os.Hostname(); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if addrs, err := net.DefaultResolver.LookupIPAddr(ctx, name); err == nil {
			for _, a := range addrs {
				ips = append(ips, a.IP)
			}
		}
	}
	return ips
}

// isLocal: the endpoint is this machine, so its sensors describe the
// server's hardware. A remote server gets none — two machines never share
// one panel.
func (r resolver) isLocal(ctx context.Context, host string) bool {
	h := strings.Trim(strings.ToLower(host), "[]")
	switch h {
	case "":
		return false
	case "127.0.0.1", "::1", "localhost", "0.0.0.0", "::":
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var addrs []net.IP
	if ip := net.ParseIP(h); ip != nil {
		addrs = []net.IP{ip}
	} else if found, err := r.lookup(ctx, h); err == nil {
		addrs = found
	}
	if len(addrs) == 0 {
		return false
	}
	var own []net.IP
	for _, a := range addrs {
		if a.IsLoopback() || a.IsUnspecified() {
			return true
		}
		if own == nil {
			own = r.own()
		}
		for _, o := range own {
			if a.Equal(o) {
				return true
			}
		}
	}
	return false
}
