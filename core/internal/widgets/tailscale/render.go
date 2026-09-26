package tailscale

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

// Key expiry is the classic silent tailnet outage: everything works until it
// abruptly does not. Warn while there is still time to re-authenticate.
const keyWarnDays = 7

// poll is one collect as the panel needs it.
type poll struct {
	available bool
	missing   bool // no tailscale CLI
	windows   bool
	st        *status
	exposure  *exposure // nil: serve status did not answer
	pinned    bool      // the watch list is the user's, not "seen online"
	now       time.Time
}

func render(p *poll) model.Snapshot {
	s := model.Snapshot{Width: 420}
	if !p.available {
		return unavailable(p)
	}
	st := p.st
	var watched, down []*peer
	for _, pr := range st.Peers {
		if pr.Watched {
			watched = append(watched, pr)
			if !pr.Online {
				down = append(down, pr)
			}
		}
	}
	sort.Slice(down, func(i, j int) bool { return down[i].Host < down[j].Host })
	up := len(watched) - len(down)
	expiring := st.HasExpiry && st.ExpiryDays <= keyWarnDays

	switch {
	case st.Backend != "Running":
		s.State, s.StateLabel = model.StateCrit, backendWords(st.Backend)
	case !st.SelfOnline:
		s.State, s.StateLabel = model.StateCrit, "this node offline"
	case len(down) == 1:
		s.State, s.StateLabel = model.StateWarn, down[0].Host+" offline"
	case len(down) > 1:
		s.State, s.StateLabel = model.StateWarn, fmt.Sprintf("%d peers offline", len(down))
	case len(st.Health) > 0:
		// The fact, in the label's budget; the messages lead the panel.
		n := len(st.Health)
		s.State, s.StateLabel = model.StateWarn, fmt.Sprintf("%d health %s", n, plural(n, "warning", "warnings"))
	case expiring:
		s.State, s.StateLabel = model.StateWarn, fmt.Sprintf("key expires in %.0fd", st.ExpiryDays)
	default:
		s.State, s.StateLabel = model.StateOK, fmt.Sprintf("%d %s up", up, plural(up, "peer", "peers"))
	}

	switch {
	case s.State == model.StateCrit:
		s.Glance = model.Glance{Text: "off"}
	case expiring && len(down) == 0 && len(st.Health) == 0:
		s.Glance = model.Glance{Text: fmt.Sprintf("%.0fd", st.ExpiryDays)}
	case len(watched) > 99:
		s.Glance = model.Glance{Text: strconv.Itoa(up)}
	default:
		s.Glance = model.Glance{Text: fmt.Sprintf("%d/%d", up, len(watched))}
	}
	if s.State == model.StateWarn || s.State == model.StateCrit {
		s.Glance.Tone = s.State.Tone()
	}

	if st.Backend != "Running" {
		s.Issues = append(s.Issues, model.Issue{ID: "backend:" + st.Backend, Text: backendSentence(st.Backend), Tone: model.ToneCrit})
	}
	for _, pr := range down {
		s.Issues = append(s.Issues, model.Issue{ID: "down:" + pr.Host, Text: pr.Host + " is offline", Tone: model.ToneWarn})
	}
	for _, h := range st.Health {
		s.Issues = append(s.Issues, model.Issue{ID: "health:" + h, Text: cut(h, 80), Tone: model.ToneWarn})
	}

	if st.IP != "" {
		s.Menu = append(s.Menu, model.MenuItem{ID: "copy-ip", Label: "Copy address  " + st.IP, Copy: st.IP})
	}
	if st.DNS != "" {
		s.Menu = append(s.Menu, model.MenuItem{ID: "copy-dns", Label: "Copy MagicDNS name", Copy: st.DNS})
	}
	s.Pages = []model.Page{{ID: "main", Blocks: panel(p, down)}}
	return s
}

// backendWords says tailscaled's backend state in lowercase words.
func backendWords(state string) string {
	switch state {
	case "NeedsLogin":
		return "needs login"
	case "NeedsMachineAuth":
		return "needs approval"
	case "NoState":
		return "no state"
	}
	return strings.ToLower(state)
}

func backendSentence(state string) string {
	switch state {
	case "Stopped":
		return "Tailscale is stopped"
	case "NeedsLogin":
		return "Tailscale needs a login"
	case "NeedsMachineAuth":
		return "This node waits for approval"
	case "Starting":
		return "Tailscale is starting"
	}
	return "tailscaled: " + state
}

