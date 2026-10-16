package docker

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

// data is one collect: every container, grouped, and the engine's numbers.
type data struct {
	available bool
	missing   bool // no docker CLI at all
	windows   bool
	units     []*unit
	all       []*container
	sys       *system // nil until first measured
	portainer string
}

// system is the System page's slow numbers, refreshed every five minutes.
type system struct {
	engine, compose string
	dangling        int
	df              map[string]dfRow // empty: docker system df did not answer
}

// pending is what an action is doing right now, by "unit:<name>" (every
// container and unit it touches) or "prune:<label>" — two scopes, so a
// container named "images" never reads as a prune in progress.
type pending map[string]string

func (p pending) of(scope, name string) (string, bool) {
	verb, ok := p[scope+":"+name]
	return verb, ok
}

// busy says whether any of the unit, or any of its members, is being acted on.
func (p pending) busy(u *unit) bool {
	if _, ok := p.of("unit", u.Name); ok {
		return true
	}
	for _, c := range u.Members {
		if _, ok := p.of("unit", c.Name); ok {
			return true
		}
	}
	return false
}

func pendingRow(name, verb string) model.Row {
	return model.Row{Name: name, Value: verb + "…", Tone: model.ToneBusy, LED: model.ToneBusy}
}

// Every Docker action asks twice (decision Q1); remove cannot be undone.
var confirms = map[string]string{
	"start": "start?", "stop": "stop?", "restart": "restart?", "resume": "resume?", "remove": "remove?",
}

func actions(scope, name string, verbs ...string) []model.Action {
	var out []model.Action
	for _, v := range verbs {
		out = append(out, model.Action{
			ID: scope + "." + v, Label: v, Confirm: confirms[v], Danger: v == "remove",
			Args: map[string]string{scope: name},
		})
	}
	return out
}

// unitActions are a row's buttons; none while any of it is being acted on.
func unitActions(u *unit, p pending) []model.Action {
	if p.busy(u) {
		return nil
	}
	if len(u.Running) > 0 {
		var out []model.Action
		for _, c := range u.Running {
			if c.State == "paused" {
				out = actions("unit", u.Name, "resume")
				break
			}
		}
		return append(out, actions("unit", u.Name, "restart", "stop")...)
	}
	out := actions("unit", u.Name, "start", "remove")
	if u.Stack && len(u.Members) > 1 {
		out[1].Confirm = "remove stack?"
	}
	return out
}

// containerActions are an alert's buttons; none while the container or its
// whole project is being acted on.
func containerActions(c *container, p pending) []model.Action {
	if _, ok := p.of("unit", c.Name); ok {
		return nil
	}
	if _, ok := p.of("unit", c.Project); ok && c.Project != "" {
		return nil
	}
	switch c.State {
	case "running", "restarting":
		return actions("container", c.Name, "restart", "stop")
	case "paused":
		return actions("container", c.Name, "resume")
	}
	return actions("container", c.Name, "start", "remove")
}

