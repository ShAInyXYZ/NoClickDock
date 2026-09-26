package docker

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

const actTimeout = 120 * time.Second

// validName is what docker allows a container (and compose a project) to be
// called. Names come from the last inspect, never from the interface, but
// one is checked again before it goes into an argv.
var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

var (
	ongoing = map[string]string{
		"start": "starting", "stop": "stopping", "restart": "restarting",
		"remove": "removing", "resume": "resuming", "prune": "pruning",
	}
	done = map[string]string{
		"start": "Started", "stop": "Stopped", "restart": "Restarted",
		"remove": "Removed", "resume": "Resumed", "prune": "Pruned",
	}
	// plain is each verb as a docker command, for a container or a list.
	plain = map[string][]string{
		"start": {"start"}, "stop": {"stop"}, "restart": {"restart"},
		"remove": {"rm"}, "resume": {"unpause"},
	}
)

// plan is one action as commands: what to run, what to run instead when
// that fails, and which rows read "<verb>ing…" meanwhile.
type plan struct {
	verb     string
	label    string
	marks    []string
	cmd      []string
	fallback []string
	prune    bool
}

// Act runs an action the panel offered. The hub has already checked that
// the last snapshot carried it with exactly these arguments; the name is
// checked again against the last inspect and docker's own naming rule.
func (w *Widget) Act(ctx context.Context, action string, args map[string]string) (string, error) {
	w.mu.Lock()
	d := w.last
	w.mu.Unlock()
	if d == nil || !d.available {
		return "", errors.New("Docker is not reachable")
	}
	p, err := w.plan(ctx, d, action, args)
	if err != nil {
		return "", err
	}
	return w.run(ctx, binary(), p)
}

func (w *Widget) plan(ctx context.Context, d *data, action string, args map[string]string) (plan, error) {
	scope, verb, _ := strings.Cut(action, ".")
	switch scope {
	case "unit":
		name := args["unit"]
		if !validName.MatchString(name) {
			return plan{}, fmt.Errorf("%q is not a container name", name)
		}
		var u *unit
		for _, x := range d.units {
			if x.Name == name {
				u = x
			}
		}
		if u == nil {
			return plan{}, fmt.Errorf("%s is gone", name)
		}
		return w.unitPlan(ctx, u, verb)
	case "container":
		name := args["container"]
		if !validName.MatchString(name) {
			return plan{}, fmt.Errorf("%q is not a container name", name)
		}
		for _, c := range d.all {
			if c.Name == name {
				return containerPlan(c, verb)
			}
		}
		return plan{}, fmt.Errorf("%s is gone", name)
	case "prune":
		p := plan{verb: "prune", prune: true}
		switch verb {
		case "images":
			p.label, p.cmd = "images", []string{"image", "prune", "-a", "-f"}
		case "containers":
			p.label, p.cmd = "containers", []string{"container", "prune", "-f"}
		case "builder":
			p.label, p.cmd = "build cache", []string{"builder", "prune", "-f"}
		default:
			return plan{}, fmt.Errorf("unknown action %q", action)
		}
		p.marks = []string{"prune:" + p.label}
		return p, nil
	}
	return plan{}, fmt.Errorf("unknown action %q", action)
}

// unitPlan drives a compose stack by its project (order, networks — v2 needs
// no compose file for stop, start, restart and down), falling back to plain
// docker on its members; a standalone container by its name.
func (w *Widget) unitPlan(ctx context.Context, u *unit, verb string) (plan, error) {
	if _, ok := plain[verb]; !ok {
		return plan{}, fmt.Errorf("unknown action %q", "unit."+verb)
	}
	names := u.names()
	for _, n := range names {
		if !validName.MatchString(n) {
			return plan{}, fmt.Errorf("%q is not a container name", n)
		}
	}
	p := plan{verb: verb, label: u.Name, marks: []string{"unit:" + u.Name}}
	for _, n := range names {
		p.marks = append(p.marks, "unit:"+n)
	}
	if verb == "resume" {
		// Only the paused ones: unpausing a running container fails.
		var paused []string
		for _, c := range u.Running {
			if c.State == "paused" {
				paused = append(paused, c.Name)
			}
		}
		p.cmd = append([]string{"unpause", "--"}, paused...)
		return p, nil
	}
	members := append(append(append([]string{}, plain[verb]...), "--"), names...)
	if u.Stack && w.hasCompose(ctx) {
		cverb := verb
		if verb == "remove" {
			cverb = "down"
		}
		p.cmd, p.fallback = []string{"compose", "-p", u.Name, cverb}, members
		return p, nil
	}
	p.cmd = members
	return p, nil
}

func containerPlan(c *container, verb string) (plan, error) {
	args, ok := plain[verb]
	if !ok {
		return plan{}, fmt.Errorf("unknown action %q", "container."+verb)
	}
	return plan{
		verb: verb, label: c.Name, marks: []string{"unit:" + c.Name},
		cmd: append(append(append([]string{}, args...), "--"), c.Name),
	}, nil
}

// hasCompose asks once per run whether docker compose v2 is there.
func (w *Widget) hasCompose(ctx context.Context) bool {
	w.mu.Lock()
	known := w.compose
	w.mu.Unlock()
	if known != nil {
		return *known
	}
	_, err := sys.Run(ctx, 5*time.Second, binary(), "compose", "version")
	has := err == nil
	w.mu.Lock()
	w.compose = &has
	w.mu.Unlock()
	return has
}

// run marks the rows busy and shows them so, runs the command (and its
// fallback when it fails), then says what happened in one line.
func (w *Widget) run(ctx context.Context, bin string, p plan) (string, error) {
	if bin == "" {
		return "", errors.New("docker is not installed")
	}
	w.mu.Lock()
	for _, m := range p.marks {
		if verb, busy := w.pending[m]; busy {
			w.mu.Unlock()
			return "", fmt.Errorf("%s is still %s", p.label, verb)
		}
	}
	for _, m := range p.marks {
		w.pending[m] = ongoing[p.verb]
	}
	w.mu.Unlock()
	w.poke()

	stdout, stderr, err := sys.Output(ctx, actTimeout, bin, p.cmd...)
	if err != nil && p.fallback != nil {
		stdout, stderr, err = sys.Output(ctx, actTimeout, bin, p.fallback...)
	}

	w.mu.Lock()
	for _, m := range p.marks {
		delete(w.pending, m)
	}
	if p.prune {
		w.sysAt = time.Time{} // what df said is wrong now
	}
	w.mu.Unlock()

	if err == nil {
		return done[p.verb] + " " + p.label, nil
	}
	w.poke() // the hub refreshes after a success only
	tail := lastLine(stderr)
	if tail == "" {
		tail = lastLine(stdout)
	}
	if tail == "" {
		tail = err.Error()
	}
	return "", fmt.Errorf("%s: %s", p.label, cut(tail, 70))
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
