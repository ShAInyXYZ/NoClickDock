package docker

import (
	"fmt"
	"sort"
	"strings"
)

const (
	recentH  = 24     // a crash newer than this is still news
	dormantH = 7 * 24 // stopped longer than this is dormant
)

// unit is what the panel shows as one row: a compose project, or a
// standalone container on its own.
type unit struct {
	Name    string
	Stack   bool
	Members []*container

	Running []*container // running or paused
	Bad     []*container // restarting, or running and unhealthy
	Crashed []*container
	AgeH    float64 // most recent stop among the stopped members; -1 unknown
	Mem     float64 // bytes, running members
	CPU     float64 // %, running members
}

// group makes units of containers, sorted by name without regard to case.
func group(cs []*container) []*unit {
	byKey := map[string]*unit{}
	var units []*unit
	for _, c := range cs {
		key := c.Project
		if key == "" {
			key = c.Name
		}
		u := byKey[key]
		if u == nil {
			u = &unit{Name: key, Stack: c.Project != "", AgeH: -1}
			byKey[key] = u
			units = append(units, u)
		}
		u.Members = append(u.Members, c)
	}
	for _, u := range units {
		for _, c := range u.Members {
			if c.State == "running" || c.State == "paused" {
				u.Running = append(u.Running, c)
				u.Mem += c.MemB
				u.CPU += c.CPU
			}
			if c.State == "restarting" || (c.State == "running" && c.Health == "unhealthy") {
				u.Bad = append(u.Bad, c)
			}
			if c.Crashed {
				u.Crashed = append(u.Crashed, c)
			}
			if c.State != "running" && c.AgeH >= 0 && (u.AgeH < 0 || c.AgeH < u.AgeH) {
				u.AgeH = c.AgeH
			}
		}
	}
	sort.SliceStable(units, func(i, j int) bool {
		return strings.ToLower(units[i].Name) < strings.ToLower(units[j].Name)
	})
	return units
}

// down is a unit with nothing running and nothing trying to: the Stopped
// page's. A restart-looping one is neither up nor down; it is an alert.
func (u *unit) down() bool { return len(u.Running) == 0 && len(u.Bad) == 0 }

func (u *unit) names() []string {
	out := make([]string, len(u.Members))
	for i, c := range u.Members {
		out[i] = c.Name
	}
	return out
}

// attention is one container that needs a human today, and why.
type attention struct {
	u      *unit
	c      *container
	reason string // "restarting", "unhealthy", "OOM-killed", "exit 1"
}

// serious is a reason that makes the widget critical.
func (a attention) serious() bool { return a.reason == "restarting" || a.reason == "unhealthy" }

// needs lists, unit by unit, every container restarting or unhealthy (any
// age), then every crash of the last 24 hours.
func needs(units []*unit) []attention {
	var out []attention
	for _, u := range units {
		for _, c := range u.Bad {
			reason := "unhealthy"
			if c.State == "restarting" {
				reason = "restarting"
			}
			out = append(out, attention{u, c, reason})
		}
		for _, c := range u.Crashed {
			if c.AgeH >= 0 && c.AgeH < recentH {
				reason := fmt.Sprintf("exit %d", c.Exit)
				if c.OOM {
					reason = "OOM-killed"
				}
				out = append(out, attention{u, c, reason})
			}
		}
	}
	return out
}

var exitMeaning = map[int]string{
	0: "exited cleanly", 1: "error", 2: "shell misuse", 125: "daemon error",
	126: "command not executable", 127: "command not found", 128: "invalid exit",
	130: "SIGINT", 134: "abort (SIGABRT)", 139: "segfault (SIGSEGV)",
	143: "stopped (SIGTERM)", 255: "exit 255",
}

// why says in one line what happened to a stopped container.
func why(c *container) string {
	if c.State == "created" {
		return "created, never started"
	}
	var w string
	switch {
	case c.OOM:
		w = "OOM-killed (137)"
	case !c.HasExit:
		w = c.State
	case c.Exit == 0 || c.Exit == 143:
		w = exitMeaning[c.Exit]
	case c.Exit == 137:
		w = "killed (137, docker stop timeout or kill)"
	default:
		meaning, ok := exitMeaning[c.Exit]
		if !ok {
			meaning = "error"
		}
		w = fmt.Sprintf("exit %d · %s", c.Exit, meaning)
	}
	if c.Policy != "" && c.Crashed {
		w += " · restart=" + c.Policy + " but not back"
	}
	return w
}

// shortCause is an alert's or a stopped row's cause in a few characters.
func shortCause(c *container) string {
	if c.OOM {
		return "OOM (137)"
	}
	return fmt.Sprintf("exit %d", c.Exit)
}
