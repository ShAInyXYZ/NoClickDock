package claude

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
)

// limit is one window of the plan, as `claude -p /usage` reports it — the
// only authoritative percent-of-limit there is.
type limit struct {
	Label  string // "Session", "Week", "Week · Fable"
	Pct    float64
	Resets time.Time // zero when unknown
}

var limitRE = regexp.MustCompile(`(?i)^Current\s+(session|week)\s*(?:\(([^)]+)\))?\s*:\s*(\d+(?:\.\d+)?)\s*%\s*used(?:\s*[·•-]\s*resets\s+(.+?))?\s*$`)

// parseLimits reads the CLI's "Current session: 12% used · resets …"
// lines, in the order printed. Anything else — the account line, the
// breakdown of what used the limits — is ignored.
func parseLimits(out string, now time.Time) []limit {
	var rows []limit
	for _, line := range strings.Split(out, "\n") {
		m := limitRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		pct, err := strconv.ParseFloat(m[3], 64)
		if err != nil {
			continue
		}
		scope := "Session"
		if strings.EqualFold(m[1], "week") {
			scope = "Week"
		}
		label := scope
		switch qual := strings.TrimSpace(m[2]); {
		case strings.HasPrefix(strings.ToLower(qual), "all"):
			label = "Week"
		case qual != "":
			label = scope + " · " + qual
		}
		row := limit{Label: label, Pct: pct}
		if raw := strings.TrimSpace(m[4]); raw != "" {
			if t, ok := parseReset(raw, now); ok {
				row.Resets = t
			} else {
				// Never passed through verbatim (DESIGN: time only moves
				// forward): the percentage alone is still true. Logged once,
				// for the parser's tests.
				unreadOnce.Do(func() { log.Printf("claude: a reset the parser cannot read: %q", raw) })
			}
		}
		rows = append(rows, row)
	}
	return rows
}

var zoneRE = regexp.MustCompile(`\s*\(([^)]*)\)\s*$`)

func stripZone(s string) string { return strings.TrimSpace(zoneRE.ReplaceAllString(s, "")) }

// parseReset reads the CLI's reset time — "Sep 5, 3:59pm (Europe/Paris)",
// "Sep 5, 3pm", "3:59pm" — in the zone it names (else local time). A date
// without a year is this year's, or next year's when that is closer.
func parseReset(raw string, now time.Time) (time.Time, bool) {
	loc := time.Local
	if m := zoneRE.FindStringSubmatch(raw); m != nil {
		if l, err := time.LoadLocation(strings.TrimSpace(m[1])); err == nil {
			loc = l
		}
	}
	text := strings.ToLower(stripZone(raw))
	nowIn := now.In(loc)
	for _, layout := range []string{"Jan 2, 3:04pm", "Jan 2, 3pm", "Jan 2 3:04pm", "Jan 2 3pm", "Jan 2, 15:04", "Jan 2 15:04"} {
		t, err := time.ParseInLocation(layout, text, loc)
		if err != nil {
			continue
		}
		t = time.Date(nowIn.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc)
		if t.Before(nowIn.AddDate(0, -6, 0)) { // "Jan 3" read in late December
			t = t.AddDate(1, 0, 0)
		}
		return t, true
	}
	for _, layout := range []string{"3:04pm", "3pm", "15:04"} {
		t, err := time.ParseInLocation(layout, text, loc)
		if err != nil {
			continue
		}
		t = time.Date(nowIn.Year(), nowIn.Month(), nowIn.Day(), t.Hour(), t.Minute(), 0, 0, loc)
		if t.Before(nowIn) {
			t = t.AddDate(0, 0, 1)
		}
		return t, true
	}
	return time.Time{}, false
}

// binary is how to run the Claude Code CLI: on PATH, else where its
// installers put it. An npm shim on Windows (claude.cmd) is run through
// cmd.exe with a fixed argv — never a shell string.
func binary() (name string, prefix []string) {
	home, _ := os.UserHomeDir()
	var fallbacks []string
	if runtime.GOOS == "windows" {
		fallbacks = append(fallbacks, filepath.Join(home, ".local", "bin", "claude.exe"))
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			fallbacks = append(fallbacks, filepath.Join(appdata, "npm", "claude.cmd"))
		}
	} else {
		fallbacks = append(fallbacks, filepath.Join(home, ".local", "bin", "claude"), filepath.Join(home, ".claude", "local", "claude"))
	}
	return command(sys.Find("claude", fallbacks...))
}

// command runs a found CLI: a batch shim through cmd.exe, anything else
// directly.
func command(path string) (name string, prefix []string) {
	if path == "" {
		return "", nil
	}
	if ext := strings.ToLower(filepath.Ext(path)); ext == ".cmd" || ext == ".bat" {
		return "cmd.exe", []string{"/d", "/c", path}
	}
	return path, nil
}

// noPersist keeps `claude -p` from writing a transcript and from firing the
// user's SessionStart hooks: without it every reading left a session behind
// (the Python widget left ~1,450). Never --bare: that skips the login and
// answers with an empty cost summary.
const noPersist = "--no-session-persistence"

// flagRefused: the installed CLI said it does not know noPersist. Remembered
// for the life of the process, so the refusal is paid once.
var flagRefused atomic.Bool

var unreadOnce sync.Once

// usageArgs is the fixed argv after the program (and a shim's prefix).
func usageArgs(prefix []string, withFlag bool) []string {
	args := append(append([]string{}, prefix...), "-p", "/usage")
	if withFlag {
		args = append(args, noPersist)
	}
	return args
}

// refusesFlag is an older CLI's "error: unknown option '--no-session-persistence'".
func refusesFlag(text string) bool {
	t := strings.ToLower(text)
	if !strings.Contains(t, noPersist) {
		return false
	}
	for _, word := range []string{"unknown option", "unknown argument", "unrecognized", "unrecognised"} {
		if strings.Contains(t, word) {
			return true
		}
	}
	return false
}

type runFunc func(ctx context.Context, dir string, timeout time.Duration, name string, args ...string) (stdout, stderr string, err error)

// askUsage runs `/usage` without persistence; a CLI that refuses the flag is
// asked once more without it, and never offered the flag again.
func askUsage(ctx context.Context, run runFunc, dir, name string, prefix []string) string {
	withFlag := !flagRefused.Load()
	out, errOut, err := run(ctx, dir, limitsTimeout, name, usageArgs(prefix, withFlag)...)
	if err != nil && withFlag && refusesFlag(out+"\n"+errOut+"\n"+err.Error()) {
		flagRefused.Store(true)
		log.Printf("claude: this CLI has no %s; each limits reading now leaves a transcript", noPersist)
		out, _, err = run(ctx, dir, limitsTimeout, name, usageArgs(prefix, false)...)
	}
	if err != nil && out == "" {
		return ""
	}
	return out
}

// readLimits asks the CLI, from the user's home (never the core's working
// directory, whose project settings would apply). Nothing — no CLI, a
// timeout, changed wording — is an empty list; the caller keeps what it had.
func readLimits(ctx context.Context, now time.Time) []limit {
	name, prefix := binary()
	if name == "" {
		return nil
	}
	home, _ := os.UserHomeDir()
	return parseLimits(askUsage(ctx, sys.OutputIn, home, name, prefix), now)
}