func unavailable(p *poll) model.Snapshot {
	s := model.Snapshot{
		Width: 420, State: model.StateIdle, StateLabel: "tailscaled unreachable",
		Glance: model.Glance{Text: "off", Tone: model.ToneIdle},
	}
	empty := model.Empty{Text: "tailscaled is not running on this machine. Start it with", Commands: []string{"sudo systemctl start tailscaled"}}
	switch {
	case p.missing:
		s.StateLabel = "not installed"
		empty = model.Empty{Text: "tailscale is not installed on this machine."}
	case p.windows:
		s.StateLabel = "Tailscale not running"
		empty = model.Empty{Text: "Tailscale is not running. Start the Tailscale app."}
	}
	s.Pages = []model.Page{{ID: "main", Blocks: model.Blocks{empty}}}
	return s
}

func panel(p *poll, down []*peer) model.Blocks {
	st := p.st
	ok, warn, crit, idle := model.ToneOK, model.ToneWarn, model.ToneCrit, model.ToneIdle
	var out, health, offline, tail model.Blocks

	// When the daemon has something to say, it outranks the node's facts.
	if len(st.Health) > 0 {
		health = append(health, model.Section{Title: "Health", Tone: crit})
		for _, h := range st.Health {
			health = append(health, model.Note{Text: h, Tone: warn})
		}
	}

	// This node, and the two strings people come here to copy.
	selfLED := ok
	if st.Backend != "Running" || !st.SelfOnline {
		selfLED = crit
	}
	out = append(out, model.Section{Title: "This node"}, model.Row{Name: st.Self, Value: backendWords(st.Backend), LED: selfLED})
	if st.IP != "" {
		out = append(out, model.Row{Name: "Address", Value: st.IP})
	}
	if st.DNS != "" {
		out = append(out, model.Row{Name: "MagicDNS", Value: st.DNS})
	}
	switch {
	case st.Update:
		out = append(out, model.Row{Name: "Version", Value: st.Version + " · update available", Tone: warn, LED: warn})
	case st.Version != "":
		out = append(out, model.Row{Name: "Version", Value: st.Version})
	}
	if st.HasExpiry {
		r := model.Row{Name: "Key expires", Value: fmt.Sprintf("%.0fd", st.ExpiryDays)}
		if st.ExpiryDays <= keyWarnDays {
			r.Tone, r.LED = warn, warn
		}
		out = append(out, r)
	}
	if st.ExitNode != "" {
		// Your traffic leaves through another machine: always said.
		out = append(out, model.Row{Name: "Exit node", Value: st.ExitNode, Tone: warn, LED: warn})
	}

	// What this machine publishes, and what the tailnet offers. Nothing
	// exposed is said out loud: it is the reassuring case.
	out = append(out, model.Section{Title: "Exposure"})
	switch e := p.exposure; {
	case e == nil || e.unknown:
		out = append(out, model.Row{Name: "Serve / funnel", Value: "unknown", LED: idle})
	case len(e.serve) == 0 && len(e.funnel) == 0 && len(e.services) == 0:
		out = append(out, model.Row{Name: "Serve / funnel", Value: "nothing exposed", LED: ok})
	default:
		// A terminal session's serve ends with the terminal; say so.
		session := ""
		if e.session {
			session = " · session"
		}
		if len(e.funnel) > 0 {
			var ports []string
			for _, hp := range e.funnel {
				ports = append(ports, ":"+hp[strings.LastIndex(hp, ":")+1:])
			}
			out = append(out, model.Row{Name: "Funnel", Value: strings.Join(ports, ", ") + " · public internet" + session, Tone: crit, LED: crit})
		}
		if len(e.serve) > 0 {
			out = append(out, model.Row{Name: "Serve", Value: serveValue(e.serve) + session, Tone: warn, LED: warn})
		}
		if len(e.services) > 0 {
			out = append(out, model.Row{Name: "Services", Value: strings.Join(e.services, ", "), Tone: warn, LED: warn})
		}
	}
	if st.ExitOffered {
		out = append(out, model.Row{Name: "Exit node", Value: "offered by this node", Tone: warn, LED: warn})
	}
	for _, r := range st.Routes {
		out = append(out, model.Row{Name: "Route advertised", Value: r, Tone: warn, LED: warn})
	}
	var exits []string
	var routes model.Blocks
	for _, pr := range st.Peers {
		if pr.ExitOption {
			exits = append(exits, pr.Host)
		}
		for _, r := range pr.Routes {
			routes = append(routes, model.Row{Name: "Route via " + pr.Host, Value: r})
		}
	}
	if len(exits) > 0 {
		sort.Strings(exits)
		out = append(out, model.Row{Name: "Exit nodes available", Value: strings.Join(exits, ", ")})
	}
	out = append(out, routes...)

	// Every peer is named: "5 dormant" says nothing you can act on; the NAS
	// against an old phone says everything. Online peers show their path —
	// relayed is the one thing here you can usually fix — and their rate.
	var online, dormant []*peer
	for _, pr := range st.Peers {
		switch {
		case pr.Online:
			online = append(online, pr)
		case !pr.Watched:
			dormant = append(dormant, pr)
		}
	}
	if len(online) > 0 {
		sort.SliceStable(online, func(i, j int) bool {
			if online[i].Active != online[j].Active {
				return online[i].Active
			}
			return online[i].Host < online[j].Host
		})
		out = append(out, model.Section{Title: "Online", Meta: strconv.Itoa(len(online))})
		for _, pr := range online {
			path := "idle"
			switch {
			case pr.Direct:
				path = "direct"
			case pr.Relay != "":
				path = "relay " + pr.Relay
			}
			bits := []string{pr.OS, path}
			if pr.HasRate {
				bits = append(bits, fmtRate(pr.Rate))
			}
			// Online is ok, quiet or not; a relayed path is the one thing to
			// fix, and its lamp says so with its value.
			r := model.Row{Name: pr.Host, Value: strings.Join(nonEmpty(bits), " · "), LED: ok}
			if pr.Relay != "" && !pr.Direct {
				r.Tone, r.LED = warn, warn
			}
			out = append(out, r)
		}
	}
	if len(down) > 0 {
		offline = append(offline, model.Section{Title: "Watched · offline", Meta: strconv.Itoa(len(down)), Tone: warn})
		for _, pr := range down {
			when := sys.TimeAgo(pr.Last, p.now)
			if when == "" {
				when = "offline"
			}
			offline = append(offline, model.Row{Name: pr.Host, Value: when, Tone: warn, LED: warn})
		}
	}
	if len(dormant) > 0 {
		// Most recently seen first, never-seen last. A peer whose key has
		// expired cannot come back without a login: the chips pull it out
		// as a row of its own.
		sort.SliceStable(dormant, func(i, j int) bool {
			a, b := dormant[i].Last, dormant[j].Last
			if a.IsZero() != b.IsZero() {
				return b.IsZero()
			}
			return a.After(b)
		})
		chips := model.Chips{Columns: 2}
		for _, pr := range dormant {
			c := model.Chip{Label: pr.Host, Tone: idle, Value: sys.TimeAgo(pr.Last, p.now)}
			if c.Value == "" {
				c.Value = "-"
			}
			if pr.KeyExpired {
				c.Tone, c.Value = warn, "key expired"
			}
			chips.Items = append(chips.Items, c)
		}
		tail = append(tail, model.Section{Title: "Dormant", Meta: strconv.Itoa(len(dormant))}, chips)
	}

	switch {
	case len(st.Peers) == 0:
		tail = append(tail, model.Note{Text: "No peers in this tailnet", Tone: idle})
	case !p.pinned:
		tail = append(tail, model.Note{Text: "Pin peers in Settings → Tailscale"})
	}

	// Problems first (A19): what the header says leads. A node that is down
	// is explained by the daemon's health and "This node"; otherwise the
	// watched peers that are offline come first.
	var order []model.Blocks
	if st.Backend != "Running" || !st.SelfOnline {
		order = []model.Blocks{health, out, offline, tail}
	} else {
		order = []model.Blocks{offline, health, out, tail}
	}
	var all model.Blocks
	for _, part := range order {
		all = append(all, part...)
	}
	return all
}

// serveValue: one port with where it goes, or the ports.
func serveValue(ss []served) string {
	if len(ss) == 1 {
		if ss[0].target != "" {
			return fmt.Sprintf(":%d → %s", ss[0].port, ss[0].target)
		}
		return fmt.Sprintf(":%d", ss[0].port)
	}
	var ports []string
	for _, s := range ss {
		ports = append(ports, fmt.Sprintf(":%d", s.port))
	}
	return strings.Join(ports, ", ")
}

func nonEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