func prune(id, label, confirm string) []model.Action {
	return []model.Action{{ID: id, Label: label, Confirm: confirm, Danger: true}}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// render draws a collect as the panel, the dial and the toasts' issues.
func render(d *data, p pending) model.Snapshot {
	s := model.Snapshot{Width: 440}
	if !d.available {
		return unavailable(d)
	}

	att := needs(d.units)
	serious := 0
	for _, a := range att {
		if a.serious() {
			serious++
			text := a.c.Name + " is unhealthy"
			if a.reason == "restarting" {
				text = a.c.Name + " is restart-looping"
			}
			s.Issues = append(s.Issues, model.Issue{ID: a.reason + ":" + a.c.Name, Text: text, Tone: model.ToneCrit})
		}
	}
	running := 0
	for _, u := range d.units {
		running += len(u.Running)
	}
	switch {
	case serious > 0:
		s.State = model.StateCrit
		s.StateLabel = fmt.Sprintf("%d %s unhealthy", serious, plural(serious, "container", "containers"))
	case len(att) > 0:
		s.State = model.StateWarn
		s.StateLabel = fmt.Sprintf("%d crashed today", len(att))
	default:
		s.State = model.StateOK
		s.StateLabel = fmt.Sprintf("%d running", running)
	}
	s.Glance = model.Glance{Text: strconv.Itoa(running)}
	if s.State == model.StateWarn || s.State == model.StateCrit {
		s.Glance.Tone = s.State.Tone()
	}

	now := model.Page{ID: "now", Label: "Now", Blocks: nowPage(d, att, running, p)}
	if len(att) > 0 {
		now.Badge, now.Tone = strconv.Itoa(len(att)), model.ToneWarn
		if serious > 0 {
			now.Tone = model.ToneCrit
		}
	}
	stopped := model.Page{ID: "stopped", Label: "Stopped", Blocks: stoppedPage(d, p)}
	var down, broken int
	for _, u := range d.units {
		if u.down() {
			down++
			if len(u.Crashed) > 0 {
				broken++
			}
		}
	}
	if down > 0 {
		stopped.Badge = strconv.Itoa(down)
		if broken > 0 {
			stopped.Tone = model.ToneCrit
		}
	}
	s.Pages = []model.Page{now, stopped, {ID: "system", Label: "System", Blocks: systemPage(d, p)}}

	if d.sys != nil && d.sys.engine != "" {
		s.Footer = "engine " + short(d.sys.engine) + " · no compose"
		if d.sys.compose != "" {
			s.Footer = "engine " + short(d.sys.engine) + " · compose " + short(d.sys.compose)
		}
	}
	if d.portainer != "" {
		s.Menu = []model.MenuItem{{ID: "open-portainer", Label: "Open Portainer", URL: d.portainer}}
	}
	return s
}

// short is a version's major.minor: "27.3.1" → "27.3", "v2.29.7" → "2.29".
func short(version string) string {
	parts := strings.SplitN(strings.TrimPrefix(version, "v"), ".", 3)
	return strings.Join(parts[:min(len(parts), 2)], ".")
}

func unavailable(d *data) model.Snapshot {
	s := model.Snapshot{
		Width: 440, State: model.StateIdle, StateLabel: "daemon unreachable",
		Glance: model.Glance{Text: "off", Tone: model.ToneIdle},
	}
	empty := model.Empty{Text: "Docker is not running, or this user cannot reach its socket. Check with", Commands: []string{"docker info"}}
	switch {
	case d.missing:
		s.StateLabel = "not installed"
		empty = model.Empty{Text: "docker is not installed on this machine."}
	case d.windows:
		s.StateLabel = "Docker Desktop not running"
		empty = model.Empty{Text: "Docker Desktop is not running."}
	}
	s.Pages = []model.Page{{ID: "now", Blocks: model.Blocks{empty}}}
	return s
}

// nowPage: what needs a human, then what runs, then a count of the rest.
func nowPage(d *data, att []attention, running int, p pending) model.Blocks {
	if len(d.units) == 0 {
		return model.Blocks{model.Empty{Text: "No containers on this machine."}}
	}
	var out model.Blocks
	alerted := map[*container]bool{}
	if len(att) > 0 {
		tone := model.ToneWarn
		for _, a := range att {
			if a.serious() {
				tone = model.ToneCrit
			}
		}
		out = append(out, model.Section{Title: "Needs attention", Meta: strconv.Itoa(len(att)), Tone: tone})
		for _, a := range att {
			alerted[a.c] = true
			if verb, ok := p.of("unit", a.c.Name); ok {
				out = append(out, pendingRow(a.c.Name, verb))
				continue
			}
			out = append(out, alert(a, p))
		}
	}

	var live []*unit
	for _, u := range d.units {
		// A standalone container already alerted on is not listed twice (M2-d).
		if len(u.Running) > 0 && !(len(u.Members) == 1 && alerted[u.Members[0]]) {
			live = append(live, u)
		}
	}
	sort.SliceStable(live, func(i, j int) bool { return live[i].Mem > live[j].Mem })
	if len(live) > 0 {
		out = append(out, model.Section{Title: "Running", Meta: fmt.Sprintf("%d %s · %d %s",
			len(live), plural(len(live), "stack", "stacks"), running, plural(running, "container", "containers"))})
		for _, u := range live {
			if verb, ok := p.of("unit", u.Name); ok {
				out = append(out, pendingRow(u.Name, verb))
				continue
			}
			out = append(out, runningRow(u, p))
		}
	} else if len(att) == 0 {
		out = append(out, model.Note{Text: "Nothing running", Tone: model.ToneIdle})
	}

	var down, broken int
	for _, u := range d.units {
		if u.down() {
			down++
			if len(u.Crashed) > 0 {
				broken++
			}
		}
	}
	if down > 0 {
		text := fmt.Sprintf("%d stopped", down)
		if broken > 0 {
			text += fmt.Sprintf(" · %d broken", broken)
		}
		out = append(out, model.Section{Title: "Elsewhere"}, model.Note{Text: text + " — see Stopped"})
	}
	return out
}

// alert is one container in trouble: the short cause and its age in the
// meta (at most 24 characters, DESIGN A12), the last thing it wrote under
// it, then what else is known, when there is room for a second line.
func alert(a attention, p pending) model.Alert {
	c := a.c
	cause, age, detail := a.reason, c.UpH, []string{}
	if a.serious() {
		if c.UpH >= 0 {
			detail = append(detail, "up "+dur(c.UpH))
		}
	} else {
		cause, age = shortCause(c), c.AgeH
		// The meta has the code; what it means only when it says more.
		if meaning, ok := exitMeaning[c.Exit]; ok && !c.OOM && c.Exit != 1 {
			detail = append(detail, meaning)
		}
		if c.Policy != "" {
			detail = append(detail, "restart="+c.Policy+" but not back")
		}
	}
	meta := cause
	if age >= 0 && len([]rune(cause+" · "+dur(age))) <= 24 {
		meta = cause + " · " + dur(age)
	}
	if c.Restarts > 0 {
		detail = append(detail, fmt.Sprintf("restarted %d×", c.Restarts))
	}
	body := c.Image
	if c.Log != "" {
		body = c.Log
	}
	if len(detail) > 0 {
		body += "\n" + strings.Join(detail, " · ")
	}
	tone := model.ToneWarn
	if a.serious() {
		tone = model.ToneCrit
	}
	return model.Alert{Title: c.Name, Meta: meta, Body: body, Mono: c.Log != "", Tone: tone, Actions: containerActions(c, p)}
}

func runningRow(u *unit, p pending) model.Row {
	var head, sub string
	led, tone := model.ToneOK, model.ToneNone
	if u.Stack && len(u.Members) > 1 {
		head = fmt.Sprintf("%d/%d", len(u.Running), len(u.Members))
		up := map[*container]bool{}
		var names, down []string
		for _, c := range u.Running {
			up[c] = true
			names = append(names, serviceName(c))
		}
		for _, c := range u.Members {
			if !up[c] {
				down = append(down, serviceName(c))
			}
		}
		sort.Strings(names)
		sub = strings.Join(names, ", ")
		if len(down) > 0 {
			led, tone = model.ToneWarn, model.ToneWarn // lamp and value agree
			sort.Strings(down)
			sub += "   down: " + strings.Join(down, ", ")
		}
	} else {
		c := u.Running[0]
		head = "up " + dur(c.UpH)
		if c.State == "paused" {
			head = "paused"
		}
		if c.Health == "starting" || c.State == "paused" {
			led = model.ToneWarn
		}
		bits := append([]string{c.Image}, c.Ports...)
		if c.Health != "" {
			bits = append(bits, c.Health)
		}
		if c.Restarts > 0 {
			bits = append(bits, fmt.Sprintf("restarted %d×", c.Restarts))
			led = model.ToneWarn
		}
		sub = strings.Join(nonEmpty(bits), " · ")
	}
	value := []string{head, fmtBytesShort(u.Mem)}
	if u.CPU >= 1 {
		value = append(value, fmt.Sprintf("%.0f%%", u.CPU))
	}
	return model.Row{Name: u.Name, Sub: sub, Value: strings.Join(nonEmpty(value), " · "), Tone: tone, LED: led, Actions: unitActions(u, p)}
}

func serviceName(c *container) string {
	if c.Service != "" {
		return c.Service
	}
	return c.Name
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

// stoppedPage: every unit with nothing running, nothing collapsed — broken
// ones first, then this week's, then the dormant.
func stoppedPage(d *data, p pending) model.Blocks {
	var broken, recent, dormant []*unit
	for _, u := range d.units {
		switch {
		case !u.down():
		case len(u.Crashed) > 0:
			broken = append(broken, u)
		case u.AgeH >= 0 && u.AgeH < dormantH:
			recent = append(recent, u)
		default:
			dormant = append(dormant, u)
		}
	}
	if len(broken)+len(recent)+len(dormant) == 0 {
		return model.Blocks{model.Note{Text: "Everything is running", Tone: model.ToneIdle}}
	}
	// Most recent first; never-stopped (unknown) last.
	byAge := func(us []*unit) {
		sort.SliceStable(us, func(i, j int) bool { return known(us[i].AgeH) < known(us[j].AgeH) })
	}
	byAge(broken)
	byAge(recent)
	byAge(dormant)

	var out model.Blocks
	group := func(title, phrase string, us []*unit, tone model.Tone) {
		if len(us) == 0 {
			return
		}
		out = append(out, model.Section{Title: title, Meta: fmt.Sprintf("%d · %s", len(us), phrase), Tone: tone})
		for _, u := range us {
			out = append(out, stoppedRow(u, p))
		}
	}
	group("Broken", "crashed, not touched since", broken, model.ToneCrit)
	group("Stopped", "this week", recent, model.ToneNone)
	group("Dormant", "not run in a week", dormant, model.ToneNone)
	return out
}

func known(h float64) float64 {
	if h < 0 {
		return 1e9
	}
	return h
}

func stoppedRow(u *unit, p pending) model.Row {
	if verb, ok := p.of("unit", u.Name); ok {
		return pendingRow(u.Name, verb)
	}
	worst := u.Members[0]
	if len(u.Crashed) > 0 {
		worst = u.Crashed[0]
	}
	when := ago(u.AgeH)
	if when == "" {
		when = "never started"
	}
	sub := why(worst)
	if len(u.Members) > 1 {
		if len(u.Crashed) > 0 {
			sub = fmt.Sprintf("%d/%d crashed · %s", len(u.Crashed), len(u.Members), sub)
		} else {
			sub = fmt.Sprintf("%d services · %s", len(u.Members), sub)
		}
	}
	value := when
	if worst.Log != "" {
		// The last log line beats the exit code as a diagnostic; the code
		// moves to the value.
		sub = worst.Log
		if worst.HasExit {
			code := fmt.Sprintf("exit %d", worst.Exit)
			if worst.OOM {
				code = "OOM"
			}
			value = code + " · " + when
		}
	}
	led := model.ToneIdle
	if len(u.Crashed) > 0 {
		led = model.ToneCrit
	}
	return model.Row{Name: u.Name, Sub: sub, Value: value, LED: led, Actions: unitActions(u, p)}
}

// systemPage: the engine, then what could be reclaimed and the buttons
// that reclaim it.
func systemPage(d *data, p pending) model.Blocks {
	running, stopped := 0, 0
	for _, c := range d.all {
		switch c.State {
		case "running":
			running++
		case "exited", "created", "dead": // what `docker container prune` removes
			stopped++
		}
	}
	sys := d.sys
	switch {
	case sys == nil:
		return model.Blocks{model.Note{Text: "Asking docker for its disk use…", Tone: model.ToneIdle}}
	case len(sys.df) == 0:
		return model.Blocks{
			model.Row{Name: "Containers", Value: fmt.Sprintf("%d · %d running", len(d.all), running)},
			model.Note{Text: "docker system df did not answer", Tone: model.ToneIdle},
		}
	}

	// One row per kind of thing docker keeps: how many (in the sub), what
	// could come back of what it takes, and the button that takes it back.
	// The engine and compose versions are the footer's.
	row := func(label, key, sub string, acts []model.Action) model.Row {
		if verb, ok := p.of("prune", key); ok {
			return pendingRow(label, verb)
		}
		r := sys.df[key]
		led := model.ToneIdle
		if r.recl > 20e9 {
			led = model.ToneWarn
		}
		return model.Row{Name: label, Sub: sub, Value: fmtDec(r.recl) + " of " + fmtDec(r.size), LED: led, Actions: acts}
	}
	orQ := func(s string) string {
		if s == "" {
			return "?"
		}
		return s
	}
	img, vol := sys.df["images"], sys.df["local volumes"]
	var imgActs, ctrActs []model.Action
	if img.recl > 0 {
		imgActs = prune("prune.images", "prune unused", "free "+fmtDec(img.recl)+"?")
	}
	if stopped > 0 {
		ctrActs = prune("prune.containers", "prune stopped", fmt.Sprintf("remove %d?", stopped))
	}
	return model.Blocks{
		model.Section{Title: "Disk", Meta: "reclaimable of used"},
		row("Images", "images", fmt.Sprintf("%s · %d dangling · prune drops every image no container uses", orQ(img.total), sys.dangling), imgActs),
		row("Containers", "containers", fmt.Sprintf("%d · %d running · prune removes the %d stopped", len(d.all), running, stopped), ctrActs),
		row("Build cache", "build cache", "rebuilds cost time, not data", prune("prune.builder", "prune", "prune?")),
		// Volumes hold data: never pruned from this panel.
		row("Volumes", "local volumes", fmt.Sprintf("%s · %s in use · data, never pruned from here", orQ(vol.total), orQ(vol.active)), nil),
		model.Note{Text: "Buttons ask twice. Nothing in use is ever removed."},
	}
}
